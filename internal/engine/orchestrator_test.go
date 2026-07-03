package engine

import (
	"context"
	"os"
	"path/filepath"
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
	os.Setenv("ORCH_TEST_USER", "env_user")
	defer os.Unsetenv("ORCH_TEST_USER")

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
		Providers: map[string]config.ProviderConfig{
			"my_repo": {
				Provider:     "yaml",
				DatabasePath: yamlPath,
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	ctx := context.Background()

	// 1. Test recursive resolution
	// my_repo://entries.ssh_prod.username should resolve to env://ORCH_TEST_USER which resolves to "env_user"
	val, err := orch.Resolve(ctx, "my_repo://entries.ssh_prod.username")
	if err != nil {
		t.Fatalf("failed to resolve: %v", err)
	}
	if val != "env_user" {
		t.Errorf("expected 'env_user', got %q", val)
	}

	// 2. Test GetEntry with recursive resolution inside attributes
	entry, err := orch.GetEntry(ctx, "my_repo://ssh_prod")
	if err != nil {
		t.Fatalf("failed to GetEntry: %v", err)
	}
	if entry.Attributes["username"] != "env_user" {
		t.Errorf("expected resolved username 'env_user', got %v", entry.Attributes["username"])
	}

	// 3. Test Search matching using expr
	// Match both tag "auth:ssh" and not tag "deprecated"
	results, err := orch.Search(ctx, `"auth:ssh" in tags and not ("deprecated" in tags)`, nil)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 matches (ssh_prod, ssh_minimal), got %d", len(results))
	}

	// Match query on attributes
	results, err = orch.Search(ctx, `bit_strength == 2048`, nil)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 match (ssh_staging), got %d", len(results))
	} else if results[0].Path != "ssh_staging" {
		t.Errorf("expected 'ssh_staging', got %q", results[0].Path)
	}

	// 4. Test search:// URI scheme in Resolve
	// Resolves Hostname or Password dynamically
	val, err = orch.Resolve(ctx, `search://tags=auth:ssh,env:prod/password`)
	if err != nil {
		t.Fatalf("failed to resolve search:// URI: %v", err)
	}
	if val != "my_raw_password" {
		t.Errorf("expected 'my_raw_password', got %q", val)
	}

	// 5. Test missing fields gracefulness: ssh_minimal does not have bit_strength,
	// so evaluating bit_strength > 3000 should fail evaluation on it but pass
	// overall and return ssh_prod.
	results, err = orch.Search(ctx, `bit_strength > 3000`, nil)
	if err != nil {
		t.Fatalf("Search with missing fields query failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 match, got %d", len(results))
	} else if results[0].Path != "ssh_prod" {
		t.Errorf("expected 'ssh_prod', got %q", results[0].Path)
	}
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
