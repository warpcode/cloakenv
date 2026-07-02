package provider

import (
	"context"
	"fmt"
	"os"
)

// EnvProvider implements SecretProvider for the built-in env:// scheme.
// It reads values directly from the current process environment.
// Designed for headless servers and CI pipelines where keyring access
// is unavailable.
type EnvProvider struct{}

// NewEnvProvider returns a new environment variable provider instance.
func NewEnvProvider() *EnvProvider {
	return &EnvProvider{}
}

// Scheme returns "env".
func (e *EnvProvider) Scheme() string {
	return "env"
}

// Initialize is a no-op for the env provider.
func (e *EnvProvider) Initialize(_ context.Context, _ ProviderConfig) error {
	return nil
}

// GetSecret reads an environment variable by name.
// Location format: the bare variable name (e.g., "MY_API_KEY").
// Returns an error if the variable is unset or empty.
func (e *EnvProvider) GetSecret(_ context.Context, location string) (string, error) {
	val, ok := os.LookupEnv(location)
	if !ok {
		return "", fmt.Errorf("environment variable %q is not set", location)
	}

	if val == "" {
		return "", fmt.Errorf("environment variable %q is set but empty", location)
	}

	return val, nil
}

// SetSecret returns an error because environment variables are read-only at runtime.
func (e *EnvProvider) SetSecret(_ context.Context, _ string, _ string) error {
	return fmt.Errorf("env provider is read-only")
}

// DeleteSecret returns an error because environment variables are read-only at runtime.
func (e *EnvProvider) DeleteSecret(_ context.Context, _ string) error {
	return fmt.Errorf("env provider is read-only")
}

// Validate is a no-op for the env provider.
func (e *EnvProvider) Validate(settings map[string]string) error {
	return nil
}
