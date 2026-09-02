package engine

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
)

func TestCheckAccess(t *testing.T) {
	ctx := context.Background()

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"valid_vault": {
				Provider: "custom_vault",
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	// We'll configure a failing vault using a provider that doesn't exist
	// which will cause initVaultProvider to return an "unsupported provider type" error.
	orch.config.Vaults["failing_vault"] = config.VaultConfig{
		Provider: "unsupported_provider",
	}

	tests := []struct {
		name      string
		vaultName string
		wantErr   string
	}{
		{
			name:      "valid_vault",
			vaultName: "valid_vault",
			wantErr:   "",
		},
		{
			name:      "unknown_vault",
			vaultName: "nonexistent_vault",
			wantErr:   "unknown scheme or vault",
		},
		{
			name:      "failing_vault",
			vaultName: "failing_vault",
			wantErr:   "unsupported provider type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := orch.CheckAccess(ctx, tt.vaultName)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestClearCache(t *testing.T) {
	keyring.MockInit()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())

	cfg := &config.Config{}
	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	t.Run("missing cache provider", func(t *testing.T) {
		origCache := orch.providerManager.builtins["cache"]
		delete(orch.providerManager.builtins, "cache")
		defer func() { orch.providerManager.builtins["cache"] = origCache }()

		err := orch.ClearCache(context.Background())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "cache provider not registered") {
			t.Errorf("expected error message to contain 'cache provider not registered', got %q", err.Error())
		}
	})

	t.Run("initialization failure", func(t *testing.T) {
		origCache := orch.providerManager.builtins["cache"]
		orch.providerManager.builtins["cache"] = &failInitProvider{}
		defer func() { orch.providerManager.builtins["cache"] = origCache }()
		delete(orch.providerManager.initializedBuiltins, "cache")

		err := orch.ClearCache(context.Background())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "init failed") {
			t.Errorf("expected error message to contain 'init failed', got %q", err.Error())
		}
	})

	t.Run("invalid cache provider type", func(t *testing.T) {
		origCache := orch.providerManager.builtins["cache"]
		orch.providerManager.builtins["cache"] = provider.NewEnvProvider()
		defer func() { orch.providerManager.builtins["cache"] = origCache }()
		delete(orch.providerManager.initializedBuiltins, "cache")

		err := orch.ClearCache(context.Background())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "invalid cache provider type") {
			t.Errorf("expected error message to contain 'invalid cache provider type', got %q", err.Error())
		}
	})

	t.Run("successful clear cache with mock", func(t *testing.T) {
		origCache := orch.providerManager.builtins["cache"]
		mockCache := &mockCacheProvider{}
		orch.providerManager.builtins["cache"] = mockCache
		defer func() { orch.providerManager.builtins["cache"] = origCache }()
		delete(orch.providerManager.initializedBuiltins, "cache")

		err := orch.ClearCache(context.Background())
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !mockCache.clearCalled {
			t.Errorf("expected mock cache to have clearCalled = true")
		}
	})
}

func TestProviderManagerUnknownSchemeDoesNotAllocateLock(t *testing.T) {
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"known_vault": {
				Provider: "custom_vault",
			},
		},
	}
	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	ctx := context.Background()
	_, _, err = orch.providerManager.GetProvider(ctx, "nonexistent_vault")
	if err == nil {
		t.Fatal("expected error for nonexistent vault, got nil")
	}

	orch.providerManager.mu.Lock()
	lockCount := len(orch.providerManager.initLocks)
	orch.providerManager.mu.Unlock()

	if lockCount != 0 {
		t.Errorf("expected 0 initLocks allocated for unknown vault, got %d", lockCount)
	}
}

type countingInitProvider struct {
	initCount atomic.Int64
}

func (p *countingInitProvider) Scheme() string { return "counting" }
func (p *countingInitProvider) Initialize(_ context.Context, _ provider.ProviderConfig) error {
	p.initCount.Add(1)
	return nil
}
func (p *countingInitProvider) Validate(_ map[string]string) error { return nil }
func (p *countingInitProvider) GetSecret(_ context.Context, _ string) (string, error) {
	return "val", nil
}
func (p *countingInitProvider) SetSecret(_ context.Context, _, _ string) error { return nil }
func (p *countingInitProvider) DeleteSecret(_ context.Context, _ string) error { return nil }

func TestProviderManagerConcurrentInitialization(t *testing.T) {
	t.Run("builtin concurrent initialization race", func(t *testing.T) {
		counting := &countingInitProvider{}
		builtins := map[string]provider.SecretProvider{
			"counting": counting,
		}
		pm := NewProviderManager(&config.Config{}, builtins, nil)
		ctx := context.Background()

		const numWorkers = 50
		var wg sync.WaitGroup
		wg.Add(numWorkers)

		for range numWorkers {
			go func() {
				defer wg.Done()
				p, isBuiltin, err := pm.GetProvider(ctx, "counting")
				if err != nil {
					t.Errorf("GetProvider failed: %v", err)
					return
				}
				if !isBuiltin {
					t.Errorf("expected isBuiltin to be true")
				}
				if p != provider.SecretProvider(counting) {
					t.Errorf("expected provider instance %p, got %p", counting, p)
				}
			}()
		}

		wg.Wait()

		if count := counting.initCount.Load(); count != 1 {
			t.Errorf("expected Initialize to be called exactly 1 time, got %d", count)
		}
	})

	t.Run("vault concurrent initialization race", func(t *testing.T) {
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"test_vault": {
					Provider: "custom_vault",
					Entities: map[string]map[string]any{
						"app": {
							"KEY": "val",
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

		const numWorkers = 50
		var wg sync.WaitGroup
		wg.Add(numWorkers)
		providers := make([]provider.SecretProvider, numWorkers)

		for i := range numWorkers {
			idx := i
			go func() {
				defer wg.Done()
				p, isBuiltin, err := orch.providerManager.GetProvider(ctx, "test_vault")
				if err != nil {
					t.Errorf("GetProvider failed: %v", err)
					return
				}
				if isBuiltin {
					t.Errorf("expected isBuiltin to be false")
				}
				providers[idx] = p
			}()
		}

		wg.Wait()

		first := providers[0]
		if first == nil {
			t.Fatal("first provider is nil")
		}
		for i, p := range providers {
			if p != first {
				t.Errorf("worker %d got different provider instance (%p vs %p)", i, p, first)
			}
		}
	})
}
