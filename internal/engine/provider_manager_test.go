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
				Entities: config.ToCustomEntityMap(map[string]map[string]any{
					"app": {
						"env:OPENROUTER_API_KEY": "sk-test-secret",
						"UNMAPPED":               "unmapped_value",
					},
				}),
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
				Entities: config.ToCustomEntityMap(map[string]map[string]any{
					"app": {
						"env:OPENROUTER_API_KEY": "sk-test-secret",
						"env:test:foo":           "bar",
						"INCLUDED_FIELD":         "included_val",
						"EXCLUDED_FIELD":         "excluded_val",
					},
				}),
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
				Entities: config.ToCustomEntityMap(map[string]map[string]any{
					"app": {
						"Password": "pass_val",
					},
				}),
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

func TestProviderManager_MappedFieldsAlwaysShowRegardlessOfFilters(t *testing.T) {
	rule1 := config.MappingRule{Match: "env:(.*)", Key: "$1"}
	_ = rule1.Compile()

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"override_vault": {
				Provider:      "custom_vault",
				Mapping:       []config.MappingRule{rule1},
				IncludeFields: []string{"UNMAPPED_KEPT"},
				ExcludeFields: []string{"UNMAPPED_DROPPED", "MAPPED_KEY"},
				Entities: config.ToCustomEntityMap(map[string]map[string]any{
					"service": {
						"env:MAPPED_KEY":   "mapped_secret_value",
						"UNMAPPED_KEPT":    "kept_val",
						"UNMAPPED_DROPPED": "dropped_val",
					},
				}),
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	ctx := context.Background()

	// 1. Resolve resolves mapped key even though exclude_fields has "*" and "MAPPED_KEY"
	val, err := orch.Resolve(ctx, "override_vault://service:MAPPED_KEY")
	if err != nil {
		t.Fatalf("expected Resolve for mapped key to succeed, got %v", err)
	}
	if val != "mapped_secret_value" {
		t.Errorf("expected 'mapped_secret_value', got %q", val)
	}

	// 2. Resolve resolves unmapped key in include_fields
	valKept, err := orch.Resolve(ctx, "override_vault://service:UNMAPPED_KEPT")
	if err != nil {
		t.Fatalf("expected Resolve for UNMAPPED_KEPT to succeed, got %v", err)
	}
	if valKept != "kept_val" {
		t.Errorf("expected 'kept_val', got %q", valKept)
	}

	// 3. Resolve fails for unmapped key matching exclude_fields
	_, err = orch.Resolve(ctx, "override_vault://service:UNMAPPED_DROPPED")
	if err == nil {
		t.Fatal("expected Resolve for UNMAPPED_DROPPED to fail, got nil")
	}

	// 4. GetEntry shows mapped key and included unmapped key, but not excluded unmapped key
	entry, err := orch.GetEntry(ctx, "override_vault://service")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if entry.Attributes["MAPPED_KEY"] != "mapped_secret_value" {
		t.Errorf("expected mapped MAPPED_KEY in GetEntry, got %v", entry.Attributes["MAPPED_KEY"])
	}
	if entry.Attributes["UNMAPPED_KEPT"] != "kept_val" {
		t.Errorf("expected UNMAPPED_KEPT in GetEntry, got %v", entry.Attributes["UNMAPPED_KEPT"])
	}
	// 5. When exclude_fields is wildcard "*", mapped key STILL shows and resolves
	cfgWildcard := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"wildcard_vault": {
				Provider:      "custom_vault",
				Mapping:       []config.MappingRule{rule1},
				ExcludeFields: []string{"*"},
				Entities: config.ToCustomEntityMap(map[string]map[string]any{
					"service": {
						"env:MAPPED_KEY": "wildcard_secret",
						"OTHER_FIELD":    "other_val",
					},
				}),
			},
		},
	}
	orchWildcard, err := NewOrchestrator(cfgWildcard)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}
	valWildcard, err := orchWildcard.Resolve(ctx, "wildcard_vault://service:MAPPED_KEY")
	if err != nil {
		t.Fatalf("expected Resolve for mapped key under exclude_fields=['*'] to succeed, got %v", err)
	}
	if valWildcard != "wildcard_secret" {
		t.Errorf("expected 'wildcard_secret', got %q", valWildcard)
	}
	entryWildcard, err := orchWildcard.GetEntry(ctx, "wildcard_vault://service")
	if err != nil {
		t.Fatalf("GetEntry under exclude_fields=['*'] failed: %v", err)
	}
	if entryWildcard.Attributes["MAPPED_KEY"] != "wildcard_secret" {
		t.Errorf("expected mapped key in GetEntry under exclude_fields=['*'], got %v", entryWildcard.Attributes["MAPPED_KEY"])
	}
	if _, ok := entryWildcard.Attributes["OTHER_FIELD"]; ok {
		t.Error("expected OTHER_FIELD to be excluded by exclude_fields=['*']")
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
					Entities: config.ToCustomEntityMap(map[string]map[string]any{
						"app": {
							"KEY": "val",
						},
					}),
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

func TestProviderManager_KeyFormatOverride(t *testing.T) {
	ctx := context.Background()

	cfg := &config.Config{
		KeyFormatOverride: "snake",
		Vaults: map[string]config.VaultConfig{
			"vault_env": {
				Provider:  "custom_vault",
				KeyFormat: "env",
			},
			"vault_none": {
				Provider:  "custom_vault",
				KeyFormat: "env",
			},
		},
	}
	// Manually override after config definition to test "none"
	cfg.KeyFormatOverride = "none"

	pm := NewProviderManager(cfg, nil, nil)

	// Ensure that when KeyFormatOverride is "none", no KeyFormatProvider is applied
	p, err := pm.getVaultProvider(ctx, "vault_none")
	if err != nil {
		t.Fatalf("failed to get vault provider: %v", err)
	}

	_, isKFP := p.(*provider.SearchableValueResolvableKeyFormatProvider)
	if isKFP {
		t.Errorf("expected provider not to be wrapped in KeyFormatProvider when override is 'none'")
	}

	cfg.KeyFormatOverride = "snake"

	p2, err := pm.getVaultProvider(ctx, "vault_env")
	if err != nil {
		t.Fatalf("failed to get vault provider: %v", err)
	}

	kfp, ok := p2.(interface {
		Format() string
	})
	if !ok {
		t.Fatalf("expected provider to be wrapped in KeyFormatProvider")
	}

	if kfp.Format() != "snake" {
		t.Errorf("expected key format 'snake', got %q", kfp.Format())
	}
}

func TestProviderManager_SetGetEntriesFunc_And_SetRenderTemplateFunc(t *testing.T) {
	keyring.MockInit()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())

	ctx := context.Background()

	tests := []struct {
		name              string
		setGetEntries     bool
		setRenderTemplate bool
		composedEntity    config.CustomEntityConfig
		wantErr           bool
		wantErrSub        string
		checkFn           func(t *testing.T, entry provider.Entry, getEntriesCalled, renderTemplateCalled bool)
	}{
		{
			name:              "both callbacks set are propagated to custom_vault provider",
			setGetEntries:     true,
			setRenderTemplate: true,
			composedEntity: config.CustomEntityConfig{
				Title:      "Composed App",
				IsComposed: true,
				Merge:      []string{"mock://entries"},
				Attributes: map[string]any{
					"RENDERED_KEY": "{{ .attributes.SRC_KEY }}",
				},
			},
			wantErr: false,
			checkFn: func(t *testing.T, entry provider.Entry, getEntriesCalled, renderTemplateCalled bool) {
				if !getEntriesCalled {
					t.Errorf("expected getEntriesFunc to have been called")
				}
				if !renderTemplateCalled {
					t.Errorf("expected renderTemplateFunc to have been called")
				}
				if entry.Attributes["RENDERED_KEY"] != "rendered:src_val" {
					t.Errorf("RENDERED_KEY = %v, want 'rendered:src_val'", entry.Attributes["RENDERED_KEY"])
				}
			},
		},
		{
			name:              "missing getEntriesFunc returns error when composed entity has merge",
			setGetEntries:     false,
			setRenderTemplate: true,
			composedEntity: config.CustomEntityConfig{
				Title:      "Composed App",
				IsComposed: true,
				Merge:      []string{"mock://entries"},
			},
			wantErr:    true,
			wantErrSub: "merge execution not supported in this context",
		},
		{
			name:              "missing renderTemplateFunc returns error when composed entity has template",
			setGetEntries:     true,
			setRenderTemplate: false,
			composedEntity: config.CustomEntityConfig{
				Title:      "Composed App",
				IsComposed: true,
				Merge:      []string{"mock://entries"},
				Attributes: map[string]any{
					"RENDERED_KEY": "{{ .attributes.SRC_KEY }}",
				},
			},
			wantErr:    true,
			wantErrSub: "template rendering not supported in this context",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Vaults: map[string]config.VaultConfig{
					"test_vault": {
						Provider: "custom_vault",
						Entities: map[string]config.CustomEntityConfig{
							"comp": tt.composedEntity,
						},
					},
				},
			}

			pm := NewProviderManager(cfg, nil, nil)
			var getEntriesCalled bool
			var renderTemplateCalled bool

			if tt.setGetEntries {
				pm.SetGetEntriesFunc(func(ctx context.Context, uri string) ([]provider.Entry, error) {
					getEntriesCalled = true
					return []provider.Entry{
						{
							Title: "Src",
							Attributes: map[string]any{
								"SRC_KEY": "src_val",
							},
						},
					}, nil
				})
			}

			if tt.setRenderTemplate {
				pm.SetRenderTemplateFunc(func(ctx context.Context, tmplStr string, data any) (string, error) {
					renderTemplateCalled = true
					return "rendered:src_val", nil
				})
			}

			p, _, err := pm.GetProvider(ctx, "test_vault")
			if err != nil {
				t.Fatalf("GetProvider failed: %v", err)
			}

			sp, ok := p.(provider.SearchableProvider)
			if !ok {
				t.Fatalf("provider does not implement SearchableProvider")
			}

			entry, err := sp.GetEntry(ctx, "comp")
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetEntry() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr {
				if tt.wantErrSub != "" && !strings.Contains(err.Error(), tt.wantErrSub) {
					t.Errorf("error %q does not contain expected substring %q", err.Error(), tt.wantErrSub)
				}
			} else if tt.checkFn != nil {
				tt.checkFn(t, entry, getEntriesCalled, renderTemplateCalled)
			}
		})
	}
}
