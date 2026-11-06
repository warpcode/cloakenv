package engine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/engine"
)

func TestEnvVault_StandardPipelineAndDecorators(t *testing.T) {
	t.Setenv("CLOAKENV_ENVTEST_SECRET_API_KEY", "sk-12345")
	t.Setenv("CLOAKENV_ENVTEST_SECRET_DB_PASS", "pass-67890")
	t.Setenv("CLOAKENV_ENVTEST_IGNORE_ME", "ignored")

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"env": {
				Provider:      "env",
				IncludeFields: []string{"CLOAKENV_ENVTEST_SECRET_API_KEY", "vault", "path", "provider"},
				ExcludeFields: []string{"CLOAKENV_ENVTEST_SECRET_DB_PASS"},
				KeyFormat:     "snake",
				Attributes: map[string]any{
					"custom_attr": "computed-value",
				},
			},
		},
	}

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	ctx := context.Background()

	t.Run("GetEntryFull", func(t *testing.T) {
		entry, err := orch.GetEntry(ctx, "env://")
		if err != nil {
			t.Fatalf("GetEntry('env://') failed: %v", err)
		}

		// API_KEY formatted to snake: cloakenv_envtest_secret_api_key
		if val, ok := entry.Attributes["cloakenv_envtest_secret_api_key"]; !ok || val != "sk-12345" {
			t.Errorf("expected attribute 'cloakenv_envtest_secret_api_key' to be 'sk-12345', got %v", val)
		}

		// DB_PASS was excluded
		if _, ok := entry.Attributes["cloakenv_envtest_secret_db_pass"]; ok {
			t.Error("expected 'cloakenv_envtest_secret_db_pass' to be excluded")
		}

		// IGNORE_ME was not in include_fields
		if _, ok := entry.Attributes["cloakenv_envtest_ignore_me"]; ok {
			t.Error("expected 'cloakenv_envtest_ignore_me' to be excluded by include_fields")
		}

		// custom_attr injected by AttributesProvider
		if val, ok := entry.Attributes["custom_attr"]; !ok || val != "computed-value" {
			t.Errorf("expected 'custom_attr' = 'computed-value', got %v", val)
		}
	})

	t.Run("GetSecretSingleVariable", func(t *testing.T) {
		// Single variable lookup with raw variable name
		val, err := orch.Resolve(ctx, `{{ get "env://CLOAKENV_ENVTEST_SECRET_API_KEY" }}`)
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if val != "sk-12345" {
			t.Errorf("expected 'sk-12345', got %q", val)
		}
	})

	t.Run("SearchScopedEnvVault", func(t *testing.T) {
		results, err := orch.Search(ctx, `cloakenv_envtest_secret_api_key == "sk-12345"`, []string{"env"})
		if err != nil {
			t.Fatalf("Search with --vault env failed: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		res := results[0]
		if res.Vault != "env" {
			t.Errorf("expected vault 'env', got %q", res.Vault)
		}
		if val, ok := res.Entry.Attributes["cloakenv_envtest_secret_api_key"]; !ok || val != "sk-12345" {
			t.Errorf("expected 'cloakenv_envtest_secret_api_key' in search result, got %v", val)
		}
	})
}

func TestEnvVault_NamedInstanceAndSearchScoping(t *testing.T) {
	t.Setenv("CLOAKENV_NAMED_FOO", "foo_val")

	searchableTrue := true
	searchableFalse := false

	t.Run("UnscopedSearchExcludesEnvByDefault", func(t *testing.T) {
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"custom_static": {
					Provider: "custom_vault",
					Entities: config.ToCustomEntityMap(map[string]map[string]any{
						"entry1": {"key": "val1"},
					}),
				},
			},
		}

		orch, err := engine.NewOrchestrator(cfg)
		if err != nil {
			t.Fatalf("NewOrchestrator failed: %v", err)
		}

		ctx := context.Background()

		// Unscoped global search
		results, err := orch.Search(ctx, "", nil)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		for _, r := range results {
			if r.Vault == "env" || r.Provider == "env" {
				t.Errorf("unscoped search unexpectedly contained env vault: %+v", r)
			}
		}

		// Explicit --vault env search
		resultsScoped, err := orch.Search(ctx, "", []string{"env"})
		if err != nil {
			t.Fatalf("Search with --vault env failed: %v", err)
		}
		if len(resultsScoped) != 1 {
			t.Fatalf("expected 1 result from --vault env, got %d", len(resultsScoped))
		}
		if resultsScoped[0].Path != "env" {
			t.Errorf("expected Path 'env', got %q", resultsScoped[0].Path)
		}
	})

	t.Run("UnscopedSearchIncludesEnvWhenSearchableTrue", func(t *testing.T) {
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"env": {
					Provider:   "env",
					Searchable: &searchableTrue,
				},
			},
		}

		orch, err := engine.NewOrchestrator(cfg)
		if err != nil {
			t.Fatalf("NewOrchestrator failed: %v", err)
		}

		ctx := context.Background()

		results, err := orch.Search(ctx, "", nil)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		found := false
		for _, r := range results {
			if r.Vault == "env" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected unscoped search to include env when searchable: true")
		}
	})

	t.Run("CustomNamedEnvVault", func(t *testing.T) {
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"sys_env": {
					Provider: "env",
					Mapping: []config.MappingRule{
						{
							Match: "^CLOAKENV_NAMED_(.*)$",
							Key:   "RENAMED_$1",
						},
					},
				},
				"hidden_env": {
					Provider:   "env",
					Searchable: &searchableFalse,
				},
			},
		}

		orch, err := engine.NewOrchestrator(cfg)
		if err != nil {
			t.Fatalf("NewOrchestrator failed: %v", err)
		}

		ctx := context.Background()

		// sys_env is included in unscoped search by default (like any custom vault)
		results, err := orch.Search(ctx, "", nil)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		foundSys := false
		for _, r := range results {
			if r.Vault == "sys_env" {
				foundSys = true
				if val, ok := r.Entry.Attributes["RENAMED_FOO"]; !ok || val != "foo_val" {
					t.Errorf("expected RENAMED_FOO = 'foo_val' in sys_env result, got %v", val)
				}
			}
			if r.Vault == "hidden_env" {
				t.Error("hidden_env should not be in unscoped search results")
			}
		}
		if !foundSys {
			t.Error("expected sys_env to be found in unscoped search")
		}

		// GetEntry on custom named vault
		entry, err := orch.GetEntry(ctx, "sys_env://")
		if err != nil {
			t.Fatalf("GetEntry('sys_env://') failed: %v", err)
		}
		if val, ok := entry.Attributes["RENAMED_FOO"]; !ok || val != "foo_val" {
			t.Errorf("expected RENAMED_FOO = 'foo_val', got %v", val)
		}

		// Scoped search on hidden_env should return error
		_, err = orch.Search(ctx, "", []string{"hidden_env"})
		if err == nil || !strings.Contains(err.Error(), "is not searchable") {
			t.Errorf("expected 'is not searchable' error, got %v", err)
		}
	})
}

func TestEnvVault_MergeAndBuildEnv(t *testing.T) {
	t.Setenv("CLOAKENV_MERGE_TEST_VAR1", "v1")
	t.Setenv("CLOAKENV_MERGE_TEST_VAR2", "v2")

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"env": {
				Provider:      "env",
				IncludeFields: []string{"CLOAKENV_MERGE_TEST_*"},
			},
		},
	}

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("NewOrchestrator failed: %v", err)
	}

	ctx := context.Background()

	envVars, err := orch.BuildEnv(ctx, engine.EnvConfig{
		Merges: []string{"env://"},
	})
	if err != nil {
		t.Fatalf("BuildEnv failed: %v", err)
	}

	envMap := make(map[string]string)
	for _, kv := range envVars {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	if envMap["CLOAKENV_MERGE_TEST_VAR1"] != "v1" {
		t.Errorf("expected CLOAKENV_MERGE_TEST_VAR1='v1', got %q", envMap["CLOAKENV_MERGE_TEST_VAR1"])
	}
	if envMap["CLOAKENV_MERGE_TEST_VAR2"] != "v2" {
		t.Errorf("expected CLOAKENV_MERGE_TEST_VAR2='v2', got %q", envMap["CLOAKENV_MERGE_TEST_VAR2"])
	}
}
