package provider

import (
	"context"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestKeePassProvider(t *testing.T) {
	keyring.MockInit()

	kp := NewKeePassProvider()
	ctx := context.Background()

	if kp.Scheme() != "keepass" {
		t.Errorf("expected scheme 'keepass', got %q", kp.Scheme())
	}

	// Validate check
	if err := kp.Validate(map[string]string{"database_path": "a"}); err != nil {
		t.Errorf("expected validation success, got %v", err)
	}
	if err := kp.Validate(nil); err == nil {
		t.Errorf("expected validation failure for nil settings")
	}

	// Initialize before keyring setup should fail
	cfg := ProviderConfig{
		Settings: map[string]string{
			"database_path": "../../testdata/testDB.kdbx",
			"remote_name":   "testdb",
		},
	}
	if err := kp.Initialize(ctx, cfg); err == nil {
		t.Errorf("expected Initialize to fail without stored keyring credentials")
	}

	// Setup keyring credentials for remote testdb
	// prefix: cloakenv, account: provider/testdb, password: password123
	if err := keyring.Set("cloakenv", "provider/testdb", "password123"); err != nil {
		t.Fatalf("failed to set mock credentials: %v", err)
	}

	// Initialize should now succeed
	if err := kp.Initialize(ctx, cfg); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Test credentials deletion in memory
	if kp.db.Credentials != nil {
		t.Errorf("expected kp.db.Credentials to be nil after unlock")
	}

	// Test GetSecret for default password (no attribute suffix)
	val, err := kp.GetSecret(ctx, "website/Test Website")
	if err != nil {
		t.Errorf("GetSecret default failed: %v", err)
	}
	if val != "testPassword123!" {
		t.Errorf("expected 'testPassword123!', got %q", val)
	}

	// Test GetSecret with UserName attribute
	val, err = kp.GetSecret(ctx, "website/Test Website:UserName")
	if err != nil {
		t.Errorf("GetSecret UserName failed: %v", err)
	}
	if val != "user@email.com" {
		t.Errorf("expected 'user@email.com', got %q", val)
	}

	// Test GetSecret with file attachment attribute
	val, err = kp.GetSecret(ctx, "website/Test Website:hello.txt")
	if err != nil {
		t.Errorf("GetSecret attachment failed: %v", err)
	}
	if val != "Hello, World! Extraction Test Successful." {
		t.Errorf("expected attachment content, got %q", val)
	}

	// Test path resolution with Root prefix (fallback path skip)
	val, err = kp.GetSecret(ctx, "Root/website/Test Website")
	if err != nil {
		t.Errorf("GetSecret with Root prefix failed: %v", err)
	}
	if val != "testPassword123!" {
		t.Errorf("expected 'testPassword123!', got %q", val)
	}

	// Test GetEntry
	entry, err := kp.GetEntry(ctx, "website/Test Website")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if entry.Title != "Test Website" {
		t.Errorf("expected title 'Test Website', got %q", entry.Title)
	}

	// Test Search (verify path doesn't contain Root/)
	results, err := kp.Search(ctx, SearchQuery{Title: "Test Website"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Path != "website/Test Website" {
		t.Errorf("expected path 'website/Test Website', got %q", results[0].Path)
	}

	// Test read-only constraints
	if err := kp.SetSecret(ctx, "a", "b"); err == nil {
		t.Errorf("expected SetSecret to fail")
	}
	if err := kp.DeleteSecret(ctx, "a"); err == nil {
		t.Errorf("expected DeleteSecret to fail")
	}
}
