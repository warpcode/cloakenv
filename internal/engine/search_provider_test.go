package engine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/engine"
	"github.com/warpcode/cloakenv/internal/provider"
)

func TestSearchProviderIntegration_EndToEnd(t *testing.T) {
	ctx := context.Background()

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"work": {
				Provider: "env",
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
	if err := orch.InjectMockVaultForTest("work", []provider.SearchResult{
		{
			Provider: "search",
			Vault:    "work",
			Path:     "openrouter_key",
			Entry: provider.Entry{
				Title:      "OpenRouter Key",
				Tags:       []string{"service:openrouter", "prod"},
				Attributes: map[string]any{"api_key": "sk-or-test-secret-value"},
			},
		},
		{
			Provider: "search",
			Vault:    "work",
			Path:     "github_token",
			Entry: provider.Entry{
				Title:      "GitHub Token",
				Tags:       []string{"service:github"},
				Attributes: map[string]any{"token": "ghp_test123"},
			},
		},
	}); err != nil {
		t.Fatalf("InjectMockVaultForTest failed: %v", err)
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
			"vault_a": {Provider: "env"},
			"vault_b": {Provider: "env"},
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
	if err := orch.InjectMockVaultForTest("vault_a", []provider.SearchResult{
		{
			Provider: "search", Vault: "vault_a", Path: "key_a",
			Entry: provider.Entry{Title: "Key A", Tags: []string{"service:openrouter"}, Attributes: map[string]any{"Password": "pass_a"}},
		},
	}); err != nil {
		t.Fatalf("InjectMockVaultForTest(vault_a) failed: %v", err)
	}
	if err := orch.InjectMockVaultForTest("vault_b", []provider.SearchResult{
		{
			Provider: "search", Vault: "vault_b", Path: "key_b",
			Entry: provider.Entry{Title: "Key B", Tags: []string{"service:openrouter"}, Attributes: map[string]any{"Password": "pass_b"}},
		},
	}); err != nil {
		t.Fatalf("InjectMockVaultForTest(vault_b) failed: %v", err)
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
			"kp": {Provider: "env"},
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
	if err := orch.InjectMockVaultForTest("kp", []provider.SearchResult{
		{
			Provider: "search", Vault: "kp", Path: "my_entry",
			Entry: provider.Entry{
				Title: "My Entry",
				Tags:  []string{"dotenv:jules"},
				Attributes: map[string]any{
					"env:API_KEY": "sk-jules-secret",
					"OTHER":       "unmapped_val",
				},
			},
		},
	}); err != nil {
		t.Fatalf("InjectMockVaultForTest(kp) failed: %v", err)
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
				Provider: "env",
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
	if err := orch.InjectMockVaultForTest("kp", []provider.SearchResult{
		{
			Provider: "search", Vault: "kp", Path: "my_entry",
			Entry: provider.Entry{
				Title: "My Entry",
				Tags:  []string{"dotenv:jules"},
				Attributes: map[string]any{
					"env:API_KEY": "sk-jules-secret",
					"OTHER":       "unmapped_val",
				},
			},
		},
	}); err != nil {
		t.Fatalf("InjectMockVaultForTest failed: %v", err)
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
				Provider: "env",
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
	if err := orch.InjectMockVaultForTest("kp", []provider.SearchResult{
		{
			Provider: "search", Vault: "kp", Path: "my_entry",
			Entry: provider.Entry{
				Title: "My Entry",
				Tags:  []string{"dotenv:jules"},
				Attributes: map[string]any{
					"env:API_KEY": "sk-jules-secret",
					"OTHER":       "unmapped_val",
				},
			},
		},
	}); err != nil {
		t.Fatalf("InjectMockVaultForTest failed: %v", err)
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

func TestSearchProviderIntegration_KeyFormatOverrideLowestPriority(t *testing.T) {
	ctx := context.Background()

	rule := config.MappingRule{Match: "^env:(.*)$", Key: "$1"}
	_ = rule.Compile()

	makeOrch := func(t *testing.T, keyFormatOverride string, kpKeyFormat string) *engine.Orchestrator {
		t.Helper()
		cfg := &config.Config{
			KeyFormatOverride: keyFormatOverride,
			Vaults: map[string]config.VaultConfig{
				"kp": {
					Provider:  "env",
					KeyFormat: kpKeyFormat,
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
		if err := orch.InjectMockVaultForTest("kp", []provider.SearchResult{
			{
				Provider: "search", Vault: "kp", Path: "my_entry",
				Entry: provider.Entry{
					Title: "My Entry",
					Tags:  []string{"dotenv:jules"},
					Attributes: map[string]any{
						"env:API_KEY": "sk-jules-secret",
						"OTHER":       "unmapped_val",
					},
				},
			},
		}); err != nil {
			t.Fatalf("InjectMockVaultForTest failed: %v", err)
		}
		return orch
	}

	t.Run("OverrideEnv", func(t *testing.T) {
		orch := makeOrch(t, "env", "")

		results, err := orch.Search(ctx, "", []string{"jules"})
		if err != nil {
			t.Fatalf("unexpected search error: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0].Entry.Attributes["API_KEY"] != "sk-jules-secret" {
			t.Errorf("expected API_KEY to be preserved with -f env, got %v", results[0].Entry.Attributes)
		}
	})

	t.Run("OverrideSnake", func(t *testing.T) {
		orch := makeOrch(t, "snake", "")

		results, err := orch.Search(ctx, "", []string{"jules"})
		if err != nil {
			t.Fatalf("unexpected search error: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0].Entry.Attributes["api_key"] != "sk-jules-secret" {
			t.Errorf("expected api_key in snake_case, got %v", results[0].Entry.Attributes)
		}
	})

	t.Run("SourceVaultConfiguredKeyFormatSnake", func(t *testing.T) {
		orch := makeOrch(t, "", "snake")

		results, err := orch.Search(ctx, "", []string{"jules"})
		if err != nil {
			t.Fatalf("unexpected search error: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0].Entry.Attributes["API_KEY"] != "sk-jules-secret" {
			t.Errorf("expected API_KEY preserved from source vault with key_format: snake, got %v", results[0].Entry.Attributes)
		}
	})
}

func TestSearchProviderIntegration_Sources(t *testing.T) {
	ctx := context.Background()

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"vault_a": {Provider: "env"},
			"vault_b": {Provider: "env"},
			"vault_c": {
				Provider:      "env",
				IncludeFields: []string{"ALLOWED_SECRET"},
				ExcludeFields: []string{"LEAKED_SECRET"},
			},
			"grouped": {
				Provider:     "search",
				SourceVaults: []string{"vault_a", "vault_b"},
				Sources: []string{
					"vault_a://svc_a",
					`"tier:edge" in tags`,
					"vault_b://",
					`search://"tier:edge" in tags`,
				},
			},
			"grouped_search_only": {
				Provider: "search",
				Sources: []string{
					`search://_meta_.provider == "env" and _meta_.vault == "vault_b" and "tier:edge" in _meta_.tags/TOKEN`,
				},
			},
			"grouped_filtered": {
				Provider: "search",
				Sources:  []string{"vault_c://svc_c"},
			},
		},
	}

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("NewOrchestrator failed: %v", err)
	}

	if err := orch.InjectMockVaultForTest("vault_a", []provider.SearchResult{
		{
			Provider: "env", Vault: "vault_a", Path: "svc_a",
			Entry: provider.Entry{Title: "Service A", Attributes: map[string]any{"API_KEY": "alpha"}},
		},
	}); err != nil {
		t.Fatalf("InjectMockVaultForTest(vault_a) failed: %v", err)
	}
	if err := orch.InjectMockVaultForTest("vault_b", []provider.SearchResult{
		{
			Provider: "env", Vault: "vault_b", Path: "edge_svc",
			Entry: provider.Entry{Title: "Edge Service", Tags: []string{"tier:edge"}, Attributes: map[string]any{"TOKEN": "beta"}},
		},
	}); err != nil {
		t.Fatalf("InjectMockVaultForTest(vault_b) failed: %v", err)
	}
	if err := orch.InjectMockVaultForTest("vault_c", []provider.SearchResult{
		{
			Provider: "env", Vault: "vault_c", Path: "svc_c",
			Entry: provider.Entry{
				Title: "Service C",
				Attributes: map[string]any{
					"ALLOWED_SECRET": "allowed_value",
					"LEAKED_SECRET":  "should_be_blocked",
				},
			},
		},
	}); err != nil {
		t.Fatalf("InjectMockVaultForTest(vault_c) failed: %v", err)
	}

	valA, err := orch.Resolve(ctx, "grouped://svc_a:API_KEY")
	if err != nil || valA != "alpha" {
		t.Fatalf("Resolve(grouped://svc_a:API_KEY) = (%q, %v), want alpha", valA, err)
	}

	valB, err := orch.Resolve(ctx, "grouped://edge_svc:TOKEN")
	if err != nil || valB != "beta" {
		t.Fatalf("Resolve(grouped://edge_svc:TOKEN) = (%q, %v), want beta", valB, err)
	}

	// Verify source-vault filtering is strictly enforced through search.sources
	valAllowed, err := orch.Resolve(ctx, "grouped_filtered://svc_c:ALLOWED_SECRET")
	if err != nil || valAllowed != "allowed_value" {
		t.Fatalf("Resolve(grouped_filtered://svc_c:ALLOWED_SECRET) = (%q, %v), want allowed_value", valAllowed, err)
	}

	_, err = orch.Resolve(ctx, "grouped_filtered://svc_c:LEAKED_SECRET")
	if err == nil {
		t.Fatalf("expected error resolving LEAKED_SECRET through grouped_filtered, got nil")
	}

	groupedResults, err := orch.Search(ctx, "", []string{"grouped"})
	if err != nil {
		t.Fatalf("Search(grouped) failed: %v", err)
	}
	if len(groupedResults) != 2 {
		t.Fatalf("expected 2 deduplicated results from grouped, got %d: %+v", len(groupedResults), groupedResults)
	}

	searchOnlyResults, err := orch.Search(ctx, "", []string{"grouped_search_only"})
	if err != nil {
		t.Fatalf("Search(grouped_search_only) failed: %v", err)
	}
	if len(searchOnlyResults) != 1 {
		t.Fatalf("expected 1 result from grouped_search_only, got %d: %+v", len(searchOnlyResults), searchOnlyResults)
	}
	if searchOnlyResults[0].Path != "edge_svc" {
		t.Errorf("expected canonical path edge_svc from grouped_search_only, got %q", searchOnlyResults[0].Path)
	}

	valSearchOnly, err := orch.Resolve(ctx, "grouped_search_only://edge_svc:TOKEN")
	if err != nil || valSearchOnly != "beta" {
		t.Fatalf("Resolve(grouped_search_only://edge_svc:TOKEN) = (%q, %v), want beta", valSearchOnly, err)
	}
}
