package provider

import (
	"context"
	"testing"
)

func TestSearchVaultProvider(t *testing.T) {
	ctx := context.Background()

	mockRunner := func(ctx context.Context, expressionStr string, repoScopes []string) ([]SearchResult, error) {
		return []SearchResult{
			{
				Provider: "yaml",
				Vault:    "work",
				Path:     "api/openrouter",
				Entry: Entry{
					Title: "OpenRouter API Key",
					Tags:  []string{"service:openrouter", "env:prod"},
					Attributes: map[string]any{
						"api_key":  "sk-or-v1-12345",
						"Password": "sk-or-v1-12345",
					},
				},
			},
			{
				Provider: "yaml",
				Vault:    "work",
				Path:     "api/openai",
				Entry: Entry{
					Title: "OpenAI API Key",
					Tags:  []string{"service:openai"},
					Attributes: map[string]any{
						"api_key": "sk-proj-abcde",
					},
				},
			},
		}, nil
	}

	p := NewSearchVaultProvider()

	// 1. Validation during initialize
	err := p.Initialize(ctx, ProviderConfig{})
	if err == nil {
		t.Error("expected error initializing with empty ProviderConfig")
	}

	cfg := ProviderConfig{
		Settings: map[string]string{
			"vault_name": "openrouter",
		},
		SourceVaults: []string{"work"},
		Query:        `tags contains "service:openrouter"`,
		SearchRunner: mockRunner,
	}

	if err := p.Initialize(ctx, cfg); err != nil {
		t.Fatalf("failed to initialize SearchVaultProvider: %v", err)
	}

	if p.Scheme() != "search" {
		t.Errorf("expected scheme search, got %q", p.Scheme())
	}

	if !p.SupportsValueResolution() {
		t.Error("expected SupportsValueResolution() to be true")
	}

	// 2. Read-only checks
	if err := p.SetSecret(ctx, "loc", "val"); err == nil {
		t.Error("expected SetSecret to fail (read-only)")
	}
	if err := p.DeleteSecret(ctx, "loc"); err == nil {
		t.Error("expected DeleteSecret to fail (read-only)")
	}

	// 3. GetSecret
	val, err := p.GetSecret(ctx, "api/openrouter:api_key")
	if err != nil {
		t.Fatalf("GetSecret failed: %v", err)
	}
	if val != "sk-or-v1-12345" {
		t.Errorf("expected sk-or-v1-12345, got %q", val)
	}

	// Default attribute "Password"
	val, err = p.GetSecret(ctx, "api/openrouter")
	if err != nil {
		t.Fatalf("GetSecret default attr failed: %v", err)
	}
	if val != "sk-or-v1-12345" {
		t.Errorf("expected sk-or-v1-12345, got %q", val)
	}

	// Non-existent entry
	_, err = p.GetSecret(ctx, "nonexistent:api_key")
	if err == nil {
		t.Error("expected error for nonexistent entry")
	}

	// 4. GetEntry
	entry, err := p.GetEntry(ctx, "api/openrouter")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if entry.Title != "OpenRouter API Key" {
		t.Errorf("expected title 'OpenRouter API Key', got %q", entry.Title)
	}

	// 5. Search
	results, err := p.Search(ctx, SearchQuery{})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Vault != "openrouter" || results[0].Provider != "search" {
		t.Errorf("expected synthetic result vault 'openrouter' and provider 'search', got vault %q provider %q", results[0].Vault, results[0].Provider)
	}

	// Search filtered by query
	results, err = p.Search(ctx, SearchQuery{Title: "OpenRouter"})
	if err != nil || len(results) != 1 {
		t.Errorf("expected 1 result matching title 'OpenRouter', got %d (err: %v)", len(results), err)
	}
}
