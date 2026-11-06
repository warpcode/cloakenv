package provider

import (
	"context"
	"errors"
	"fmt"
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

// MockKeyringStore is an in-memory, thread-safe implementation of KeyringStore for testing.
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

// OSKeyringProvider implements SecretProvider for the built-in keyring:// scheme.
// It delegates to the native OS credential store:
//   - macOS: Keychain Services
//   - Linux: Secret Service API via D-Bus (GNOME Keyring / KWallet)
//   - Windows: Credential Manager
//
// OSKeyringProvider is intentionally non-searchable: the OS keyring has no
// enumeration API and exposing all stored credentials in search results would
// be a security risk. It does not implement SearchableProvider.
type OSKeyringProvider struct {
	store KeyringStore
}

var _ WritableProvider = (*OSKeyringProvider)(nil)

// NewOSKeyringProvider returns a new keyring provider instance using the default OS keyring store.
func NewOSKeyringProvider() *OSKeyringProvider {
	return &OSKeyringProvider{
		store: defaultKeyringStore{},
	}
}

// NewOSKeyringProviderWithStore returns a new keyring provider instance with a custom KeyringStore.
func NewOSKeyringProviderWithStore(store KeyringStore) *OSKeyringProvider {
	if store == nil {
		store = defaultKeyringStore{}
	}
	return &OSKeyringProvider{
		store: store,
	}
}

// Scheme returns "keyring".
func (o *OSKeyringProvider) Scheme() string {
	return "keyring"
}

// Initialize is a no-op for the keyring provider; the OS handles session state.
func (o *OSKeyringProvider) Initialize(_ context.Context, _ ProviderConfig) error {
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

// SetSecret stores a secret in the OS keyring.
// Location format: "service/account" (e.g., "cloakenv/work").
func (o *OSKeyringProvider) SetSecret(_ context.Context, location string, value string) error {
	service, account, err := parseKeyringLocation(location)
	if err != nil {
		return err
	}
	return o.store.Set(service, account, value)
}

// SetRawSecret stores a secret in the OS keyring using raw service and account strings.
// Used by the "auth login" subcommand.
func (o *OSKeyringProvider) SetRawSecret(service, account, password string) error {
	return o.store.Set(service, account, password)
}

// DeleteRawSecret removes a secret from the OS keyring using raw service and account strings.
// Used by the "auth forget" subcommand.
func (o *OSKeyringProvider) DeleteRawSecret(service, account string) error {
	return o.store.Delete(service, account)
}

// DeleteSecret removes a secret from the OS keyring.
// Location format: "service/account" (e.g., "cloakenv/work").
func (o *OSKeyringProvider) DeleteSecret(_ context.Context, location string) error {
	service, account, err := parseKeyringLocation(location)
	if err != nil {
		return err
	}
	return o.store.Delete(service, account)
}

// parseKeyringLocation splits a "service/account" string.
func parseKeyringLocation(location string) (string, string, error) {
	// Defense-in-depth: Reject null bytes in keyring locations.
	// Null bytes can cause string truncation or memory safety issues in native C-bindings
	// and system APIs (macOS Keychain, Linux Secret Service / libsecret, Windows Credential Manager).
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
func (o *OSKeyringProvider) Validate(settings map[string]string) error {
	return nil
}
