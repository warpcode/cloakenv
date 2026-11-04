package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"
)

// KeyringStore abstracts the underlying keyring storage operations.
type KeyringStore interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

type defaultKeyringStore struct{}

func (s *defaultKeyringStore) Get(service, user string) (string, error) {
	return keyring.Get(service, user)
}

func (s *defaultKeyringStore) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}

func (s *defaultKeyringStore) Delete(service, user string) error {
	return keyring.Delete(service, user)
}

// MockKeyringStore is an in-memory, thread-safe implementation of KeyringStore for testing.
type MockKeyringStore struct {
	mu        sync.RWMutex
	store     map[string]map[string]string
	mockError error
}

// NewMockKeyringStore returns an initialized in-memory mock keyring store.
func NewMockKeyringStore() *MockKeyringStore {
	return &MockKeyringStore{
		store: make(map[string]map[string]string),
	}
}

// SetError injects an error to be returned by subsequent operations.
func (m *MockKeyringStore) SetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mockError = err
}

// Get retrieves a secret for the given service and user.
func (m *MockKeyringStore) Get(service, user string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.mockError != nil {
		return "", m.mockError
	}
	if svcMap, ok := m.store[service]; ok {
		if val, ok := svcMap[user]; ok {
			return val, nil
		}
	}
	return "", keyring.ErrNotFound
}

// Set stores a secret for the given service and user.
func (m *MockKeyringStore) Set(service, user, password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.mockError != nil {
		return m.mockError
	}
	if m.store == nil {
		m.store = make(map[string]map[string]string)
	}
	if m.store[service] == nil {
		m.store[service] = make(map[string]string)
	}
	m.store[service][user] = password
	return nil
}

// Delete removes a secret for the given service and user.
func (m *MockKeyringStore) Delete(service, user string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.mockError != nil {
		return m.mockError
	}
	if m.store != nil {
		if svcMap, ok := m.store[service]; ok {
			if _, ok := svcMap[user]; ok {
				delete(svcMap, user)
				return nil
			}
		}
	}
	return keyring.ErrNotFound
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

// NewOSKeyringProvider returns a new keyring provider instance with the default OS store.
func NewOSKeyringProvider() *OSKeyringProvider {
	return NewOSKeyringProviderWithStore(&defaultKeyringStore{})
}

// NewOSKeyringProviderWithStore returns a new keyring provider instance using the supplied KeyringStore.
func NewOSKeyringProviderWithStore(store KeyringStore) *OSKeyringProvider {
	if store == nil {
		store = &defaultKeyringStore{}
	}
	return &OSKeyringProvider{store: store}
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
