package engine

import (
	"context"
	"errors"

	"github.com/warpcode/cloakenv/internal/provider"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
)

func TestOrchestratorRecursiveAndSearch(t *testing.T) {
	// Create temp dir
	tempDir, err := os.MkdirTemp("", "cloakenv-orch-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Set up environment variable for testing env:// resolution
	t.Setenv("ORCH_TEST_USER", "env_user")

	// Create entries.yaml
	// Here password maps to ${env://ORCH_TEST_USER}, demonstrating recursive resolution!
	yamlContent := `
entries:
  ssh_prod:
    tags:
      - auth:ssh
      - env:prod
    title: "Production SSH Key"
    username: ${env://ORCH_TEST_USER}
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
		// ${my_repo://entries.ssh_prod.username} should resolve to ${env://ORCH_TEST_USER} which resolves to "env_user"
		val, err := orch.Resolve(ctx, "${my_repo://entries.ssh_prod.username}")
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
		val, err := orch.Resolve(ctx, `${search://tags=auth:ssh,env:prod/password}`)
		if err != nil {
			t.Fatalf("failed to resolve search:// URI: %v", err)
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

func TestSearchURIEncoding(t *testing.T) {
	// Test parseSearchURI helper logic via a mock check
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
}

func TestOrchestratorVaultsAndSearch(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cloakenv-vaults-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

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
				Entities: map[string]map[string]any{
					"custom1": {
						"username": "custom_user",
						"Password": "custom_password",
					},
				},
			},
			"custom_single": {
				Provider: "custom_vault",
				Entities: map[string]map[string]any{
					"Static_Flat": {
						"db_user": "postgres",
						"db_port": 5432,
						"tags":    []any{"static", "local"},
					},
				},
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
				Entities: map[string]map[string]any{
					"secret_entry": {
						"Password": "hidden_pass",
					},
				},
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
		val, err := orch.Resolve(ctx, "${custom_static://custom1}")
		if err != nil || val != "custom_password" {
			t.Errorf("expected 'custom_password', got: %v (err: %v)", val, err)
		}

		// Custom static specific key
		val, err = orch.Resolve(ctx, "${custom_static://custom1:username}")
		if err != nil || val != "custom_user" {
			t.Errorf("expected 'custom_user', got: %v (err: %v)", val, err)
		}

		// Custom single — entity named "Static_Flat", access specific attribute
		val, err = orch.Resolve(ctx, "${custom_single://Static_Flat:db_user}")
		if err != nil || val != "postgres" {
			t.Errorf("expected 'postgres', got: %v (err: %v)", val, err)
		}

		// Flat file attributes
		val, err = orch.Resolve(ctx, "${flat_file://api_key}")
		if err != nil || val != "super_secret_api_token" {
			t.Errorf("expected 'super_secret_api_token', got: %v (err: %v)", val, err)
		}
	})

	// 3. Verify Serialization of structured values
	t.Run("ValueSerialization", func(t *testing.T) {
		// Metadata (should be serialized YAML)
		val, err := orch.Resolve(ctx, "${flat_file://metadata}")
		if err != nil {
			t.Fatalf("failed to resolve metadata: %v", err)
		}
		expectedYAML := "cluster: us-east-1\nowner: devops"
		if val != expectedYAML {
			t.Errorf("expected serialized metadata YAML, got %q", val)
		}

		// Servers (should be serialized YAML array)
		val, err = orch.Resolve(ctx, "${flat_file://servers}")
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
}

// TestResolveValues verifies that the resolve_values vault config flag
// gates URI resolution of attribute values.
func TestResolveValues(t *testing.T) {
	ctx := context.Background()

	// vault_b holds a plain secret value.
	// vault_a holds an attribute whose value is a URI pointing into vault_b.
	boolPtr := func(b bool) *bool { return &b }

	makeConfig := func(resolveValues bool) *config.Config {
		return &config.Config{
			Vaults: map[string]config.VaultConfig{
				"vault_a": {
					Provider:      "custom_vault",
					ResolveValues: resolveValues,
					Entities: map[string]map[string]any{
						"entity1": {
							"Password": "${vault_b://entry1:secret}",
						},
					},
				},
				"vault_b": {
					Provider:   "custom_vault",
					Searchable: boolPtr(true),
					Entities: map[string]map[string]any{
						"entry1": {
							"secret": "resolved_value",
						},
					},
				},
			},
		}
	}

	t.Run("ResolveValues_On", func(t *testing.T) {
		orch, err := NewOrchestrator(makeConfig(true))
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}

		val, err := orch.Resolve(ctx, "${vault_a://entity1:Password}")
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if val != "resolved_value" {
			t.Errorf("expected resolved_value, got %q", val)
		}
	})

	t.Run("ResolveValues_Off", func(t *testing.T) {
		orch, err := NewOrchestrator(makeConfig(false))
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}

		val, err := orch.Resolve(ctx, "${vault_a://entity1:Password}")
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		// Should return the raw URI string, not the resolved secret.
		if val != "${vault_b://entry1:secret}" {
			t.Errorf("expected raw URI, got %q", val)
		}
	})

	t.Run("ResolveValues_Off_GetEntry", func(t *testing.T) {
		// GetEntry must also honour resolve_values: false — attribute values
		// that are URIs must be returned raw without being resolved.
		orch, err := NewOrchestrator(makeConfig(false))
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}

		entry, err := orch.GetEntry(ctx, "vault_a://entity1")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		raw, ok := entry.Attributes["Password"]
		if !ok {
			t.Fatal("expected 'Password' attribute in entry")
		}
		if raw != "${vault_b://entry1:secret}" {
			t.Errorf("expected raw URI in attribute, got %q", raw)
		}
	})

	t.Run("ResolveValues_CircularReference", func(t *testing.T) {
		// vault_c.entry references vault_c itself — forms a cycle.
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"vault_c": {
					Provider:      "custom_vault",
					ResolveValues: true,
					Entities: map[string]map[string]any{
						"entry": {
							"Password": "${vault_c://entry:Password}",
						},
					},
				},
			},
		}

		orch, err := NewOrchestrator(cfg)
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}

		_, err = orch.Resolve(ctx, "${vault_c://entry:Password}")
		if err == nil {
			t.Error("expected error due to circular reference, got nil")
		}
		if !strings.Contains(err.Error(), "max depth") {
			t.Errorf("expected max depth error, got: %v", err)
		}
	})

	t.Run("ResolveValues_RejectedOnNonCustomVault", func(t *testing.T) {
		// resolve_values must be rejected at startup for non-custom_vault providers.
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"bad_yaml": {
					Provider:      "yaml",
					VaultPath:     "/tmp/irrelevant.yaml",
					ResolveValues: true,
				},
			},
		}

		_, err := NewOrchestrator(cfg)
		if err == nil {
			t.Fatal("expected error for resolve_values on yaml provider, got nil")
		}
		if !strings.Contains(err.Error(), "resolve_values") {
			t.Errorf("expected resolve_values error, got: %v", err)
		}
	})
}

// TestGetEntry_AttributeSelector verifies that GetEntry returns a synthetic
// single-key entry when the URI contains an :attr suffix, instead of the full
// entry from the provider.
func TestGetEntry_AttributeSelector(t *testing.T) {
	ctx := context.Background()

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"vault_x": {
				Provider: "custom_vault",
				Entities: map[string]map[string]any{
					"myentity": {
						"Password": "s3cr3t",
						"UserName": "alice",
						"Notes":    "some notes",
					},
				},
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	t.Run("single_attr_only", func(t *testing.T) {
		entry, err := orch.GetEntry(ctx, "vault_x://myentity:Password")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		if len(entry.Attributes) != 1 {
			t.Errorf("expected 1 attribute, got %d: %v", len(entry.Attributes), entry.Attributes)
		}
		val, ok := entry.Attributes["Password"]
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
		if len(entry.Attributes) != 3 {
			t.Errorf("expected 3 attributes, got %d: %v", len(entry.Attributes), entry.Attributes)
		}
	})
}

// TestBuiltinsNonSearchable verifies that built-in schemes (env, keyring, cache)
// are not searchable by default and cannot be registered as vault names.
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
	})

	t.Run("SearchErrorOnBuiltins", func(t *testing.T) {
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{},
		}

		orch, err := NewOrchestrator(cfg)
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}

		// Searching with "env" scope should fail and tell the user it doesn't support searching.
		_, err = orch.Search(ctx, `title == "test"`, []string{"env"})
		if err == nil {
			t.Fatal("expected error searching env, got nil")
		}
		if !strings.Contains(err.Error(), "does not support searching") {
			t.Errorf("expected 'does not support searching' error, got: %v", err)
		}

		// Searching with "keyring" scope should fail similarly.
		_, err = orch.Search(ctx, `title == "test"`, []string{"keyring"})
		if err == nil {
			t.Fatal("expected error searching keyring, got nil")
		}
		if !strings.Contains(err.Error(), "does not support searching") {
			t.Errorf("expected 'does not support searching' error, got: %v", err)
		}
	})
}

func TestOrchestratorBuildEnvMerges(t *testing.T) {
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"my_vault": {
				Provider: "custom_vault",
				Entities: map[string]map[string]any{
					"app_one": {
						"DB_USER": "user1",
						"DB_PASS": "pass1",
						"PORT":    "3000",
					},
					"app_two": {
						"PORT":    "5000",
						"DB_USER": "user2",
					},
				},
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}
	ctx := context.Background()

	t.Run("MergeMultipleSourcesAndOverrides", func(t *testing.T) {
		merges := []string{
			"my_vault://app_one",
			"my_vault://app_two",
		}

		// Result expectations:
		// app_one attributes: DB_USER=user1, DB_PASS=pass1, PORT=3000
		// app_two updates: PORT=5000, DB_USER=user2 (overwriting user1 and 3000)
		// Explicit: DB_PASS=explicit_pass
		explicit := map[string]string{
			"DB_PASS": "explicit_pass",
		}

		res, err := orch.BuildEnv(ctx, explicit, merges, nil)
		if err != nil {
			t.Fatalf("failed to build env: %v", err)
		}

		envMap := make(map[string]string)
		for _, item := range res {
			k, v, _ := strings.Cut(item, "=")
			envMap[k] = v
		}

		if envMap["DB_USER"] != "user2" {
			t.Errorf("expected DB_USER=user2, got %q", envMap["DB_USER"])
		}
		if envMap["DB_PASS"] != "explicit_pass" {
			t.Errorf("expected DB_PASS=explicit_pass (explicit override), got %q", envMap["DB_PASS"])
		}
		if envMap["PORT"] != "5000" {
			t.Errorf("expected PORT=5000 (app_two override), got %q", envMap["PORT"])
		}
	})

	t.Run("WhitelistFiltersMerges", func(t *testing.T) {
		merges := []string{
			"my_vault://app_one",
		}
		whitelist := []string{"DB_USER"}
		explicit := map[string]string{
			"DB_PASS": "explicit_pass", // Explicit is never filtered
		}

		res, err := orch.BuildEnv(ctx, explicit, merges, whitelist)
		if err != nil {
			t.Fatalf("failed to build env: %v", err)
		}

		envMap := make(map[string]string)
		for _, item := range res {
			k, v, _ := strings.Cut(item, "=")
			envMap[k] = v
		}

		if envMap["DB_USER"] != "user1" {
			t.Errorf("expected DB_USER=user1, got %q", envMap["DB_USER"])
		}
		if envMap["DB_PASS"] != "explicit_pass" {
			t.Errorf("expected DB_PASS=explicit_pass, got %q", envMap["DB_PASS"])
		}
		if _, exists := envMap["PORT"]; exists {
			t.Errorf("expected PORT to be filtered out by whitelist, but it exists")
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
				Provider: "custom_vault",
				Entities: map[string]map[string]any{
					"database": {
						"password": "supersecretpassword",
					},
				},
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	t.Run("Single expansion in a value", func(t *testing.T) {
		val, err := orch.Resolve(ctx, "${env://EXP_USER}")
		if err != nil {
			t.Fatalf("failed to resolve: %v", err)
		}
		if val != "jane" {
			t.Errorf("expected 'jane', got %q", val)
		}
	})

	t.Run("Multiple expansions in a single value", func(t *testing.T) {
		val, err := orch.Resolve(ctx, "${env://EXP_USER}-${env://EXP_HOST}")
		if err != nil {
			t.Fatalf("failed to resolve: %v", err)
		}
		if val != "jane-local" {
			t.Errorf("expected 'jane-local', got %q", val)
		}
	})

	t.Run("Expansion embedded in longer strings (URIs, file paths)", func(t *testing.T) {
		val, err := orch.Resolve(ctx, "mysql://${env://EXP_USER}:${vault_exp://database:password}@${env://EXP_HOST}:3306/db")
		if err != nil {
			t.Fatalf("failed to resolve: %v", err)
		}
		expected := "mysql://jane:supersecretpassword@local:3306/db"
		if val != expected {
			t.Errorf("expected %q, got %q", expected, val)
		}
	})

	t.Run("Escaped ${} remains literal", func(t *testing.T) {
		// $$ becomes literal $
		// $${env://EXP_USER} becomes literal ${env://EXP_USER}
		val, err := orch.Resolve(ctx, "$${env://EXP_USER}")
		if err != nil {
			t.Fatalf("failed to resolve: %v", err)
		}
		if val != "${env://EXP_USER}" {
			t.Errorf("expected '${env://EXP_USER}', got %q", val)
		}

		// my$$password becomes literal my$password
		val, err = orch.Resolve(ctx, "my$$password")
		if err != nil {
			t.Fatalf("failed to resolve: %v", err)
		}
		if val != "my$password" {
			t.Errorf("expected 'my$password', got %q", val)
		}
	})

	t.Run("Provider resolution errors surface useful messages", func(t *testing.T) {
		_, err := orch.ResolveWithKey(ctx, "mysql://${env://NON_EXISTENT_VAR_FOR_EXP}", "DATABASE_URI")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		expectedPart := "in configuration key \"DATABASE_URI\""
		if !strings.Contains(err.Error(), expectedPart) {
			t.Errorf("expected error message to contain %q, got %q", expectedPart, err.Error())
		}
		expectedExprPart := "failed to resolve expansion \"env://NON_EXISTENT_VAR_FOR_EXP\""
		if !strings.Contains(err.Error(), expectedExprPart) {
			t.Errorf("expected error message to contain %q, got %q", expectedExprPart, err.Error())
		}
	})

	t.Run("Nested expansions are disallowed", func(t *testing.T) {
		_, err := orch.Resolve(ctx, "${env://${EXP_USER}}")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		expectedPart := "nested expansions are not supported"
		if !strings.Contains(err.Error(), expectedPart) {
			t.Errorf("expected error message to contain %q, got %q", expectedPart, err.Error())
		}
	})
}

type failInitProvider struct{}

func (f *failInitProvider) Scheme() string { return "failinit" }
func (f *failInitProvider) Initialize(_ context.Context, _ provider.ProviderConfig) error {
	return errors.New("init failed")
}
func (f *failInitProvider) GetSecret(_ context.Context, _ string) (string, error) { return "", nil }
func (f *failInitProvider) SetSecret(_ context.Context, _, _ string) error        { return nil }
func (f *failInitProvider) DeleteSecret(_ context.Context, _ string) error        { return nil }
func (f *failInitProvider) Validate(_ map[string]string) error                    { return nil }
func (f *failInitProvider) Search(_ context.Context, _ provider.SearchQuery) ([]provider.SearchResult, error) {
	return nil, nil
}
func (f *failInitProvider) GetEntry(_ context.Context, _ string) (provider.Entry, error) {
	return provider.Entry{}, nil
}

func TestGetEntry(t *testing.T) {
	ctx := context.Background()

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"vault_x": {
				Provider: "custom_vault",
				Entities: map[string]map[string]any{
					"myentity": {
						"Password":  "s3cr3t",
						"recursive": "vault_x://myentity",
					},
					"error_attr": {
						"Password": "${vault_missing://nonexistent}",
					},
				},
				ResolveValues: true,
			},
			"vault_y": {
				Provider: "custom_vault",
				Entities: map[string]map[string]any{
					"target": {
						"Password": "pass",
					},
					"no_resolve": {
						"Password": "vault_y://target",
					},
				},
				ResolveValues: false,
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	orch.builtins["failinit"] = &failInitProvider{}

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
			uri:     "keyring://foo",
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
				entry, err = orch.getEntryRecursive(ctx, tt.uri, tt.depth)
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
				if len(entry.Attributes) != len(tt.wantAttrs) {
					t.Errorf("expected %d attributes, got %d: %v", len(tt.wantAttrs), len(entry.Attributes), entry.Attributes)
				}
				for k, v := range tt.wantAttrs {
					if entry.Attributes[k] != v {
						t.Errorf("expected attribute %q to be %q, got %q", k, v, entry.Attributes[k])
					}
				}
			}
		})
	}
}

func TestMatchCommand(t *testing.T) {
	tests := []struct {
		name        string
		ruleCommand string
		cmdArgs     []string
		wantMatch   bool
	}{
		{
			name:        "empty rule or args",
			ruleCommand: "",
			cmdArgs:     []string{"aws"},
			wantMatch:   false,
		},
		{
			name:        "nil cmdArgs",
			ruleCommand: "aws",
			cmdArgs:     nil,
			wantMatch:   false,
		},
		{
			name:        "exact executable name",
			ruleCommand: "aws",
			cmdArgs:     []string{"aws", "s3", "ls"},
			wantMatch:   true,
		},
		{
			name:        "full executable path basename",
			ruleCommand: "aws",
			cmdArgs:     []string{"/usr/local/bin/aws", "ec2", "describe-instances"},
			wantMatch:   true,
		},
		{
			name:        "exact path match",
			ruleCommand: "/usr/bin/python3",
			cmdArgs:     []string{"/usr/bin/python3", "script.py"},
			wantMatch:   true,
		},
		{
			name:        "case insensitive executable match",
			ruleCommand: "AWS",
			cmdArgs:     []string{"aws", "s3"},
			wantMatch:   true,
		},
		{
			name:        "subcommand prefix match",
			ruleCommand: "git push",
			cmdArgs:     []string{"git", "push", "origin", "main"},
			wantMatch:   true,
		},
		{
			name:        "subcommand prefix mismatch",
			ruleCommand: "git push",
			cmdArgs:     []string{"git", "status"},
			wantMatch:   false,
		},
		{
			name:        "glob pattern executable match",
			ruleCommand: "kubectl*",
			cmdArgs:     []string{"kubectl-prod", "get", "pods"},
			wantMatch:   true,
		},
		{
			name:        "glob pattern script match",
			ruleCommand: "*.sh",
			cmdArgs:     []string{"./deploy.sh", "staging"},
			wantMatch:   true,
		},
		{
			name:        "glob pattern full command match",
			ruleCommand: "npm run *",
			cmdArgs:     []string{"npm", "run", "build"},
			wantMatch:   true,
		},
		{
			name:        "non matching executable",
			ruleCommand: "terraform",
			cmdArgs:     []string{"helm", "install"},
			wantMatch:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchCommand(tt.ruleCommand, tt.cmdArgs)
			if got != tt.wantMatch {
				t.Errorf("MatchCommand(%q, %v) = %v, want %v", tt.ruleCommand, tt.cmdArgs, got, tt.wantMatch)
			}
		})
	}
}

func TestBuildEnvForCommand_Autoload(t *testing.T) {
	ctx := context.Background()
	t.Setenv("TEST_REGION", "us-east-1")

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"aws_dev": {
				Provider:     "custom_vault",
				SingleEntity: boolPtr(true),
				Attributes: map[string]any{
					"AWS_ACCESS_KEY_ID":     "AKIA1111",
					"AWS_SECRET_ACCESS_KEY": "secret1111",
					"EXTRA_VAR":             "should_be_filtered",
				},
			},
			"k8s_vault": {
				Provider: "custom_vault",
				Entities: map[string]map[string]any{
					"staging": {
						"KUBECONFIG": "/path/to/staging.conf",
					},
				},
			},
		},
		Autoload: []config.AutoloadRule{
			{
				Match:  "aws",
				Vaults: []string{"aws_dev"},
				Env: map[string]string{
					"AWS_DEFAULT_REGION": "env://TEST_REGION",
				},
				Whitelist: []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_DEFAULT_REGION"},
			},
			{
				Match: "kubectl*",
				Merge: []string{"k8s_vault://staging"},
				Env: map[string]string{
					"K8S_ENV": "staging",
				},
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	t.Run("Matching command autoloads vaults, env, and applies whitelist", func(t *testing.T) {
		cmdArgs := []string{"aws", "s3", "ls"}
		_, res, err := orch.BuildEnvForCommand(ctx, cmdArgs, nil, nil, nil)
		if err != nil {
			t.Fatalf("failed to build env: %v", err)
		}

		envMap := make(map[string]string)
		for _, item := range res {
			k, v, _ := strings.Cut(item, "=")
			envMap[k] = v
		}

		if envMap["AWS_ACCESS_KEY_ID"] != "AKIA1111" {
			t.Errorf("expected AWS_ACCESS_KEY_ID=AKIA1111, got %q", envMap["AWS_ACCESS_KEY_ID"])
		}
		if envMap["AWS_SECRET_ACCESS_KEY"] != "secret1111" {
			t.Errorf("expected AWS_SECRET_ACCESS_KEY=secret1111, got %q", envMap["AWS_SECRET_ACCESS_KEY"])
		}
		if envMap["AWS_DEFAULT_REGION"] != "us-east-1" {
			t.Errorf("expected AWS_DEFAULT_REGION=us-east-1, got %q", envMap["AWS_DEFAULT_REGION"])
		}
		if _, exists := envMap["EXTRA_VAR"]; exists {
			t.Errorf("expected EXTRA_VAR to be filtered out by autoload whitelist")
		}
	})

	t.Run("CLI explicit flag overrides autoload env", func(t *testing.T) {
		cmdArgs := []string{"aws", "s3", "ls"}
		explicit := map[string]string{
			"AWS_DEFAULT_REGION": "us-west-2",
		}
		_, res, err := orch.BuildEnvForCommand(ctx, cmdArgs, explicit, nil, nil)
		if err != nil {
			t.Fatalf("failed to build env: %v", err)
		}

		envMap := make(map[string]string)
		for _, item := range res {
			k, v, _ := strings.Cut(item, "=")
			envMap[k] = v
		}

		if envMap["AWS_DEFAULT_REGION"] != "us-west-2" {
			t.Errorf("expected CLI explicit flag us-west-2 to override autoload, got %q", envMap["AWS_DEFAULT_REGION"])
		}
	})

	t.Run("Matching glob command autoloads merge URI and env", func(t *testing.T) {
		cmdArgs := []string{"kubectl-prod", "get", "pods"}
		_, res, err := orch.BuildEnvForCommand(ctx, cmdArgs, nil, nil, nil)
		if err != nil {
			t.Fatalf("failed to build env: %v", err)
		}

		envMap := make(map[string]string)
		for _, item := range res {
			k, v, _ := strings.Cut(item, "=")
			envMap[k] = v
		}

		if envMap["KUBECONFIG"] != "/path/to/staging.conf" {
			t.Errorf("expected KUBECONFIG=/path/to/staging.conf, got %q", envMap["KUBECONFIG"])
		}
		if envMap["K8S_ENV"] != "staging" {
			t.Errorf("expected K8S_ENV=staging, got %q", envMap["K8S_ENV"])
		}
	})

	t.Run("Non matching command does not apply autoload rules", func(t *testing.T) {
		cmdArgs := []string{"helm", "install"}
		_, res, err := orch.BuildEnvForCommand(ctx, cmdArgs, nil, nil, nil)
		if err != nil {
			t.Fatalf("failed to build env: %v", err)
		}

		envMap := make(map[string]string)
		for _, item := range res {
			k, v, _ := strings.Cut(item, "=")
			envMap[k] = v
		}

		if _, exists := envMap["AWS_ACCESS_KEY_ID"]; exists {
			t.Errorf("did not expect AWS_ACCESS_KEY_ID for helm command")
		}
		if _, exists := envMap["KUBECONFIG"]; exists {
			t.Errorf("did not expect KUBECONFIG for helm command")
		}
	})

	t.Run("Regex match and command transformation substitution", func(t *testing.T) {
		regexCfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"litellm_vault": {
					Provider:     "custom_vault",
					SingleEntity: boolPtr(true),
					Attributes: map[string]any{
						"LITELLM_KEY": "sk-12345",
					},
				},
			},
			Autoload: []config.AutoloadRule{
				{
					Match:   `^litellm\s+(.*)$`,
					Command: `uvx --with 'litellm[proxy]' --with 'fastapi<0.116' litellm \1`,
					Vaults:  []string{"litellm_vault"},
				},
			},
		}

		orchRegex, err := NewOrchestrator(regexCfg)
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}

		cmdArgs := []string{"litellm", "--config", "~/.config/litellm/config.yaml"}
		newCmdArgs, res, err := orchRegex.BuildEnvForCommand(ctx, cmdArgs, nil, nil, nil)
		if err != nil {
			t.Fatalf("failed to build env for command: %v", err)
		}

		expectedArgs := []string{"uvx", "--with", "litellm[proxy]", "--with", "fastapi<0.116", "litellm", "--config", "~/.config/litellm/config.yaml"}
		if len(newCmdArgs) != len(expectedArgs) {
			t.Fatalf("expected %d args, got %d (%v)", len(expectedArgs), len(newCmdArgs), newCmdArgs)
		}
		for idx, arg := range newCmdArgs {
			if arg != expectedArgs[idx] {
				t.Errorf("arg[%d]: expected %q, got %q", idx, expectedArgs[idx], arg)
			}
		}

		envMap := make(map[string]string)
		for _, item := range res {
			k, v, _ := strings.Cut(item, "=")
			envMap[k] = v
		}
		if envMap["LITELLM_KEY"] != "sk-12345" {
			t.Errorf("expected LITELLM_KEY=sk-12345, got %q", envMap["LITELLM_KEY"])
		}
	})

	t.Run("Validation rejects autoload rule with empty match", func(t *testing.T) {
		invalidCfg := &config.Config{
			Autoload: []config.AutoloadRule{
				{Match: "", Command: "echo 1"},
			},
		}
		_, err := NewOrchestrator(invalidCfg)
		if err == nil {
			t.Fatal("expected error for empty autoload match, got nil")
		}
	})
}

func TestSplitCommand(t *testing.T) {
	tests := []struct {
		input   string
		want    []string
		wantErr bool
	}{
		{
			input:   "uvx --with 'litellm[proxy]' --with 'fastapi<0.116' litellm --config config.yaml",
			want:    []string{"uvx", "--with", "litellm[proxy]", "--with", "fastapi<0.116", "litellm", "--config", "config.yaml"},
			wantErr: false,
		},
		{
			input:   `echo "hello world" 'foo bar'`,
			want:    []string{"echo", "hello world", "foo bar"},
			wantErr: false,
		},
		{
			input:   `C:\Users\runner\AppData\Local\Temp\tool.exe arg1 arg2`,
			want:    []string{`C:\Users\runner\AppData\Local\Temp\tool.exe`, "arg1", "arg2"},
			wantErr: false,
		},
		{
			input:   `"C:\Program Files\Tool\tool.exe" --flag "value"`,
			want:    []string{`C:\Program Files\Tool\tool.exe`, "--flag", "value"},
			wantErr: false,
		},
		{
			input:   `tool\ name arg`,
			want:    []string{"tool name", "arg"},
			wantErr: false,
		},
		{
			input:   `cmd 'unclosed quote`,
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := splitCommand(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("splitCommand(%q) err = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr {
				if len(got) != len(tt.want) {
					t.Fatalf("splitCommand(%q) len = %d, want %d", tt.input, len(got), len(tt.want))
				}
				for i := range got {
					if got[i] != tt.want[i] {
						t.Errorf("token %d = %q, want %q", i, got[i], tt.want[i])
					}
				}
			}
		})
	}
}

func boolPtr(b bool) *bool {
	return &b
}
