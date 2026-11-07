package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/expr-lang/expr/ast"
	"github.com/zalando/go-keyring"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
)

func TestOrchestratorRecursiveAndSearch(t *testing.T) {
	// Create temp dir
	tempDir := t.TempDir()

	// Set up environment variable for testing env:// resolution
	t.Setenv("ORCH_TEST_USER", "env_user")

	// Create entries.yaml
	// Here username maps to {{ get "env://ORCH_TEST_USER" }}, demonstrating recursive resolution!
	yamlContent := `
entries:
  ssh_prod:
    tags:
      - auth:ssh
      - env:prod
    title: "Production SSH Key"
    username: '{{ get "env://ORCH_TEST_USER" }}'
    password: "my_raw_password"
    bit_strength: 4096
  ssh_staging:
    tags:
      - auth:ssh
      - env:staging
      - deprecated
    title: "Staging SSH Key"
    username: "stage_user"
    password: "stage_password"
    bit_strength: 2048
  ssh_minimal:
    tags:
      - auth:ssh
    title: "Minimal Key"
`
	yamlPath := filepath.Join(tempDir, "entries.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write temp yaml file: %v", err)
	}

	// Create Orchestrator with mock config
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"my_repo": {
				Provider:  "yaml",
				VaultPath: yamlPath,
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	ctx := context.Background()

	t.Run("RecursiveResolution", func(t *testing.T) {
		// 1. Test recursive resolution
		// {{ get "my_repo://entries.ssh_prod.username" }} should resolve to {{ get "env://ORCH_TEST_USER" }} which resolves to "env_user"
		val, err := orch.Resolve(ctx, `{{ get "my_repo://entries.ssh_prod.username" }}`)
		if err != nil {
			t.Fatalf("failed to resolve: %v", err)
		}
		if val != "env_user" {
			t.Errorf("expected 'env_user', got %q", val)
		}
	})

	t.Run("GetEntry", func(t *testing.T) {
		// 2. Test GetEntry with recursive resolution inside attributes
		entry, err := orch.GetEntry(ctx, "my_repo://ssh_prod")
		if err != nil {
			t.Fatalf("failed to GetEntry: %v", err)
		}
		if entry.Attributes["username"] != "env_user" {
			t.Errorf("expected resolved username 'env_user', got %v", entry.Attributes["username"])
		}
	})

	t.Run("SearchExprByTag", func(t *testing.T) {
		// 3. Test Search matching using expr
		// Match both tag "auth:ssh" and not tag "deprecated"
		results, err := orch.Search(ctx, `"auth:ssh" in tags and not ("deprecated" in tags)`, nil)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 2 {
			t.Errorf("expected 2 matches (ssh_prod, ssh_minimal), got %d", len(results))
		}
	})

	t.Run("SearchExprByAttribute", func(t *testing.T) {
		// Match query on attributes
		results, err := orch.Search(ctx, `bit_strength == 2048`, nil)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 1 {
			t.Errorf("expected 1 match (ssh_staging), got %d", len(results))
		} else if results[0].Path != "ssh_staging" {
			t.Errorf("expected 'ssh_staging', got %q", results[0].Path)
		}
	})

	t.Run("SearchURI", func(t *testing.T) {
		// 4. Test search:// URI scheme in Resolve
		// Resolves Hostname or Password dynamically
		val, err := orch.Resolve(ctx, `{{ get "search://tags=auth:ssh,env:prod/password" }}`)
		if err != nil {
			t.Fatalf("failed to resolve search:// URI: %v", err)
		}
		if val != "my_raw_password" {
			t.Errorf("expected 'my_raw_password', got %q", val)
		}
	})

	t.Run("SearchExprByMetaTags", func(t *testing.T) {
		results, err := orch.Search(ctx, `"auth:ssh" in _meta_.tags and not ("deprecated" in _meta_.tags)`, nil)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 2 {
			t.Errorf("expected 2 matches using _meta_.tags, got %d", len(results))
		}
	})

	t.Run("SearchExprByMetaProviderAndVault", func(t *testing.T) {
		results, err := orch.Search(ctx, `_meta_.provider == "yaml" and _meta_.vault == "my_repo" and "env:prod" in _meta_.tags`, nil)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 1 {
			t.Errorf("expected 1 match using _meta_.provider and _meta_.vault, got %d", len(results))
		} else if results[0].Path != "ssh_prod" {
			t.Errorf("expected 'ssh_prod', got %q", results[0].Path)
		}
	})

	t.Run("SearchURIMetaTags", func(t *testing.T) {
		val, err := orch.Resolve(ctx, `{{ get "search://_meta_.tags=auth:ssh,env:prod/password" }}`)
		if err != nil {
			t.Fatalf("failed to resolve search://_meta_.tags URI: %v", err)
		}
		if val != "my_raw_password" {
			t.Errorf("expected 'my_raw_password', got %q", val)
		}
	})
	t.Run("MissingFields", func(t *testing.T) {
		// 5. Test missing fields gracefulness: ssh_minimal does not have bit_strength,
		// so evaluating bit_strength > 3000 should fail evaluation on it but pass
		// overall and return ssh_prod.
		results, err := orch.Search(ctx, `bit_strength > 3000`, nil)
		if err != nil {
			t.Fatalf("Search with missing fields query failed: %v", err)
		}
		if len(results) != 1 {
			t.Errorf("expected 1 match, got %d", len(results))
		} else if results[0].Path != "ssh_prod" {
			t.Errorf("expected 'ssh_prod', got %q", results[0].Path)
		}
	})

	t.Run("SecurityValidation", func(t *testing.T) {
		// 6. Test security validation: disallow function calls and method calls
		_, err := orch.Search(ctx, `print(tags)`, nil)
		if err == nil || !strings.Contains(err.Error(), "function calls are not allowed") {
			t.Errorf("expected error about function calls, got: %v", err)
		}

		_, err = orch.Search(ctx, `title.ToUpper() == "TEST"`, nil)
		if err == nil || !strings.Contains(err.Error(), "method calls are not allowed") {
			t.Errorf("expected error about method calls, got: %v", err)
		}
	})
}

type delayedSearchableProvider struct {
	delay       time.Duration
	err         error
	items       []provider.SearchResult
	inFlight    *atomic.Int64
	maxInFlight *atomic.Int64
}

func (d *delayedSearchableProvider) Scheme() string { return "delayed" }
func (d *delayedSearchableProvider) Initialize(ctx context.Context, cfg provider.ProviderConfig) error {
	return nil
}
func (d *delayedSearchableProvider) GetSecret(ctx context.Context, location string) (string, error) {
	return "", nil
}
func (d *delayedSearchableProvider) SetSecret(ctx context.Context, location, value string) error {
	return nil
}
func (d *delayedSearchableProvider) DeleteSecret(ctx context.Context, location string) error {
	return nil
}
func (d *delayedSearchableProvider) Validate(settings map[string]string) error {
	return nil
}
func (d *delayedSearchableProvider) Search(ctx context.Context, query provider.SearchQuery) ([]provider.SearchResult, error) {
	if d.inFlight != nil && d.maxInFlight != nil {
		current := d.inFlight.Add(1)
		defer d.inFlight.Add(-1)

		for {
			max := d.maxInFlight.Load()
			if current <= max {
				break
			}
			if d.maxInFlight.CompareAndSwap(max, current) {
				break
			}
		}
	}

	select {
	case <-time.After(d.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if d.err != nil {
		return nil, d.err
	}
	return d.items, nil
}
func (d *delayedSearchableProvider) GetEntry(ctx context.Context, location string) (provider.Entry, error) {
	return provider.Entry{}, nil
}

func TestSearcher_MultiVaultConcurrencyAndError(t *testing.T) {
	t.Run("ConcurrentMultiVaultExecution", func(t *testing.T) {
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"v1": {Provider: "custom_vault"},
				"v2": {Provider: "custom_vault"},
				"v3": {Provider: "custom_vault"},
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
				"v_ok":   {Provider: "custom_vault"},
				"v_fail": {Provider: "custom_vault"},
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
		if !strings.Contains(err.Error(), "failed to retrieve entries from repo \"v_fail\"") {
			t.Errorf("unexpected error message: %v", err)
		}
		if !strings.Contains(err.Error(), "db decryption failure") {
			t.Errorf("expected underlying error in message, got: %v", err)
		}
	})
}

func TestSearchURIEncoding(t *testing.T) {
	t.Run("Valid", func(t *testing.T) {
		exprQuery, attr, err := parseSearchURI("tags=auth:ssh,env:prod&title=bastion/Password")
		if err != nil {
			t.Fatalf("failed to parse: %v", err)
		}
		expectedExpr := `"auth:ssh" in tags and "env:prod" in tags and title contains "bastion"`
		if exprQuery != expectedExpr {
			t.Errorf("expected query %q, got %q", expectedExpr, exprQuery)
		}
		if attr != "Password" {
			t.Errorf("expected attribute 'Password', got %q", attr)
		}
	})

	t.Run("DynamicAttributes", func(t *testing.T) {
		exprQuery, attr, err := parseSearchURI("username=admin&url=https://example.com/api_key")
		if err != nil {
			t.Fatalf("failed to parse: %v", err)
		}
		expectedExpr := `username == "admin" and url == "https://example.com"`
		if exprQuery != expectedExpr {
			t.Errorf("expected query %q, got %q", expectedExpr, exprQuery)
		}
		if attr != "api_key" {
			t.Errorf("expected attribute 'api_key', got %q", attr)
		}
	})

	t.Run("GroupParameter", func(t *testing.T) {
		exprQuery, attr, err := parseSearchURI("group=Production&username=admin/Password")
		if err != nil {
			t.Fatalf("failed to parse: %v", err)
		}
		expectedExpr := `(group.name == "Production" or group.path == "Production") and username == "admin"`
		if exprQuery != expectedExpr {
			t.Errorf("expected query %q, got %q", expectedExpr, exprQuery)
		}
		if attr != "Password" {
			t.Errorf("expected attribute 'Password', got %q", attr)
		}
	})

	t.Run("MemberAttributes", func(t *testing.T) {
		exprQuery, attr, err := parseSearchURI("_meta_.vault=testdb&group.name=website/token")
		if err != nil {
			t.Fatalf("failed to parse: %v", err)
		}
		expectedExpr := `_meta_.vault == "testdb" and group.name == "website"`
		if exprQuery != expectedExpr {
			t.Errorf("expected query %q, got %q", expectedExpr, exprQuery)
		}
		if attr != "token" {
			t.Errorf("expected attribute 'token', got %q", attr)
		}
	})

	t.Run("DefaultPasswordAttribute", func(t *testing.T) {
		exprQuery, attr, err := parseSearchURI("group=Production&username=admin")
		if err != nil {
			t.Fatalf("failed to parse: %v", err)
		}
		expectedExpr := `(group.name == "Production" or group.path == "Production") and username == "admin"`
		if exprQuery != expectedExpr {
			t.Errorf("expected query %q, got %q", expectedExpr, exprQuery)
		}
		if attr != "Password" {
			t.Errorf("expected attribute 'Password', got %q", attr)
		}
	})

	t.Run("InvalidParamKeys", func(t *testing.T) {
		invalidCases := []string{
			"invalid key=value/Password",
			"bad(key)=value/Password",
			"123key=value/Password",
			"key..dot=value/Password",
			"a-b=value/Password",
			".leading=value/Password",
			"trailing.=value/Password",
		}
		for _, uri := range invalidCases {
			_, _, err := parseSearchURI(uri)
			if err == nil {
				t.Errorf("expected error for invalid key in %q, got nil", uri)
			}
			if !strings.Contains(err.Error(), "invalid search parameter") {
				t.Errorf("unexpected error message for %q: %v", uri, err)
			}
		}
	})

	t.Run("EmptyConditions", func(t *testing.T) {
		emptyCases := []string{
			"/Password",
			"title=&username=/Password",
			"=&tags=/Password",
		}
		for _, uri := range emptyCases {
			_, _, err := parseSearchURI(uri)
			if err == nil {
				t.Errorf("expected error for empty conditions in %q, got nil", uri)
			}
			if !strings.Contains(err.Error(), "no query conditions specified") {
				t.Errorf("unexpected error message for %q: %v", uri, err)
			}
		}
	})
}

func TestOrchestratorVaultsAndSearch(t *testing.T) {
	tempDir := t.TempDir()

	// Write single_db.yaml content
	singleDbContent := `
title: "My Flat Secrets Database"
tags: [env:prod, local]
api_key: "super_secret_api_token"
port: 8080
metadata:
  owner: "devops"
  cluster: "us-east-1"
servers:
  - "bastion.example.com"
  - "app.example.com"
`
	singleDbPath := filepath.Join(tempDir, "single_db.yaml")
	if err := os.WriteFile(singleDbPath, []byte(singleDbContent), 0644); err != nil {
		t.Fatalf("failed to write single db file: %v", err)
	}

	isTrue := true
	isFalse := false

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"custom_static": {
				Provider: "custom_vault",
				Entities: config.ToCustomEntityMap(map[string]map[string]any{
					"custom1": {
						"username": "custom_user",
						"Password": "custom_password",
					},
				}),
			},
			"custom_single": {
				Provider: "custom_vault",
				Entities: config.ToCustomEntityMap(map[string]map[string]any{
					"Static_Flat": {
						"db_user": "postgres",
						"db_port": 5432,
						"tags":    []any{"static", "local"},
					},
				}),
			},
			"flat_file": {
				Provider:     "yaml",
				VaultPath:    singleDbPath,
				SingleEntity: &isTrue,
				EntityName:   "Prod Flat File",
				Tags:         []string{"flat", "prod"},
			},
			"non_searchable": {
				Provider:   "custom_vault",
				Searchable: &isFalse,
				Entities: config.ToCustomEntityMap(map[string]map[string]any{
					"secret_entry": {
						"Password": "hidden_pass",
					},
				}),
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	ctx := context.Background()

	// 1. Verify CheckAccess
	t.Run("CheckAccess", func(t *testing.T) {
		for _, vaultName := range []string{"custom_static", "custom_single", "flat_file", "non_searchable"} {
			if err := orch.CheckAccess(ctx, vaultName); err != nil {
				t.Errorf("expected access to vault %q, got error: %v", vaultName, err)
			}
		}

		if err := orch.CheckAccess(ctx, "non_existent"); err == nil {
			t.Error("expected error for non_existent vault access, got nil")
		}
	})

	// 2. Verify Retrievals
	t.Run("RetrieveSecrets", func(t *testing.T) {
		// Custom static default Password
		val, err := orch.Resolve(ctx, `{{ get "custom_static://custom1" }}`)
		if err != nil || val != "custom_password" {
			t.Errorf("expected 'custom_password', got: %v (err: %v)", val, err)
		}

		// Custom static specific key
		val, err = orch.Resolve(ctx, `{{ get "custom_static://custom1:username" }}`)
		if err != nil || val != "custom_user" {
			t.Errorf("expected 'custom_user', got: %v (err: %v)", val, err)
		}

		// Custom single — entity named "Static_Flat", access specific attribute
		val, err = orch.Resolve(ctx, `{{ get "custom_single://Static_Flat:db_user" }}`)
		if err != nil || val != "postgres" {
			t.Errorf("expected 'postgres', got: %v (err: %v)", val, err)
		}

		// Flat file attributes
		val, err = orch.Resolve(ctx, `{{ get "flat_file://api_key" }}`)
		if err != nil || val != "super_secret_api_token" {
			t.Errorf("expected 'super_secret_api_token', got: %v (err: %v)", val, err)
		}
	})

	// 3. Verify Serialization of structured values
	t.Run("ValueSerialization", func(t *testing.T) {
		// Metadata (should be serialized YAML)
		val, err := orch.Resolve(ctx, `{{ get "flat_file://metadata" }}`)
		if err != nil {
			t.Fatalf("failed to resolve metadata: %v", err)
		}
		expectedYAML := "cluster: us-east-1\nowner: devops"
		if val != expectedYAML {
			t.Errorf("expected serialized metadata YAML, got %q", val)
		}

		// Servers (should be serialized YAML array)
		val, err = orch.Resolve(ctx, `{{ get "flat_file://servers" }}`)
		if err != nil {
			t.Fatalf("failed to resolve servers: %v", err)
		}
		expectedArrayYAML := "- bastion.example.com\n- app.example.com"
		if val != expectedArrayYAML {
			t.Errorf("expected serialized servers YAML, got %q", val)
		}
	})

	// 4. Verify Entry Structure and Searches
	t.Run("EntryShowAndSearch", func(t *testing.T) {
		// GetEntry for single entity flat file (ignores location, returns single entry)
		entry, err := orch.GetEntry(ctx, "flat_file://")
		if err != nil {
			t.Fatalf("failed to GetEntry: %v", err)
		}
		if entry.Title != "Prod Flat File" {
			t.Errorf("expected title 'Prod Flat File', got %q", entry.Title)
		}

		// Search title substring (matches flat_file's "Prod Flat File" and custom_single's "Static_Flat")
		results, err := orch.Search(ctx, `title contains "Flat"`, nil)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 2 {
			t.Errorf("expected 2 results (flat_file, custom_single/Static_Flat), got %d", len(results))
		}

		// Search tag membership
		results, err = orch.Search(ctx, `"local" in tags`, nil)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 1 || results[0].Vault != "custom_single" {
			t.Errorf("expected 1 result from custom_single, got %v", results)
		}

		// Search excluded non-searchable vault
		results, err = orch.Search(ctx, `Password == "hidden_pass"`, nil)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("expected 0 results, got %d", len(results))
		}

		// Scoped search on non-searchable vault (should return error)
		_, err = orch.Search(ctx, `Password == "hidden_pass"`, []string{"non_searchable"})
		if err == nil || !strings.Contains(err.Error(), "is not searchable") {
			t.Errorf("expected searchable error, got: %v", err)
		}
	})

	// 5. Verify dynamic search:// resolution with arbitrary entry attributes
	t.Run("SearchURISchemeWithArbitraryAttributes", func(t *testing.T) {
		val, err := orch.Resolve(ctx, `{{ get "search://username=custom_user/Password" }}`)
		if err != nil || val != "custom_password" {
			t.Errorf("expected 'custom_password', got: %v (err: %v)", val, err)
		}

		val, err = orch.Resolve(ctx, `{{ get "search://db_user=postgres/db_port" }}`)
		if err != nil || val != "5432" {
			t.Errorf("expected '5432', got: %v (err: %v)", val, err)
		}

		val, err = orch.Resolve(ctx, `{{ get "search://port=8080/api_key" }}`)
		if err != nil || val != "super_secret_api_token" {
			t.Errorf("expected 'super_secret_api_token', got: %v (err: %v)", val, err)
		}

		val, err = orch.Resolve(ctx, `{{ get "search://tags=local&db_user=postgres/db_port" }}`)
		if err != nil || val != "5432" {
			t.Errorf("expected '5432', got: %v (err: %v)", val, err)
		}

		_, err = orch.Resolve(ctx, `{{ get "search://username=nonexistent/Password" }}`)
		if err == nil || !strings.Contains(err.Error(), "no secrets found matching search query") {
			t.Errorf("expected no secrets found error, got: %v", err)
		}
	})
}

func TestBuiltinsNonSearchable(t *testing.T) {
	ctx := context.Background()

	t.Run("CollisionValidation", func(t *testing.T) {
		// Vault configuration trying to use a built-in name "env"
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"env": {
					Provider: "custom_vault",
				},
			},
		}

		_, err := NewOrchestrator(cfg)
		if err == nil {
			t.Fatal("expected error when vault name conflicts with built-in scheme, got nil")
		}
		if !strings.Contains(err.Error(), "conflicts with built-in scheme") {
			t.Errorf("expected conflict error, got: %v", err)
		}

		// Vault configuration trying to use reserved scheme "search"
		cfgSearch := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"search": {
					Provider: "custom_vault",
				},
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

		// Searching with "keyring" scope should fail as keyring does not support searching.
		_, err = orch.Search(ctx, `title == "test"`, []string{"keyring"})
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
			"vault1": {
				Provider: "custom_vault",
				Entities: config.ToCustomEntityMap(map[string]map[string]any{
					"item1": {
						"Password": "pass",
					},
				}),
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	const numGoroutines = 30
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for range numGoroutines {
		go func() {
			defer wg.Done()
			results, err := orch.Search(ctx, `Password == "pass"`, nil)
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

type dummyNode struct {
	ast.Node
}

func TestVisitorVisit(t *testing.T) {
	t.Run("ExistingErrorShortCircuit", func(t *testing.T) {
		origErr := fmt.Errorf("existing error")
		vErr := origErr
		v := &visitor{err: &vErr}

		var callNode ast.Node = &ast.CallNode{}
		v.Visit(&callNode)

		if !errors.Is(vErr, origErr) {
			t.Errorf("expected vErr to remain %v, got %v", origErr, vErr)
		}
	})

	t.Run("AllowedNodes", func(t *testing.T) {
		allowedNodes := []struct {
			name string
			node ast.Node
		}{
			{"IdentifierNode", &ast.IdentifierNode{}},
			{"IntegerNode", &ast.IntegerNode{}},
			{"FloatNode", &ast.FloatNode{}},
			{"BoolNode", &ast.BoolNode{}},
			{"StringNode", &ast.StringNode{}},
			{"BytesNode", &ast.BytesNode{}},
			{"ConstantNode", &ast.ConstantNode{}},
			{"UnaryNode", &ast.UnaryNode{}},
			{"BinaryNode", &ast.BinaryNode{}},
			{"ArrayNode", &ast.ArrayNode{}},
			{"MapNode", &ast.MapNode{}},
			{"PairNode", &ast.PairNode{}},
			{"NilNode", &ast.NilNode{}},
			{"SliceNode", &ast.SliceNode{}},
			{"BuiltinNode", &ast.BuiltinNode{}},
			{"PredicateNode", &ast.PredicateNode{}},
			{"PointerNode", &ast.PointerNode{}},
			{"ConditionalNode", &ast.ConditionalNode{}},
			{"ChainNode", &ast.ChainNode{}},
			{"VariableDeclaratorNode", &ast.VariableDeclaratorNode{}},
			{"SequenceNode", &ast.SequenceNode{}},
			{"MemberNodeNonMethod", &ast.MemberNode{Method: false}},
		}

		for _, tc := range allowedNodes {
			t.Run(tc.name, func(t *testing.T) {
				var vErr error
				v := &visitor{err: &vErr}
				v.Visit(&tc.node)
				if vErr != nil {
					t.Errorf("expected no error for safe node type %s, got: %v", tc.name, vErr)
				}
			})
		}
	})

	t.Run("DisallowedNodes", func(t *testing.T) {
		disallowedNodes := []struct {
			name        string
			node        ast.Node
			errContains string
		}{
			{
				name:        "CallNode",
				node:        &ast.CallNode{},
				errContains: "function calls are not allowed in search expressions",
			},
			{
				name:        "MemberNodeMethod",
				node:        &ast.MemberNode{Method: true},
				errContains: "method calls are not allowed in search expressions",
			},
			{
				name:        "UnsupportedNode",
				node:        &dummyNode{},
				errContains: "expression node type *engine.dummyNode is not allowed",
			},
		}

		for _, tc := range disallowedNodes {
			t.Run(tc.name, func(t *testing.T) {
				var vErr error
				v := &visitor{err: &vErr}
				v.Visit(&tc.node)
				if vErr == nil {
					t.Fatalf("expected error for node %s, got nil", tc.name)
				}
				if !strings.Contains(vErr.Error(), tc.errContains) {
					t.Errorf("expected error containing %q, got: %v", tc.errContains, vErr)
				}
			})
		}
	})
}

func TestValidateExpression(t *testing.T) {
	tests := []struct {
		name        string
		expr        string
		wantErr     bool
		errContains string
	}{
		{
			name:    "ValidExpression",
			expr:    `title == "test" and "tag1" in tags`,
			wantErr: false,
		},
		{
			name:        "ParseError",
			expr:        `1 +`,
			wantErr:     true,
			errContains: "failed to parse expression",
		},
		{
			name:        "ForbiddenFunctionCall",
			expr:        `foo()`,
			wantErr:     true,
			errContains: "function calls are not allowed in search expressions",
		},
		{
			name:        "ForbiddenMethodCall",
			expr:        `title.Upper()`,
			wantErr:     true,
			errContains: "method calls are not allowed in search expressions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExpression(tt.expr)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateExpression(%q) error = %v, wantErr = %v", tt.expr, err, tt.wantErr)
			}
			if tt.wantErr && !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("expected error containing %q, got: %v", tt.errContains, err)
			}
		})
	}
}

func TestSearchNilConfig(t *testing.T) {
	orch, err := NewOrchestrator(nil)
	if err != nil {
		t.Fatalf("failed to create orchestrator with nil config: %v", err)
	}

	ctx := context.Background()
	_, err = orch.Search(ctx, `title == "test"`, nil)
	if err == nil {
		t.Fatal("expected error searching with nil config, got nil")
	}
	if !strings.Contains(err.Error(), "no configuration loaded") {
		t.Errorf("expected 'no configuration loaded' error, got %v", err)
	}

	_, err = orch.Search(ctx, `title == "test"`, []string{"keyring"})
	if err == nil {
		t.Fatal("expected error searching with nil config and builtin scope, got nil")
	}
	if !strings.Contains(err.Error(), "no configuration loaded") {
		t.Errorf("expected 'no configuration loaded' error, got %v", err)
	}
}

func TestSearcher_InferredRoot_PreResolutionExclusion(t *testing.T) {
	tempDir := t.TempDir()

	// YAML file has "entities" root key, but EntitiesRootKey will be omitted from VaultConfig.
	// staticProvider infers entitiesRootKey = "entities".
	// The entry has a secret URI reference that must never be dereferenced when excluded.
	yamlContent := `
entities:
  api:
    title: "api_service"
    username: "api_user"
    secret_ref: '{{ get "tripwire://must_not_be_called" }}'
`
	yamlPath := filepath.Join(tempDir, "inferred_root.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write yaml fixture: %v", err)
	}

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"yaml_vault": {
				Provider:  "yaml",
				VaultPath: yamlPath,
				// EntitiesRootKey intentionally omitted to test inferred root handling
			},
			"virtual_search": {
				Provider:      "search",
				SourceVaults:  []string{"yaml_vault"},
				ResolveValues: true,
				ExcludeFields: []string{"entities.*.secret_ref"},
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	tw := &tripwireProvider{}
	orch.providerManager.vaultCache["tripwire"] = tw

	ctx := context.Background()

	// With exclude_fields: ["entities.*.secret_ref"] on virtual_search, the inferred root key
	// "entities" must be passed to ApplyToEntry before resolution, causing secret_ref to be stripped
	// so the tripwire provider is never called.
	results, err := orch.Search(ctx, `title == "api_service"`, []string{"virtual_search"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	entry := results[0].Entry
	if _, ok := entry.Attributes["secret_ref"]; ok {
		t.Errorf("expected secret_ref to be excluded before resolution, but it was present: %v", entry.Attributes["secret_ref"])
	}
	if entry.Attributes["username"] != "api_user" {
		t.Errorf("expected username 'api_user', got %v", entry.Attributes["username"])
	}
	if tw.called {
		t.Errorf("pre-resolution isolation violated during search: tripwire was dereferenced for excluded field")
	}

	// Also verify that GetEntry on virtual_search strips the field before dynamic attribute resolution
	entryRes, err := orch.GetEntry(ctx, "virtual_search://api")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if _, ok := entryRes.Attributes["secret_ref"]; ok {
		t.Errorf("expected secret_ref to be excluded in GetEntry, but it was present: %v", entryRes.Attributes["secret_ref"])
	}
	if tw.called {
		t.Errorf("pre-resolution isolation violated during GetEntry: tripwire was dereferenced for excluded field")
	}
}

func TestSearcher_OverlappingIncludeLayers(t *testing.T) {
	entry := provider.Entry{
		Title: "test_entry",
		Attributes: map[string]any{
			"env:PROD":  "prod_val",
			"env:DEV":   "dev_val",
			"app:PROD":  "app_val",
			"something": "other",
		},
	}

	// Layer 1: include_fields: ["env:*"]
	// Layer 2: include_fields: ["*:PROD"]
	ctx := provider.WithFieldPolicy(context.Background(), []string{"env:*"}, nil)
	ctx = provider.WithFieldPolicy(ctx, []string{"*:PROD"}, nil)

	fp := provider.FieldPolicyFromContext(ctx)
	if fp == nil {
		t.Fatal("expected non-nil FieldPolicy")
	}

	filtered := fp.ApplyToEntry(entry, "test_entry", nil)

	// "env:PROD" matches both layers and must be kept.
	if filtered.Attributes["env:PROD"] != "prod_val" {
		t.Errorf("expected env:PROD to be kept, got: %v", filtered.Attributes["env:PROD"])
	}
	// "env:DEV" fails layer 2 ("*:PROD") and must be excluded.
	if _, ok := filtered.Attributes["env:DEV"]; ok {
		t.Errorf("expected env:DEV to be excluded by layer 2")
	}
	// "app:PROD" fails layer 1 ("env:*") and must be excluded.
	if _, ok := filtered.Attributes["app:PROD"]; ok {
		t.Errorf("expected app:PROD to be excluded by layer 1")
	}
	// "something" fails both layers and must be excluded.
	if _, ok := filtered.Attributes["something"]; ok {
		t.Errorf("expected something to be excluded")
	}
}

func TestSearcher_UUIDAndGroupExpression(t *testing.T) {
	keyring.MockInit()
	if err := keyring.Set("cloakenv", "provider/testdb", "password123"); err != nil {
		t.Fatalf("failed to set mock credentials: %v", err)
	}

	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CACHE_HOME", tempDir)
	t.Setenv("LocalAppData", tempDir)

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"testdb": {
				Provider:  "keepass",
				VaultPath: "../../testdata/testDB.kdbx",
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	ctx := context.Background()

	t.Run("SearchByUUID", func(t *testing.T) {
		results, err := orch.Search(ctx, `uuid != ""`, nil)
		if err != nil {
			t.Fatalf("Search by uuid failed: %v", err)
		}
		if len(results) == 0 {
			t.Errorf("expected results matching uuid != \"\", got 0")
		}
	})

	t.Run("SearchByGroup", func(t *testing.T) {
		results, err := orch.Search(ctx, `group.name == "website"`, nil)
		if err != nil {
			t.Fatalf("Search by group.name failed: %v", err)
		}
		if len(results) == 0 {
			t.Errorf("expected results matching group.name == \"website\", got 0")
		}
	})

	t.Run("SearchURIByGroup", func(t *testing.T) {
		val, err := orch.Resolve(ctx, `{{ get "search://group=website/Password" }}`)
		if err != nil {
			t.Fatalf("failed to resolve by group: %v", err)
		}
		if val != "testPassword123!" {
			t.Errorf("expected 'testPassword123!', got %q", val)
		}
	})

	t.Run("SearchURIByGroupAndUsername", func(t *testing.T) {
		val, err := orch.Resolve(ctx, `{{ get "search://group=website&username=user@email.com/Password" }}`)
		if err != nil {
			t.Fatalf("failed to resolve by group and username: %v", err)
		}
		if val != "testPassword123!" {
			t.Errorf("expected 'testPassword123!', got %q", val)
		}
	})

	t.Run("SearchURIByUsernameAttachment", func(t *testing.T) {
		val, err := orch.Resolve(ctx, `{{ get "search://username=user@email.com/hello.txt" }}`)
		if err != nil {
			t.Fatalf("failed to resolve attachment by username: %v", err)
		}
		if val != "Hello, World! Extraction Test Successful." {
			t.Errorf("expected attachment content, got %q", val)
		}
	})

	t.Run("SearchURIByGroupPath", func(t *testing.T) {
		val, err := orch.Resolve(ctx, `{{ get "search://group.path=website/Password" }}`)
		if err != nil {
			t.Fatalf("failed to resolve by group.path: %v", err)
		}
		if val != "testPassword123!" {
			t.Errorf("expected 'testPassword123!', got %q", val)
		}
	})
}
