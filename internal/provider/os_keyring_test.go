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

func TestOSKeyringProvider_Validate(t *testing.T) {
	p := NewOSKeyringProvider()

	tests := []struct {
		name     string
		settings map[string]string
		wantErr  bool
	}{
		{
			name:     "nil settings",
			settings: nil,
			wantErr:  false,
		},
		{
			name:     "empty settings map",
			settings: map[string]string{},
			wantErr:  false,
		},
		{
			name: "populated settings map",
			settings: map[string]string{
				"service": "test",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := p.Validate(tt.settings)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestOSKeyringProvider_RawSecrets(t *testing.T) {
	keyring.MockInit()

	kp := NewOSKeyringProvider()

	service := "raw-test-service"
	account := "raw-test-account"
	secretVal := "raw-secret-content"

	// Set raw secret
	if err := kp.SetRawSecret(service, account, secretVal); err != nil {
		t.Fatalf("SetRawSecret failed: %v", err)
	}

	// Verify using go-keyring directly since there's no GetRawSecret
	got, err := keyring.Get(service, account)
	if err != nil {
		t.Fatalf("Failed to verify raw secret set: %v", err)
	}
	if got != secretVal {
		t.Errorf("expected %q, got %q", secretVal, got)
	}

	// Delete raw secret
	if err := kp.DeleteRawSecret(service, account); err != nil {
		t.Fatalf("DeleteRawSecret failed: %v", err)
	}

	// Verify delete
	_, err = keyring.Get(service, account)
	if err == nil {
		t.Errorf("expected error getting deleted raw secret")
	}

	// Delete non-existent raw secret
	err = kp.DeleteRawSecret("non-existent-service", "non-existent-account")
	if err == nil {
		t.Errorf("expected error deleting non-existent raw secret")
	}
}
