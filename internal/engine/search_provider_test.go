package engine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/engine"
)

func TestSearchProviderIntegration_EndToEnd(t *testing.T) {
	ctx := context.Background()

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"work": {
				Provider: "custom_vault",
				Entities: map[string]map[string]any{
					"openrouter_key": {
						"title":   "OpenRouter Key",
						"tags":    []string{"service:openrouter", "prod"},
						"api_key": "sk-or-test-secret-value",
					},
					"github_token": {
						"title": "GitHub Token",
						"tags":  []string{"service:github"},
						"token": "ghp_test123",
					},
				},
			},
			"openrouter_vault": {
				Provider:     "search",
				SourceVaults: []string{"work"},
				Query:        `"service:openrouter" in tags`,
			},
		},
	}

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	t.Run("Resolve secret using path:attr syntax", func(t *testing.T) {
		val, err := orch.Resolve(ctx, "openrouter_vault://openrouter_key:api_key")
		if err != nil {
			t.Fatalf("unexpected error resolving secret: %v", err)
		}
		if val != "sk-or-test-secret-value" {
			t.Errorf("expected 'sk-or-test-secret-value', got %q", val)
		}
	})

	t.Run("GetEntry retrieves structured entry", func(t *testing.T) {
		entry, err := orch.GetEntry(ctx, "openrouter_vault://openrouter_key")
		if err != nil {
			t.Fatalf("unexpected error getting entry: %v", err)
		}
		if entry.Title != "OpenRouter Key" {
			t.Errorf("expected title 'OpenRouter Key', got %q", entry.Title)
		}
		if entry.Attributes["api_key"] != "sk-or-test-secret-value" {
			t.Errorf("expected api_key 'sk-or-test-secret-value', got %v", entry.Attributes["api_key"])
		}
	})

	t.Run("Global un-scoped search excludes search vault", func(t *testing.T) {
		results, err := orch.Search(ctx, "", nil)
		if err != nil {
			t.Fatalf("unexpected search error: %v", err)
		}
		for _, r := range results {
			if r.Vault == "openrouter_vault" {
				t.Errorf("un-scoped global search should exclude search vaults, but found vault %q", r.Vault)
			}
		}
	})

	t.Run("Scoped search targeting search vault explicitly", func(t *testing.T) {
		results, err := orch.Search(ctx, "", []string{"openrouter_vault"})
		if err != nil {
			t.Fatalf("unexpected search error: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result from openrouter_vault, got %d", len(results))
		}
		if results[0].Vault != "openrouter_vault" {
			t.Errorf("expected Vault 'openrouter_vault', got %q", results[0].Vault)
		}
	})
}

func TestSearchProviderIntegration_RejectsSearchableFlag(t *testing.T) {
	searchableTrue := true

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"openrouter": {
				Provider:     "search",
				SourceVaults: []string{"work"},
				Query:        `"service:openrouter" in tags`,
				Searchable:   &searchableTrue,
			},
		},
	}

	_, err := engine.NewOrchestrator(cfg)
	if err == nil {
		t.Fatal("expected NewOrchestrator to fail when searchable flag is configured on search provider")
	}
	if !strings.Contains(err.Error(), "search provider does not support the searchable flag") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSearchProviderIntegration_MultiSourceVaults(t *testing.T) {
	ctx := context.Background()

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"vault_a": {
				Provider: "custom_vault",
				Entities: map[string]map[string]any{
					"key_a": {
						"title":    "Key A",
						"tags":     []string{"service:openrouter"},
						"Password": "pass_a",
					},
				},
			},
			"vault_b": {
				Provider: "custom_vault",
				Entities: map[string]map[string]any{
					"key_b": {
						"title":    "Key B",
						"tags":     []string{"service:openrouter"},
						"Password": "pass_b",
					},
				},
			},
			"combined": {
				Provider:     "search",
				SourceVaults: []string{"vault_a", "vault_b"},
				Query:        `"service:openrouter" in tags`,
			},
		},
	}

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	valA, err := orch.Resolve(ctx, "combined://key_a:Password")
	if err != nil {
		t.Fatalf("unexpected error resolving key_a: %v", err)
	}
	if valA != "pass_a" {
		t.Errorf("expected 'pass_a', got %q", valA)
	}

	valB, err := orch.Resolve(ctx, "combined://key_b:Password")
	if err != nil {
		t.Fatalf("unexpected error resolving key_b: %v", err)
	}
	if valB != "pass_b" {
		t.Errorf("expected 'pass_b', got %q", valB)
	}
}

func TestSearchProviderIntegration_SourceVaultValidation(t *testing.T) {
	t.Run("empty source_vaults rejected", func(t *testing.T) {
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"search_vault": {
					Provider:     "search",
					SourceVaults: []string{},
					Query:        "true",
				},
			},
		}
		_, err := engine.NewOrchestrator(cfg)
		if err == nil || !strings.Contains(err.Error(), "search provider requires at least one source vault") {
			t.Errorf("expected error requiring at least one source vault, got: %v", err)
		}
	})

	t.Run("self-reference in source_vaults rejected", func(t *testing.T) {
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"search_vault": {
					Provider:     "search",
					SourceVaults: []string{"search_vault"},
					Query:        "true",
				},
			},
		}
		_, err := engine.NewOrchestrator(cfg)
		if err == nil || !strings.Contains(err.Error(), "search provider cannot reference itself in source_vaults") {
			t.Errorf("expected self-reference error, got: %v", err)
		}
	})

	t.Run("nonexistent source vault rejected", func(t *testing.T) {
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"search_vault": {
					Provider:     "search",
					SourceVaults: []string{"nonexistent_vault"},
					Query:        "true",
				},
			},
		}
		_, err := engine.NewOrchestrator(cfg)
		if err == nil || !strings.Contains(err.Error(), "source vault \"nonexistent_vault\" does not exist") {
			t.Errorf("expected nonexistent vault error, got: %v", err)
		}
	})
}

func TestSearchProviderIntegration_CyclicDependencyDetection(t *testing.T) {
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"search_a": {
				Provider:     "search",
				SourceVaults: []string{"search_b"},
				Query:        "true",
			},
			"search_b": {
				Provider:     "search",
				SourceVaults: []string{"search_a"},
				Query:        "true",
			},
		},
	}
	_, err := engine.NewOrchestrator(cfg)
	if err == nil || !strings.Contains(err.Error(), "cyclic dependency detected in search vaults") {
		t.Errorf("expected cyclic dependency error, got: %v", err)
	}
}
