package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
)

func TestSearchVaultProviderIntegration(t *testing.T) {
	ctx := context.Background()

	isTrue := true

	// 1. Validation error when searchable is specified for search vault
	t.Run("RejectsSearchableFlag", func(t *testing.T) {
		invalidCfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"openrouter": {
					Provider:     "search",
					SourceVaults: []string{"work"},
					Query:        `"service:openrouter" in tags`,
					Searchable:   &isTrue,
				},
			},
		}

		_, err := NewOrchestrator(invalidCfg)
		if err == nil {
			t.Fatal("expected NewOrchestrator to fail when searchable is provided on search vault")
		}
		if !strings.Contains(err.Error(), "search provider does not support the searchable flag") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	// 2. Validation error when source_vaults or query is missing
	t.Run("RejectsMissingSourceVaultsOrQuery", func(t *testing.T) {
		noSourceCfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"openrouter": {
					Provider: "search",
					Query:    `"service:openrouter" in tags`,
				},
			},
		}
		_, err := NewOrchestrator(noSourceCfg)
		if err == nil {
			t.Fatal("expected NewOrchestrator to fail when source_vaults is missing")
		}
		if !strings.Contains(err.Error(), "search provider requires source_vaults") {
			t.Errorf("unexpected error message: %v", err)
		}

		noQueryCfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"openrouter": {
					Provider:     "search",
					SourceVaults: []string{"work"},
				},
			},
		}
		_, err = NewOrchestrator(noQueryCfg)
		if err == nil {
			t.Fatal("expected NewOrchestrator to fail when query is missing")
		}
		if !strings.Contains(err.Error(), "search provider requires query") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	validCfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"work": {
				Provider: "custom_vault",
				Entities: map[string]map[string]any{
					"openrouter_key": {
						"Title":    "OpenRouter Production Key",
						"tags":     []string{"service:openrouter", "env:prod"},
						"api_key":  "sk-or-v1-openrouter-secret",
						"Password": "sk-or-v1-openrouter-secret",
					},
					"openai_key": {
						"Title":    "OpenAI Secret Key",
						"tags":     []string{"service:openai", "env:prod"},
						"api_key":  "sk-proj-openai-secret",
						"Password": "sk-proj-openai-secret",
					},
				},
			},
			"openrouter": {
				Provider:     "search",
				SourceVaults: []string{"work"},
				Query:        `"service:openrouter" in tags`,
			},
		},
	}

	orch, err := NewOrchestrator(validCfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	// 3. Resolve secret from search vault
	t.Run("ResolveFromSearchVault", func(t *testing.T) {
		val, err := orch.Resolve(ctx, "${openrouter://openrouter_key:api_key}")
		if err != nil {
			t.Fatalf("failed to resolve secret from search vault: %v", err)
		}
		if val != "sk-or-v1-openrouter-secret" {
			t.Errorf("expected 'sk-or-v1-openrouter-secret', got %q", val)
		}

		// Attempting to resolve entry not matched by search vault query
		_, err = orch.Resolve(ctx, "${openrouter://openai_key:api_key}")
		if err == nil {
			t.Fatal("expected error resolving openai_key from openrouter search vault, got nil")
		}
	})

	// 4. GetEntry from search vault
	t.Run("GetEntryFromSearchVault", func(t *testing.T) {
		entry, err := orch.GetEntry(ctx, "openrouter://openrouter_key")
		if err != nil {
			t.Fatalf("failed to GetEntry from search vault: %v", err)
		}
		if entry.Title != "OpenRouter Production Key" {
			t.Errorf("expected title 'OpenRouter Production Key', got %q", entry.Title)
		}
	})

	// 5. Scoped search on search vault
	t.Run("ScopedSearchOnSearchVault", func(t *testing.T) {
		results, err := orch.Search(ctx, `"env:prod" in tags`, []string{"openrouter"})
		if err != nil {
			t.Fatalf("Search on scoped search vault failed: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0].Vault != "openrouter" || results[0].Path != "openrouter_key" {
			t.Errorf("unexpected search result: %+v", results[0])
		}
	})

	// 6. Global search excludes search vault by default (to avoid duplicates or unintended recursive queries)
	t.Run("GlobalSearchExcludesSearchVault", func(t *testing.T) {
		results, err := orch.Search(ctx, `"env:prod" in tags`, nil)
		if err != nil {
			t.Fatalf("Global search failed: %v", err)
		}
		// Expect 2 entries from "work" vault ("openrouter_key" and "openai_key"), none from "openrouter" search vault
		for _, r := range results {
			if r.Vault == "openrouter" {
				t.Errorf("global search should exclude search vault, but found result from vault %q", r.Vault)
			}
		}
	})
}
