package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
			name: "empty service rejected",
			testFn: func(t *testing.T, kp *OSKeyringProvider, store *MockKeyringStore) {
				if err := kp.SetRawSecret("", "acc", "val"); err == nil {
					t.Errorf("expected error setting raw secret with empty service")
				}
				if err := kp.DeleteRawSecret("", "acc"); err == nil {
					t.Errorf("expected error deleting raw secret with empty service")
				}
			},
		},
		{
			name: "empty account rejected",
			testFn: func(t *testing.T, kp *OSKeyringProvider, store *MockKeyringStore) {
				if err := kp.SetRawSecret("svc", "", "val"); err == nil {
					t.Errorf("expected error setting raw secret with empty account")
				}
				if err := kp.DeleteRawSecret("svc", ""); err == nil {
					t.Errorf("expected error deleting raw secret with empty account")
				}
			},
		},
		{
			name: "empty password allowed",
			testFn: func(t *testing.T, kp *OSKeyringProvider, store *MockKeyringStore) {
				if err := kp.SetRawSecret("svc", "acc", ""); err != nil {
					t.Errorf("expected setting empty password to succeed, got %v", err)
				}
			},
		},
		{
			name: "null byte in service rejected",
			testFn: func(t *testing.T, kp *OSKeyringProvider, store *MockKeyringStore) {
				badServices := []string{"svc\x00", "\x00svc", "s\x00vc"}
				for _, svc := range badServices {
					if err := kp.SetRawSecret(svc, "acc", "val"); err == nil {
						t.Errorf("expected error setting raw secret with null byte in service: %q", svc)
					}
					if err := kp.DeleteRawSecret(svc, "acc"); err == nil {
						t.Errorf("expected error deleting raw secret with null byte in service: %q", svc)
					}
				}
			},
		},
		{
			name: "null byte in account rejected",
			testFn: func(t *testing.T, kp *OSKeyringProvider, store *MockKeyringStore) {
				badAccounts := []string{"acc\x00", "\x00acc", "a\x00cc"}
				for _, acc := range badAccounts {
					if err := kp.SetRawSecret("svc", acc, "val"); err == nil {
						t.Errorf("expected error setting raw secret with null byte in account: %q", acc)
					}
					if err := kp.DeleteRawSecret("svc", acc); err == nil {
						t.Errorf("expected error deleting raw secret with null byte in account: %q", acc)
					}
				}
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

func TestOSKeyringProvider_SearchFlatEntry(t *testing.T) {
	store := NewMockKeyringStore()
	_ = store.Set("myservice", "API_TOKEN", "tok-123")
	_ = store.Set("myservice", "DB_PASS", "db-456")
	// Internal keys must never be exposed via flat Search entries.
	_ = store.Set("myservice", keyringAccount, "internal-aes-key")
	_ = store.Set("myservice", "provider/work", "internal-master-pw")

	p := NewOSKeyringProviderWithStore(store)
	ctx := context.Background()
	if err := p.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{"keyring_prefix": "myservice"},
	}); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	entries, err := p.Search(ctx, SearchQuery{})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 flat Entry from keyring Search, got %d", len(entries))
	}
	e := entries[0]
	if e.Title() != "OS Keyring" || e.Path() != "keyring" {
		t.Errorf("unexpected metadata: title=%q path=%q", e.Title(), e.Path())
	}
	if e["API_TOKEN"] != "tok-123" || e["DB_PASS"] != "db-456" {
		t.Errorf("expected stored user keys in flat Entry, got %+v", e)
	}
	if _, leaked := e[keyringAccount]; leaked {
		t.Errorf("internal cache master key must not be exposed in keyring Search")
	}
	if _, leaked := e["provider/work"]; leaked {
		t.Errorf("internal provider credential must not be exposed in keyring Search")
	}

	// GetEntry with empty location returns same single flat entry.
	allEntry, err := p.GetEntry(ctx, "")
	if err != nil {
		t.Fatalf("GetEntry(\"\") failed: %v", err)
	}
	if allEntry["API_TOKEN"] != "tok-123" {
		t.Errorf("GetEntry(\"\") API_TOKEN = %v, want tok-123", allEntry["API_TOKEN"])
	}

	// GetEntry with specific account returns single-key entry.
	singleEntry, err := p.GetEntry(ctx, "API_TOKEN")
	if err != nil {
		t.Fatalf("GetEntry(API_TOKEN) failed: %v", err)
	}
	if singleEntry["API_TOKEN"] != "tok-123" {
		t.Errorf("GetEntry(API_TOKEN) = %v, want tok-123", singleEntry["API_TOKEN"])
	}
}

type nonEnumerableKeyringStore struct {
	secrets map[string]string
}

func (n *nonEnumerableKeyringStore) Get(service, account string) (string, error) {
	v, ok := n.secrets[service+"/"+account]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return v, nil
}

func (n *nonEnumerableKeyringStore) Set(service, account, password string) error {
	if n.secrets == nil {
		n.secrets = make(map[string]string)
	}
	n.secrets[service+"/"+account] = password
	return nil
}

func (n *nonEnumerableKeyringStore) Delete(service, account string) error {
	delete(n.secrets, service+"/"+account)
	return nil
}

func TestOSKeyringProvider_BlocksInternalAccountsEverywhere(t *testing.T) {
	store := NewMockKeyringStore()
	_ = store.Set("myservice", keyringAccount, "internal-token")
	_ = store.Set("myservice", cacheDiskKeyringAccount, "disk-aes-key")
	_ = store.Set("myservice", "cache-master-key", "legacy-master")
	_ = store.Set("myservice", "provider/work", "work-pass")
	_ = store.Set("myservice", "cache/item", "cached-val")
	_ = store.Set("myservice", cacheKeyringAccountPrefix+"item", "cached-v2-val")
	_ = store.Set("myservice", "API_TOKEN", "user-secret")

	p := NewOSKeyringProviderWithStore(store)
	ctx := context.Background()
	if err := p.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{"keyring_prefix": "myservice"},
	}); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	entries, err := p.Search(ctx, SearchQuery{})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	blocked := []string{
		keyringAccount,
		cacheDiskKeyringAccount,
		"cache-master-key",
		"provider/work",
		"cache/item",
		cacheKeyringAccountPrefix + "item",
	}
	for _, key := range blocked {
		if _, leaked := entries[0][key]; leaked {
			t.Errorf("internal key %q leaked in Search()", key)
		}
		if _, err := p.GetEntry(ctx, key); err == nil {
			t.Errorf("GetEntry(%q) should deny internal key lookup", key)
		}
		if _, err := p.GetEntry(ctx, "myservice/"+key); err == nil {
			t.Errorf("GetEntry(%q) should deny internal key lookup", "myservice/"+key)
		}
	}
	if entries[0]["API_TOKEN"] != "user-secret" {
		t.Errorf("expected API_TOKEN to remain accessible, got %v", entries[0]["API_TOKEN"])
	}
}

func TestOSKeyringProvider_NonEnumerableStoreRequiresExplicitKeys(t *testing.T) {
	store := &nonEnumerableKeyringStore{
		secrets: map[string]string{
			"cloakenv/API_TOKEN": "tok-abc",
		},
	}
	p := NewOSKeyringProviderWithStore(store)
	ctx := context.Background()

	if _, err := p.Search(ctx, SearchQuery{}); err == nil || !strings.Contains(err.Error(), "query") {
		t.Fatalf("expected error explaining query requirement on non-enumerable store, got %v", err)
	}
	if _, err := p.GetEntry(ctx, ""); err == nil {
		t.Fatalf("expected GetEntry empty location to fail on non-enumerable store with empty keys")
	}

	if err := p.Initialize(ctx, ProviderConfig{Query: "API_TOKEN"}); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	entries, err := p.Search(ctx, SearchQuery{})
	if err != nil {
		t.Fatalf("Search with explicit key failed: %v", err)
	}
	if len(entries) != 1 || entries[0]["API_TOKEN"] != "tok-abc" {
		t.Fatalf("unexpected entries from explicit key lookup: %+v", entries)
	}
}

func TestOSKeyringProvider_CloneWithStoreIsolatesState(t *testing.T) {
	store := &nonEnumerableKeyringStore{
		secrets: map[string]string{
			"svcA/KEY_A": "val-a",
			"svcB/KEY_B": "val-b",
		},
	}
	base := NewOSKeyringProviderWithStore(store)
	c1 := base.CloneWithStore()
	c2 := base.CloneWithStore()

	ctx := context.Background()
	if err := c1.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{"keyring_prefix": "svcA"},
		Query:    "KEY_A",
	}); err != nil {
		t.Fatalf("c1 init failed: %v", err)
	}
	if err := c2.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{"keyring_prefix": "svcB"},
		Query:    "KEY_B",
	}); err != nil {
		t.Fatalf("c2 init failed: %v", err)
	}

	e1, err := c1.Search(ctx, SearchQuery{})
	if err != nil || e1[0]["KEY_A"] != "val-a" || e1[0]["KEY_B"] != nil {
		t.Fatalf("c1 state contaminated: entries=%+v err=%v", e1, err)
	}
	e2, err := c2.Search(ctx, SearchQuery{})
	if err != nil || e2[0]["KEY_B"] != "val-b" || e2[0]["KEY_A"] != nil {
		t.Fatalf("c2 state contaminated: entries=%+v err=%v", e2, err)
	}
}

func TestOSKeyringProviderInternalAccountCaseInsensitiveIsolation(t *testing.T) {
	t.Parallel()
	store := NewMockKeyringStore()
	_ = store.Set("cloakenv", "CACHE_DISK_TOKEN", "disk-key-secret")
	_ = store.Set("cloakenv", "Cache_Token", "cache-token-secret")
	_ = store.Set("cloakenv", "CACHE-MASTER-KEY", "master-key-secret")
	_ = store.Set("cloakenv", "PROVIDER/work", "keepass-master-pw")
	_ = store.Set("cloakenv", "CACHE/item1", "cached-item")
	_ = store.Set("cloakenv", "CACHE_V2/item2", "cached-item-v2")
	_ = store.Set("cloakenv", "allowed_api_key", "ok-val")

	kp := NewOSKeyringProviderWithStore(store)
	ctx := context.Background()
	if err := kp.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{"service": "cloakenv"},
		Query:    "CACHE_DISK_TOKEN,Cache_Token,CACHE-MASTER-KEY,PROVIDER/work,CACHE/item1,CACHE_V2/item2,allowed_api_key",
	}); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	internalAccounts := []string{
		"CACHE_DISK_TOKEN",
		"Cache_Token",
		"CACHE-MASTER-KEY",
		"PROVIDER/work",
		"CACHE/item1",
		"CACHE_V2/item2",
		" provider/work ",
	}
	for _, acct := range internalAccounts {
		if _, err := kp.GetSecret(ctx, "cloakenv/"+acct); err == nil {
			t.Errorf("GetSecret(%q) should have been denied", acct)
		}
		if _, err := kp.GetEntry(ctx, acct); err == nil {
			t.Errorf("GetEntry(%q) should have been denied", acct)
		}
	}

	entries, err := kp.Search(ctx, SearchQuery{})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 flat keyring entry, got %d", len(entries))
	}
	if entries[0]["allowed_api_key"] != "ok-val" {
		t.Fatalf("expected allowed_api_key=ok-val, got %v", entries[0]["allowed_api_key"])
	}
	for _, acct := range internalAccounts {
		if _, leaked := entries[0][acct]; leaked {
			t.Errorf("Search leaked internal account %q", acct)
		}
	}
}
