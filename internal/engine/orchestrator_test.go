package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
)

type failInitProvider struct{}

func (f *failInitProvider) Scheme() string { return "failinit" }
func (f *failInitProvider) Initialize(_ context.Context, _ provider.ProviderConfig) error {
	return errors.New("init failed")
}
func (f *failInitProvider) GetSecret(_ context.Context, _ string) (string, error) { return "", nil }
func (f *failInitProvider) SetSecret(_ context.Context, _, _ string) error        { return nil }
func (f *failInitProvider) DeleteSecret(_ context.Context, _ string) error        { return nil }
func (f *failInitProvider) Validate(_ map[string]string) error                    { return nil }
func (f *failInitProvider) Search(_ context.Context, _ provider.SearchQuery) ([]provider.SearchResult, error) {
	return nil, nil
}
func (f *failInitProvider) GetEntry(_ context.Context, _ string) (provider.Entry, error) {
	return provider.Entry{}, nil
}

type mockCacheProvider struct {
	clearCalled bool
}

func (m *mockCacheProvider) Scheme() string { return "cache" }
func (m *mockCacheProvider) Initialize(_ context.Context, _ provider.ProviderConfig) error {
	return nil
}
func (m *mockCacheProvider) GetSecret(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (m *mockCacheProvider) SetSecret(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockCacheProvider) DeleteSecret(_ context.Context, _ string) error {
	return nil
}
func (m *mockCacheProvider) Validate(_ map[string]string) error {
	return nil
}
func (m *mockCacheProvider) ClearCache() error {
	m.clearCalled = true
	return nil
}

func TestOrchestratorFacade(t *testing.T) {
	keyring.MockInit()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())

	ctx := context.Background()
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"test_vault": {Provider: "env"},
		},
		Envs: map[string][]config.VaultSourceConfig{
			"deploy_env": {
				{
					Vault: "test_vault",
					Query: `{{ eq .title "Service A" }}`,
					Rules: []config.RuleConfig{{Type: "pass"}},
				},
			},
		},
		Autoload: []config.AutoloadRule{
			{
				Match: "deploy",
				Envs:  []config.AutoloadEnvItem{config.NamedEnvItem("deploy_env")},
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}
	if err := injectMockVault(orch, "test_vault", []provider.SearchResult{
		{
			Provider: "search", Vault: "test_vault", Path: "service_a",
			Entry: provider.Entry{
				Title:      "Service A",
				Tags:       []string{"prod", "api"},
				Attributes: map[string]any{"API_KEY": "secret-api-key"},
			},
		},
	}); err != nil {
		t.Fatalf("injectMockVault failed: %v", err)
	}

	t.Run("Resolve and ResolveWithKey", func(t *testing.T) {
		val, err := orch.Resolve(ctx, `{{ get "test_vault://service_a:API_KEY" }}`)
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if val != "secret-api-key" {
			t.Errorf("Resolve() = %q, want %q", val, "secret-api-key")
		}

		valKey, err := orch.ResolveWithKey(ctx, `{{ get "test_vault://service_a:API_KEY" }}`, "MY_KEY")
		if err != nil {
			t.Fatalf("ResolveWithKey failed: %v", err)
		}
		if valKey != "secret-api-key" {
			t.Errorf("ResolveWithKey() = %q, want %q", valKey, "secret-api-key")
		}
	})

	t.Run("GetEntry", func(t *testing.T) {
		entry, err := orch.GetEntry(ctx, "test_vault://service_a")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		if entry.Attributes["API_KEY"] != "secret-api-key" {
			t.Errorf("GetEntry() API_KEY = %v, want %q", entry.Attributes["API_KEY"], "secret-api-key")
		}
	})

	t.Run("Search", func(t *testing.T) {
		results, err := orch.Search(ctx, `{{ hasTag "prod" .tags }}`, nil)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 1 {
			t.Errorf("Search() len = %d, want 1", len(results))
		}
	})

	t.Run("BuildEnv and BuildEnvForCommand", func(t *testing.T) {
		env, err := orch.BuildEnv(ctx, EnvConfig{Explicit: map[string]string{"CUSTOM": "test_vault://service_a:API_KEY"}, EmptyEnv: true})
		if err != nil {
			t.Fatalf("BuildEnv failed: %v", err)
		}
		if len(env) != 1 || env[0] != "CUSTOM=secret-api-key" {
			t.Errorf("BuildEnv() = %v, want [CUSTOM=secret-api-key]", env)
		}

		args, cmdEnv, err := orch.BuildEnvForCommand(ctx, EnvConfig{CmdArgs: []string{"deploy", "--run"}, EmptyEnv: true})
		if err != nil {
			t.Fatalf("BuildEnvForCommand failed: %v", err)
		}
		if len(args) != 2 || args[0] != "deploy" {
			t.Errorf("BuildEnvForCommand args = %v, want [deploy, --run]", args)
		}
		foundAPIKey := false
		for _, e := range cmdEnv {
			if e == "API_KEY=secret-api-key" {
				foundAPIKey = true
				break
			}
		}
		if !foundAPIKey {
			t.Errorf("BuildEnvForCommand expected API_KEY=secret-api-key in env %v", cmdEnv)
		}
	})

	t.Run("KeyringPrefix and Accessors", func(t *testing.T) {
		if prefix := orch.KeyringPrefix(); prefix != "cloakenv" {
			t.Errorf("KeyringPrefix() = %q, want 'cloakenv'", prefix)
		}
		if kr := orch.Keyring(); kr == nil {
			t.Errorf("Keyring() returned nil")
		}
	})

	t.Run("CheckAccess", func(t *testing.T) {
		if err := orch.CheckAccess(ctx, "test_vault"); err != nil {
			t.Errorf("CheckAccess(test_vault) unexpected error: %v", err)
		}
		if err := orch.CheckAccess(ctx, "unknown"); err == nil {
			t.Errorf("CheckAccess(unknown) expected error, got nil")
		}
	})

	t.Run("AutoloadAliasCheckers", func(t *testing.T) {
		rule, matched, err := orch.MatchRunAlias([]string{"deploy"})
		if err != nil {
			t.Fatalf("MatchRunAlias returned unexpected error: %v", err)
		}
		if !matched || rule.Match != "deploy" {
			t.Errorf("MatchRunAlias(deploy) = (%v, %v), want rule with match 'deploy'", rule, matched)
		}
		if !orch.IsRunAlias([]string{"deploy"}) {
			t.Errorf("IsRunAlias(deploy) = false, want true")
		}

		var nilOrch *Orchestrator
		if _, nilMatched, _ := nilOrch.MatchRunAlias([]string{"deploy"}); nilMatched {
			t.Errorf("nilOrch.MatchRunAlias should return false")
		}
		if nilOrch.IsRunAlias([]string{"deploy"}) {
			t.Errorf("nilOrch.IsRunAlias should return false")
		}
	})

	t.Run("Write and Delete", func(t *testing.T) {
		err := orch.Write(ctx, "keyring://service_a/api_key", "new-value")
		if err != nil {
			t.Fatalf("Write failed: %v", err)
		}
		val, err := orch.Resolve(ctx, `{{ get "keyring://service_a/api_key" }}`)
		if err != nil || val != "new-value" {
			t.Errorf("after Write, Resolve() = (%q, %v), want ('new-value', nil)", val, err)
		}

		err = orch.Delete(ctx, "keyring://service_a/api_key")
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}
	})

	t.Run("ClearCache", func(t *testing.T) {
		err := orch.ClearCache(ctx)
		if err != nil {
			t.Fatalf("ClearCache failed: %v", err)
		}
	})

	t.Run("PruneCache", func(t *testing.T) {
		stats, err := orch.PruneCache(ctx)
		if err != nil {
			t.Fatalf("PruneCache failed: %v", err)
		}
		if stats.Count != 0 {
			t.Errorf("expected 0 pruned entries on fresh cache, got %d", stats.Count)
		}
	})

	t.Run("Login and Forget NonKeePass", func(t *testing.T) {
		err := orch.Login(ctx, "test_vault")
		if err == nil || !strings.Contains(err.Error(), "does not support authentication") {
			t.Errorf("Login non-keepass expected unsupported error, got: %v", err)
		}

		err = orch.Forget(ctx, "test_vault")
		if err == nil || !strings.Contains(err.Error(), "does not support authentication") {
			t.Errorf("Forget non-keepass expected unsupported error, got: %v", err)
		}
	})
}

func TestOrchestrator_FieldFiltering(t *testing.T) {
	keyring.MockInit()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())

	ctx := context.Background()
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"filtered_vault": {
				Provider: "env",
				IncludeFields: []string{
					"env:*",
					"UserName",
				},
				ExcludeFields: []string{
					"*.notes",
					"secret:*",
				},
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}
	if err := injectMockVault(orch, "filtered_vault", []provider.SearchResult{
		{
			Provider: "search", Vault: "filtered_vault", Path: "app_config",
			Entry: provider.Entry{
				Title: "app_config",
				Attributes: map[string]any{
					"UserName":        "app_admin",
					"Password":        "super_secret_pw",
					"env:PORT":        "8080",
					"env:HOST":        "127.0.0.1",
					"env:notes.notes": "internal notes",
					"secret:token":    "bearer_12345",
					"db.notes":        "production database notes",
				},
			},
		},
	}); err != nil {
		t.Fatalf("injectMockVault failed: %v", err)
	}

	t.Run("GetEntry_FilteredAttributes", func(t *testing.T) {
		entry, err := orch.GetEntry(ctx, "filtered_vault://app_config")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}

		if len(entry.Attributes) != 3 {
			t.Fatalf("expected 3 attributes after filtering, got %d (%v)", len(entry.Attributes), entry.Attributes)
		}

		if entry.Attributes["UserName"] != "app_admin" {
			t.Errorf("UserName = %v, want 'app_admin'", entry.Attributes["UserName"])
		}
		if entry.Attributes["env:PORT"] != "8080" {
			t.Errorf("env:PORT = %v, want '8080'", entry.Attributes["env:PORT"])
		}
		if entry.Attributes["env:HOST"] != "127.0.0.1" {
			t.Errorf("env:HOST = %v, want '127.0.0.1'", entry.Attributes["env:HOST"])
		}

		// Ensure excluded/non-included fields are removed
		for _, excluded := range []string{"Password", "secret:token", "db.notes", "env:notes.notes"} {
			if _, ok := entry.Attributes[excluded]; ok {
				t.Errorf("field %q should have been filtered out", excluded)
			}
		}
	})

	t.Run("Search_FilteredAttributes", func(t *testing.T) {
		results, err := orch.Search(ctx, "", []string{"filtered_vault"})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		if len(results) != 1 {
			t.Fatalf("expected 1 search result, got %d", len(results))
		}

		resEntry := results[0].Entry
		if len(resEntry.Attributes) != 3 {
			t.Fatalf("expected 3 attributes in search result entry, got %d (%v)", len(resEntry.Attributes), resEntry.Attributes)
		}
	})

	t.Run("Resolve_AllowedField", func(t *testing.T) {
		val, err := orch.Resolve(ctx, `{{ get "filtered_vault://app_config:env:PORT" }}`)
		if err != nil {
			t.Fatalf("Resolve env:PORT failed: %v", err)
		}
		if val != "8080" {
			t.Errorf("Resolve env:PORT = %q, want '8080'", val)
		}
	})

	t.Run("Resolve_ExcludedField", func(t *testing.T) {
		_, err := orch.Resolve(ctx, `{{ get "filtered_vault://app_config:Password" }}`)
		if err == nil {
			t.Fatalf("expected error resolving excluded field Password, got nil")
		}

		_, errToken := orch.Resolve(ctx, `{{ get "filtered_vault://app_config:secret:token" }}`)
		if errToken == nil {
			t.Fatalf("expected error resolving excluded field secret:token, got nil")
		}
	})
}

func TestOrchestrator_FieldFiltering_WrappedDirectProviderResolution(t *testing.T) {
	keyring.MockInit()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())
	t.Setenv("MY_APP_SECRET", "super_secret_from_env")

	ctx := context.Background()
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"filtered_src": {
				Provider:      "env",
				IncludeFields: []string{"api_key"},
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}
	if err := injectMockVault(orch, "filtered_src", []provider.SearchResult{
		{
			Provider: "search", Vault: "filtered_src", Path: "app",
			Entry: provider.Entry{
				Title: "app",
				Attributes: map[string]any{
					"api_key":      `{{ get "env://MY_APP_SECRET" }}`,
					"excluded_key": `{{ get "env://MY_APP_SECRET" }}`,
				},
			},
		},
	}); err != nil {
		t.Fatalf("injectMockVault failed: %v", err)
	}

	// 1. Allowed field with nested URI should resolve through to the env provider value
	val, err := orch.Resolve(ctx, `{{ get "filtered_src://api_key" }}`)
	if err != nil {
		t.Fatalf("failed to resolve filtered_src://api_key: %v", err)
	}
	if val != "super_secret_from_env" {
		t.Errorf("Resolve = %q, want 'super_secret_from_env'", val)
	}

	// 2. Also check GetEntry has the resolved attribute value
	entry, err := orch.GetEntry(ctx, "filtered_src://")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if entry.Attributes["api_key"] != "super_secret_from_env" {
		t.Errorf("GetEntry attribute api_key = %v, want 'super_secret_from_env'", entry.Attributes["api_key"])
	}
	if _, ok := entry.Attributes["excluded_key"]; ok {
		t.Errorf("excluded_key should be filtered out")
	}

	// 3. Excluded key must not be resolvable
	_, errExcluded := orch.Resolve(ctx, `{{ get "filtered_src://excluded_key" }}`)
	if errExcluded == nil {
		t.Fatalf("expected error resolving excluded_key, got nil")
	}
}

func TestOrchestrator_GetEntries(t *testing.T) {
	keyring.MockInit()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())

	ctx := context.Background()

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"test_vault": {Provider: "env"},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}
	if err := injectMockVault(orch, "test_vault", []provider.SearchResult{
		{
			Provider: "search",
			Vault:    "test_vault",
			Path:     "flat_entry",
			Entry: provider.Entry{
				Title: "Flat Entry",
				Tags:  []string{"tag1"},
				Attributes: map[string]any{
					"KEY1": "val1",
				},
			},
		},
	}); err != nil {
		t.Fatalf("injectMockVault failed: %v", err)
	}

	tests := []struct {
		name      string
		uri       string
		wantCount int
		wantErr   bool
		checkFn   func(t *testing.T, entries []provider.Entry)
	}{
		{
			name:      "flat entry lookup returns single entry",
			uri:       "test_vault://flat_entry",
			wantCount: 1,
			wantErr:   false,
			checkFn: func(t *testing.T, entries []provider.Entry) {
				if entries[0].Title != "Flat Entry" {
					t.Errorf("Title = %q, want 'Flat Entry'", entries[0].Title)
				}
				if entries[0].Attributes["KEY1"] != "val1" {
					t.Errorf("KEY1 = %v, want 'val1'", entries[0].Attributes["KEY1"])
				}
			},
		},
		{
			name:      "direct GetEntries with search URL parameters",
			uri:       "search://title=Flat Entry",
			wantCount: 1,
			wantErr:   false,
			checkFn: func(t *testing.T, entries []provider.Entry) {
				if entries[0].Title != "Flat Entry" {
					t.Errorf("Title = %q, want 'Flat Entry'", entries[0].Title)
				}
			},
		},
		{
			name:      "direct GetEntries with search Expr '==' equality expression",
			uri:       `search://{{ eq .title "Flat Entry" }}`,
			wantCount: 1,
			wantErr:   false,
			checkFn: func(t *testing.T, entries []provider.Entry) {
				if entries[0].Title != "Flat Entry" {
					t.Errorf("Title = %q, want 'Flat Entry'", entries[0].Title)
				}
			},
		},
		{
			name:      "invalid uri scheme returns error",
			uri:       "invalid_uri",
			wantCount: 0,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries, err := orch.GetEntries(ctx, tt.uri)
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetEntries(%q) error = %v, wantErr = %v", tt.uri, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(entries) != tt.wantCount {
				t.Fatalf("GetEntries(%q) count = %d, want %d", tt.uri, len(entries), tt.wantCount)
			}
			if tt.checkFn != nil {
				tt.checkFn(t, entries)
			}
		})
	}
}

func TestOrchestrator_CyclicMergeDepthError(t *testing.T) {
	keyring.MockInit()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())

	ctx := context.Background()

	tests := []struct {
		name       string
		vaults     map[string]config.VaultConfig
		mockVault  string
		entries    []provider.SearchResult
		targetURI  string
		wantErrSub string
	}{
		{
			name: "two entities mutually merging cause max depth error",
			vaults: map[string]config.VaultConfig{
				"loop_vault": {
					Provider: "env",
				},
			},
			mockVault: "loop_vault",
			entries: []provider.SearchResult{
				{
					Provider: "search", Vault: "loop_vault", Path: "entity_a",
					Entry: provider.Entry{
						Title: "Entity A",
						Attributes: map[string]any{
							"attr": `{{ get "loop_vault://entity_b:attr" }}`,
						},
					},
				},
				{
					Provider: "search", Vault: "loop_vault", Path: "entity_b",
					Entry: provider.Entry{
						Title: "Entity B",
						Attributes: map[string]any{
							"attr": `{{ get "loop_vault://entity_a:attr" }}`,
						},
					},
				},
			},
			targetURI:  "loop_vault://entity_a",
			wantErrSub: "reached max depth 5",
		},
		{
			name: "self-merging composed entity causes max depth error",
			vaults: map[string]config.VaultConfig{
				"self_vault": {
					Provider: "env",
				},
			},
			mockVault: "self_vault",
			entries: []provider.SearchResult{
				{
					Provider: "search", Vault: "self_vault", Path: "self_loop",
					Entry: provider.Entry{
						Title: "Self Loop",
						Attributes: map[string]any{
							"attr": `{{ get "self_vault://self_loop:attr" }}`,
						},
					},
				},
			},
			targetURI:  "self_vault://self_loop",
			wantErrSub: "reached max depth 5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{Vaults: tt.vaults}
			orch, err := NewOrchestrator(cfg)
			if err != nil {
				t.Fatalf("failed to create orchestrator: %v", err)
			}
			if err := orch.InjectMockVaultForTest(tt.mockVault, tt.entries); err != nil {
				t.Fatalf("failed to inject mock vault: %v", err)
			}
			_, err = orch.GetEntry(ctx, tt.targetURI)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErrSub)
			}
			if !strings.Contains(err.Error(), tt.wantErrSub) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErrSub)
			}
		})
	}
}
