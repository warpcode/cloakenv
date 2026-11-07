package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/warpcode/cloakenv/internal/provider"
)

// maxPersistentExecBytes caps the maximum stdout payload size written to the
// persistent encrypted cache layer (1 MiB). Outputs exceeding this threshold
// are still returned and memoized in memory for the current run, but are not
// persisted to disk.
const maxPersistentExecBytes = 1024 * 1024

// defaultExecTimeout bounds external command execution when no shorter deadline
// is enforced.
const defaultExecTimeout = 30 * time.Second

// execResult stores the cached output or error of an exec invocation.
// Both successful outputs and execution errors are memoized for the duration
// of a single Orchestrator run so identical {{ exec ... }} calls execute at
// most once per run without repeating subprocess side effects.
type execResult struct {
	out string
	err error
}

// execFlight tracks an active in-flight execution for singleflight deduplication.
type execFlight struct {
	done chan struct{}
	res  execResult
}

// execMemo provides thread-safe singleflight deduplication, in-memory per-run memoisation,
// and optional persistent encrypted cache backing for {{ exec ... }} command invocations.
type execMemo struct {
	mu       sync.Mutex
	cache    map[string]execResult
	inFlight map[string]*execFlight
}

// newExecMemo creates a new execMemo instance.
func newExecMemo() *execMemo {
	return &execMemo{
		cache:    make(map[string]execResult),
		inFlight: make(map[string]*execFlight),
	}
}

// canonicalExecKey generates a collision-resistant key for a canonical command invocation.
// Each token (binary path and arguments) is prefixed by its byte length and separated by null bytes.
func canonicalExecKey(binPath string, strArgs []string) string {
	var b strings.Builder
	b.WriteString(strconv.Itoa(len(binPath)))
	b.WriteByte(':')
	b.WriteString(binPath)
	for _, arg := range strArgs {
		b.WriteByte('\x00')
		b.WriteString(strconv.Itoa(len(arg)))
		b.WriteByte(':')
		b.WriteString(arg)
	}
	return b.String()
}

// currentExecContextFingerprint computes a deterministic SHA-256 digest of the
// process working directory and sorted environment variables without persisting
// raw environment values.
func currentExecContextFingerprint() string {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	env := slices.Clone(os.Environ())
	slices.Sort(env)

	h := sha256.New()
	_, _ = h.Write([]byte(strconv.Itoa(len(cwd))))
	_, _ = h.Write([]byte(":"))
	_, _ = h.Write([]byte(cwd))
	for _, kv := range env {
		_, _ = h.Write([]byte{'\x00'})
		_, _ = h.Write([]byte(strconv.Itoa(len(kv))))
		_, _ = h.Write([]byte(":"))
		_, _ = h.Write([]byte(kv))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// persistentCacheKey maps the canonical command key and execution context
// fingerprint (cwd + environment digest) to a hashed identifier under the exec
// cache namespace.
func persistentCacheKey(canonKey, ctxFingerprint string) string {
	h := sha256.New()
	_, _ = h.Write([]byte(strconv.Itoa(len(canonKey))))
	_, _ = h.Write([]byte(":"))
	_, _ = h.Write([]byte(canonKey))
	_, _ = h.Write([]byte{'\x00'})
	_, _ = h.Write([]byte(strconv.Itoa(len(ctxFingerprint))))
	_, _ = h.Write([]byte(":"))
	_, _ = h.Write([]byte(ctxFingerprint))
	return "exec:" + hex.EncodeToString(h.Sum(nil))
}

// execute executes an external command or returns the memoised/cached result.
// It deduplicates concurrent calls using singleflight so identical commands in the same
// Orchestrator lifecycle execute at most once. Both successful outputs and command errors
// are memoised for the remainder of the run. If cacheProv is provided and has a positive TTL,
// successful results (up to maxPersistentExecBytes) are read from and written to the
// persistent encrypted cache layer.
func (m *execMemo) execute(
	ctx context.Context,
	binPath string,
	strArgs []string,
	cacheProv provider.SecretProvider,
	cacheTTL time.Duration,
	runFn func(execCtx context.Context) (string, error),
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	canonKey := canonicalExecKey(binPath, strArgs)

	m.mu.Lock()
	// 1. In-memory memoisation check
	if res, ok := m.cache[canonKey]; ok {
		m.mu.Unlock()
		return res.out, res.err
	}

	// 2. Flight deduplication check
	if flight, ok := m.inFlight[canonKey]; ok {
		m.mu.Unlock()
		select {
		case <-flight.done:
			return flight.res.out, flight.res.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	// 3. Register new flight and spawn leader worker with detached context
	// so one caller's cancellation does not abort the shared subprocess for
	// other concurrent waiters.
	flight := &execFlight{
		done: make(chan struct{}),
	}
	m.inFlight[canonKey] = flight
	m.mu.Unlock()

	detachedBase := context.WithoutCancel(ctx)
	go func() {
		var res execResult
		completed := false

		defer func() {
			if r := recover(); r != nil {
				res = execResult{
					err: fmt.Errorf("exec: internal panic during command execution: %v", r),
				}
			} else if !completed {
				res = execResult{
					err: fmt.Errorf("exec: command execution aborted unexpectedly"),
				}
			}

			m.mu.Lock()
			flight.res = res
			if completed {
				m.cache[canonKey] = res
			}
			delete(m.inFlight, canonKey)
			close(flight.done)
			m.mu.Unlock()
		}()

		execCtx, cancel := context.WithTimeout(detachedBase, defaultExecTimeout)
		defer cancel()

		cacheHit := false
		var cacheKey string
		if cacheProv != nil && cacheTTL > 0 {
			cacheKey = persistentCacheKey(canonKey, currentExecContextFingerprint())
			if cachedVal, err := cacheProv.GetSecret(execCtx, cacheKey); err == nil {
				res = execResult{out: cachedVal, err: nil}
				cacheHit = true
			}
		}

		if !cacheHit {
			out, err := runFn(execCtx)
			res = execResult{out: out, err: err}

			if err == nil && cacheProv != nil && cacheTTL > 0 && len(out) <= maxPersistentExecBytes {
				ttlCtx := execCtx
				if _, ok := ttlCtx.Value(provider.ContextKeyTTL).(time.Duration); !ok {
					ttlCtx = context.WithValue(ttlCtx, provider.ContextKeyTTL, cacheTTL)
				}
				_ = cacheProv.SetSecret(ttlCtx, cacheKey, out)
			}
		}

		completed = true
	}()

	select {
	case <-flight.done:
		return flight.res.out, flight.res.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
