package provider

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// EnvProvider implements read-only SecretProvider and SearchableProvider for the
// built-in "env" scheme. It reads from the current process environment and emits
// a single flat Entry containing all environment key/value pairs.
type EnvProvider struct{}

// NewEnvProvider returns a new EnvProvider instance.
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

// GetSecret retrieves a single environment variable by name.
// Supports "VAR_NAME", ":VAR_NAME", and "env:VAR_NAME".
func (e *EnvProvider) GetSecret(_ context.Context, location string) (string, error) {
	key := strings.TrimPrefix(location, "env:")
	key = strings.TrimPrefix(key, ":")
	if key == "" {
		return "", fmt.Errorf("env provider: empty environment variable name")
	}
	val, ok := os.LookupEnv(key)
	if !ok {
		return "", fmt.Errorf("environment variable %q is not set", key)
	}
	if val == "" {
		return "", fmt.Errorf("environment variable %q is set but empty", key)
	}
	return val, nil
}

// Validate is a no-op for the env provider.
func (e *EnvProvider) Validate(_ map[string]string) error {
	return nil
}

// getAllEnvEntry builds a single flat Entry containing all current process
// environment variables alongside canonical metadata keys.
func (e *EnvProvider) getAllEnvEntry() Entry {
	environ := os.Environ()
	entry := make(Entry, len(environ)+3)
	for _, pair := range environ {
		k, v, ok := strings.Cut(pair, "=")
		if !ok || k == "" {
			continue
		}
		entry[k] = v
	}
	entry["title"] = "Environment Variables"
	entry["path"] = "env"
	entry["tags"] = []string{}
	return entry
}

// Search returns a single flat Entry representing the current process environment.
func (e *EnvProvider) Search(_ context.Context, query SearchQuery) ([]Entry, error) {
	entry := e.getAllEnvEntry()
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

// GetEntry retrieves the flat Entry of all environment variables when location
// is empty or "env", or a single-variable Entry when a specific variable is named.
func (e *EnvProvider) GetEntry(_ context.Context, location string) (Entry, error) {
	key := strings.TrimPrefix(location, "env:")
	key = strings.TrimPrefix(key, ":")
	if key == "" || location == "env" {
		return e.getAllEnvEntry(), nil
	}
	val, ok := os.LookupEnv(key)
	if !ok {
		return nil, fmt.Errorf("environment variable %q is not set", key)
	}
	return Entry{
		"title": "Environment Variables",
		"path":  key,
		"tags":  []string{},
		key:     val,
	}, nil
}
