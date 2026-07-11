package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zalando/go-keyring"
)

// OSKeyringProvider implements SecretProvider for the built-in keyring:// scheme.
// It delegates to the native OS credential store:
//   - macOS: Keychain Services
//   - Linux: Secret Service API via D-Bus (GNOME Keyring / KWallet)
//   - Windows: Credential Manager
//
// OSKeyringProvider is intentionally non-searchable: the OS keyring has no
// enumeration API and exposing all stored credentials in search results would
// be a security risk. It does not implement SearchableProvider.
type OSKeyringProvider struct{}

// NewOSKeyringProvider returns a new keyring provider instance.
func NewOSKeyringProvider() *OSKeyringProvider {
	return &OSKeyringProvider{}
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

	secret, err := keyring.Get(service, account)
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
	return keyring.Set(service, account, value)
}

// SetRawSecret stores a secret in the OS keyring using raw service and account strings.
// Used by the "config set-vault-pass" subcommand.
func (o *OSKeyringProvider) SetRawSecret(service, account, password string) error {
	return keyring.Set(service, account, password)
}

// DeleteRawSecret removes a secret from the OS keyring using raw service and account strings.
// Used by the "config clear-vault-pass" subcommand.
func (o *OSKeyringProvider) DeleteRawSecret(service, account string) error {
	return keyring.Delete(service, account)
}

// DeleteSecret removes a secret from the OS keyring.
// Location format: "service/account" (e.g., "cloakenv/work").
func (o *OSKeyringProvider) DeleteSecret(_ context.Context, location string) error {
	service, account, err := parseKeyringLocation(location)
	if err != nil {
		return err
	}
	return keyring.Delete(service, account)
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
