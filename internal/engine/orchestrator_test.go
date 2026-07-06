package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cloakenv/internal/config"
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
	// Here password maps to env://ORCH_TEST_USER, demonstrating recursive resolution!
	yamlContent := `
entries:
  ssh_prod:
    tags:
      - auth:ssh
      - env:prod
    title: "Production SSH Key"
    username: env://ORCH_TEST_USER
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
		// my_repo://entries.ssh_prod.username should resolve to env://ORCH_TEST_USER which resolves to "env_user"
		val, err := orch.Resolve(ctx, "my_repo://entries.ssh_prod.username")
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
		val, err := orch.Resolve(ctx, `search://tags=auth:ssh,env:prod/password`)
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
				Provider:  "yaml",
				VaultPath: singleDbPath,
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
		val, err := orch.Resolve(ctx, "custom_static://custom1")
		if err != nil || val != "custom_password" {
			t.Errorf("expected 'custom_password', got: %v (err: %v)", val, err)
		}

		// Custom static specific key
		val, err = orch.Resolve(ctx, "custom_static://custom1:username")
		if err != nil || val != "custom_user" {
			t.Errorf("expected 'custom_user', got: %v (err: %v)", val, err)
		}

		// Custom single — entity named "Static_Flat", access specific attribute
		val, err = orch.Resolve(ctx, "custom_single://Static_Flat:db_user")
		if err != nil || val != "postgres" {
			t.Errorf("expected 'postgres', got: %v (err: %v)", val, err)
		}

		// Flat file attributes
		val, err = orch.Resolve(ctx, "flat_file://api_key")
		if err != nil || val != "super_secret_api_token" {
			t.Errorf("expected 'super_secret_api_token', got: %v (err: %v)", val, err)
		}
	})

	// 3. Verify Serialization of structured values
	t.Run("ValueSerialization", func(t *testing.T) {
		// Metadata (should be serialized YAML)
		val, err := orch.Resolve(ctx, "flat_file://metadata")
		if err != nil {
			t.Fatalf("failed to resolve metadata: %v", err)
		}
		expectedYAML := "cluster: us-east-1\nowner: devops"
		if val != expectedYAML {
			t.Errorf("expected serialized metadata YAML, got %q", val)
		}

		// Servers (should be serialized YAML array)
		val, err = orch.Resolve(ctx, "flat_file://servers")
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
