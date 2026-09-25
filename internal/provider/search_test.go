package provider_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/warpcode/cloakenv/internal/provider"
)

func TestSearchProvider_SchemeAndMetadata(t *testing.T) {
	p := provider.NewSearchProvider()
	if p.Scheme() != "search" {
		t.Errorf("expected Scheme() = 'search', got %q", p.Scheme())
	}
	if !p.SupportsValueResolution() {
		t.Errorf("expected SupportsValueResolution() = true")
	}
}

func TestSearchProvider_Validate(t *testing.T) {
	p := provider.NewSearchProvider()

	t.Run("rejects searchable flag", func(t *testing.T) {
		err := p.Validate(map[string]string{"searchable": "true"})
		if err == nil {
			t.Fatal("expected error validating searchable setting, got nil")
		}
		if !strings.Contains(err.Error(), "search provider does not support the searchable flag") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("valid settings without searchable", func(t *testing.T) {
		err := p.Validate(map[string]string{"query": "tags contains 'foo'"})
		if err != nil {
			t.Errorf("expected no validation error, got: %v", err)
		}
	})
}

func TestSearchProvider_ReadOnly(t *testing.T) {
	p := provider.NewSearchProvider()
	ctx := context.Background()

	if err := p.SetSecret(ctx, "path", "val"); err == nil {
		t.Error("expected SetSecret to return error")
	}
	if err := p.DeleteSecret(ctx, "path"); err == nil {
		t.Error("expected DeleteSecret to return error")
	}
}

func TestSearchProvider_UnconfiguredExecutor(t *testing.T) {
	p := provider.NewSearchProvider()
	ctx := context.Background()

	if _, err := p.GetSecret(ctx, "attr"); err == nil {
		t.Error("expected GetSecret to fail when executor is nil")
	}
	if _, err := p.GetEntry(ctx, "entry"); err == nil {
		t.Error("expected GetEntry to fail when executor is nil")
	}
	if _, err := p.Search(ctx, provider.SearchQuery{}); err == nil {
		t.Error("expected Search to fail when executor is nil")
	}
}

func TestSearchProvider_GetSecretAndGetEntry(t *testing.T) {
	p := provider.NewSearchProvider()
	ctx := context.Background()

	err := p.Initialize(ctx, provider.ProviderConfig{
		Settings: map[string]string{
			"vault_name": "openrouter_vault",
			"query":      `tags contains "service:openrouter"`,
		},
		SourceVaults: []string{"work"},
		Query:        `tags contains "service:openrouter"`,
	})
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	mockResults := []provider.SearchResult{
		{
			Provider: "yaml",
			Vault:    "work",
			Path:     "openrouter_key",
			Entry: provider.Entry{
				Title: "OpenRouter API Key",
				Tags:  []string{"service:openrouter"},
				Attributes: map[string]any{
					"api_key":  "sk-or-v1-123456",
					"Password": "secret_password",
				},
			},
		},
		{
			Provider: "yaml",
			Vault:    "work",
			Path:     "backup_key",
			Entry: provider.Entry{
				Title: "OpenRouter Backup Key",
				Tags:  []string{"service:openrouter"},
				Attributes: map[string]any{
					"Password": "backup_secret_password",
				},
			},
		},
	}

	p.SetSearchExecutor(func(ctx context.Context, query string, sourceVaults []string, depth int) ([]provider.SearchResult, error) {
		if query != `tags contains "service:openrouter"` {
			t.Errorf("unexpected query passed to executor: %q", query)
		}
		if len(sourceVaults) != 1 || sourceVaults[0] != "work" {
			t.Errorf("unexpected sourceVaults passed to executor: %v", sourceVaults)
		}
		return mockResults, nil
	})

	t.Run("GetSecret with path:attribute syntax", func(t *testing.T) {
		val, err := p.GetSecret(ctx, "openrouter_key:api_key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != "sk-or-v1-123456" {
			t.Errorf("expected 'sk-or-v1-123456', got %q", val)
		}
	})

	t.Run("GetSecret with title:attribute syntax", func(t *testing.T) {
		val, err := p.GetSecret(ctx, "OpenRouter API Key:Password")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != "secret_password" {
			t.Errorf("expected 'secret_password', got %q", val)
		}
	})

	t.Run("GetSecret with vault/path:attribute syntax", func(t *testing.T) {
		val, err := p.GetSecret(ctx, "work/backup_key:Password")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != "backup_secret_password" {
			t.Errorf("expected 'backup_secret_password', got %q", val)
		}
	})

	t.Run("GetSecret missing entry", func(t *testing.T) {
		_, err := p.GetSecret(ctx, "nonexistent:Password")
		if err == nil {
			t.Fatal("expected error for missing entry, got nil")
		}
	})

	t.Run("GetSecret missing attribute", func(t *testing.T) {
		_, err := p.GetSecret(ctx, "openrouter_key:missing_attr")
		if err == nil {
			t.Fatal("expected error for missing attribute, got nil")
		}
	})

	t.Run("GetEntry by path", func(t *testing.T) {
		entry, err := p.GetEntry(ctx, "openrouter_key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if entry.Title != "OpenRouter API Key" {
			t.Errorf("expected title 'OpenRouter API Key', got %q", entry.Title)
		}
	})

	t.Run("Search with criteria filter", func(t *testing.T) {
		searchResults, err := p.Search(ctx, provider.SearchQuery{Title: "Backup"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(searchResults) != 1 {
			t.Fatalf("expected 1 result, got %d", len(searchResults))
		}
		if searchResults[0].Vault != "openrouter_vault" {
			t.Errorf("expected Vault 'openrouter_vault', got %q", searchResults[0].Vault)
		}
		if searchResults[0].Provider != "search" {
			t.Errorf("expected Provider 'search', got %q", searchResults[0].Provider)
		}
	})

	t.Run("Search with tags filter", func(t *testing.T) {
		searchResults, err := p.Search(ctx, provider.SearchQuery{Tags: []string{"SERVICE:OPENROUTER"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(searchResults) != 2 {
			t.Fatalf("expected 2 results, got %d", len(searchResults))
		}
	})
}

func TestSearchProvider_SingleResultShortcut(t *testing.T) {
	p := provider.NewSearchProvider()
	ctx := context.Background()

	err := p.Initialize(ctx, provider.ProviderConfig{
		Settings: map[string]string{"vault_name": "single_view"},
		Query:    "title == 'solo'",
	})
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	singleResult := []provider.SearchResult{
		{
			Provider: "custom_vault",
			Vault:    "work",
			Path:     "solo_path",
			Entry: provider.Entry{
				Title: "Solo Entry",
				Attributes: map[string]any{
					"my_key":   "secret_key_val",
					"Password": "default_password",
				},
			},
		},
	}

	p.SetSearchExecutor(func(ctx context.Context, query string, sourceVaults []string, depth int) ([]provider.SearchResult, error) {
		return singleResult, nil
	})

	t.Run("GetSecret direct attribute lookup on single result", func(t *testing.T) {
		val, err := p.GetSecret(ctx, "my_key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != "secret_key_val" {
			t.Errorf("expected 'secret_key_val', got %q", val)
		}
	})

	t.Run("GetSecret default Password attribute on empty location", func(t *testing.T) {
		val, err := p.GetSecret(ctx, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != "default_password" {
			t.Errorf("expected 'default_password', got %q", val)
		}
	})

	t.Run("GetEntry with empty location on single result", func(t *testing.T) {
		entry, err := p.GetEntry(ctx, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if entry.Title != "Solo Entry" {
			t.Errorf("expected 'Solo Entry', got %q", entry.Title)
		}
	})
}

func TestSearchProvider_ExecutorError(t *testing.T) {
	p := provider.NewSearchProvider()
	ctx := context.Background()

	p.SetSearchExecutor(func(ctx context.Context, query string, sourceVaults []string, depth int) ([]provider.SearchResult, error) {
		return nil, errors.New("database connection failed")
	})

	if _, err := p.GetSecret(ctx, "key"); err == nil {
		t.Error("expected error when executor fails")
	}
	if _, err := p.GetEntry(ctx, "key"); err == nil {
		t.Error("expected error when executor fails")
	}
	if _, err := p.Search(ctx, provider.SearchQuery{}); err == nil {
		t.Error("expected error when executor fails")
	}
}

func TestSearchProvider_CaseInsensitiveAttributesAndFallthrough(t *testing.T) {
	p := provider.NewSearchProvider()
	ctx := context.Background()

	err := p.Initialize(ctx, provider.ProviderConfig{
		Settings: map[string]string{"vault_name": "multi_view"},
		Query:    "true",
	})
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	mockResults := []provider.SearchResult{
		{
			Provider: "yaml",
			Vault:    "vault_a",
			Path:     "app",
			Entry: provider.Entry{
				Title: "App in Vault A",
				Tags:  []string{"web", "prod"},
				Attributes: map[string]any{
					"password": "lowercase_pass",
					"username": "admin",
				},
			},
		},
		{
			Provider: "yaml",
			Vault:    "vault_b",
			Path:     "app",
			Entry: provider.Entry{
				Title: "App in Vault B",
				Tags:  []string{"web", "backend"},
				Attributes: map[string]any{
					"api_key": "secret_api_key",
				},
			},
		},
	}

	p.SetSearchExecutor(func(ctx context.Context, query string, sourceVaults []string, depth int) ([]provider.SearchResult, error) {
		return mockResults, nil
	})

	t.Run("case-insensitive attribute lookup", func(t *testing.T) {
		val, err := p.GetSecret(ctx, "vault_b/app:API_KEY")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != "secret_api_key" {
			t.Errorf("expected 'secret_api_key', got %q", val)
		}
	})

	t.Run("title and tags pseudo-attributes", func(t *testing.T) {
		title, err := p.GetSecret(ctx, "vault_a/app:TITLE")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if title != "App in Vault A" {
			t.Errorf("expected 'App in Vault A', got %q", title)
		}

		tags, err := p.GetSecret(ctx, "vault_a/app:tags")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(tags, "web") {
			t.Errorf("expected tags to contain 'web', got %q", tags)
		}
	})

	t.Run("default password attribute matches lowercase password", func(t *testing.T) {
		val, err := p.GetSecret(ctx, "vault_a/app")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != "lowercase_pass" {
			t.Errorf("expected 'lowercase_pass', got %q", val)
		}
	})

	t.Run("falls through multiple matching entries to find attribute", func(t *testing.T) {
		// "app" matches both vault_a/app and vault_b/app. vault_a has no api_key, vault_b has api_key.
		val, err := p.GetSecret(ctx, "app:api_key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != "secret_api_key" {
			t.Errorf("expected 'secret_api_key', got %q", val)
		}
	})
}

func TestSearchProvider_MaxDepth(t *testing.T) {
	p := provider.NewSearchProvider()
	ctx := context.WithValue(context.Background(), provider.ContextKeyDepth, 5)

	err := p.Initialize(ctx, provider.ProviderConfig{
		Settings: map[string]string{"vault_name": "depth_test"},
		Query:    "true",
	})
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	p.SetSearchExecutor(func(ctx context.Context, query string, sourceVaults []string, depth int) ([]provider.SearchResult, error) {
		return nil, nil
	})

	_, err = p.GetSecret(ctx, "key:attr")
	if err == nil || !strings.Contains(err.Error(), "maximum recursion depth exceeded") {
		t.Errorf("expected max depth exceeded error, got: %v", err)
	}
}

func TestSearchProvider_AttributeMapPrecedenceOverMetadata(t *testing.T) {
	ctx := context.Background()
	p := provider.NewSearchProvider()

	err := p.Initialize(ctx, provider.ProviderConfig{
		Settings: map[string]string{"vault_name": "precedence_test"},
		Query:    "true",
	})
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	p.SetSearchExecutor(func(ctx context.Context, query string, sourceVaults []string, depth int) ([]provider.SearchResult, error) {
		return []provider.SearchResult{
			{
				Vault: "source",
				Path:  "custom_entry",
				Entry: provider.Entry{
					Title: "metadata_title",
					Tags:  []string{"meta_tag"},
					Attributes: map[string]any{
						"Title": "secret_title_attr",
						"Tags":  "secret_tags_attr",
					},
				},
			},
			{
				Vault: "source",
				Path:  "fallback_entry",
				Entry: provider.Entry{
					Title: "fallback_title",
					Tags:  []string{"fallback_tag"},
					Attributes: map[string]any{
						"Password": "secret_password",
					},
				},
			},
		}, nil
	})

	// When Attributes has "Title" or "Tags", attribute map value must take precedence over Entry metadata
	valTitle, err := p.GetSecret(ctx, "custom_entry:Title")
	if err != nil {
		t.Fatalf("unexpected error getting Title: %v", err)
	}
	if valTitle != "secret_title_attr" {
		t.Errorf("Title = %q, want 'secret_title_attr' (attribute map precedence)", valTitle)
	}

	valTags, err := p.GetSecret(ctx, "custom_entry:Tags")
	if err != nil {
		t.Fatalf("unexpected error getting Tags: %v", err)
	}
	if valTags != "secret_tags_attr" {
		t.Errorf("Tags = %q, want 'secret_tags_attr' (attribute map precedence)", valTags)
	}

	// When Attributes does NOT have "Title" or "Tags", it falls back to Entry metadata
	valMetaTitle, err := p.GetSecret(ctx, "fallback_entry:Title")
	if err != nil {
		t.Fatalf("unexpected error getting fallback Title: %v", err)
	}
	if valMetaTitle != "fallback_title" {
		t.Errorf("Title = %q, want 'fallback_title' (metadata fallback)", valMetaTitle)
	}

	valMetaTags, err := p.GetSecret(ctx, "fallback_entry:Tags")
	if err != nil {
		t.Fatalf("unexpected error getting fallback Tags: %v", err)
	}
	if !strings.Contains(valMetaTags, "fallback_tag") {
		t.Errorf("Tags = %q, want to contain 'fallback_tag' (metadata fallback)", valMetaTags)
	}
}

func TestSearchProvider_GetSecretWithRaw(t *testing.T) {
	ctx := context.Background()
	p := provider.NewSearchProvider()
	p.SetSearchExecutor(func(_ context.Context, _ string, _ []string, _ int) ([]provider.SearchResult, error) {
		return []provider.SearchResult{
			{
				Vault: "source",
				Path:  "services/api",
				Entry: provider.Entry{
					Title: "api_service",
					Tags:  []string{"env:prod"},
					Attributes: map[string]any{
						"config": map[string]any{
							"endpoint": "https://api.internal",
							"token":    "tok_raw_123",
						},
						"tokens":   []any{"t1", "t2"},
						"Password": "plain_password",
					},
				},
			},
		}, nil
	})

	// 1. Raw map attribute
	key, rawVal, resPath, title, err := p.GetSecretWithRaw(ctx, "config")
	if err != nil {
		t.Fatalf("GetSecretWithRaw(config) failed: %v", err)
	}
	if key != "config" {
		t.Errorf("expected canonicalKey 'config', got %q", key)
	}
	if resPath != "services/api" {
		t.Errorf("expected resultPath 'services/api', got %q", resPath)
	}
	if title != "api_service" {
		t.Errorf("expected title 'api_service', got %q", title)
	}
	m, ok := rawVal.(map[string]any)
	if !ok || m["endpoint"] != "https://api.internal" || m["token"] != "tok_raw_123" {
		t.Errorf("unexpected rawVal: %v", rawVal)
	}

	// 2. Raw slice attribute
	key, rawVal, resPath, title, err = p.GetSecretWithRaw(ctx, "tokens")
	if err != nil {
		t.Fatalf("GetSecretWithRaw(tokens) failed: %v", err)
	}
	if key != "tokens" {
		t.Errorf("expected canonicalKey 'tokens', got %q", key)
	}
	if resPath != "services/api" {
		t.Errorf("expected resultPath 'services/api', got %q", resPath)
	}
	if title != "api_service" {
		t.Errorf("expected title 'api_service', got %q", title)
	}
	s, ok := rawVal.([]any)
	if !ok || len(s) != 2 || s[0] != "t1" {
		t.Errorf("unexpected rawVal slice: %v", rawVal)
	}

	// 3. Default password
	key, rawVal, resPath, title, err = p.GetSecretWithRaw(ctx, "default")
	if err != nil {
		t.Fatalf("GetSecretWithRaw(default) failed: %v", err)
	}
	if key != "Password" || rawVal != "plain_password" {
		t.Errorf("unexpected default password: key=%q, rawVal=%v", key, rawVal)
	}
	if resPath != "services/api" {
		t.Errorf("expected resultPath 'services/api', got %q", resPath)
	}
	if title != "api_service" {
		t.Errorf("expected title 'api_service', got %q", title)
	}
}
