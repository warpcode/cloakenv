package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
)

func TestImplicitBuiltinEnvVault_EnvsAndTemplates(t *testing.T) {
	t.Setenv("CLOAKENV_IMPLICIT_API_KEY", "sk-implicit-env-secret")
	t.Setenv("CLOAKENV_IMPLICIT_REGION", "us-west-2")

	// No vaults: section declared at all — built-in "env" vault resolves implicitly.
	cfg := &config.Config{
		Envs: map[string][]config.VaultSourceConfig{
			"app_env": {
				{
					Vault: "env",
					Rules: []config.RuleConfig{
						{
							Type:  "match",
							Match: `^CLOAKENV_IMPLICIT_(.+)$`,
							Key:   "$1",
						},
					},
				},
			},
		},
		Templates: map[string]config.TemplateConfig{
			"app_cfg": {
				Driver:   "template",
				Template: `key={{ (index .entries 0).API_KEY }} region={{ (index .entries 0).REGION }}`,
				Vaults: []config.VaultSourceConfig{
					{
						Vault: "env",
						Rules: []config.RuleConfig{
							{
								Type:  "match",
								Match: `^CLOAKENV_IMPLICIT_(.+)$`,
								Key:   "$1",
							},
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

	ctx := context.Background()

	eb := NewEnvsBuilder(cfg, orch.providerManager, NewRuleEngine())
	envRes, err := eb.Build(ctx, "app_env")
	if err != nil {
		t.Fatalf("EnvsBuilder.Build failed: %v", err)
	}
	if envRes["API_KEY"] != "sk-implicit-env-secret" || envRes["REGION"] != "us-west-2" {
		t.Fatalf("unexpected env map: %+v", envRes)
	}

	tplRes, err := NewTemplatesRenderer(cfg, orch.providerManager, NewRuleEngine(), eb).Render(ctx, "app_cfg")
	if err != nil {
		t.Fatalf("TemplatesRenderer.Render failed: %v", err)
	}
	if tplRes != "key=sk-implicit-env-secret region=us-west-2" {
		t.Fatalf("unexpected template output: %q", tplRes)
	}
}

func TestImplicitBuiltinKeyringVault_EnvsAndTemplates(t *testing.T) {
	mockStore := provider.NewMockKeyringStore()
	_ = mockStore.Set("cloakenv", "api_token", "kr-secret-token-42")
	_ = mockStore.Set("cloakenv", "db_pass", "kr-secret-pass-99")
	// Internal keyring keys must be ignored by Search()
	_ = mockStore.Set("cloakenv", "cache_token", "internal-cache-token")
	_ = mockStore.Set("cloakenv", "cache-master-key", "internal-master")
	_ = mockStore.Set("cloakenv", "provider/work", "internal-vault-pw")

	// No vaults: section declared — built-in "keyring" vault resolves implicitly.
	cfg := &config.Config{
		Envs: map[string][]config.VaultSourceConfig{
			"kr_env": {
				{
					Vault: "keyring",
					Rules: []config.RuleConfig{{Type: "pass"}},
				},
			},
		},
		Templates: map[string]config.TemplateConfig{
			"kr_cfg": {
				Driver: "dotenv",
				Vaults: []config.VaultSourceConfig{
					{
						Vault: "keyring",
						Rules: []config.RuleConfig{{Type: "pass"}},
					},
				},
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("NewOrchestrator failed: %v", err)
	}
	// Replace built-in keyring store with an enumerable mock store.
	krProv := provider.NewOSKeyringProviderWithStore(mockStore)
	_ = krProv.Initialize(context.Background(), provider.ProviderConfig{
		Settings: map[string]string{"keyring_prefix": cfg.KeyringPrefix()},
	})
	orch.providerManager.builtins["keyring"] = krProv
	orch.providerManager.initializedBuiltins["keyring"] = true

	ctx := context.Background()

	eb := NewEnvsBuilder(cfg, orch.providerManager, NewRuleEngine())
	envRes, err := eb.Build(ctx, "kr_env")
	if err != nil {
		t.Fatalf("EnvsBuilder.Build(keyring) failed: %v", err)
	}
	if envRes["API_TOKEN"] != "kr-secret-token-42" || envRes["DB_PASS"] != "kr-secret-pass-99" {
		t.Fatalf("unexpected keyring env map: %+v", envRes)
	}
	if _, leaked := envRes["CACHE_MASTER_KEY"]; leaked {
		t.Fatal("internal cache-master-key leaked into env")
	}

	tplRes, err := NewTemplatesRenderer(cfg, orch.providerManager, NewRuleEngine(), eb).Render(ctx, "kr_cfg")
	if err != nil {
		t.Fatalf("TemplatesRenderer.Render(keyring) failed: %v", err)
	}
	if !strings.Contains(tplRes, `API_TOKEN="kr-secret-token-42"`) || !strings.Contains(tplRes, `DB_PASS="kr-secret-pass-99"`) {
		t.Fatalf("unexpected keyring dotenv output:\n%s", tplRes)
	}
	if strings.Contains(tplRes, "internal-master") || strings.Contains(tplRes, "internal-vault-pw") {
		t.Fatalf("internal keyring keys leaked into template output:\n%s", tplRes)
	}
}

func TestEnvVault_ReadOnlyDirectResolution(t *testing.T) {
	t.Setenv("CLOAKENV_ENVTEST_SECRET_API_KEY", "sk-12345")

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"env": {
				Provider: "env",
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	ctx := context.Background()

	val, err := orch.Resolve(ctx, `{{ get "env://CLOAKENV_ENVTEST_SECRET_API_KEY" }}`)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if val != "sk-12345" {
		t.Errorf("expected 'sk-12345', got %q", val)
	}

	entry, err := orch.GetEntry(ctx, "env://")
	if err != nil {
		t.Fatalf("GetEntry('env://') failed: %v", err)
	}
	if entry["CLOAKENV_ENVTEST_SECRET_API_KEY"] != "sk-12345" {
		t.Errorf("expected raw flat key CLOAKENV_ENVTEST_SECRET_API_KEY='sk-12345', got %v", entry["CLOAKENV_ENVTEST_SECRET_API_KEY"])
	}
}
