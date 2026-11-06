package provider

import (
	"context"
	"fmt"
	"os"
	"strings"
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
	loc := location
	if strings.HasPrefix(loc, "env:") {
		loc = strings.TrimPrefix(loc, "env:")
	}
	if strings.HasPrefix(loc, ":") {
		loc = strings.TrimPrefix(loc, ":")
	}

	val, ok := os.LookupEnv(loc)
	if !ok {
		return "", fmt.Errorf("environment variable %q is not set", loc)
	}

	if val == "" {
		return "", fmt.Errorf("environment variable %q is set but empty", loc)
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

// getAllEnvEntry constructs the canonical single entry representing all current environment variables.
func (e *EnvProvider) getAllEnvEntry() Entry {
	attrs := make(map[string]any)
	for _, envStr := range os.Environ() {
		k, v, ok := strings.Cut(envStr, "=")
		if ok && k != "" {
			attrs[k] = v
		}
	}
	return Entry{
		Title:      "Environment Variables",
		Tags:       []string{},
		Attributes: attrs,
	}
}

// Search retrieves all entries matching the query criteria.
// EnvProvider represents the host environment as a single-entry vault.
func (e *EnvProvider) Search(_ context.Context, query SearchQuery) ([]SearchResult, error) {
	entry := e.getAllEnvEntry()

	if query.Title != "" && !strings.Contains(strings.ToLower(entry.Title), strings.ToLower(query.Title)) {
		return []SearchResult{}, nil
	}
	if query.Path != "" && !strings.Contains(strings.ToLower("env"), strings.ToLower(query.Path)) {
		return []SearchResult{}, nil
	}
	if len(query.Tags) > 0 {
		return []SearchResult{}, nil
	}

	return []SearchResult{
		{
			Path:  "env",
			Entry: entry,
		},
	}, nil
}

// GetEntry retrieves a complete structured entry representing environment variables.
// When location is empty or "env", it returns the single entry containing all environment variables.
// When location specifies a variable name, it returns an entry containing that single variable for backwards compatibility.
func (e *EnvProvider) GetEntry(_ context.Context, location string) (Entry, error) {
	key := location
	if strings.HasPrefix(key, "env:") {
		key = strings.TrimPrefix(key, "env:")
	}
	if strings.HasPrefix(key, ":") {
		key = strings.TrimPrefix(key, ":")
	}

	if key == "" || location == "env" {
		return e.getAllEnvEntry(), nil
	}

	val, ok := os.LookupEnv(key)
	if !ok {
		return Entry{}, fmt.Errorf("environment variable %q is not set", key)
	}

	attrs := make(map[string]any)
	attrs[key] = val

	return Entry{
		Title:      "Environment Variables",
		Tags:       []string{},
		Attributes: attrs,
	}, nil
}

// GetEntryWithPath retrieves a structured entry along with its authoritative path.
func (e *EnvProvider) GetEntryWithPath(ctx context.Context, location string) (Entry, string, error) {
	entry, err := e.GetEntry(ctx, location)
	if err != nil {
		return Entry{}, "", err
	}
	key := location
	if strings.HasPrefix(key, "env:") {
		key = strings.TrimPrefix(key, "env:")
	}
	if strings.HasPrefix(key, ":") {
		key = strings.TrimPrefix(key, ":")
	}
	if key == "" || location == "env" {
		return entry, "env", nil
	}
	return entry, key, nil
}
