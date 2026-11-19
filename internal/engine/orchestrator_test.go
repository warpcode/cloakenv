package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
		Templates: map[string]config.TemplateConfig{
			"deploy_cfg": {
				Driver: "json",
				Vaults: []config.VaultSourceConfig{
					{
						Vault: "test_vault",
						Query: `{{ hasTag "prod" .tags }}`,
						Rules: []config.RuleConfig{{Type: "pass"}},
					},
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
	cfg.CompileAutoloadRules()

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

	t.Run("EnvsBuilder and TemplatesRenderer", func(t *testing.T) {
		envMap, err := orch.EnvsBuilder().Build(ctx, "deploy_env")
		if err != nil {
			t.Fatalf("EnvsBuilder.Build failed: %v", err)
		}
		if envMap["API_KEY"] != "secret-api-key" {
			t.Errorf("API_KEY = %q, want %q", envMap["API_KEY"], "secret-api-key")
		}

		rendered, err := orch.TemplatesRenderer().Render(ctx, "deploy_cfg")
		if err != nil {
			t.Fatalf("TemplatesRenderer.Render failed: %v", err)
		}
		if !strings.Contains(rendered, "secret-api-key") {
			t.Errorf("expected rendered JSON to contain secret, got %q", rendered)
		}
	})

	t.Run("BuildEnvForCommand", func(t *testing.T) {
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

	t.Run("ClearCache and PruneCache", func(t *testing.T) {
		if err := orch.ClearCache(ctx); err != nil {
			t.Fatalf("ClearCache failed: %v", err)
		}
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

func TestOrchestrator_AuditLoggingZeroLeak(t *testing.T) {
	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	t.Setenv("CLOAKENV_AUDIT_SECRET", "super-secret-audit-value-999")

	cfg := &config.Config{
		Audit: config.AuditConfig{
			Enabled: true,
			LogPath: auditPath,
			Format:  "json",
		},
		Envs: map[string][]config.VaultSourceConfig{
			"audit_env": {
				{
					Vault: "env",
					Rules: []config.RuleConfig{{Type: "pass", Include: []string{"CLOAKENV_AUDIT_SECRET"}}},
				},
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("NewOrchestrator failed: %v", err)
	}
	if _, err := orch.EnvsBuilder().Build(context.Background(), "audit_env"); err != nil {
		t.Fatalf("EnvsBuilder.Build failed: %v", err)
	}
	if err := orch.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("failed to read audit log: %v", err)
	}
	logContent := string(data)
	if !strings.Contains(logContent, "env://") || !strings.Contains(logContent, "SUCCESS") {
		t.Errorf("expected audit entry for env:// SUCCESS, got:\n%s", logContent)
	}
	if strings.Contains(logContent, "super-secret-audit-value-999") {
		t.Fatalf("SECURITY LEAK: audit log contained plaintext secret:\n%s", logContent)
	}
}
