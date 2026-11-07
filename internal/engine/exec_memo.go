package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/warpcode/cloakenv/internal/provider"
)

// execResult stores the cached output or error of an exec invocation.
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

// persistentCacheKey maps the canonical key to a hashed identifier under the exec cache namespace.
func persistentCacheKey(canonKey string) string {
	h := sha256.Sum256([]byte(canonKey))
	return "exec:" + hex.EncodeToString(h[:])
}

// execute executes an external command or returns the memoised/cached result.
// It deduplicates concurrent calls using singleflight so identical commands in the same
// Orchestrator lifecycle execute at most once. If cacheProv is provided and has a valid TTL,
// results are read from and written to the persistent encrypted cache layer.
func (m *execMemo) execute(
	ctx context.Context,
	binPath string,
	strArgs []string,
	cacheProv provider.SecretProvider,
	cacheTTL time.Duration,
	runFn func(execCtx context.Context) (string, error),
) (string, error) {
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

	// 3. Mark as in-flight
	flight := &execFlight{
		done: make(chan struct{}),
	}
	m.inFlight[canonKey] = flight
	m.mu.Unlock()

	// Execute command or fetch from persistent cache
	var res execResult

	// Check persistent cache backing if available
	cacheKey := persistentCacheKey(canonKey)
	if cacheProv != nil && cacheTTL > 0 {
		if cachedVal, err := cacheProv.GetSecret(ctx, cacheKey); err == nil && cachedVal != "" {
			res = execResult{out: cachedVal, err: nil}
		}
	}

	if res.out == "" && res.err == nil {
		out, err := runFn(ctx)
		res = execResult{out: out, err: err}

		// If execution was successful and persistent cache is enabled, store in cache
		if err == nil && cacheProv != nil && cacheTTL > 0 {
			ttlCtx := ctx
			if _, ok := ttlCtx.Value(provider.ContextKeyTTL).(time.Duration); !ok {
				ttlCtx = context.WithValue(ttlCtx, provider.ContextKeyTTL, cacheTTL)
			}
			_ = cacheProv.SetSecret(ttlCtx, cacheKey, out)
		}
	}

	// Publish result to waiters and memoise in-memory
	m.mu.Lock()
	flight.res = res
	close(flight.done)
	delete(m.inFlight, canonKey)
	// Only memoise in memory if context was not canceled before completion
	if ctx.Err() == nil || res.err == nil {
		m.cache[canonKey] = res
	}
	m.mu.Unlock()

	return res.out, res.err
}
