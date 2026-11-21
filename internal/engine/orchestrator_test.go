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
func (f *failInitProvider) Validate(_ map[string]string) error                    { return nil }
func (f *failInitProvider) Search(_ context.Context, _ provider.SearchQuery) ([]provider.Entry, error) {
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
			"test_vault": {
				Provider: "env",
			},
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
	orch.providerManager.vaultCache["test_vault"] = newTestMemoryProvider(map[string]provider.Entry{
		"service_a": {
			"title":   "Service A",
			"path":    "service_a",
			"tags":    []string{"prod", "api"},
			"API_KEY": "secret-api-key",
		},
	})

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
		if entry["API_KEY"] != "secret-api-key" {
			t.Errorf("GetEntry() API_KEY = %v, want %q", entry["API_KEY"], "secret-api-key")
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

func TestOrchestrator_GetEntries(t *testing.T) {
	keyring.MockInit()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())

	ctx := context.Background()

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"test_vault": {
				Provider: "env",
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}
	orch.providerManager.vaultCache["test_vault"] = newTestMemoryProvider(map[string]provider.Entry{
		"flat_entry": {
			"title": "Flat Entry",
			"path":  "flat_entry",
			"tags":  []string{"tag1"},
			"KEY1":  "val1",
		},
	})

	entries, err := orch.GetEntries(ctx, "test_vault://flat_entry")
	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}
	if len(entries) != 1 || entries[0].Title() != "Flat Entry" || entries[0]["KEY1"] != "val1" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}
