package provider

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestOSKeyringProvider(t *testing.T) {
	t.Parallel()

	store := NewMockKeyringStore()
	kp := NewOSKeyringProviderWithStore(store)
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

	// Test null bytes in location format
	nullByteLocations := []string{
		"test\x00svc/account",
		"service/test\x00account",
		"svc/acc\x00",
		"\x00svc/account",
	}
	for _, badLoc := range nullByteLocations {
		if err := kp.SetSecret(ctx, badLoc, "val"); err == nil {
			t.Errorf("expected error setting location with null byte: %q", badLoc)
		}
		if _, err := kp.GetSecret(ctx, badLoc); err == nil {
			t.Errorf("expected error getting location with null byte: %q", badLoc)
		}
		if err := kp.DeleteSecret(ctx, badLoc); err == nil {
			t.Errorf("expected error deleting location with null byte: %q", badLoc)
		}
	}
}

func TestOSKeyringProvider_Validate(t *testing.T) {
	t.Parallel()

	p := NewOSKeyringProviderWithStore(NewMockKeyringStore())

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
			t.Parallel()
			err := p.Validate(tt.settings)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestOSKeyringProvider_RawSecrets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setupFn func(kp *OSKeyringProvider)
		testFn  func(t *testing.T, kp *OSKeyringProvider, store *MockKeyringStore)
	}{
		{
			name: "set and retrieve",
			testFn: func(t *testing.T, kp *OSKeyringProvider, store *MockKeyringStore) {
				service, account, secretVal := "svc", "acc", "val"
				if err := kp.SetRawSecret(service, account, secretVal); err != nil {
					t.Fatalf("SetRawSecret failed: %v", err)
				}
				got, err := store.Get(service, account)
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
			testFn: func(t *testing.T, kp *OSKeyringProvider, store *MockKeyringStore) {
				if err := kp.DeleteRawSecret("svc", "acc"); err != nil {
					t.Fatalf("DeleteRawSecret failed: %v", err)
				}
				if _, err := store.Get("svc", "acc"); err == nil {
					t.Errorf("expected error getting deleted raw secret")
				}
			},
		},
		{
			name: "delete non-existent",
			setupFn: func(kp *OSKeyringProvider) {
				_ = kp.SetRawSecret("svc", "acc", "val")
			},
			testFn: func(t *testing.T, kp *OSKeyringProvider, store *MockKeyringStore) {
				if err := kp.DeleteRawSecret("other-svc", "other-acc"); err == nil {
					t.Errorf("expected error deleting non-existent raw secret")
				}
			},
		},
		{
			name: "delete from empty store",
			testFn: func(t *testing.T, kp *OSKeyringProvider, store *MockKeyringStore) {
				if err := kp.DeleteRawSecret("svc", "acc"); err == nil {
					t.Errorf("expected error deleting from empty store")
				}
			},
		},
		{
			name: "empty service",
			testFn: func(t *testing.T, kp *OSKeyringProvider, store *MockKeyringStore) {
				errSet := kp.SetRawSecret("", "acc", "val")
				_ = errSet
				errDel := kp.DeleteRawSecret("", "acc")
				if errDel == nil && errSet != nil {
					t.Errorf("expected error deleting empty service")
				}
			},
		},
		{
			name: "empty account",
			testFn: func(t *testing.T, kp *OSKeyringProvider, store *MockKeyringStore) {
				_ = kp.SetRawSecret("svc", "", "val")
				_ = kp.DeleteRawSecret("svc", "")
			},
		},
		{
			name: "empty password",
			testFn: func(t *testing.T, kp *OSKeyringProvider, store *MockKeyringStore) {
				_ = kp.SetRawSecret("svc", "acc", "")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := NewMockKeyringStore()
			kp := NewOSKeyringProviderWithStore(store)
			if tt.setupFn != nil {
				tt.setupFn(kp)
			}
			tt.testFn(t, kp, store)
		})
	}
}

func TestMockKeyringStore_Concurrency(t *testing.T) {
	t.Parallel()

	store := NewMockKeyringStore()
	kp := NewOSKeyringProviderWithStore(store)
	ctx := context.Background()

	const workers = 20
	const iterations = 50

	var wg sync.WaitGroup
	wg.Add(workers)

	for i := range workers {
		workerID := i
		go func() {
			defer wg.Done()
			service := fmt.Sprintf("service-%d", workerID%5)
			account := fmt.Sprintf("account-%d", workerID)
			location := fmt.Sprintf("%s/%s", service, account)

			for j := range iterations {
				val := fmt.Sprintf("val-%d-%d", workerID, j)
				if err := kp.SetSecret(ctx, location, val); err != nil {
					t.Errorf("worker %d set failed: %v", workerID, err)
					return
				}

				got, err := kp.GetSecret(ctx, location)
				if err != nil {
					t.Errorf("worker %d get failed: %v", workerID, err)
					return
				}
				if got != val {
					t.Errorf("worker %d got %q, want %q", workerID, got, val)
					return
				}

				if err := kp.DeleteSecret(ctx, location); err != nil {
					t.Errorf("worker %d delete failed: %v", workerID, err)
					return
				}

				if _, err := kp.GetSecret(ctx, location); err == nil {
					t.Errorf("worker %d expected error after delete", workerID)
					return
				}
			}
		}()
	}

	wg.Wait()
}

func TestNewOSKeyringProviderWithStore_Nil(t *testing.T) {
	t.Parallel()

	kp := NewOSKeyringProviderWithStore(nil)
	if kp == nil {
		t.Fatal("expected non-nil provider")
	}
	if kp.store == nil {
		t.Fatal("expected non-nil default store when nil is passed")
	}
}

func TestMockKeyringStore_DeleteCases(t *testing.T) {
	t.Parallel()

	store := NewMockKeyringStore()

	// Delete on empty store returns ErrNotFound
	if err := store.Delete("svc", "acc"); !errors.Is(err, keyring.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	// Set one account
	if err := store.Set("svc", "acc1", "val1"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Delete non-existent account in existing service
	if err := store.Delete("svc", "acc2"); !errors.Is(err, keyring.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	// Delete existing account
	if err := store.Delete("svc", "acc1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Service map should be cleaned up when last account is deleted
	if err := store.Delete("svc", "acc1"); !errors.Is(err, keyring.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
