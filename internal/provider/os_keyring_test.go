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
	tests := []struct {
		name    string
		setupFn func(kp *OSKeyringProvider)
		testFn  func(t *testing.T, kp *OSKeyringProvider)
	}{
		{
			name: "set and retrieve",
			testFn: func(t *testing.T, kp *OSKeyringProvider) {
				service, account, secretVal := "svc", "acc", "val"
				if err := kp.SetRawSecret(service, account, secretVal); err != nil {
					t.Fatalf("SetRawSecret failed: %v", err)
				}
				got, err := keyring.Get(service, account)
				if err != nil {
					t.Fatalf("Failed to verify raw secret set: %v", err)
				}
				if got != secretVal {
					t.Errorf("expected %q, got %q", secretVal, got)
				}
			},
		},
		{
			name: "delete existing",
			setupFn: func(kp *OSKeyringProvider) {
				_ = kp.SetRawSecret("svc", "acc", "val")
			},
			testFn: func(t *testing.T, kp *OSKeyringProvider) {
				if err := kp.DeleteRawSecret("svc", "acc"); err != nil {
					t.Fatalf("DeleteRawSecret failed: %v", err)
				}
				if _, err := keyring.Get("svc", "acc"); err == nil {
					t.Errorf("expected error getting deleted raw secret")
				}
			},
		},
		{
			name: "delete non-existent",
			setupFn: func(kp *OSKeyringProvider) {
				_ = kp.SetRawSecret("svc", "acc", "val")
			},
			testFn: func(t *testing.T, kp *OSKeyringProvider) {
				if err := kp.DeleteRawSecret("other-svc", "other-acc"); err == nil {
					t.Errorf("expected error deleting non-existent raw secret")
				}
			},
		},
		{
			name: "delete from empty store",
			testFn: func(t *testing.T, kp *OSKeyringProvider) {
				if err := kp.DeleteRawSecret("svc", "acc"); err == nil {
					t.Errorf("expected error deleting from empty store")
				}
			},
		},
		{
			name: "empty service",
			testFn: func(t *testing.T, kp *OSKeyringProvider) {
				errSet := kp.SetRawSecret("", "acc", "val")
				// Different platforms behave differently; the mock keyring accepts it.
				// We only ensure it doesn't panic.
				_ = errSet
				errDel := kp.DeleteRawSecret("", "acc")
				if errDel == nil && errSet != nil {
					t.Errorf("expected error deleting empty service")
				}
			},
		},
		{
			name: "empty account",
			testFn: func(t *testing.T, kp *OSKeyringProvider) {
				_ = kp.SetRawSecret("svc", "", "val")
				_ = kp.DeleteRawSecret("svc", "")
			},
		},
		{
			name: "empty password",
			testFn: func(t *testing.T, kp *OSKeyringProvider) {
				_ = kp.SetRawSecret("svc", "acc", "")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keyring.MockInit()
			kp := NewOSKeyringProvider()
			if tt.setupFn != nil {
				tt.setupFn(kp)
			}
			tt.testFn(t, kp)
		})
	}
}
