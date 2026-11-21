package engine

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
)

type delayedSearchableProvider struct {
	delay       time.Duration
	err         error
	items       []provider.SearchResult
	inFlight    *atomic.Int64
	maxInFlight *atomic.Int64
}

func (d *delayedSearchableProvider) Scheme() string { return "delayed" }
func (d *delayedSearchableProvider) Initialize(_ context.Context, _ provider.ProviderConfig) error {
	return nil
}
func (d *delayedSearchableProvider) GetSecret(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (d *delayedSearchableProvider) SetSecret(_ context.Context, _, _ string) error {
	return nil
}
func (d *delayedSearchableProvider) DeleteSecret(_ context.Context, _ string) error {
	return nil
}
func (d *delayedSearchableProvider) Validate(_ map[string]string) error {
	return nil
}
func (d *delayedSearchableProvider) Search(ctx context.Context, _ provider.SearchQuery) ([]provider.SearchResult, error) {
	if d.inFlight != nil && d.maxInFlight != nil {
		current := d.inFlight.Add(1)
		defer d.inFlight.Add(-1)

		for {
			curMax := d.maxInFlight.Load()
			if current <= curMax || d.maxInFlight.CompareAndSwap(curMax, current) {
				break
			}
		}
	}

	if d.delay > 0 {
		select {
		case <-time.After(d.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if d.err != nil {
		return nil, d.err
	}
	return d.items, nil
}
func (d *delayedSearchableProvider) GetEntry(_ context.Context, _ string) (provider.Entry, error) {
	return provider.Entry{}, nil
}

func TestSearcher_GoTemplatePredicates(t *testing.T) {
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"vault_a": {Provider: "env"},
		},
	}
	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("NewOrchestrator failed: %v", err)
	}

	orch.providerManager.vaultCache["vault_a"] = &delayedSearchableProvider{
		items: []provider.SearchResult{
			{
				Provider: "keepass",
				Vault:    "vault_a",
				Path:     "prod/db",
				Entry: provider.Entry{
					Title: "Primary DB",
					UUID:  "uuid-1",
					Tags:  []string{"LiteLLM", "prod"},
					Group: &provider.GroupInfo{Name: "prod", Path: "infra/prod", UUID: "g-1"},
					Attributes: map[string]any{
						"host": "db.internal",
						"port": "5432",
					},
				},
			},
			{
				Provider: "keepass",
				Vault:    "vault_a",
				Path:     "staging/api",
				Entry: provider.Entry{
					Title: "Staging API",
					UUID:  "uuid-2",
					Tags:  []string{"staging"},
					Group: &provider.GroupInfo{Name: "staging", Path: "infra/staging", UUID: "g-2"},
					Attributes: map[string]any{
						"host": "api.staging",
						"port": "8080",
					},
				},
			},
		},
	}

	ctx := context.Background()

	t.Run("hasTag case-insensitive", func(t *testing.T) {
		res, err := orch.Search(ctx, `{{ hasTag "litellm" .tags }}`, []string{"vault_a"})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(res) != 1 || res[0].Entry.Title != "Primary DB" {
			t.Fatalf("unexpected results: %+v", res)
		}
	})

	t.Run("group.name equality", func(t *testing.T) {
		res, err := orch.Search(ctx, `{{ eq .group.name "staging" }}`, []string{"vault_a"})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(res) != 1 || res[0].Entry.Title != "Staging API" {
			t.Fatalf("unexpected results: %+v", res)
		}
	})

	t.Run("combined predicate with attribute", func(t *testing.T) {
		res, err := orch.Search(ctx, `{{ and (hasTag "prod" .tags) (eq .port "5432") }}`, []string{"vault_a"})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(res) != 1 || res[0].Path != "prod/db" {
			t.Fatalf("unexpected results: %+v", res)
		}
	})

	t.Run("empty query returns all entries deterministically sorted", func(t *testing.T) {
		res, err := orch.Search(ctx, "", []string{"vault_a"})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(res) != 2 || res[0].Path != "prod/db" || res[1].Path != "staging/api" {
			t.Fatalf("unexpected results: %+v", res)
		}
	})

	t.Run("non-true template output skips entries", func(t *testing.T) {
		res, err := orch.Search(ctx, `{{ .title }}`, []string{"vault_a"})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(res) != 0 {
			t.Fatalf("expected 0 results for non-true template output, got %d", len(res))
		}
	})

	t.Run("invalid template syntax returns error", func(t *testing.T) {
		_, err := orch.Search(ctx, `{{ eq .title "unclosed`, []string{"vault_a"})
		if err == nil || !strings.Contains(err.Error(), "invalid query template") {
			t.Fatalf("expected invalid query template error, got: %v", err)
		}
	})

	t.Run("missingkey error fails execution", func(t *testing.T) {
		_, err := orch.Search(ctx, `{{ eq .nonexistent_field "x" }}`, []string{"vault_a"})
		if err == nil || !strings.Contains(err.Error(), "query execution failed") {
			t.Fatalf("expected missingkey query execution error, got: %v", err)
		}
	})
}

func TestSearcher_URLQueryParams(t *testing.T) {
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"vault_a": {Provider: "env"},
		},
	}
	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("NewOrchestrator failed: %v", err)
	}

	orch.providerManager.vaultCache["vault_a"] = &delayedSearchableProvider{
		items: []provider.SearchResult{
			{
				Provider: "keepass",
				Vault:    "vault_a",
				Path:     "servers/ssh/bastion",
				Entry: provider.Entry{
					Title: "Bastion Host",
					Tags:  []string{"auth:ssh", "env:prod"},
					Group: &provider.GroupInfo{Name: "Production", Path: "infra/Production"},
					Attributes: map[string]any{
						"username": "admin",
						"url":      "https://example.com/path",
						"notes":    "",
					},
				},
			},
		},
	}

	ctx := context.Background()

	cases := []struct {
		name      string
		query     string
		wantCount int
		wantErr   bool
	}{
		{name: "tags and title", query: "tags=auth:ssh,env:prod&title=Bastion", wantCount: 1},
		{name: "group name match", query: "group=Production&username=admin", wantCount: 1},
		{name: "group path match", query: "group=infra/Production", wantCount: 1},
		{name: "percent-decoded attribute", query: "url=https://example.com%2Fpath", wantCount: 1},
		{name: "empty attribute value", query: "notes=", wantCount: 1},
		{name: "nested group.name", query: "group.name=Production&_meta_.vault=vault_a", wantCount: 1},
		{name: "non-matching tag", query: "tags=nonexistent", wantCount: 0},
		{name: "malformed empty key", query: "=value", wantErr: true},
		{name: "invalid search key", query: "123bad=value", wantErr: true},
		{name: "bare expr without template braces rejected", query: `"prod" in tags`, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := orch.Search(ctx, tc.query, []string{"vault_a"})
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for query %q, got nil", tc.query)
				}
				return
			}
			if err != nil {
				t.Fatalf("Search(%q) failed: %v", tc.query, err)
			}
			if len(res) != tc.wantCount {
				t.Fatalf("Search(%q) returned %d results, want %d", tc.query, len(res), tc.wantCount)
			}
		})
	}
}

func TestParseSearchURI(t *testing.T) {
	tests := []struct {
		name      string
		location  string
		wantQuery string
		wantAttr  string
		wantErr   bool
	}{
		{
			name:      "url params with attr",
			location:  "tags=auth:ssh,env:prod&title=bastion/Password",
			wantQuery: "tags=auth:ssh,env:prod&title=bastion",
			wantAttr:  "Password",
		},
		{
			name:      "url params default Password attr",
			location:  "group=Production&username=admin",
			wantQuery: "group=Production&username=admin",
			wantAttr:  "Password",
		},
		{
			name:      "template query with attr",
			location:  `{{ hasTag "api" .tags }}/Token`,
			wantQuery: `{{ hasTag "api" .tags }}`,
			wantAttr:  "Token",
		},
		{
			name:      "template query default Password attr",
			location:  `{{ eq .title "api" }}`,
			wantQuery: `{{ eq .title "api" }}`,
			wantAttr:  "Password",
		},
		{
			name:     "empty location",
			location: "",
			wantErr:  true,
		},
		{
			name:     "empty key parameter",
			location: "=value/Password",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, attr, err := parseSearchURI(tt.location)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got nil", tt.location)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSearchURI(%q) error = %v", tt.location, err)
			}
			if q != tt.wantQuery || attr != tt.wantAttr {
				t.Fatalf("parseSearchURI(%q) = (%q, %q), want (%q, %q)", tt.location, q, attr, tt.wantQuery, tt.wantAttr)
			}
		})
	}
}

func TestSearcher_RejectsSearchProviderType(t *testing.T) {
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"virtual": {
				Provider: "search",
			},
		},
	}
	_, err := NewOrchestrator(cfg)
	if err == nil || !strings.Contains(err.Error(), "unsupported provider type") {
		t.Fatalf("expected unsupported provider type error for search vault, got: %v", err)
	}
}
