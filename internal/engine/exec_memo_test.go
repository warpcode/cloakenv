package engine

import (
	"context"
	"errors"
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

		runFn := func(ctx context.Context) (string, error) {
			callCount.Add(1)
			return "tok_123", nil
		}

		ctx := context.Background()
		for i := 0; i < 5; i++ {
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

		runFn := func(ctx context.Context) (string, error) {
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

		for i := 0; i < goroutines; i++ {
			go func(idx int) {
				defer wg.Done()
				res, err := memo.execute(ctx, "/bin/token-gen", []string{"--user", "bob"}, nil, 0, runFn)
				results[idx] = res
				errs[idx] = err
			}(i)
		}

		// Allow some goroutines to queue into flight deduplication
		time.Sleep(10 * time.Millisecond)
		close(gate)
		wg.Wait()

		if got := callCount.Load(); got != 1 {
			t.Fatalf("concurrent runFn call count = %d, want 1", got)
		}

		for i := 0; i < goroutines; i++ {
			if errs[i] != nil {
				t.Fatalf("goroutine %d failed: %v", i, errs[i])
			}
			if results[i] != "tok_concurrent" {
				t.Fatalf("goroutine %d expected tok_concurrent, got %q", i, results[i])
			}
		}
	})

	t.Run("context cancellation during in-flight wait does not affect completed run", func(t *testing.T) {
		memo := newExecMemo()
		gate := make(chan struct{})

		runFn := func(ctx context.Context) (string, error) {
			<-gate
			return "tok_slow", nil
		}

		// Goroutine 1 starts the flight
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = memo.execute(context.Background(), "/bin/slow", nil, nil, 0, runFn)
		}()

		time.Sleep(10 * time.Millisecond)

		// Goroutine 2 cancels while waiting on flight
		cancelCtx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := memo.execute(cancelCtx, "/bin/slow", nil, nil, 0, runFn)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}

		close(gate)
		wg.Wait()

		// Subsequent call receives the memoised result
		res, err := memo.execute(context.Background(), "/bin/slow", nil, nil, 0, runFn)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res != "tok_slow" {
			t.Fatalf("expected tok_slow, got %q", res)
		}
	})
}

func TestExecMemo_PersistentCacheBacking(t *testing.T) {
	mockCache := newMockMemoryCacheProvider()
	var callCount atomic.Int64

	runFn := func(ctx context.Context) (string, error) {
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
}

func TestExecMemo_OrchestratorIntegration(t *testing.T) {
	keyring.MockInit()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

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
							"token_a": `{{ exec "echo" "shared-token" }}`,
							"token_b": `{{ exec "echo" "shared-token" }}`,
							"token_c": `{{ exec "echo" "shared-token" }}`,
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
	valA, err := orch.Resolve(ctx, "custom://app:token_a")
	if err != nil {
		t.Fatalf("Resolve token_a failed: %v", err)
	}
	if valA != "shared-token" {
		t.Fatalf("expected shared-token, got %q", valA)
	}

	valB, err := orch.Resolve(ctx, "custom://app:token_b")
	if err != nil {
		t.Fatalf("Resolve token_b failed: %v", err)
	}
	if valB != "shared-token" {
		t.Fatalf("expected shared-token, got %q", valB)
	}

	valC, err := orch.Resolve(ctx, "custom://app:token_c")
	if err != nil {
		t.Fatalf("Resolve token_c failed: %v", err)
	}
	if valC != "shared-token" {
		t.Fatalf("expected shared-token, got %q", valC)
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
}
