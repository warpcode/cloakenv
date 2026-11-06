package provider

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"text/template"

	"github.com/warpcode/cloakenv/internal/config"
)

func TestAttributesProvider_GetEntry(t *testing.T) {
	ctx := context.Background()
	underlying := &mockSearchableProvider{
		entries: map[string]Entry{
			"test-entry": {
				Title: "test-entry",
				Attributes: map[string]any{
					"existing": "value",
				},
			},
		},
	}
	attributes := map[string]any{
		"injected": "injected-value",
		"existing": "overwritten",
	}

	p := NewAttributesProvider(underlying, attributes)
	searchable, ok := p.(SearchableProvider)
	if !ok {
		t.Fatalf("expected NewAttributesProvider to return a SearchableProvider")
	}

	entry, err := searchable.GetEntry(ctx, "test-entry")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if val, ok := entry.Attributes["injected"]; !ok || val != "injected-value" {
		t.Errorf("expected injected attribute 'injected' to be 'injected-value', got %v", val)
	}
	if val, ok := entry.Attributes["existing"]; !ok || val != "overwritten" {
		t.Errorf("expected existing attribute to be overwritten with 'overwritten', got %v", val)
	}
}

func TestAttributesProvider_Search(t *testing.T) {
	ctx := context.Background()
	underlying := &mockSearchableProvider{
		results: []SearchResult{
			{
				Entry: Entry{
					Title: "test-entry",
					Attributes: map[string]any{
						"existing": "value",
					},
				},
			},
		},
	}
	attributes := map[string]any{
		"injected": "injected-value",
	}

	p := NewAttributesProvider(underlying, attributes)
	searchable, ok := p.(SearchableProvider)
	if !ok {
		t.Fatalf("expected NewAttributesProvider to return a SearchableProvider")
	}

	results, err := searchable.Search(ctx, SearchQuery{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	entry := results[0].Entry
	if val, ok := entry.Attributes["injected"]; !ok || val != "injected-value" {
		t.Errorf("expected injected attribute 'injected' to be 'injected-value', got %v", val)
	}
}

func TestAttributesProvider_GetSecret(t *testing.T) {
	ctx := context.Background()
	underlying := &mockSearchableProvider{
		entries: map[string]Entry{
			"test-entry": {
				Title: "test-entry",
				Attributes: map[string]any{
					"existing": "value",
				},
			},
		},
	}
	attributes := map[string]any{
		"injected": "injected-value",
	}

	p := NewAttributesProvider(underlying, attributes)

	// Test getting an existing attribute from underlying (fallback)
	// Actually mockSearchableProvider needs GetSecret implementation for this to work
	// Let's rely on injected attribute being returned
	val, err := p.GetSecret(ctx, "test-entry:injected")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "injected-value" {
		t.Errorf("expected 'injected-value', got %q", val)
	}
}

func TestAttributesProvider_TemplateRendering(t *testing.T) {
	ctx := context.Background()
	underlying := &mockSearchableProvider{
		entries: map[string]Entry{
			"prod/api": {
				Title: "API Service",
				Attributes: map[string]any{
					"username": "deploy-user",
					"port":     "8080",
				},
			},
		},
		results: []SearchResult{
			{
				Provider: "custom_vault",
				Vault:    "main",
				Path:     "prod/api",
				Entry: Entry{
					Title: "API Service",
					Attributes: map[string]any{
						"username": "deploy-user",
						"port":     "8080",
					},
				},
			},
		},
	}

	attributes := map[string]any{
		"url":         "https://{{ lower .meta.title }}.internal:{{ .attributes.port }}",
		"auth_header": "Bearer {{ upper .attributes.username }}",
		"static_tag":  "managed",
	}

	p := NewAttributesProvider(underlying, attributes)
	searchable, ok := p.(SearchableProvider)
	if !ok {
		t.Fatalf("expected SearchableProvider interface")
	}

	t.Run("GetEntry renders meta and attributes templates", func(t *testing.T) {
		entry, err := searchable.GetEntry(ctx, "prod/api")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		if entry.Attributes["url"] != "https://api service.internal:8080" {
			t.Errorf("url = %q, want 'https://api service.internal:8080'", entry.Attributes["url"])
		}
		if entry.Attributes["auth_header"] != "Bearer DEPLOY-USER" {
			t.Errorf("auth_header = %q, want 'Bearer DEPLOY-USER'", entry.Attributes["auth_header"])
		}
		if entry.Attributes["static_tag"] != "managed" {
			t.Errorf("static_tag = %q, want 'managed'", entry.Attributes["static_tag"])
		}
	})

	t.Run("Search renders meta and attributes templates", func(t *testing.T) {
		results, err := searchable.Search(ctx, SearchQuery{})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		entry := results[0].Entry
		if entry.Attributes["url"] != "https://api service.internal:8080" {
			t.Errorf("Search url = %q, want 'https://api service.internal:8080'", entry.Attributes["url"])
		}
		if entry.Attributes["auth_header"] != "Bearer DEPLOY-USER" {
			t.Errorf("Search auth_header = %q, want 'Bearer DEPLOY-USER'", entry.Attributes["auth_header"])
		}
	})

	t.Run("GetSecret resolves and renders templated attributes", func(t *testing.T) {
		valUrl, err := p.GetSecret(ctx, "prod/api:url")
		if err != nil {
			t.Fatalf("GetSecret url failed: %v", err)
		}
		if valUrl != "https://api service.internal:8080" {
			t.Errorf("GetSecret url = %q, want 'https://api service.internal:8080'", valUrl)
		}

		valAuth, err := p.GetSecret(ctx, "prod/api:auth_header")
		if err != nil {
			t.Fatalf("GetSecret auth_header failed: %v", err)
		}
		if valAuth != "Bearer DEPLOY-USER" {
			t.Errorf("GetSecret auth_header = %q, want 'Bearer DEPLOY-USER'", valAuth)
		}

		valStatic, err := p.GetSecret(ctx, "prod/api:static_tag")
		if err != nil {
			t.Fatalf("GetSecret static_tag failed: %v", err)
		}
		if valStatic != "managed" {
			t.Errorf("GetSecret static_tag = %q, want 'managed'", valStatic)
		}
	})
}

func TestAttributesProvider_InterfacePreservation(t *testing.T) {
	attributes := map[string]any{"injected": "value"}

	t.Run("only searchable", func(t *testing.T) {
		underlying := &mockSearchableProvider{}
		p := NewAttributesProvider(underlying, attributes)

		if _, ok := p.(SearchableProvider); !ok {
			t.Error("expected SearchableProvider interface to be preserved")
		}
		if _, ok := p.(ValueResolvableProvider); ok {
			t.Error("did not expect ValueResolvableProvider interface")
		}
	})

	t.Run("only value resolvable", func(t *testing.T) {
		underlying := &mockValueResolvableProvider{}
		p := NewAttributesProvider(underlying, attributes)

		if _, ok := p.(SearchableProvider); ok {
			t.Error("did not expect SearchableProvider interface")
		}
		if _, ok := p.(ValueResolvableProvider); !ok {
			t.Error("expected ValueResolvableProvider interface to be preserved")
		}
	})

	t.Run("both interfaces", func(t *testing.T) {
		underlying := &mockSearchableValueResolvableProvider{}
		p := NewAttributesProvider(underlying, attributes)

		if _, ok := p.(SearchableProvider); !ok {
			t.Error("expected SearchableProvider interface to be preserved")
		}
		if _, ok := p.(ValueResolvableProvider); !ok {
			t.Error("expected ValueResolvableProvider interface to be preserved")
		}
	})
}

func TestAttributesProvider_OriginalStateAccess(t *testing.T) {
	ctx := context.Background()

	rawAttributes := map[string]any{
		"db_user": "pguser",
		"db_pass": "supersecret",
		"host":    "db.internal",
		"port":    "5432",
	}
	rawEntry := Entry{
		Title:      "prod-db",
		Tags:       []string{"db", "production"},
		UUID:       "uuid-1234",
		Attributes: rawAttributes,
	}
	rawSnapshot := rawEntry.Clone()

	// Simulate entry after Stage 2 filtering (db_user and db_pass excluded from output attributes)
	filteredAttributes := map[string]any{
		"host": "db.internal",
		"port": "5432",
	}
	filteredEntry := Entry{
		Title:      "prod-db",
		Tags:       []string{"db", "production"},
		UUID:       "uuid-1234",
		Attributes: filteredAttributes,
		Original:   &rawSnapshot,
	}

	underlying := &mockSearchableProvider{
		entries: map[string]Entry{
			"prod-db": filteredEntry,
		},
		results: []SearchResult{
			{
				Provider: "keepass",
				Vault:    "infra",
				Path:     "prod-db",
				Entry:    filteredEntry,
			},
		},
	}

	attributes := map[string]any{
		"database_url": "postgres://{{ .original.attributes.db_user }}:{{ .original.attributes.db_pass }}@{{ .attributes.host }}:{{ .attributes.port }}/{{ .original.meta.title }}",
	}

	p := NewAttributesProvider(underlying, attributes)
	searchable, ok := p.(SearchableProvider)
	if !ok {
		t.Fatalf("expected SearchableProvider interface")
	}

	t.Run("GetEntry resolves composite attribute from .original.attributes while omitting excluded fields", func(t *testing.T) {
		entry, err := searchable.GetEntry(ctx, "prod-db")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}

		wantURL := "postgres://pguser:supersecret@db.internal:5432/prod-db"
		if entry.Attributes["database_url"] != wantURL {
			t.Errorf("database_url = %v, want %v", entry.Attributes["database_url"], wantURL)
		}
		if _, hasUser := entry.Attributes["db_user"]; hasUser {
			t.Errorf("db_user should be omitted from output attributes, but was present")
		}
		if _, hasPass := entry.Attributes["db_pass"]; hasPass {
			t.Errorf("db_pass should be omitted from output attributes, but was present")
		}
		if entry.Original == nil {
			t.Fatalf("entry.Original must be preserved")
		}
		if entry.Original.Attributes["db_user"] != "pguser" || entry.Original.Attributes["db_pass"] != "supersecret" {
			t.Errorf("entry.Original.Attributes = %v, want db_user and db_pass preserved", entry.Original.Attributes)
		}
	})

	t.Run("GetSecret resolves composite attribute referencing .original", func(t *testing.T) {
		val, err := p.GetSecret(ctx, "prod-db:database_url")
		if err != nil {
			t.Fatalf("GetSecret failed: %v", err)
		}
		wantURL := "postgres://pguser:supersecret@db.internal:5432/prod-db"
		if val != wantURL {
			t.Errorf("GetSecret database_url = %q, want %q", val, wantURL)
		}
	})

	t.Run("Search resolves composite attribute and preserves .original", func(t *testing.T) {
		results, err := searchable.Search(ctx, SearchQuery{})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		resEntry := results[0].Entry
		wantURL := "postgres://pguser:supersecret@db.internal:5432/prod-db"
		if resEntry.Attributes["database_url"] != wantURL {
			t.Errorf("Search database_url = %v, want %v", resEntry.Attributes["database_url"], wantURL)
		}
		if _, hasUser := resEntry.Attributes["db_user"]; hasUser {
			t.Errorf("db_user should be omitted from search result attributes")
		}
		if resEntry.Original == nil || resEntry.Original.Attributes["db_user"] != "pguser" {
			t.Errorf("search result Original not preserved: %v", resEntry.Original)
		}
	})
}

func TestAttributesProvider_MultiStagePipeline(t *testing.T) {
	ctx := context.Background()

	rawAttributes := map[string]any{
		"db_user": "admin",
		"db_pass": "secret123",
		"host":    "localhost",
		"port":    "5432",
	}
	baseEntry := Entry{
		Title:      "my-service",
		Attributes: rawAttributes,
	}

	baseProvider := &mockSearchableProvider{
		entries: map[string]Entry{
			"my-service": baseEntry,
		},
		results: []SearchResult{
			{
				Path:  "my-service",
				Entry: baseEntry,
			},
		},
	}

	// Stage 2: FilteringProvider (excludes db_*)
	filterProv := NewFilteringProvider(baseProvider, nil, []string{"db_*"})

	// Stage 3: MappingProvider
	mapRule := config.MappingRule{
		Match: "^host$",
		Key:   "server_host",
	}
	if err := mapRule.Compile(); err != nil {
		t.Fatalf("compile rule failed: %v", err)
	}
	mapProv, err := NewMappingProvider(filterProv, []config.MappingRule{mapRule})
	if err != nil {
		t.Fatalf("NewMappingProvider failed: %v", err)
	}

	// Stage 4: KeyFormatProvider (env format)
	formatProv := NewKeyFormatProvider(mapProv, "env")

	// Stage 5: AttributesProvider (references .original and .output / .attributes)
	computedAttrs := map[string]any{
		"DATABASE_URL": "postgres://{{ .original.attributes.db_user }}:{{ .original.attributes.db_pass }}@{{ .output.attributes.SERVER_HOST }}:{{ .attributes.PORT }}",
	}
	pipeline := NewAttributesProvider(formatProv, computedAttrs)

	searchable, ok := pipeline.(SearchableProvider)
	if !ok {
		t.Fatalf("expected SearchableProvider interface from pipeline")
	}

	t.Run("Pipeline GetEntry enforces stages and preserves .original", func(t *testing.T) {
		entry, err := searchable.GetEntry(ctx, "my-service")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}

		// Check Stage 5 computed attribute
		wantURL := "postgres://admin:secret123@localhost:5432"
		if entry.Attributes["DATABASE_URL"] != wantURL {
			t.Errorf("DATABASE_URL = %v, want %v", entry.Attributes["DATABASE_URL"], wantURL)
		}

		// Check Stage 4 formatted keys
		if entry.Attributes["SERVER_HOST"] != "localhost" {
			t.Errorf("SERVER_HOST = %v, want 'localhost'", entry.Attributes["SERVER_HOST"])
		}
		if entry.Attributes["PORT"] != "5432" {
			t.Errorf("PORT = %v, want '5432'", entry.Attributes["PORT"])
		}

		// Check Stage 2 excluded fields are omitted from output
		if _, hasUser := entry.Attributes["db_user"]; hasUser {
			t.Errorf("db_user should be excluded from output")
		}
		if _, hasPass := entry.Attributes["db_pass"]; hasPass {
			t.Errorf("db_pass should be excluded from output")
		}
		if _, hasDBUser := entry.Attributes["DB_USER"]; hasDBUser {
			t.Errorf("DB_USER should be excluded from output")
		}
		if _, hasDBPass := entry.Attributes["DB_PASS"]; hasDBPass {
			t.Errorf("DB_PASS should be excluded from output")
		}

		// Check Stage 1 Original is preserved
		if entry.Original == nil {
			t.Fatalf("entry.Original was not preserved through pipeline")
		}
		if entry.Original.Attributes["db_user"] != "admin" || entry.Original.Attributes["db_pass"] != "secret123" {
			t.Errorf("Original attributes = %v, want admin & secret123", entry.Original.Attributes)
		}
	})

	t.Run("Pipeline GetSecret resolves computed attribute and rejects excluded field", func(t *testing.T) {
		val, err := pipeline.GetSecret(ctx, "my-service:DATABASE_URL")
		if err != nil {
			t.Fatalf("GetSecret DATABASE_URL failed: %v", err)
		}
		wantURL := "postgres://admin:secret123@localhost:5432"
		if val != wantURL {
			t.Errorf("DATABASE_URL = %q, want %q", val, wantURL)
		}

		_, err = pipeline.GetSecret(ctx, "my-service:db_pass")
		if err == nil {
			t.Errorf("expected GetSecret for excluded db_pass to fail, but it succeeded")
		}
	})
}

func TestAttributesProvider_UnifiedTemplateRenderer(t *testing.T) {
	ctx := context.Background()

	underlying := &mockSearchableProvider{
		entries: map[string]Entry{
			"app": {
				Title: "My App",
				Attributes: map[string]any{
					"raw_tokens": []string{"alpha", "beta", "gamma"},
				},
			},
		},
	}

	attributes := map[string]any{
		"joined": "{{ .attributes.raw_tokens | join \":\" }}",
		"upper":  "{{ upper .meta.title }}",
	}

	p := NewAttributesProvider(underlying, attributes)
	if tr, ok := p.(interface{ SetRenderTemplateFunc(TemplateRenderFunc) }); ok {
		tr.SetRenderTemplateFunc(func(ctx context.Context, tmplStr string, data any) (string, error) {
			// Simulate unified template engine with join and upper
			tmpl, err := template.New("test").Funcs(template.FuncMap{
				"upper": strings.ToUpper,
				"join": func(sep string, v any) string {
					if slice, ok := v.([]string); ok {
						return strings.Join(slice, sep)
					}
					return fmt.Sprintf("%v", v)
				},
			}).Parse(tmplStr)
			if err != nil {
				return "", err
			}
			var buf bytes.Buffer
			if err := tmpl.Execute(&buf, data); err != nil {
				return "", err
			}
			return buf.String(), nil
		})
	} else {
		t.Fatalf("expected provider to implement SetRenderTemplateFunc")
	}

	searchable := p.(SearchableProvider)
	entry, err := searchable.GetEntry(ctx, "app")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}

	if entry.Attributes["joined"] != "alpha:beta:gamma" {
		t.Errorf("joined = %v, want 'alpha:beta:gamma'", entry.Attributes["joined"])
	}
	if entry.Attributes["upper"] != "MY APP" {
		t.Errorf("upper = %v, want 'MY APP'", entry.Attributes["upper"])
	}
}

// mock providers for testing
type mockSecretProvider struct{}

func (m *mockSecretProvider) Scheme() string                                           { return "mock" }
func (m *mockSecretProvider) Initialize(ctx context.Context, cfg ProviderConfig) error { return nil }
func (m *mockSecretProvider) GetSecret(ctx context.Context, location string) (string, error) {
	return "", fmt.Errorf("not found")
}
func (m *mockSecretProvider) SetSecret(ctx context.Context, location, value string) error { return nil }
func (m *mockSecretProvider) DeleteSecret(ctx context.Context, location string) error     { return nil }
func (m *mockSecretProvider) Validate(settings map[string]string) error                   { return nil }

type mockSearchableProvider struct {
	mockSecretProvider
	entries map[string]Entry
	results []SearchResult
}

func (m *mockSearchableProvider) GetEntry(ctx context.Context, location string) (Entry, error) {
	if entry, ok := m.entries[location]; ok {
		return entry, nil
	}
	return Entry{}, fmt.Errorf("not found")
}

func (m *mockSearchableProvider) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	return m.results, nil
}

type mockValueResolvableProvider struct {
	mockSecretProvider
}

func (m *mockValueResolvableProvider) SupportsValueResolution() bool { return true }

type mockSearchableValueResolvableProvider struct {
	mockSearchableProvider
}

func (m *mockSearchableValueResolvableProvider) SupportsValueResolution() bool { return true }
