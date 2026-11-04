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

	mockStore := NewMockKeyringStore()
	kp := NewOSKeyringProviderWithStore(mockStore)
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
		tt := tt
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
		tt := tt
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

	const numGoroutines = 20
	const numOps = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		g := g
		go func() {
			defer wg.Done()
			service := fmt.Sprintf("svc-%d", g%4)
			account := fmt.Sprintf("acc-%d", g)

			for i := 0; i < numOps; i++ {
				val := fmt.Sprintf("secret-%d-%d", g, i)
				location := fmt.Sprintf("%s/%s", service, account)

				if err := kp.SetSecret(ctx, location, val); err != nil {
					t.Errorf("goroutine %d: SetSecret failed: %v", g, err)
					return
				}

				got, err := kp.GetSecret(ctx, location)
				if err != nil {
					t.Errorf("goroutine %d: GetSecret failed: %v", g, err)
					return
				}
				if got != val {
					t.Errorf("goroutine %d: GetSecret got %q, want %q", g, got, val)
					return
				}

				if err := kp.DeleteSecret(ctx, location); err != nil && !errors.Is(err, keyring.ErrNotFound) {
					t.Errorf("goroutine %d: DeleteSecret failed: %v", g, err)
					return
				}
			}
		}()
	}

	wg.Wait()
}

func TestMockKeyringStore_ErrorInjection(t *testing.T) {
	t.Parallel()

	store := NewMockKeyringStore()
	kp := NewOSKeyringProviderWithStore(store)
	ctx := context.Background()

	injectedErr := errors.New("simulated keyring fault")
	store.SetError(injectedErr)

	if _, err := kp.GetSecret(ctx, "svc/acc"); !errors.Is(err, injectedErr) {
		t.Errorf("GetSecret expected error %v, got %v", injectedErr, err)
	}

	if err := kp.SetSecret(ctx, "svc/acc", "val"); !errors.Is(err, injectedErr) {
		t.Errorf("SetSecret expected error %v, got %v", injectedErr, err)
	}

	if err := kp.DeleteSecret(ctx, "svc/acc"); !errors.Is(err, injectedErr) {
		t.Errorf("DeleteSecret expected error %v, got %v", injectedErr, err)
	}

	if err := kp.SetRawSecret("svc", "acc", "val"); !errors.Is(err, injectedErr) {
		t.Errorf("SetRawSecret expected error %v, got %v", injectedErr, err)
	}

	if err := kp.DeleteRawSecret("svc", "acc"); !errors.Is(err, injectedErr) {
		t.Errorf("DeleteRawSecret expected error %v, got %v", injectedErr, err)
	}

	// Clear error and verify operations work again
	store.SetError(nil)
	if err := kp.SetSecret(ctx, "svc/acc", "val"); err != nil {
		t.Errorf("SetSecret failed after clearing error: %v", err)
	}
	got, err := kp.GetSecret(ctx, "svc/acc")
	if err != nil || got != "val" {
		t.Errorf("GetSecret failed after clearing error, got: %q, err: %v", got, err)
	}
}

func TestNewOSKeyringProviderWithStore_NilFallback(t *testing.T) {
	t.Parallel()

	kp := NewOSKeyringProviderWithStore(nil)
	if kp == nil {
		t.Fatal("expected non-nil provider")
	}
	if kp.store == nil {
		t.Fatal("expected non-nil store fallback")
	}
}
