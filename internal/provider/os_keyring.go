package provider

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"
)

// KeyringStore abstracts the low-level keyring storage operations.
type KeyringStore interface {
	Get(service, account string) (string, error)
	Set(service, account, password string) error
	Delete(service, account string) error
}

// EnumerableKeyringStore is an optional KeyringStore capability for listing
// all stored account/value pairs under a given service namespace.
type EnumerableKeyringStore interface {
	List(service string) (map[string]string, error)
}

type defaultKeyringStore struct{}

func (defaultKeyringStore) Get(service, account string) (string, error) {
	return keyring.Get(service, account)
}

func (defaultKeyringStore) Set(service, account, password string) error {
	return keyring.Set(service, account, password)
}

func (defaultKeyringStore) Delete(service, account string) error {
	return keyring.Delete(service, account)
}

// MockKeyringStore is an in-memory, thread-safe implementation of KeyringStore
// and EnumerableKeyringStore for testing.
type MockKeyringStore struct {
	mu      sync.RWMutex
	secrets map[string]map[string]string
}

// NewMockKeyringStore returns an initialized in-memory MockKeyringStore.
func NewMockKeyringStore() *MockKeyringStore {
	return &MockKeyringStore{
		secrets: make(map[string]map[string]string),
	}
}

// Get retrieves a stored password for the given service and account.
func (m *MockKeyringStore) Get(service, account string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	accounts, ok := m.secrets[service]
	if !ok {
		return "", keyring.ErrNotFound
	}
	val, ok := accounts[account]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return val, nil
}

// Set stores a password for the given service and account.
func (m *MockKeyringStore) Set(service, account, password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.secrets[service]; !ok {
		m.secrets[service] = make(map[string]string)
	}
	m.secrets[service][account] = password
	return nil
}

// Delete removes a stored password for the given service and account.
func (m *MockKeyringStore) Delete(service, account string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	accounts, ok := m.secrets[service]
	if !ok {
		return keyring.ErrNotFound
	}
	if _, ok := accounts[account]; !ok {
		return keyring.ErrNotFound
	}
	delete(accounts, account)
	if len(accounts) == 0 {
		delete(m.secrets, service)
	}
	return nil
}

// List returns a snapshot of all account/password pairs stored for service.
func (m *MockKeyringStore) List(service string) (map[string]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	accounts, ok := m.secrets[service]
	if !ok {
		return map[string]string{}, nil
	}
	out := make(map[string]string, len(accounts))
	maps.Copy(out, accounts)
	return out, nil
}

// OSKeyringProvider implements read-only SecretProvider and SearchableProvider
// for the built-in "keyring" scheme. It delegates to the native OS credential store:
//   - macOS: Keychain Services
//   - Linux: Secret Service API via D-Bus (GNOME Keyring / KWallet)
//   - Windows: Credential Manager
//
// As a flat K/V vault, Search emits a single Entry containing stored user keys
// for the configured service namespace (excluding internal cloakenv auth/cache keys).
// Direct auth lifecycle management uses SetRawSecret and DeleteRawSecret.
type OSKeyringProvider struct {
	mu      sync.RWMutex
	store   KeyringStore
	service string
	keys    []string
}

// NewOSKeyringProvider returns a new keyring provider instance using the default OS keyring store.
func NewOSKeyringProvider() *OSKeyringProvider {
	return &OSKeyringProvider{
		store:   defaultKeyringStore{},
		service: "cloakenv",
	}
}

// NewOSKeyringProviderWithStore returns a new keyring provider instance with a custom KeyringStore.
func NewOSKeyringProviderWithStore(store KeyringStore) *OSKeyringProvider {
	if store == nil {
		store = defaultKeyringStore{}
	}
	return &OSKeyringProvider{
		store:   store,
		service: "cloakenv",
	}
}

// CloneWithStore returns a fresh OSKeyringProvider sharing only the underlying KeyringStore
// so independent configured vaults never mutate shared service or key state.
func (o *OSKeyringProvider) CloneWithStore() *OSKeyringProvider {
	o.mu.RLock()
	store := o.store
	o.mu.RUnlock()
	return NewOSKeyringProviderWithStore(store)
}

// Scheme returns "keyring".
func (o *OSKeyringProvider) Scheme() string {
	return "keyring"
}

// Initialize configures the keyring service namespace and optional key list.
func (o *OSKeyringProvider) Initialize(_ context.Context, cfg ProviderConfig) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	service := cfg.Settings["keyring_prefix"]
	if service == "" {
		service = cfg.Settings["service"]
	}
	if service == "" {
		service = "cloakenv"
	}
	o.service = service

	rawKeys := cfg.Settings["keys"]
	if rawKeys == "" {
		rawKeys = cfg.Query
	}
	o.keys = nil
	if rawKeys != "" {
		for _, k := range strings.Split(rawKeys, ",") {
			if trimmed := strings.TrimSpace(k); trimmed != "" {
				o.keys = append(o.keys, trimmed)
			}
		}
	}
	return nil
}

// GetSecret retrieves a secret from the OS keyring.
// Location format: "service/account" (e.g., "cloakenv/work").
func (o *OSKeyringProvider) GetSecret(_ context.Context, location string) (string, error) {
	service, account, err := parseKeyringLocation(location)
	if err != nil {
		return "", err
	}
	secret, err := o.store.Get(service, account)
	if err != nil {
		return "", fmt.Errorf("keyring lookup failed for %s/%s: %w", service, account, err)
	}
	return secret, nil
}

// isInternalKeyringAccount reports whether an account name is reserved for
// cloakenv's own internal credential/cache storage and must not be exposed
// in flat vault entries.
func isInternalKeyringAccount(account string) bool {
	return account == keyringAccount ||
		account == cacheDiskKeyringAccount ||
		account == "cache-master-key" ||
		strings.HasPrefix(account, "provider/") ||
		strings.HasPrefix(account, "cache/") ||
		strings.HasPrefix(account, cacheKeyringAccountPrefix)
}

// buildKeyringEntry builds a single flat Entry holding all accessible user keys
// for the configured service namespace.
func (o *OSKeyringProvider) buildKeyringEntry() (Entry, error) {
	o.mu.RLock()
	service := o.service
	if service == "" {
		service = "cloakenv"
	}
	explicitKeys := append([]string(nil), o.keys...)
	store := o.store
	o.mu.RUnlock()

	entry := Entry{
		"title": "OS Keyring",
		"path":  "keyring",
		"tags":  []string{},
	}

	if enum, ok := store.(EnumerableKeyringStore); ok {
		all, err := enum.List(service)
		if err != nil {
			return nil, fmt.Errorf("keyring list failed for %q: %w", service, err)
		}
		for k, v := range all {
			if isInternalKeyringAccount(k) {
				continue
			}
			entry[k] = v
		}
	} else if len(explicitKeys) == 0 {
		return nil, errors.New("keyring vault requires explicit keys via vault 'query' when OS keyring backend does not support service enumeration")
	}

	for _, k := range explicitKeys {
		if isInternalKeyringAccount(k) {
			continue
		}
		if _, exists := entry[k]; exists {
			continue
		}
		if err := validateRawKeyringTarget(service, k); err != nil {
			return nil, err
		}
		val, err := store.Get(service, k)
		if err != nil {
			if errors.Is(err, keyring.ErrNotFound) {
				continue
			}
			return nil, fmt.Errorf("keyring lookup failed for %s/%s: %w", service, k, err)
		}
		entry[k] = val
	}

	return entry, nil
}

// Search returns a single flat Entry containing stored keys for the keyring service.
func (o *OSKeyringProvider) Search(_ context.Context, query SearchQuery) ([]Entry, error) {
	entry, err := o.buildKeyringEntry()
	if err != nil {
		return nil, err
	}
	if query.Title != "" && !strings.Contains(strings.ToLower(entry.Title()), strings.ToLower(query.Title)) {
		return []Entry{}, nil
	}
	if query.Path != "" && !strings.Contains(strings.ToLower(entry.Path()), strings.ToLower(query.Path)) {
		return []Entry{}, nil
	}
	if len(query.Tags) > 0 {
		return []Entry{}, nil
	}
	return []Entry{entry}, nil
}

// GetEntry retrieves the flat Entry of all keyring keys when location is empty
// or "keyring", or a single-key Entry for "service/account" or "account".
func (o *OSKeyringProvider) GetEntry(_ context.Context, location string) (Entry, error) {
	trimmed := strings.TrimSpace(location)
	if trimmed == "" || trimmed == "keyring" {
		return o.buildKeyringEntry()
	}

	var service, account string
	if strings.Contains(trimmed, "/") {
		var err error
		service, account, err = parseKeyringLocation(trimmed)
		if err != nil {
			return nil, err
		}
	} else {
		o.mu.RLock()
		service = o.service
		if service == "" {
			service = "cloakenv"
		}
		o.mu.RUnlock()
		account = trimmed
		if err := validateRawKeyringTarget(service, account); err != nil {
			return nil, err
		}
	}

	if isInternalKeyringAccount(account) {
		return nil, fmt.Errorf("keyring lookup denied: %q is an internal keyring key", account)
	}
	val, err := o.store.Get(service, account)
	if err != nil {
		return nil, fmt.Errorf("keyring lookup failed for %s/%s: %w", service, account, err)
	}
	return Entry{
		"title": account,
		"path":  service + "/" + account,
		"tags":  []string{},
		account: val,
	}, nil
}

// validateRawKeyringTarget verifies that service and account are non-empty and do not contain null bytes.
func validateRawKeyringTarget(service, account string) error {
	if service == "" || account == "" {
		return errors.New("invalid keyring target: service and account must not be empty")
	}
	if strings.IndexByte(service, 0) != -1 || strings.IndexByte(account, 0) != -1 {
		return errors.New("invalid keyring target: contains null byte")
	}
	return nil
}

// SetSecret stores a secret in the OS keyring using "service/account" format.
// Used internally by auth and keyring maintenance tooling.
func (o *OSKeyringProvider) SetSecret(_ context.Context, location string, value string) error {
	service, account, err := parseKeyringLocation(location)
	if err != nil {
		return err
	}
	return o.store.Set(service, account, value)
}

// DeleteSecret removes a secret from the OS keyring using "service/account" format.
// Used internally by auth and keyring maintenance tooling.
func (o *OSKeyringProvider) DeleteSecret(_ context.Context, location string) error {
	service, account, err := parseKeyringLocation(location)
	if err != nil {
		return err
	}
	return o.store.Delete(service, account)
}

// SetRawSecret stores a secret in the OS keyring using raw service and account strings.
// Used by the "auth login" subcommand.
func (o *OSKeyringProvider) SetRawSecret(service, account, password string) error {
	if err := validateRawKeyringTarget(service, account); err != nil {
		return err
	}
	return o.store.Set(service, account, password)
}

// DeleteRawSecret removes a secret from the OS keyring using raw service and account strings.
// Used by the "auth forget" subcommand.
func (o *OSKeyringProvider) DeleteRawSecret(service, account string) error {
	if err := validateRawKeyringTarget(service, account); err != nil {
		return err
	}
	return o.store.Delete(service, account)
}

// parseKeyringLocation splits a "service/account" string.
func parseKeyringLocation(location string) (string, string, error) {
	if strings.IndexByte(location, 0) != -1 {
		return "", "", errors.New("invalid keyring location: contains null byte")
	}
	parts := strings.SplitN(location, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", errors.New("invalid keyring location; expected format: service/account")
	}
	return parts[0], parts[1], nil
}

// Validate is a no-op for the keyring provider.
func (o *OSKeyringProvider) Validate(_ map[string]string) error {
	return nil
}
