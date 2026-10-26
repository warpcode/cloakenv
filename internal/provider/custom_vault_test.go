package provider

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
)

func TestCustomVaultProvider_MultipleEntities(t *testing.T) {
	ctx := context.Background()
	p := NewCustomVaultProvider()

	cfg := ProviderConfig{
		Entities: config.ToCustomEntityMap(map[string]map[string]any{
			"entity1": {
				"Password": "pass1",
				"username": "user1",
				"tags":     "env:dev, type:db",
			},
			"entity2": {
				"Password": "pass2",
				"username": "user2",
				"tags":     []any{"env:prod"},
			},
		}),
	}

	if err := p.Initialize(ctx, cfg); err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}

	if p.Scheme() != "custom_vault" {
		t.Errorf("expected scheme custom_vault, got %q", p.Scheme())
	}

	// 1. GetSecret
	val, err := p.GetSecret(ctx, "entity1:username")
	if err != nil {
		t.Fatalf("GetSecret failed: %v", err)
	}
	if val != "user1" {
		t.Errorf("expected user1, got %q", val)
	}

	// Default attribute "Password"
	val, err = p.GetSecret(ctx, "entity2")
	if err != nil {
		t.Fatalf("GetSecret failed: %v", err)
	}
	if val != "pass2" {
		t.Errorf("expected pass2, got %q", val)
	}

	_, err = p.GetSecret(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent entity, got nil")
	}

	_, err = p.GetSecret(ctx, "entity1:nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent attribute, got nil")
	}

	// 2. GetEntry
	entry, err := p.GetEntry(ctx, "entity1")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if entry.Title != "entity1" {
		t.Errorf("expected title 'entity1', got %q", entry.Title)
	}
	if len(entry.Tags) != 2 || entry.Tags[0] != "env:dev" || entry.Tags[1] != "type:db" {
		t.Errorf("unexpected tags parsed: %v", entry.Tags)
	}

	// 2b. GetEntryTitle
	title, err := p.GetEntryTitle(ctx, "entity1")
	if err != nil {
		t.Fatalf("GetEntryTitle failed: %v", err)
	}
	if title != "entity1" {
		t.Errorf("expected title 'entity1', got %q", title)
	}

	_, err = p.GetEntryTitle(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent entity, got nil")
	}

	// 3. Search
	results, err := p.Search(ctx, SearchQuery{})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Search matching title query
	results, err = p.Search(ctx, SearchQuery{Title: "entity1"})
	if err != nil || len(results) != 1 {
		t.Errorf("expected 1 result, got %d, err: %v", len(results), err)
	}

	// Search matching tag
	results, err = p.Search(ctx, SearchQuery{Tags: []string{"env:prod"}})
	if err != nil || len(results) != 1 {
		t.Errorf("expected 1 result for tag query, got %d, err: %v", len(results), err)
	}
}

func TestCustomVaultProvider_Search(t *testing.T) {
	ctx := context.Background()
	p := NewCustomVaultProvider()

	cfg := ProviderConfig{
		Entities: config.ToCustomEntityMap(map[string]map[string]any{
			"projectA/db": {
				"Password": "pass1",
				"Title":    "Database Prod",
				"tags":     "env:prod, type:db",
			},
			"projectA/web": {
				"Password": "pass2",
				"Title":    "Web Server Prod",
				"tags":     []any{"env:prod", "type:web"},
			},
			"projectB/db": {
				"Password": "pass3",
				"Title":    "Database Staging",
				"tags":     []string{"env:staging", "type:db"},
			},
		}),
	}

	if err := p.Initialize(ctx, cfg); err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}

	tests := []struct {
		name        string
		query       SearchQuery
		wantResults int
	}{
		{
			name:        "Empty query returns all",
			query:       SearchQuery{},
			wantResults: 3,
		},
		{
			name:        "Title exact match",
			query:       SearchQuery{Title: "Database Prod"},
			wantResults: 1,
		},
		{
			name:        "Title partial match case-insensitive",
			query:       SearchQuery{Title: "database"},
			wantResults: 2,
		},
		{
			name:        "Title mismatch",
			query:       SearchQuery{Title: "Cache"},
			wantResults: 0,
		},
		{
			name:        "Path exact match",
			query:       SearchQuery{Path: "projectA/db"},
			wantResults: 1,
		},
		{
			name:        "Path partial match case-insensitive",
			query:       SearchQuery{Path: "projecta"},
			wantResults: 2,
		},
		{
			name:        "Path mismatch",
			query:       SearchQuery{Path: "projectC"},
			wantResults: 0,
		},
		{
			name:        "Tags single match",
			query:       SearchQuery{Tags: []string{"env:prod"}},
			wantResults: 2,
		},
		{
			name:        "Tags multiple match",
			query:       SearchQuery{Tags: []string{"env:prod", "type:db"}},
			wantResults: 1,
		},
		{
			name:        "Tags partial mismatch",
			query:       SearchQuery{Tags: []string{"env:prod", "type:cache"}},
			wantResults: 0,
		},
		{
			name:        "Tags case-insensitive match",
			query:       SearchQuery{Tags: []string{"ENV:PROD"}},
			wantResults: 2,
		},
		{
			name:        "Combination match",
			query:       SearchQuery{Title: "database", Path: "projectb", Tags: []string{"env:staging"}},
			wantResults: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := p.Search(ctx, tt.query)
			if err != nil {
				t.Fatalf("Search failed: %v", err)
			}
			if len(results) != tt.wantResults {
				t.Errorf("expected %d results, got %d", tt.wantResults, len(results))
			}
		})
	}
}

func TestCustomVaultProvider_Validate(t *testing.T) {
	p := NewCustomVaultProvider()

	tests := []struct {
		name     string
		settings map[string]string
	}{
		{
			name:     "nil settings",
			settings: nil,
		},
		{
			name:     "empty settings map",
			settings: map[string]string{},
		},
		{
			name:     "non-empty settings map",
			settings: map[string]string{"key": "value", "foo": "bar"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := p.Validate(tt.settings); err != nil {
				t.Errorf("Validate(%v) unexpected error: %v", tt.settings, err)
			}
		})
	}
}

func TestCustomVaultProvider_SetSecret(t *testing.T) {
	ctx := context.Background()
	p := NewCustomVaultProvider()

	err := p.SetSecret(ctx, "entity:attr", "secretval")
	if err == nil {
		t.Error("expected error for SetSecret, got nil")
	}
}

func TestCustomVaultProvider_DeleteSecret(t *testing.T) {
	ctx := context.Background()
	p := NewCustomVaultProvider()

	err := p.DeleteSecret(ctx, "entity:attr")
	if err == nil {
		t.Error("expected error for DeleteSecret, got nil")
	}
}

func TestCustomVaultProvider_SupportsValueResolution(t *testing.T) {
	p := NewCustomVaultProvider()

	if !p.SupportsValueResolution() {
		t.Error("expected SupportsValueResolution() to be true, got false")
	}
}

func TestCustomVaultProvider_GetRawAttribute(t *testing.T) {
	ctx := context.Background()
	p := NewCustomVaultProvider()

	rawMap := map[string]any{"user": "alice", "token": "secret123"}
	rawSlice := []any{"item1", "item2"}

	cfg := ProviderConfig{
		Entities: config.ToCustomEntityMap(map[string]map[string]any{
			"app": {
				"data":   rawMap,
				"tokens": rawSlice,
				"secret": "scalar_secret",
			},
		}),
	}

	if err := p.Initialize(ctx, cfg); err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}

	// 1. Map attribute
	val, err := p.GetRawAttribute("app", "data")
	if err != nil {
		t.Fatalf("GetRawAttribute(app, data) failed: %v", err)
	}
	m, ok := val.(map[string]any)
	if !ok || m["user"] != "alice" || m["token"] != "secret123" {
		t.Errorf("GetRawAttribute returned unexpected map: %v", val)
	}

	// 2. Slice attribute
	val, err = p.GetRawAttribute("app", "tokens")
	if err != nil {
		t.Fatalf("GetRawAttribute(app, tokens) failed: %v", err)
	}
	s, ok := val.([]any)
	if !ok || len(s) != 2 || s[0] != "item1" {
		t.Errorf("GetRawAttribute returned unexpected slice: %v", val)
	}

	// 3. Scalar attribute
	val, err = p.GetRawAttribute("app", "secret")
	if err != nil {
		t.Fatalf("GetRawAttribute(app, secret) failed: %v", err)
	}
	if val != "scalar_secret" {
		t.Errorf("GetRawAttribute returned %v, want scalar_secret", val)
	}

	// 4. Missing entity
	_, err = p.GetRawAttribute("missing", "data")
	if err == nil {
		t.Errorf("expected error for missing entity, got nil")
	}

	// 5. Missing attribute
	_, err = p.GetRawAttribute("app", "missing")
	if err == nil {
		t.Errorf("expected error for missing attribute, got nil")
	}
}

func TestCustomVaultProvider_ComposedEntities(t *testing.T) {
	ctx := context.Background()

	p := NewCustomVaultProvider()

	// Mock getEntriesFunc to simulate resolving merged URIs
	p.SetGetEntriesFunc(func(ctx context.Context, uri string) ([]Entry, error) {
		if uri == "keepass://infra/db" {
			return []Entry{
				{
					Title: "Database Primary",
					Tags:  []string{"db", "primary"},
					Attributes: map[string]any{
						"DB_HOST":     "10.0.0.1",
						"DB_PORT":     "5432",
						"DB_USER":     "postgres",
						"DB_PASSWORD": "secret-db-pass",
						"SECRET_KEY":  "infra-secret-to-exclude",
					},
				},
			}, nil
		}
		if uri == "search://tag=api" {
			return []Entry{
				{
					Title: "API Service 1",
					Tags:  []string{"api", "v1"},
					Attributes: map[string]any{
						"API_PORT": "8081",
					},
				},
				{
					Title: "API Service 2",
					Tags:  []string{"api", "v2"},
					Attributes: map[string]any{
						"API_PORT": "8082",
					},
				},
			}, nil
		}
		return nil, fmt.Errorf("unknown uri: %s", uri)
	})

	// Simple template renderer for testing
	p.SetRenderTemplateFunc(func(ctx context.Context, tmplStr string, data any) (string, error) {
		ctxMap, ok := data.(map[string]any)
		if !ok {
			return tmplStr, nil
		}
		meta, _ := ctxMap["meta"].(map[string]any)
		attrs, _ := ctxMap["attributes"].(map[string]any)
		entries, _ := ctxMap["entries"].([]Entry)

		res := tmplStr
		if meta != nil {
			if title, ok := meta["title"].(string); ok {
				res = strings.ReplaceAll(res, "{{ .meta.title }}", title)
			}
		}
		if attrs != nil {
			for k, v := range attrs {
				res = strings.ReplaceAll(res, fmt.Sprintf("{{ .attributes.%s }}", k), fmt.Sprintf("%v", v))
			}
		}
		if len(entries) > 0 {
			res = strings.ReplaceAll(res, "{{ len .entries }}", fmt.Sprintf("%d", len(entries)))
			res = strings.ReplaceAll(res, "{{ (index .entries 0).Title }}", entries[0].Title)
		}
		return res, nil
	})

	rule := config.MappingRule{Match: "DB_(.*)", Key: "DATABASE_$1"}
	if err := rule.Compile(); err != nil {
		t.Fatalf("rule compile failed: %v", err)
	}

	cfg := ProviderConfig{
		Entities: map[string]config.CustomEntityConfig{
			"app_composed": {
				Title:      "My Composed App",
				Tags:       []string{"app", "custom"},
				IsComposed: true,
				Merge:      []string{"keepass://infra/db", "search://tag=api"},
				ExcludeFields: []string{
					"SECRET_KEY",
				},
				Mapping: []config.MappingRule{rule},
				Attributes: map[string]any{
					"CUSTOM_ATTR":   "custom-static-val",
					"DB_URL":        "postgres://{{ .attributes.DATABASE_USER }}@{{ .attributes.DATABASE_HOST }}:{{ .attributes.DATABASE_PORT }}/app",
					"SUMMARY_TITLE": "{{ .meta.title }}",
					"ENTRY_COUNT":   "{{ len .entries }}",
					"FIRST_ENTRY":   "{{ (index .entries 0).Title }}",
				},
			},
		},
	}

	if err := p.Initialize(ctx, cfg); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	t.Run("GetEntry resolves composed entity with merge, filters, mapping, and templates", func(t *testing.T) {
		entry, err := p.GetEntry(ctx, "app_composed")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}

		if entry.Title != "My Composed App" {
			t.Errorf("Title = %q, want 'My Composed App'", entry.Title)
		}

		// Tags should include merged tags and entity tags
		tagSet := make(map[string]bool)
		for _, tag := range entry.Tags {
			tagSet[tag] = true
		}
		for _, wantTag := range []string{"db", "primary", "api", "v1", "v2", "app", "custom"} {
			if !tagSet[wantTag] {
				t.Errorf("expected tag %q in entry tags: %v", wantTag, entry.Tags)
			}
		}

		// Excluded field should not be present
		if _, ok := entry.Attributes["SECRET_KEY"]; ok {
			t.Errorf("SECRET_KEY should have been excluded")
		}

		// Mapped fields
		if entry.Attributes["DATABASE_HOST"] != "10.0.0.1" {
			t.Errorf("DATABASE_HOST = %v, want '10.0.0.1'", entry.Attributes["DATABASE_HOST"])
		}
		if entry.Attributes["DATABASE_USER"] != "postgres" {
			t.Errorf("DATABASE_USER = %v, want 'postgres'", entry.Attributes["DATABASE_USER"])
		}

		// Static attribute
		if entry.Attributes["CUSTOM_ATTR"] != "custom-static-val" {
			t.Errorf("CUSTOM_ATTR = %v, want 'custom-static-val'", entry.Attributes["CUSTOM_ATTR"])
		}

		// Templated attributes
		if entry.Attributes["DB_URL"] != "postgres://postgres@10.0.0.1:5432/app" {
			t.Errorf("DB_URL = %v, want 'postgres://postgres@10.0.0.1:5432/app'", entry.Attributes["DB_URL"])
		}
		if entry.Attributes["SUMMARY_TITLE"] != "My Composed App" {
			t.Errorf("SUMMARY_TITLE = %v, want 'My Composed App'", entry.Attributes["SUMMARY_TITLE"])
		}
		if entry.Attributes["ENTRY_COUNT"] != "3" {
			t.Errorf("ENTRY_COUNT = %v, want '3'", entry.Attributes["ENTRY_COUNT"])
		}
		if entry.Attributes["FIRST_ENTRY"] != "Database Primary" {
			t.Errorf("FIRST_ENTRY = %v, want 'Database Primary'", entry.Attributes["FIRST_ENTRY"])
		}
	})

	t.Run("GetSecret resolves composed entity merged and templated attributes", func(t *testing.T) {
		valHost, err := p.GetSecret(ctx, "app_composed:DATABASE_HOST")
		if err != nil {
			t.Fatalf("GetSecret DATABASE_HOST failed: %v", err)
		}
		if valHost != "10.0.0.1" {
			t.Errorf("GetSecret DATABASE_HOST = %q, want '10.0.0.1'", valHost)
		}

		valUrl, err := p.GetSecret(ctx, "app_composed:DB_URL")
		if err != nil {
			t.Fatalf("GetSecret DB_URL failed: %v", err)
		}
		if valUrl != "postgres://postgres@10.0.0.1:5432/app" {
			t.Errorf("GetSecret DB_URL = %q, want 'postgres://postgres@10.0.0.1:5432/app'", valUrl)
		}

		valTitle, err := p.GetSecret(ctx, "app_composed:SUMMARY_TITLE")
		if err != nil {
			t.Fatalf("GetSecret SUMMARY_TITLE failed: %v", err)
		}
		if valTitle != "My Composed App" {
			t.Errorf("GetSecret SUMMARY_TITLE = %q, want 'My Composed App'", valTitle)
		}

		// Excluded field fails
		_, err = p.GetSecret(ctx, "app_composed:SECRET_KEY")
		if err == nil {
			t.Errorf("expected error for excluded SECRET_KEY, got nil")
		}
	})
}
