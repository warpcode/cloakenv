package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
)

func TestGetEntry_AttributeSelector(t *testing.T) {
	ctx := context.Background()

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"vault_x": {
				Provider: "env",
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}
	orch.providerManager.vaultCache["vault_x"] = newTestMemoryProvider(map[string]provider.Entry{
		"myentity": {
			"Password": "s3cr3t",
			"UserName": "alice",
			"Notes":    "some notes",
		},
	})

	t.Run("single_attr_only", func(t *testing.T) {
		entry, err := orch.GetEntry(ctx, "vault_x://myentity:Password")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		if len(entry) != 1 {
			t.Errorf("expected 1 attribute, got %d: %v", len(entry), entry)
		}
		val, ok := entry["Password"]
		if !ok {
			t.Fatal("expected 'Password' key in synthetic entry")
		}
		if val != "s3cr3t" {
			t.Errorf("expected s3cr3t, got %q", val)
		}
	})

	t.Run("full_entry_without_attr", func(t *testing.T) {
		entry, err := orch.GetEntry(ctx, "vault_x://myentity")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		attrCount := 0
		for k := range entry {
			if !isReservedMetadataKey(k) {
				attrCount++
			}
		}
		if attrCount != 3 {
			t.Errorf("expected 3 attributes, got %d: %v", attrCount, entry)
		}
	})
}

func TestResolveLiteralValues(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{}
	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	tests := []struct {
		input string
		want  string
	}{
		{"just_a_string", "just_a_string"},
		{"12345", "12345"},
		{"https://example.com/api", "https://example.com/api"},
		{"some-other-scheme://foo", "some-other-scheme://foo"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := orch.Resolve(ctx, tt.input)
			if err != nil {
				t.Fatalf("Resolve failed: %v", err)
			}
			if got != tt.want {
				t.Errorf("Resolve(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestOrchestratorExpansion(t *testing.T) {
	ctx := context.Background()
	t.Setenv("EXP_USER", "jane")
	t.Setenv("EXP_HOST", "local")

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"vault_exp": {
				Provider: "env",
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}
	orch.providerManager.vaultCache["vault_exp"] = newTestMemoryProvider(map[string]provider.Entry{
		"database": {
			"password": "supersecretpassword",
		},
	})

	t.Run("Single expansion in a value", func(t *testing.T) {
		val, err := orch.Resolve(ctx, `{{ get "env://EXP_USER" }}`)
		if err != nil {
			t.Fatalf("failed to resolve: %v", err)
		}
		if val != "jane" {
			t.Errorf("expected 'jane', got %q", val)
		}
	})

	t.Run("Multiple expansions in a single value", func(t *testing.T) {
		val, err := orch.Resolve(ctx, `{{ get "env://EXP_USER" }}-{{ get "env://EXP_HOST" }}`)
		if err != nil {
			t.Fatalf("failed to resolve: %v", err)
		}
		if val != "jane-local" {
			t.Errorf("expected 'jane-local', got %q", val)
		}
	})

	t.Run("Expansion embedded in longer strings (URIs, file paths)", func(t *testing.T) {
		val, err := orch.Resolve(ctx, `mysql://{{ get "env://EXP_USER" }}:{{ get "vault_exp://database:password" }}@{{ get "env://EXP_HOST" }}:3306/db`)
		if err != nil {
			t.Fatalf("failed to resolve: %v", err)
		}
		expected := "mysql://jane:supersecretpassword@local:3306/db"
		if val != expected {
			t.Errorf("expected %q, got %q", expected, val)
		}
	})

	t.Run("Escaped {{ remains literal", func(t *testing.T) {
		val, err := orch.Resolve(ctx, `{{ "{{" }} get "env://EXP_USER" }}`)
		if err != nil {
			t.Fatalf("failed to resolve: %v", err)
		}
		if val != `{{ get "env://EXP_USER" }}` {
			t.Errorf("expected '{{ get \"env://EXP_USER\" }}', got %q", val)
		}

		// my$$password becomes literal my$$password (since $$ is no longer special)
		val, err = orch.Resolve(ctx, "my$$password")
		if err != nil {
			t.Fatalf("failed to resolve: %v", err)
		}
		if val != "my$$password" {
			t.Errorf("expected 'my$$password', got %q", val)
		}
	})

	t.Run("Provider resolution errors surface useful messages", func(t *testing.T) {
		_, err := orch.ResolveWithKey(ctx, `mysql://{{ get "env://NON_EXISTENT_VAR_FOR_EXP" }}`, "DATABASE_URI")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		expectedPart := "in configuration key \"DATABASE_URI\""
		if !strings.Contains(err.Error(), expectedPart) {
			t.Errorf("expected error message to contain %q, got %q", expectedPart, err.Error())
		}
		expectedExprPart := "env://NON_EXISTENT_VAR_FOR_EXP"
		if !strings.Contains(err.Error(), expectedExprPart) {
			t.Errorf("expected error message to contain %q, got %q", expectedExprPart, err.Error())
		}
	})

	t.Run("Nested expansions syntax error", func(t *testing.T) {
		_, err := orch.Resolve(ctx, `{{ get "env://{{ get \"EXP_USER\" }}" }}`)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestGetEntry(t *testing.T) {
	ctx := context.Background()

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"vault_x": {Provider: "env"},
			"vault_y": {Provider: "env"},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}
	orch.providerManager.vaultCache["vault_x"] = newTestMemoryProvider(map[string]provider.Entry{
		"myentity": {
			"Password":  "s3cr3t",
			"recursive": "vault_x://myentity",
		},
		"error_attr": {
			"Password": `{{ get "vault_missing://nonexistent" }}`,
		},
		"recursive_attr": {
			"Password": `{{ get "vault_x://recursive_attr:Password" }}`,
		},
	})
	orch.providerManager.vaultCache["vault_y"] = newTestMemoryProvider(map[string]provider.Entry{
		"target": {
			"Password": "pass",
		},
		"no_resolve": {
			"Password": "vault_y://target",
		},
	})

	orch.providerManager.builtins["failinit"] = &failInitProvider{}

	tests := []struct {
		name      string
		uri       string
		depth     int
		wantErr   string
		wantAttrs map[string]string
	}{
		{
			name:    "depth_exceeded",
			uri:     "vault_x://myentity",
			depth:   6,
			wantErr: "infinite secret resolution recursion detected",
		},
		{
			name:    "invalid_uri",
			uri:     "://invalid",
			wantErr: "malformed URI",
		},
		{
			name:    "attribute_selector_resolve_error",
			uri:     "vault_missing://myentity:Password",
			wantErr: "unknown scheme",
		},
		{
			name:    "get_vault_provider_error",
			uri:     "nonexistent_vault://myentity",
			wantErr: "unknown scheme or vault",
		},
		{
			name:    "searchable_get_entry_error",
			uri:     "vault_x://nonexistent_entity",
			wantErr: "not found",
		},
		{
			name:    "resolve_attr_recursive_error",
			uri:     "vault_x://error_attr",
			wantErr: "failed to resolve attribute",
		},
		{
			name:    "provider_not_searchable",
			uri:     "cache://foo",
			wantErr: "does not support structured entries",
		},
		{
			name:    "ensure_initialized_error",
			uri:     "failinit://foo",
			wantErr: "init failed",
		},
		{
			name:      "attribute_selector_success",
			uri:       "vault_y://target:Password",
			wantAttrs: map[string]string{"Password": "pass"},
		},
		{
			name:    "attribute_selector_recursion_detected",
			uri:     "vault_x://recursive_attr:Password",
			wantErr: "infinite secret resolution recursion detected",
		},
		{
			name:    "attribute_selector_with_initial_depth",
			uri:     "vault_y://target:Password",
			depth:   6,
			wantErr: "infinite secret resolution recursion detected",
		},
		{
			name:      "no_resolve_values",
			uri:       "vault_y://no_resolve",
			wantAttrs: map[string]string{"Password": "vault_y://target"},
		},
		{
			name:      "full_entry_success",
			uri:       "vault_x://myentity",
			wantAttrs: map[string]string{"Password": "s3cr3t", "recursive": "vault_x://myentity"}, // recursive doesn't loop infinitely unless actually resolved deeper?
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var entry provider.Entry
			var err error

			if tt.depth > 0 {
				entry, err = orch.resolver.GetEntryRecursive(ctx, tt.uri, tt.depth)
			} else {
				entry, err = orch.GetEntry(ctx, tt.uri)
			}

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantAttrs != nil {
				gotAttrs := make(map[string]any)
				for k, v := range entry {
					if !isReservedMetadataKey(k) {
						gotAttrs[k] = v
					}
				}
				if len(gotAttrs) != len(tt.wantAttrs) {
					t.Errorf("expected %d attributes, got %d: %v", len(tt.wantAttrs), len(gotAttrs), gotAttrs)
				}
				for k, v := range tt.wantAttrs {
					if gotAttrs[k] != v {
						t.Errorf("expected attribute %q to be %q, got %q", k, v, gotAttrs[k])
					}
				}
			}
		})
	}
}

func TestResolve_BareURIQuoteEscaping(t *testing.T) {
	ctx := context.Background()
	pm := NewProviderManager(&config.Config{}, nil, nil)
	sem := make(chan struct{}, 10)
	res := NewResolver(pm, sem)

	var receivedQuery string
	res.SetSearchFunc(func(ctx context.Context, expressionStr string, sourceVaults []string, depth int) ([]SearchResult, error) {
		receivedQuery = expressionStr
		return []SearchResult{
			{
				Entry: provider.Entry{
					"title":    "Matched Entry",
					"Password": "super_secret_password",
				},
			},
		}, nil
	})

	// Test a bare URI with quotes directly embedded in the URI string (e.g. search://"api" in tags/Password or custom vault URI)
	customVal, err := res.Resolve(ctx, `search://tags="api"/Password`)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if customVal != "super_secret_password" {
		t.Errorf("got %q, want %q", customVal, "super_secret_password")
	}
	if receivedQuery != `tags="api"` {
		t.Errorf("expected received query to be \"\\\"api\\\"\" in tags, got %q", receivedQuery)
	}

	val, err := res.Resolve(ctx, `search://title=api/Password`)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if val != "super_secret_password" {
		t.Errorf("got %q, want %q", val, "super_secret_password")
	}
	if receivedQuery != "title=api" {
		t.Errorf("expected received query to contain %q, got %q", `title contains "api"`, receivedQuery)
	}

	// Test Expr "in" operator syntax
	exprInVal, err := res.Resolve(ctx, `search://{{ hasTag "api" .tags }}/Password`)
	if err != nil {
		t.Fatalf("Resolve failed with Expr 'in' query: %v", err)
	}
	if exprInVal != "super_secret_password" {
		t.Errorf("got %q, want %q", exprInVal, "super_secret_password")
	}
	if receivedQuery != `{{ hasTag "api" .tags }}` {
		t.Errorf("expected received query %q, got %q", `"api" in tags`, receivedQuery)
	}

	// Test Expr "==" operator syntax
	exprEqVal, err := res.Resolve(ctx, `search://{{ eq .title "api" }}/Password`)
	if err != nil {
		t.Fatalf("Resolve failed with Expr '==' query: %v", err)
	}
	if exprEqVal != "super_secret_password" {
		t.Errorf("got %q, want %q", exprEqVal, "super_secret_password")
	}
	if receivedQuery != `{{ eq .title "api" }}` {
		t.Errorf("expected received query %q, got %q", `title == "api"`, receivedQuery)
	}

	// Test Expr syntax without attribute, defaulting to Password
	exprDefaultVal, err := res.Resolve(ctx, `search://{{ hasTag "api" .tags }}`)
	if err != nil {
		t.Fatalf("Resolve failed with Expr query without attribute: %v", err)
	}
	if exprDefaultVal != "super_secret_password" {
		t.Errorf("got %q, want %q", exprDefaultVal, "super_secret_password")
	}
	if receivedQuery != `{{ hasTag "api" .tags }}` {
		t.Errorf("expected received query %q, got %q", `"api" in tags`, receivedQuery)
	}

	// Test GetEntries with search:// in both URL parameter and Expr syntax
	entriesURL, err := res.GetEntries(ctx, `search://tags=api`)
	if err != nil {
		t.Fatalf("GetEntries failed with search URL params: %v", err)
	}
	if len(entriesURL) != 1 || entriesURL[0].Title() != "Matched Entry" {
		t.Errorf("unexpected entries from search URL params: %v", entriesURL)
	}

	entriesExpr, err := res.GetEntries(ctx, `search://{{ hasTag "api" .tags }}`)
	if err != nil {
		t.Fatalf("GetEntries failed with search Expr: %v", err)
	}
	if len(entriesExpr) != 1 || entriesExpr[0].Title() != "Matched Entry" {
		t.Errorf("unexpected entries from search Expr: %v", entriesExpr)
	}
}

func TestResolver_GetEntries_And_GetEntriesRecursive(t *testing.T) {
	keyring.MockInit()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())

	ctx := context.Background()

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"custom_v": {
				Provider: "env",
			},
		},
	}

	pm := NewProviderManager(cfg, nil, nil)
	pm.vaultCache["custom_v"] = newTestMemoryProvider(map[string]provider.Entry{
		"app": {
			"title":  "App Entry",
			"path":   "app",
			"tags":   []string{"prod"},
			"apiKey": "12345",
			"secret": "s3cr3t",
		},
	})
	sem := make(chan struct{}, 10)
	res := NewResolver(pm, sem)

	tests := []struct {
		name       string
		uri        string
		depth      int
		useRecurse bool
		wantCount  int
		wantErr    bool
		wantErrSub string
		checkFn    func(t *testing.T, entries []provider.Entry)
	}{
		{
			name:       "GetEntries valid URI",
			uri:        "custom_v://app",
			useRecurse: false,
			wantCount:  1,
			wantErr:    false,
			checkFn: func(t *testing.T, entries []provider.Entry) {
				if entries[0].Title() != "App Entry" {
					t.Errorf("Title = %q, want 'App Entry'", entries[0].Title())
				}
				if entries[0]["apiKey"] != "12345" {
					t.Errorf("apiKey = %v, want '12345'", entries[0]["apiKey"])
				}
			},
		},
		{
			name:       "GetEntriesRecursive at depth 0 succeeds",
			uri:        "custom_v://app",
			depth:      0,
			useRecurse: true,
			wantCount:  1,
			wantErr:    false,
			checkFn: func(t *testing.T, entries []provider.Entry) {
				if entries[0].Title() != "App Entry" {
					t.Errorf("Title = %q, want 'App Entry'", entries[0].Title())
				}
			},
		},
		{
			name:       "GetEntriesRecursive at depth 6 triggers max depth error",
			uri:        "custom_v://app",
			depth:      6,
			useRecurse: true,
			wantErr:    true,
			wantErrSub: "reached max depth 5",
		},
		{
			name:       "GetEntries with attribute selector returns single attribute",
			uri:        "custom_v://app:apiKey",
			useRecurse: false,
			wantCount:  1,
			wantErr:    false,
			checkFn: func(t *testing.T, entries []provider.Entry) {
				if entries[0]["apiKey"] != "12345" {
					t.Errorf("apiKey = %v, want '12345'", entries[0]["apiKey"])
				}
				if _, ok := entries[0]["secret"]; ok {
					t.Errorf("expected only apiKey in attribute selector, but secret is present")
				}
			},
		},
		{
			name:       "GetEntries for unknown vault returns error",
			uri:        "unknown_vault://app",
			useRecurse: false,
			wantErr:    true,
			wantErrSub: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var entries []provider.Entry
			var err error
			if tt.useRecurse {
				entries, err = res.GetEntriesRecursive(ctx, tt.uri, tt.depth)
			} else {
				entries, err = res.GetEntries(ctx, tt.uri)
			}

			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr {
				if tt.wantErrSub != "" && !strings.Contains(err.Error(), tt.wantErrSub) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErrSub)
				}
			} else {
				if len(entries) != tt.wantCount {
					t.Fatalf("entries count = %d, want %d", len(entries), tt.wantCount)
				}
				if tt.checkFn != nil {
					tt.checkFn(t, entries)
				}
			}
		})
	}
}
