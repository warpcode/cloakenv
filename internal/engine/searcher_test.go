package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

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
			{
				Provider: "keepass",
				Vault:    "vault_a",
				Path:     "misc/note",
				Entry: provider.Entry{
					Title: "Generic Note",
					UUID:  "uuid-3",
					Tags:  []string{"misc"},
					Attributes: map[string]any{
						"notes": "no port attribute here",
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
		if len(res) != 3 || res[0].Path != "misc/note" || res[1].Path != "prod/db" || res[2].Path != "staging/api" {
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

	t.Run("heterogeneous entry lacking queried attribute evaluates as non-matching", func(t *testing.T) {
		res, err := orch.Search(ctx, `{{ eq .port "5432" }}`, []string{"vault_a"})
		if err != nil {
			t.Fatalf("Search on heterogeneous entries failed: %v", err)
		}
		if len(res) != 1 || res[0].Path != "prod/db" {
			t.Fatalf("expected only prod/db, got: %+v", res)
		}
	})

	t.Run("missing attribute across all entries returns zero results cleanly", func(t *testing.T) {
		res, err := orch.Search(ctx, `{{ eq .nonexistent_field "x" }}`, []string{"vault_a"})
		if err != nil {
			t.Fatalf("Search for missing attribute failed: %v", err)
		}
		if len(res) != 0 {
			t.Fatalf("expected 0 results for missing attribute, got %d", len(res))
		}
	})

	t.Run("type mismatch error fails execution", func(t *testing.T) {
		_, err := orch.Search(ctx, `{{ eq .group "staging" }}`, []string{"vault_a"})
		if err == nil || !strings.Contains(err.Error(), "query execution failed") {
			t.Fatalf("expected query execution error for incomparable map type, got: %v", err)
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
						"token":    "abc==",
						"version":  "v>=2",
						"expr":     "a!=b",
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
		{name: "value with base64 == padding", query: "token=abc==", wantCount: 1},
		{name: "value with >= sequence", query: "version=v>=2", wantCount: 1},
		{name: "value with != sequence", query: "expr=a!=b", wantCount: 1},
		{name: "comparison == operator rejected", query: `title == "Bastion"`, wantErr: true},
		{name: "comparison != operator rejected", query: `title != "Other"`, wantErr: true},
		{name: "comparison <= operator rejected", query: "port <= 80", wantErr: true},
		{name: "comparison >= operator rejected", query: "port >= 80", wantErr: true},
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

func TestSearcher_MultiVaultConcurrencyAndError(t *testing.T) {
	t.Run("ConcurrentExecution", func(t *testing.T) {
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"v1": {Provider: "env"},
				"v2": {Provider: "env"},
				"v3": {Provider: "env"},
			},
		}
		orch, err := NewOrchestrator(cfg)
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}

		delay := 50 * time.Millisecond
		var inFlight, maxInFlight atomic.Int64
		p1 := &delayedSearchableProvider{
			delay:       delay,
			inFlight:    &inFlight,
			maxInFlight: &maxInFlight,
			items: []provider.SearchResult{
				{Vault: "v1", Path: "p1", Entry: provider.Entry{Title: "t1"}},
			},
		}
		p2 := &delayedSearchableProvider{
			delay:       delay,
			inFlight:    &inFlight,
			maxInFlight: &maxInFlight,
			items: []provider.SearchResult{
				{Vault: "v2", Path: "p2", Entry: provider.Entry{Title: "t2"}},
			},
		}
		p3 := &delayedSearchableProvider{
			delay:       delay,
			inFlight:    &inFlight,
			maxInFlight: &maxInFlight,
			items: []provider.SearchResult{
				{Vault: "v3", Path: "p3", Entry: provider.Entry{Title: "t3"}},
			},
		}

		orch.providerManager.vaultCache["v1"] = p1
		orch.providerManager.vaultCache["v2"] = p2
		orch.providerManager.vaultCache["v3"] = p3

		results, err := orch.Search(context.Background(), "", nil)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 3 {
			t.Fatalf("expected 3 results, got %d", len(results))
		}
		if maxInFlight.Load() < 2 {
			t.Errorf("multi-vault search executed sequentially: max in-flight = %d, expected >= 2", maxInFlight.Load())
		}
	})

	t.Run("ErrorPropagationAndCancellation", func(t *testing.T) {
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"v_ok":   {Provider: "env"},
				"v_fail": {Provider: "env"},
			},
		}
		orch, err := NewOrchestrator(cfg)
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}

		pOK := &delayedSearchableProvider{
			delay: 200 * time.Millisecond,
			items: []provider.SearchResult{
				{Vault: "v_ok", Path: "p_ok", Entry: provider.Entry{Title: "t_ok"}},
			},
		}
		pFail := &delayedSearchableProvider{
			delay: 10 * time.Millisecond,
			err:   errors.New("db decryption failure"),
		}
		orch.providerManager.vaultCache["v_ok"] = pOK
		orch.providerManager.vaultCache["v_fail"] = pFail

		_, err = orch.Search(context.Background(), "", nil)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), `failed to retrieve entries from repo "v_fail"`) {
			t.Errorf("unexpected error message: %v", err)
		}
		if !strings.Contains(err.Error(), "db decryption failure") {
			t.Errorf("expected underlying error in message, got: %v", err)
		}
	})
}

func TestBuiltinsNonSearchable(t *testing.T) {
	ctx := context.Background()

	t.Run("CollisionValidation", func(t *testing.T) {
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"env": {Provider: "keepass"},
			},
		}
		_, err := NewOrchestrator(cfg)
		if err == nil {
			t.Fatal("expected error when vault name conflicts with built-in scheme, got nil")
		}
		if !strings.Contains(err.Error(), "conflicts with built-in scheme") {
			t.Errorf("expected conflict error, got: %v", err)
		}

		cfgSearch := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"search": {Provider: "env"},
			},
		}
		_, err = NewOrchestrator(cfgSearch)
		if err == nil {
			t.Fatal("expected error when vault name conflicts with reserved scheme, got nil")
		}
		if !strings.Contains(err.Error(), "conflicts with reserved scheme") {
			t.Errorf("expected reserved scheme conflict error, got: %v", err)
		}
	})

	t.Run("SearchErrorOnBuiltins", func(t *testing.T) {
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{},
		}
		orch, err := NewOrchestrator(cfg)
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}
		_, err = orch.Search(ctx, `{{ eq .title "test" }}`, []string{"keyring"})
		if err == nil {
			t.Fatal("expected error searching keyring, got nil")
		}
		if !strings.Contains(err.Error(), "does not support searching") {
			t.Errorf("expected 'does not support searching' error, got: %v", err)
		}
	})
}

func TestSearchConcurrentCompilation(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"vault1": {Provider: "env"},
		},
	}
	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}
	if err := injectMockVault(orch, "vault1", []provider.SearchResult{
		{Provider: "keepass", Vault: "vault1", Path: "item1", Entry: provider.Entry{Title: "item1", Attributes: map[string]any{"Password": "pass"}}},
	}); err != nil {
		t.Fatalf("injectMockVault failed: %v", err)
	}

	const numGoroutines = 30
	var wg sync.WaitGroup
	wg.Add(numGoroutines)
	for range numGoroutines {
		go func() {
			defer wg.Done()
			results, err := orch.Search(ctx, `{{ eq .Password "pass" }}`, nil)
			if err != nil {
				t.Errorf("concurrent search failed: %v", err)
				return
			}
			if len(results) != 1 {
				t.Errorf("expected 1 result, got %d", len(results))
			}
		}()
	}
	wg.Wait()
}

func TestSearchNilConfig(t *testing.T) {
	orch, err := NewOrchestrator(nil)
	if err != nil {
		t.Fatalf("failed to create orchestrator with nil config: %v", err)
	}
	ctx := context.Background()

	_, err = orch.Search(ctx, `{{ eq .title "test" }}`, nil)
	if err == nil {
		t.Fatal("expected error searching with nil config, got nil")
	}
	if !strings.Contains(err.Error(), "no configuration loaded") {
		t.Errorf("expected 'no configuration loaded' error, got %v", err)
	}

	_, err = orch.Search(ctx, `{{ eq .title "test" }}`, []string{"keyring"})
	if err == nil {
		t.Fatal("expected error searching with nil config and builtin scope, got nil")
	}
	if !strings.Contains(err.Error(), "no configuration loaded") {
		t.Errorf("expected 'no configuration loaded' error, got %v", err)
	}
}

func TestSearcher_GroupAttributeFallback(t *testing.T) {
	keyring.MockInit()
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"grp": {Provider: "env"},
		},
	}
	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}
	if err := injectMockVault(orch, "grp", []provider.SearchResult{
		{Provider: "keepass", Vault: "grp", Path: "svc1", Entry: provider.Entry{Title: "svc1", Attributes: map[string]any{"Group": "Production", "Password": "p1"}}},
		{Provider: "keepass", Vault: "grp", Path: "svc2", Entry: provider.Entry{Title: "svc2", Attributes: map[string]any{"group": map[string]any{"name": "Staging", "path": "Infra/Staging"}, "Password": "p2"}}},
	}); err != nil {
		t.Fatalf("injectMockVault(grp) failed: %v", err)
	}

	ctx := context.Background()

	t.Run("GroupAttrCaseInsensitive_MatchesGroupName", func(t *testing.T) {
		results, err := orch.Search(ctx, `{{ eq .group.name "Production" }}`, nil)
		if err != nil {
			t.Fatalf("Search by group.name failed: %v", err)
		}
		if len(results) != 1 {
			t.Errorf("expected exactly 1 result for capitalized group attribute, got %d", len(results))
		}
	})

	t.Run("GroupAttrStructuredMap_MatchesPath", func(t *testing.T) {
		results, err := orch.Search(ctx, `{{ eq .group.path "Infra/Staging" }}`, nil)
		if err != nil {
			t.Fatalf("Search by group.path failed: %v", err)
		}
		if len(results) != 1 {
			t.Errorf("expected exactly 1 result for structured group attribute path, got %d", len(results))
		}
	})

	t.Run("SearchURIByGroupAttr", func(t *testing.T) {
		val, err := orch.Resolve(ctx, `{{ get "search://group=Production/Password" }}`)
		if err != nil {
			t.Fatalf("failed to resolve by group attribute: %v", err)
		}
		if val != "p1" {
			t.Errorf("expected 'p1', got %q", val)
		}
	})

	t.Run("RawGroupEquality_TypeMismatchErrors", func(t *testing.T) {
		_, err := orch.Search(ctx, `{{ eq .group "Production" }}`, nil)
		if err == nil {
			t.Fatal("expected error for raw group string equality after structured group change, got nil")
		}
		if !strings.Contains(err.Error(), "query execution failed") {
			t.Errorf("unexpected error for raw group string equality: %v", err)
		}
	})
}
