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

func TestSearchProviderIntegration_MappingWithExcludeAllFields(t *testing.T) {
	ctx := context.Background()

	rule := config.MappingRule{Match: "^env:(.*)$", Key: "$1"}
	_ = rule.Compile()

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"kp": {
				Provider: "custom_vault",
				Entities: map[string]map[string]any{
					"my_entry": {
						"title":       "My Entry",
						"tags":        []string{"dotenv:jules"},
						"env:API_KEY": "sk-jules-secret",
						"OTHER":       "unmapped_val",
					},
				},
			},
			"jules": {
				Provider:      "search",
				SourceVaults:  []string{"kp"},
				Query:         `"dotenv:jules" in tags`,
				ExcludeFields: []string{"*"},
				Mapping:       []config.MappingRule{rule},
			},
		},
	}

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	// 1. GetSecret on mapped key
	val, err := orch.Resolve(ctx, "jules://my_entry:API_KEY")
	if err != nil {
		t.Fatalf("unexpected error resolving secret: %v", err)
	}
	if val != "sk-jules-secret" {
		t.Errorf("expected 'sk-jules-secret', got %q", val)
	}

	// 2. GetEntry on search vault
	entry, err := orch.GetEntry(ctx, "jules://my_entry")
	if err != nil {
		t.Fatalf("unexpected error getting entry: %v", err)
	}
	if entry.Attributes["API_KEY"] != "sk-jules-secret" {
		t.Errorf("expected API_KEY in entry attributes, got %v", entry.Attributes["API_KEY"])
	}
	if _, ok := entry.Attributes["OTHER"]; ok {
		t.Errorf("expected OTHER to be excluded, but was present")
	}
	if entry.Tags != nil {
		t.Errorf("expected unmapped tags to be excluded, got %v", entry.Tags)
	}
	if entry.Title != "" {
		t.Errorf("expected unmapped title to be excluded, got %q", entry.Title)
	}

	// 3. Unmapped field should fail
	_, errOther := orch.Resolve(ctx, "jules://my_entry:OTHER")
	if errOther == nil {
		t.Errorf("expected error resolving unmapped OTHER field, got nil")
	}

	// 4. Search on virtual vault
	results, errSearch := orch.Search(ctx, `API_KEY == "sk-jules-secret"`, []string{"jules"})
	if errSearch != nil {
		t.Fatalf("unexpected error searching jules: %v", errSearch)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(results))
	}
	if results[0].Entry.Attributes["API_KEY"] != "sk-jules-secret" {
		t.Errorf("expected API_KEY in search result, got %v", results[0].Entry.Attributes["API_KEY"])
	}
	if results[0].Provider != "" {
		t.Errorf("expected Provider to be excluded by exclude_fields: ['*'], got %q", results[0].Provider)
	}
	if results[0].Vault != "" {
		t.Errorf("expected Vault to be excluded by exclude_fields: ['*'], got %q", results[0].Vault)
	}
	if results[0].Path != "" {
		t.Errorf("expected Path to be excluded by exclude_fields: ['*'], got %q", results[0].Path)
	}
}

func TestSearchProviderIntegration_IncludeAboveExclude(t *testing.T) {
	ctx := context.Background()

	rule := config.MappingRule{Match: "^env:(.*)$", Key: "$1"}
	_ = rule.Compile()

	// The exact vault configuration requested:
	//   jules:
	//     provider: "search"
	//     source_vaults: ["kp"]
	//     query: '"dotenv:jules" in tags'
	//     exclude_fields: ['*']
	//     include_fields: ['title']
	//     mapping:
	//       - match: "^env:(.*)$"
	//         key: "$1"
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"kp": {
				Provider: "custom_vault",
				Entities: map[string]map[string]any{
					"my_entry": {
						"title":       "My Entry",
						"tags":        []string{"dotenv:jules"},
						"env:API_KEY": "sk-jules-secret",
						"OTHER":       "unmapped_val",
					},
				},
			},
			"jules": {
				Provider:      "search",
				SourceVaults:  []string{"kp"},
				Query:         `"dotenv:jules" in tags`,
				ExcludeFields: []string{"*"},
				IncludeFields: []string{"title"},
				Mapping:       []config.MappingRule{rule},
			},
		},
	}

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	// 1. GetSecret on mapped key
	val, err := orch.Resolve(ctx, "jules://my_entry:API_KEY")
	if err != nil {
		t.Fatalf("unexpected error resolving secret: %v", err)
	}
	if val != "sk-jules-secret" {
		t.Errorf("expected 'sk-jules-secret', got %q", val)
	}

	// 2. GetEntry on search vault: title MUST be present because include is above exclude
	entry, err := orch.GetEntry(ctx, "jules://my_entry")
	if err != nil {
		t.Fatalf("unexpected error getting entry: %v", err)
	}
	if entry.Attributes["API_KEY"] != "sk-jules-secret" {
		t.Errorf("expected API_KEY in entry attributes, got %v", entry.Attributes["API_KEY"])
	}
	if entry.Title != "My Entry" {
		t.Errorf("expected title 'My Entry' because include_fields has 'title', got %q", entry.Title)
	}
	if _, ok := entry.Attributes["OTHER"]; ok {
		t.Errorf("expected OTHER to be excluded, but was present")
	}
	if entry.Tags != nil {
		t.Errorf("expected tags to be excluded (not in include_fields), got %v", entry.Tags)
	}

	// 3. Search on virtual vault
	results, errSearch := orch.Search(ctx, `API_KEY == "sk-jules-secret"`, []string{"jules"})
	if errSearch != nil {
		t.Fatalf("unexpected error searching jules: %v", errSearch)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(results))
	}
	if results[0].Entry.Attributes["API_KEY"] != "sk-jules-secret" {
		t.Errorf("expected API_KEY in search result, got %v", results[0].Entry.Attributes["API_KEY"])
	}
	if results[0].Entry.Title != "My Entry" {
		t.Errorf("expected Title 'My Entry' in search result, got %q", results[0].Entry.Title)
	}
	if results[0].Entry.Tags != nil {
		t.Errorf("expected Tags to be excluded in search result, got %v", results[0].Entry.Tags)
	}
	if results[0].Provider != "" {
		t.Errorf("expected Provider to be excluded by exclude_fields: ['*'], got %q", results[0].Provider)
	}
	if results[0].Vault != "" {
		t.Errorf("expected Vault to be excluded by exclude_fields: ['*'], got %q", results[0].Vault)
	}
	if results[0].Path != "" {
		t.Errorf("expected Path to be excluded by exclude_fields: ['*'], got %q", results[0].Path)
	}
}

func TestSearchProviderIntegration_MappingOnlyKeepsDefaultFields(t *testing.T) {
	ctx := context.Background()

	rule := config.MappingRule{
		Match: "^env:(.*)$",
		Key:   "$1",
	}

	// Vault config with mapping ONLY (no include_fields, no exclude_fields)
	// Default fields (title, tags, path, vault, provider) MUST SHOW!
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"kp": {
				Provider: "custom_vault",
				Entities: map[string]map[string]any{
					"my_entry": {
						"title":       "My Entry",
						"tags":        []string{"dotenv:jules"},
						"env:API_KEY": "sk-jules-secret",
						"OTHER":       "unmapped_val",
					},
				},
			},
			"jules": {
				Provider:     "search",
				SourceVaults: []string{"kp"},
				Query:        `"dotenv:jules" in tags`,
				Mapping:      []config.MappingRule{rule},
			},
		},
	}

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	// 1. GetSecret on mapped key
	val, err := orch.Resolve(ctx, "jules://my_entry:API_KEY")
	if err != nil {
		t.Fatalf("unexpected error resolving secret: %v", err)
	}
	if val != "sk-jules-secret" {
		t.Errorf("expected 'sk-jules-secret', got %q", val)
	}

	// 2. GetEntry on search vault: default fields MUST be present
	entry, err := orch.GetEntry(ctx, "jules://my_entry")
	if err != nil {
		t.Fatalf("unexpected error getting entry: %v", err)
	}
	if entry.Attributes["API_KEY"] != "sk-jules-secret" {
		t.Errorf("expected API_KEY in entry attributes, got %v", entry.Attributes["API_KEY"])
	}
	if entry.Title != "My Entry" {
		t.Errorf("expected Title 'My Entry', got %q", entry.Title)
	}
	if len(entry.Tags) != 1 || entry.Tags[0] != "dotenv:jules" {
		t.Errorf("expected tags ['dotenv:jules'], got %v", entry.Tags)
	}
	if entry.Attributes["OTHER"] != "unmapped_val" {
		t.Errorf("expected unmapped OTHER to be present, got %v", entry.Attributes["OTHER"])
	}

	// 3. Search on virtual vault: all default fields must be preserved
	results, errSearch := orch.Search(ctx, `API_KEY == "sk-jules-secret"`, []string{"jules"})
	if errSearch != nil {
		t.Fatalf("unexpected error searching jules: %v", errSearch)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(results))
	}
	res := results[0]
	if res.Entry.Attributes["API_KEY"] != "sk-jules-secret" {
		t.Errorf("expected API_KEY in search result, got %v", res.Entry.Attributes["API_KEY"])
	}
	if res.Entry.Title != "My Entry" {
		t.Errorf("expected Title 'My Entry', got %q", res.Entry.Title)
	}
	if len(res.Entry.Tags) != 1 || res.Entry.Tags[0] != "dotenv:jules" {
		t.Errorf("expected Tags ['dotenv:jules'], got %v", res.Entry.Tags)
	}
	if res.Provider != "search" {
		t.Errorf("expected Provider 'search', got %q", res.Provider)
	}
	if res.Vault != "jules" {
		t.Errorf("expected Vault 'jules', got %q", res.Vault)
	}
	if res.Path != "my_entry" {
		t.Errorf("expected Path 'my_entry', got %q", res.Path)
	}
}
