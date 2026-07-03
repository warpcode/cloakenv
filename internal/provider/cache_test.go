package provider

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

func TestCacheProvider(t *testing.T) {
	keyring.MockInit()

	tempDir, err := os.MkdirTemp("", "cloakenv-cache-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cp := NewCacheProvider()
	ctx := context.Background()

	if cp.Scheme() != "cache" {
		t.Errorf("expected scheme 'cache', got %q", cp.Scheme())
	}

	if err := cp.Validate(nil); err != nil {
		t.Errorf("Validate failed: %v", err)
	}

	// Get before initialization should fail
	_, err = cp.GetSecret(ctx, "k")
	if err == nil {
		t.Errorf("expected error getting secret before initialization")
	}
	if err := cp.SetSecret(ctx, "k", "v"); err == nil {
		t.Errorf("expected error setting secret before initialization")
	}

	// Initialize with mock prefix settings
	cfg := ProviderConfig{
		Settings: map[string]string{
			"keyring_prefix": "cloakenv_test_prefix",
		},
	}
	if err := cp.Initialize(ctx, cfg); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Override cacheDir to our tempDir for testing
	cp.cacheDir = tempDir

	location := "test_key"
	secretVal := "secret_val"

	// Get unset secret should fail
	_, err = cp.GetSecret(ctx, location)
	if err == nil {
		t.Errorf("expected error getting unset secret")
	}

	// Set secret without TTL
	if err := cp.SetSecret(ctx, location, secretVal); err != nil {
		t.Fatalf("SetSecret failed: %v", err)
	}

	// Get secret should succeed
	got, err := cp.GetSecret(ctx, location)
	if err != nil {
		t.Errorf("GetSecret failed: %v", err)
	}
	if got != secretVal {
		t.Errorf("expected %q, got %q", secretVal, got)
	}

	// Set secret with immediate TTL
	ttlCtx := context.WithValue(ctx, ContextKeyTTL, time.Nanosecond) // Will expire immediately
	if err := cp.SetSecret(ttlCtx, "expire_key", "expire_val"); err != nil {
		t.Fatalf("SetSecret with TTL failed: %v", err)
	}

	// Wait 1ms to ensure expiration
	time.Sleep(time.Millisecond)

	// Get should fail on expired cache
	_, err = cp.GetSecret(ctx, "expire_key")
	if err == nil {
		t.Errorf("expected error getting expired secret")
	}

	// Delete secret
	if err := cp.DeleteSecret(ctx, location); err != nil {
		t.Fatalf("DeleteSecret failed: %v", err)
	}

	// Get deleted should fail
	_, err = cp.GetSecret(ctx, location)
	if err == nil {
		t.Errorf("expected error getting deleted secret")
	}

	// Populate again for ClearCache
	if err := cp.SetSecret(ctx, "k1", "v1"); err != nil {
		t.Fatalf("failed to set secret: %v", err)
	}
	if err := cp.SetSecret(ctx, "k2", "v2"); err != nil {
		t.Fatalf("failed to set secret: %v", err)
	}

	if err := cp.ClearCache(); err != nil {
		t.Fatalf("ClearCache failed: %v", err)
	}

	// Verify both are deleted
	files, _ := os.ReadDir(tempDir)
	if len(files) != 0 {
		t.Errorf("expected empty cache directory after ClearCache, got %d files", len(files))
	}
}
