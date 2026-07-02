package provider

import (
	"context"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestOSKeyringProvider(t *testing.T) {
	keyring.MockInit()

	kp := NewOSKeyringProvider()
	ctx := context.Background()

	if kp.Scheme() != "keyring" {
		t.Errorf("expected scheme 'keyring', got %q", kp.Scheme())
	}

	if err := kp.Initialize(ctx, ProviderConfig{}); err != nil {
		t.Errorf("Initialize failed: %v", err)
	}

	if err := kp.Validate(nil); err != nil {
		t.Errorf("Validate failed: %v", err)
	}

	location := "test-service/test-account"
	secretVal := "secret-content"

	// Get secret should fail before set
	_, err := kp.GetSecret(ctx, location)
	if err == nil {
		t.Errorf("expected error getting unset secret")
	}

	// Set secret
	if err := kp.SetSecret(ctx, location, secretVal); err != nil {
		t.Fatalf("SetSecret failed: %v", err)
	}

	// Get secret should succeed
	got, err := kp.GetSecret(ctx, location)
	if err != nil {
		t.Errorf("GetSecret failed: %v", err)
	}
	if got != secretVal {
		t.Errorf("expected %q, got %q", secretVal, got)
	}

	// Delete secret
	if err := kp.DeleteSecret(ctx, location); err != nil {
		t.Fatalf("DeleteSecret failed: %v", err)
	}

	// Get secret should fail after delete
	_, err = kp.GetSecret(ctx, location)
	if err == nil {
		t.Errorf("expected error getting deleted secret")
	}

	// Test invalid location format
	if err := kp.SetSecret(ctx, "invalid-no-slash", "val"); err == nil {
		t.Errorf("expected error setting invalid location")
	}
	if _, err := kp.GetSecret(ctx, "invalid-no-slash"); err == nil {
		t.Errorf("expected error getting invalid location")
	}
	if err := kp.DeleteSecret(ctx, "invalid-no-slash"); err == nil {
		t.Errorf("expected error deleting invalid location")
	}
}
