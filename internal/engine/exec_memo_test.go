package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
)

type mockMemoryCacheProvider struct {
	mu      sync.Mutex
	store   map[string]string
	gets    atomic.Int64
	sets    atomic.Int64
	deletes atomic.Int64
}

func newMockMemoryCacheProvider() *mockMemoryCacheProvider {
	return &mockMemoryCacheProvider{
		store: make(map[string]string),
	}
}

func (m *mockMemoryCacheProvider) Scheme() string { return "cache" }
func (m *mockMemoryCacheProvider) Initialize(_ context.Context, _ provider.ProviderConfig) error {
	return nil
}

func (m *mockMemoryCacheProvider) GetSecret(_ context.Context, location string) (string, error) {
	m.gets.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	val, ok := m.store[location]
	if !ok {
		return "", errors.New("not found")
	}
	return val, nil
}

func (m *mockMemoryCacheProvider) SetSecret(_ context.Context, location, value string) error {
	m.sets.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store[location] = value
	return nil
}

func (m *mockMemoryCacheProvider) DeleteSecret(_ context.Context, location string) error {
	m.deletes.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.store, location)
	return nil
}

func (m *mockMemoryCacheProvider) Validate(_ map[string]string) error { return nil }

func TestCanonicalExecKey(t *testing.T) {
	t.Run("distinct arguments do not collide", func(t *testing.T) {
		k1 := canonicalExecKey("/usr/bin/echo", []string{"a", "b c"})
		k2 := canonicalExecKey("/usr/bin/echo", []string{"a b", "c"})
		if k1 == k2 {
			t.Fatalf("canonical key collision detected: %q == %q", k1, k2)
		}

		k3 := canonicalExecKey("/usr/bin/echo", []string{"foo:bar"})
		k4 := canonicalExecKey("/usr/bin/echo", []string{"foo", "bar"})
		if k3 == k4 {
			t.Fatalf("canonical key collision detected: %q == %q", k3, k4)
		}
	})
}

func TestExecMemo_DeduplicationAndConcurrency(t *testing.T) {
	t.Run("deduplicates identical calls sequentially", func(t *testing.T) {
		memo := newExecMemo()
		var callCount atomic.Int64

		runFn := func(runCtx context.Context) (string, error) {
			if err := runCtx.Err(); err != nil {
				return "", err
			}
			callCount.Add(1)
			return "tok_123", nil
		}

		ctx := context.Background()
		for range 5 {
			out, err := memo.execute(ctx, "/bin/token-gen", []string{"--user", "alice"}, nil, 0, runFn)
			if err != nil {
				t.Fatalf("execute failed: %v", err)
			}
			if out != "tok_123" {
				t.Fatalf("expected tok_123, got %q", out)
			}
		}

		if got := callCount.Load(); got != 1 {
			t.Fatalf("runFn call count = %d, want 1", got)
		}
	})

	t.Run("deduplicates concurrent in-flight calls", func(t *testing.T) {
		memo := newExecMemo()
		var callCount atomic.Int64
		gate := make(chan struct{})

		runFn := func(runCtx context.Context) (string, error) {
			if err := runCtx.Err(); err != nil {
				return "", err
			}
			callCount.Add(1)
			<-gate
			return "tok_concurrent", nil
		}

		ctx := context.Background()
		const goroutines = 20
		var wg sync.WaitGroup
		wg.Add(goroutines)

		results := make([]string, goroutines)
		errs := make([]error, goroutines)

		for i := range goroutines {
			go func(idx int) {
				defer wg.Done()
				res, err := memo.execute(ctx, "/bin/token-gen", []string{"--user", "bob"}, nil, 0, runFn)
				results[idx] = res
				errs[idx] = err
			}(i)
		}

		// Allow goroutines to queue in flight
		time.Sleep(20 * time.Millisecond)
		close(gate)
		wg.Wait()

		if got := callCount.Load(); got != 1 {
			t.Fatalf("concurrent runFn call count = %d, want 1", got)
		}

		for i := range goroutines {
			if errs[i] != nil {
				t.Fatalf("goroutine %d failed: %v", i, errs[i])
			}
			if results[i] != "tok_concurrent" {
				t.Fatalf("goroutine %d expected tok_concurrent, got %q", i, results[i])
			}
		}
	})

	t.Run("leader context cancellation does not abort shared execution for live waiter", func(t *testing.T) {
		memo := newExecMemo()
		var callCount atomic.Int64
		started := make(chan struct{})
		gate := make(chan struct{})

		runFn := func(runCtx context.Context) (string, error) {
			callCount.Add(1)
			close(started)
			select {
			case <-runCtx.Done():
				return "", runCtx.Err()
			case <-gate:
				return "tok_survived_leader_cancel", nil
			}
		}

		leaderCtx, cancelLeader := context.WithCancel(context.Background())
		defer cancelLeader()

		leaderErrCh := make(chan error, 1)
		go func() {
			_, err := memo.execute(leaderCtx, "/bin/shared-cmd", []string{"--arg"}, nil, 0, runFn)
			leaderErrCh <- err
		}()

		// Wait until leader has started the flight
		<-started

		// Start second waiter with a live context
		type waiterOutcome struct {
			out string
			err error
		}
		waiterCh := make(chan waiterOutcome, 1)
		go func() {
			out, err := memo.execute(context.Background(), "/bin/shared-cmd", []string{"--arg"}, nil, 0, runFn)
			waiterCh <- waiterOutcome{out: out, err: err}
		}()

		// Give waiter a moment to subscribe to flight.done, then cancel the leader
		time.Sleep(15 * time.Millisecond)
		cancelLeader()

		if leaderErr := <-leaderErrCh; !errors.Is(leaderErr, context.Canceled) {
			t.Fatalf("expected leader to fail with context.Canceled, got %v", leaderErr)
		}

		// Release the command execution; waiter must still receive the result
		close(gate)
		waiterRes := <-waiterCh
		if waiterRes.err != nil {
			t.Fatalf("waiter failed after leader cancellation: %v", waiterRes.err)
		}
		if waiterRes.out != "tok_survived_leader_cancel" {
			t.Fatalf("waiter got %q, want tok_survived_leader_cancel", waiterRes.out)
		}
		if got := callCount.Load(); got != 1 {
			t.Fatalf("expected callCount 1, got %d", got)
		}

		// Subsequent call hits the memoised result
		subsequentOut, err := memo.execute(context.Background(), "/bin/shared-cmd", []string{"--arg"}, nil, 0, runFn)
		if err != nil {
			t.Fatalf("subsequent call failed: %v", err)
		}
		if subsequentOut != "tok_survived_leader_cancel" {
			t.Fatalf("subsequent call got %q, want tok_survived_leader_cancel", subsequentOut)
		}
	})

	t.Run("panic in runFn releases waiters with failure and does not hang subsequent calls", func(t *testing.T) {
		memo := newExecMemo()
		started := make(chan struct{})
		gate := make(chan struct{})
		shouldPanic := atomic.Bool{}
		shouldPanic.Store(true)

		runFn := func(runCtx context.Context) (string, error) {
			if err := runCtx.Err(); err != nil {
				return "", err
			}
			if shouldPanic.Load() {
				close(started)
				<-gate
				panic("simulated runFn panic")
			}
			return "recovered_ok", nil
		}

		var wg sync.WaitGroup
		wg.Add(2)
		errs := make([]error, 2)

		go func() {
			defer wg.Done()
			_, errs[0] = memo.execute(context.Background(), "/bin/panicky", nil, nil, 0, runFn)
		}()

		<-started

		go func() {
			defer wg.Done()
			_, errs[1] = memo.execute(context.Background(), "/bin/panicky", nil, nil, 0, runFn)
		}()

		time.Sleep(15 * time.Millisecond)
		close(gate)
		wg.Wait()

		for i, err := range errs {
			if err == nil {
				t.Fatalf("caller %d expected error on panic, got nil", i)
			}
			if !strings.Contains(err.Error(), "simulated runFn panic") {
				t.Fatalf("caller %d expected panic error message, got %v", i, err)
			}
		}

		// Subsequent call must not hang and should be able to execute cleanly
		shouldPanic.Store(false)
		out, err := memo.execute(context.Background(), "/bin/panicky", nil, nil, 0, runFn)
		if err != nil {
			t.Fatalf("subsequent call after panic failed: %v", err)
		}
		if out != "recovered_ok" {
			t.Fatalf("subsequent call got %q, want recovered_ok", out)
		}
	})

	t.Run("memoizes command execution errors for the remainder of the run", func(t *testing.T) {
		memo := newExecMemo()
		var callCount atomic.Int64

		runFn := func(runCtx context.Context) (string, error) {
			if err := runCtx.Err(); err != nil {
				return "", err
			}
			callCount.Add(1)
			return "", errors.New("subcommand failed with exit status 1")
		}

		ctx := context.Background()
		for range 3 {
			_, err := memo.execute(ctx, "/bin/failing-cmd", []string{"--check"}, nil, 0, runFn)
			if err == nil {
				t.Fatalf("expected error from failing command, got nil")
			}
		}

		if got := callCount.Load(); got != 1 {
			t.Fatalf("expected failing command to execute at most once per run, got %d calls", got)
		}
	})
}

func TestExecMemo_PersistentCacheBacking(t *testing.T) {
	t.Run("persists and reuses cached output across memo instances", func(t *testing.T) {
		mockCache := newMockMemoryCacheProvider()
		var callCount atomic.Int64

		runFn := func(runCtx context.Context) (string, error) {
			if err := runCtx.Err(); err != nil {
				return "", err
			}
			callCount.Add(1)
			return "cached_token_val", nil
		}

		ctx := context.Background()

		// Run 1: with empty cache and memo 1
		memo1 := newExecMemo()
		out1, err := memo1.execute(ctx, "/usr/bin/token", []string{"--opt"}, mockCache, 10*time.Minute, runFn)
		if err != nil {
			t.Fatalf("execute failed: %v", err)
		}
		if out1 != "cached_token_val" {
			t.Fatalf("unexpected out1: %q", out1)
		}
		if got := callCount.Load(); got != 1 {
			t.Fatalf("expected callCount 1, got %d", got)
		}
		if got := mockCache.sets.Load(); got != 1 {
			t.Fatalf("expected 1 cache set, got %d", got)
		}

		// Run 2: new memo instance (new Orchestrator run), persistent cache hit!
		memo2 := newExecMemo()
		out2, err := memo2.execute(ctx, "/usr/bin/token", []string{"--opt"}, mockCache, 10*time.Minute, runFn)
		if err != nil {
			t.Fatalf("execute failed: %v", err)
		}
		if out2 != "cached_token_val" {
			t.Fatalf("unexpected out2: %q", out2)
		}
		// callCount must still be 1 (runFn was skipped due to persistent cache hit)
		if got := callCount.Load(); got != 1 {
			t.Fatalf("expected callCount 1 after persistent cache hit, got %d", got)
		}
		if got := mockCache.gets.Load(); got < 1 {
			t.Fatalf("expected cache gets >= 1, got %d", got)
		}
	})

	t.Run("successful empty stdout is treated as a persistent cache hit", func(t *testing.T) {
		mockCache := newMockMemoryCacheProvider()
		var callCount atomic.Int64

		runFn := func(runCtx context.Context) (string, error) {
			if err := runCtx.Err(); err != nil {
				return "", err
			}
			callCount.Add(1)
			return "", nil
		}

		ctx := context.Background()

		memo1 := newExecMemo()
		out1, err := memo1.execute(ctx, "/usr/bin/true", nil, mockCache, 10*time.Minute, runFn)
		if err != nil {
			t.Fatalf("first execute failed: %v", err)
		}
		if out1 != "" {
			t.Fatalf("expected empty stdout, got %q", out1)
		}
		if got := callCount.Load(); got != 1 {
			t.Fatalf("expected callCount 1, got %d", got)
		}

		// Fresh memo instance must hit persistent cache even when cached value is ""
		memo2 := newExecMemo()
		out2, err := memo2.execute(ctx, "/usr/bin/true", nil, mockCache, 10*time.Minute, runFn)
		if err != nil {
			t.Fatalf("second execute failed: %v", err)
		}
		if out2 != "" {
			t.Fatalf("expected empty stdout, got %q", out2)
		}
		if got := callCount.Load(); got != 1 {
			t.Fatalf("expected callCount to stay at 1 for cached empty stdout, got %d", got)
		}
	})

	t.Run("changing cwd or environment invalidates persistent cache key", func(t *testing.T) {
		mockCache := newMockMemoryCacheProvider()
		var callCount atomic.Int64

		runFn := func(runCtx context.Context) (string, error) {
			if err := runCtx.Err(); err != nil {
				return "", err
			}
			n := callCount.Add(1)
			return fmt.Sprintf("val_%d", n), nil
		}

		ctx := context.Background()
		dir1 := t.TempDir()
		dir2 := t.TempDir()

		t.Chdir(dir1)
		t.Setenv("CLOAKENV_EXEC_TEST_VAR", "alpha")

		memo1 := newExecMemo()
		out1, err := memo1.execute(ctx, "/usr/bin/env-cmd", []string{"arg"}, mockCache, 10*time.Minute, runFn)
		if err != nil {
			t.Fatalf("run 1 failed: %v", err)
		}
		if out1 != "val_1" {
			t.Fatalf("got %q, want val_1", out1)
		}

		// Same cwd & env with fresh memo -> cache hit
		memo2 := newExecMemo()
		out2, err := memo2.execute(ctx, "/usr/bin/env-cmd", []string{"arg"}, mockCache, 10*time.Minute, runFn)
		if err != nil {
			t.Fatalf("run 2 failed: %v", err)
		}
		if out2 != "val_1" || callCount.Load() != 1 {
			t.Fatalf("expected cache hit with val_1 and count 1, got %q (count %d)", out2, callCount.Load())
		}

		// Change environment -> cache miss
		t.Setenv("CLOAKENV_EXEC_TEST_VAR", "beta")
		memo3 := newExecMemo()
		out3, err := memo3.execute(ctx, "/usr/bin/env-cmd", []string{"arg"}, mockCache, 10*time.Minute, runFn)
		if err != nil {
			t.Fatalf("run 3 failed: %v", err)
		}
		if out3 != "val_2" || callCount.Load() != 2 {
			t.Fatalf("expected cache miss on env change (val_2, count 2), got %q (count %d)", out3, callCount.Load())
		}

		// Change working directory -> cache miss
		t.Chdir(dir2)
		memo4 := newExecMemo()
		out4, err := memo4.execute(ctx, "/usr/bin/env-cmd", []string{"arg"}, mockCache, 10*time.Minute, runFn)
		if err != nil {
			t.Fatalf("run 4 failed: %v", err)
		}
		if out4 != "val_3" || callCount.Load() != 3 {
			t.Fatalf("expected cache miss on cwd change (val_3, count 3), got %q (count %d)", out4, callCount.Load())
		}
	})

	t.Run("caps persistent cache writes at 1 MiB while preserving command output", func(t *testing.T) {
		mockCache := newMockMemoryCacheProvider()
		ctx := context.Background()

		// Exactly 1 MiB -> persisted to cache
		exactPayload := strings.Repeat("a", maxPersistentExecBytes)
		memoExact := newExecMemo()
		outExact, err := memoExact.execute(ctx, "/usr/bin/large", []string{"exact"}, mockCache, 10*time.Minute, func(runCtx context.Context) (string, error) {
			if err := runCtx.Err(); err != nil {
				return "", err
			}
			return exactPayload, nil
		})
		if err != nil {
			t.Fatalf("exact 1 MiB execute failed: %v", err)
		}
		if len(outExact) != maxPersistentExecBytes {
			t.Fatalf("got len %d, want %d", len(outExact), maxPersistentExecBytes)
		}
		if got := mockCache.sets.Load(); got != 1 {
			t.Fatalf("expected 1 MiB payload to be written to cache, sets = %d", got)
		}

		// 1 MiB + 1 byte -> skipped from persistent cache, but returned and memoised in memory
		overPayload := strings.Repeat("b", maxPersistentExecBytes+1)
		var overCalls atomic.Int64
		memoOver := newExecMemo()
		for range 2 {
			outOver, err := memoOver.execute(ctx, "/usr/bin/large", []string{"over"}, mockCache, 10*time.Minute, func(runCtx context.Context) (string, error) {
				if err := runCtx.Err(); err != nil {
					return "", err
				}
				overCalls.Add(1)
				return overPayload, nil
			})
			if err != nil {
				t.Fatalf("over 1 MiB execute failed: %v", err)
			}
			if len(outOver) != maxPersistentExecBytes+1 {
				t.Fatalf("got len %d, want %d", len(outOver), maxPersistentExecBytes+1)
			}
		}
		if got := mockCache.sets.Load(); got != 1 {
			t.Fatalf("expected >1 MiB payload to skip persistent cache write, sets = %d, want 1", got)
		}
		if got := overCalls.Load(); got != 1 {
			t.Fatalf("expected >1 MiB payload to still be memoised in-memory, calls = %d, want 1", got)
		}
	})
}

func TestExecMemo_ZeroTTLDoesNotInitializeCacheProvider(t *testing.T) {
	keyring.MockInit()
	homeDir := t.TempDir()
	cacheHome := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", cacheHome)

	// Config with no cache.default_ttl
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"custom": {
				Provider:      "custom_vault",
				ResolveValues: true,
				Entities: map[string]config.CustomEntityConfig{
					"app": {
						Attributes: map[string]any{
							"token": `{{ exec "echo" "no-cache-init" }}`,
						},
					},
				},
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("NewOrchestrator failed: %v", err)
	}

	val, err := orch.Resolve(context.Background(), "custom://app:token")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if val != "no-cache-init" {
		t.Fatalf("got %q, want no-cache-init", val)
	}

	// Verify the built-in cache provider was never initialized and no keyring token was created
	orch.providerManager.mu.Lock()
	cacheInit := orch.providerManager.initializedBuiltins["cache"]
	orch.providerManager.mu.Unlock()
	if cacheInit {
		t.Fatalf("expected cache provider to remain uninitialized when cache.default_ttl is unset")
	}

	if _, err := keyring.Get("cloakenv", "cache_token"); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("expected no cache_token in keyring when cache.default_ttl is unset, got err: %v", err)
	}
}

func TestExecMemo_OrchestratorIntegration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell script counter helper test")
	}

	keyring.MockInit()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	tmpDir := t.TempDir()
	counterFile := filepath.Join(tmpDir, "count.txt")
	scriptPath := filepath.Join(tmpDir, "count-token.sh")
	scriptContent := fmt.Sprintf("#!/bin/sh\ncount=0\nif [ -f %q ]; then count=$(cat %q); fi\ncount=$((count + 1))\necho \"$count\" > %q\necho \"shared-token-$count\"\n", counterFile, counterFile, counterFile)
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to write helper script: %v", err)
	}

	expr := fmt.Sprintf(`{{ exec %q "arg1" }}`, scriptPath)
	cfg := &config.Config{
		Cache: config.CacheConfig{
			DefaultTTL: "10m",
		},
		Vaults: map[string]config.VaultConfig{
			"custom": {
				Provider:      "custom_vault",
				ResolveValues: true,
				Entities: map[string]config.CustomEntityConfig{
					"app": {
						Attributes: map[string]any{
							"token_a": expr,
							"token_b": expr,
							"token_c": expr,
						},
					},
				},
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	ctx := context.Background()

	// Resolving app:token_a, app:token_b, app:token_c within the same Orchestrator
	// verifies memoisation across distinct TemplateEngine renders
	for _, key := range []string{"custom://app:token_a", "custom://app:token_b", "custom://app:token_c"} {
		val, err := orch.Resolve(ctx, key)
		if err != nil {
			t.Fatalf("Resolve %s failed: %v", key, err)
		}
		if val != "shared-token-1" {
			t.Fatalf("expected shared-token-1 for %s, got %q", key, val)
		}
	}

	// Verify concurrent environment resolution with BuildEnv
	envVars, err := orch.BuildEnv(ctx, EnvConfig{
		Merges: []string{"custom://app"},
	})
	if err != nil {
		t.Fatalf("BuildEnv failed: %v", err)
	}
	if len(envVars) == 0 {
		t.Fatalf("expected resolved env vars, got empty")
	}

	// Second Orchestrator instance should hit the encrypted disk cache (still count == 1)
	orch2, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create second orchestrator: %v", err)
	}
	val2, err := orch2.Resolve(ctx, "custom://app:token_a")
	if err != nil {
		t.Fatalf("orch2 Resolve failed: %v", err)
	}
	if val2 != "shared-token-1" {
		t.Fatalf("expected orch2 to hit encrypted cache and return shared-token-1, got %q", val2)
	}

	countBytes, err := os.ReadFile(counterFile)
	if err != nil {
		t.Fatalf("failed to read counter file: %v", err)
	}
	if got := strings.TrimSpace(string(countBytes)); got != "1" {
		t.Fatalf("expected subprocess to execute exactly 1 time across both orchestrator runs, got %s", got)
	}
}
