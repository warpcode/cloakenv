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

func TestProviderManager_MappingIntegration(t *testing.T) {
	rule1 := config.MappingRule{Match: "env:(.*)", Key: "$1"}
	_ = rule1.Compile()

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"mapped_vault": {
				Provider: "custom_vault",
				Mapping:  []config.MappingRule{rule1},
				Entities: map[string]map[string]any{
					"app": {
						"env:OPENROUTER_API_KEY": "sk-test-secret",
						"UNMAPPED":               "unmapped_value",
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
	p, isBuiltin, err := orch.providerManager.GetProvider(ctx, "mapped_vault")
	if err != nil {
		t.Fatalf("failed to get provider: %v", err)
	}
	if isBuiltin {
		t.Error("expected isBuiltin to be false")
	}

	val, err := p.GetSecret(ctx, "app:OPENROUTER_API_KEY")
	if err != nil {
		t.Fatalf("GetSecret failed: %v", err)
	}
	if val != "sk-test-secret" {
		t.Errorf("expected secret value 'sk-test-secret', got %q", val)
	}

	searchable, ok := p.(provider.SearchableProvider)
	if !ok {
		t.Fatal("expected provider to implement SearchableProvider")
	}

	entry, err := searchable.GetEntry(ctx, "app")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}

	if entry.Attributes["OPENROUTER_API_KEY"] != "sk-test-secret" {
		t.Errorf("expected mapped attribute OPENROUTER_API_KEY='sk-test-secret', got %v", entry.Attributes["OPENROUTER_API_KEY"])
	}
	if entry.Attributes["UNMAPPED"] != "unmapped_value" {
		t.Errorf("expected unmapped attribute UNMAPPED='unmapped_value', got %v", entry.Attributes["UNMAPPED"])
	}
}

func TestProviderManager_MappingAndFilteringComposition(t *testing.T) {
	rule1 := config.MappingRule{Match: "env:(.*)", Key: "$1"}
	_ = rule1.Compile()

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"composed_vault": {
				Provider:      "custom_vault",
				Mapping:       []config.MappingRule{rule1},
				IncludeFields: []string{"OPENROUTER_API_KEY", "test:foo", "INCLUDED_FIELD"},
				ExcludeFields: []string{"EXCLUDED_FIELD"},
				Entities: map[string]map[string]any{
					"app": {
						"env:OPENROUTER_API_KEY": "sk-test-secret",
						"env:test:foo":           "bar",
						"INCLUDED_FIELD":         "included_val",
						"EXCLUDED_FIELD":         "excluded_val",
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
	p, _, err := orch.providerManager.GetProvider(ctx, "composed_vault")
	if err != nil {
		t.Fatalf("failed to get provider: %v", err)
	}

	// 1. GetSecret for mapped key succeeds
	val, err := p.GetSecret(ctx, "app:OPENROUTER_API_KEY")
	if err != nil {
		t.Fatalf("GetSecret app:OPENROUTER_API_KEY failed: %v", err)
	}
	if val != "sk-test-secret" {
		t.Errorf("expected secret value 'sk-test-secret', got %q", val)
	}

	// 2. GetSecret for renamed original key fails
	_, err = p.GetSecret(ctx, "app:env:OPENROUTER_API_KEY")
	if err == nil {
		t.Fatal("expected GetSecret for renamed original key to fail, got nil error")
	}

	// 3. GetSecret for excluded key fails
	_, err = p.GetSecret(ctx, "app:EXCLUDED_FIELD")
	if err == nil {
		t.Fatal("expected GetSecret for excluded key to fail, got nil error")
	}

	// 4. GetEntry attributes contain mapped and included keys, but NOT excluded or renamed key
	searchable, ok := p.(provider.SearchableProvider)
	if !ok {
		t.Fatal("expected provider to implement SearchableProvider")
	}

	entry, err := searchable.GetEntry(ctx, "app")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}

	if _, ok := entry.Attributes["OPENROUTER_API_KEY"]; !ok {
		t.Error("expected OPENROUTER_API_KEY to be present in GetEntry")
	}
	if _, ok := entry.Attributes["INCLUDED_FIELD"]; !ok {
		t.Error("expected INCLUDED_FIELD to be present in GetEntry")
	}
	if _, ok := entry.Attributes["EXCLUDED_FIELD"]; ok {
		t.Error("expected EXCLUDED_FIELD to be absent from GetEntry")
	}
	if _, ok := entry.Attributes["env:OPENROUTER_API_KEY"]; ok {
		t.Error("expected env:OPENROUTER_API_KEY to be absent from GetEntry")
	}

	// 5. Verify that colon-qualified candidate filtering provided by FilteringProvider is active in the composed chain
	cfg2 := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"colon_vault": {
				Provider:      "custom_vault",
				Mapping:       []config.MappingRule{rule1},
				ExcludeFields: []string{"app:*"},
				Entities: map[string]map[string]any{
					"app": {
						"Password": "pass_val",
					},
				},
			},
		},
	}
	orch2, err := NewOrchestrator(cfg2)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}
	p2, _, err := orch2.providerManager.GetProvider(ctx, "colon_vault")
	if err != nil {
		t.Fatalf("failed to get provider: %v", err)
	}
	_, err = p2.GetSecret(ctx, "app:Password")
	if err == nil {
		t.Fatal("expected colon-qualified exclusion 'app:*' enforced by FilteringProvider to block GetSecret, got nil error")
	}
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
