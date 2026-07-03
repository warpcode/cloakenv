// Package provider defines the SecretProvider interface for pluggable
// credential backends and the shared ProviderConfig type.
package provider

import "context"

// SecretProvider is the core abstraction for all credential backends.
// Built-in providers (keyring://, env://) and remote-type providers
// (e.g., keepass) both implement this interface.
type SecretProvider interface {
	// Scheme returns the URI scheme this provider handles (e.g., "keyring", "env").
	Scheme() string

	// Initialize prepares the provider with backend-specific settings.
	// For stateless providers (env, keyring), this may be a no-op.
	// For stateful providers (keepass), this opens and decrypts the database.
	Initialize(ctx context.Context, config ProviderConfig) error

	// GetSecret resolves a secret value from the provider given a
	// scheme-specific location string (the URI path after "scheme://").
	GetSecret(ctx context.Context, location string) (string, error)

	// SetSecret writes a secret value to the provider at the given location.
	// Returns an error if the provider is read-only or if the write fails.
	SetSecret(ctx context.Context, location string, value string) error

	// DeleteSecret removes the secret at the given location from the provider.
	// Returns an error if the provider is read-only or if the deletion fails.
	DeleteSecret(ctx context.Context, location string) error

	// Validate checks if the provider-specific configuration settings are valid.
	Validate(settings map[string]string) error
}

// ProviderConfig carries backend-specific initialization parameters.
type ProviderConfig struct {
	Settings map[string]string
}

// Entry represents a multi-secret credential record with metadata.
type Entry struct {
	Title      string         `json:"title" yaml:"title"`
	Tags       []string       `json:"tags" yaml:"tags"`
	Attributes map[string]any `json:"attributes" yaml:"attributes"`
}

// SearchQuery defines criteria for filtering entries.
type SearchQuery struct {
	Tags  []string
	Title string
	Path  string
}

// SearchResult wraps a found entry with repository and location details.
type SearchResult struct {
	Repository string `json:"repository" yaml:"repository"`
	Path       string `json:"path" yaml:"path"`
	Entry      Entry  `json:"entry" yaml:"entry"`
}

// SearchableProvider is implemented by providers that support searching and entry retrieval.
type SearchableProvider interface {
	// Search retrieves all entries matching the query criteria.
	Search(ctx context.Context, query SearchQuery) ([]SearchResult, error)

	// GetEntry retrieves a complete structured entry by location.
	GetEntry(ctx context.Context, location string) (Entry, error)
}


// ContextKey is a custom type for context keys to avoid collisions.
type ContextKey string

const (
	// TTLKey is the context key for secret Time-To-Live.
	TTLKey ContextKey = "ttl"
)
