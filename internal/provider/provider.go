// Package provider defines the read-only SecretProvider and SearchableProvider
// interfaces for pluggable credential backends, along with the flat Entry type.
//
// Vault providers are pure, read-only secret retrievers: they return untouched
// raw entries as flat map[string]any maps (holding raw secret fields alongside
// canonical metadata keys "title", "path", "uuid", "tags", and "group") without
// filtering, key mapping, formatting, or output template evaluation.
package provider

import "context"

// SecretProvider is the core read-only abstraction for all credential backends.
// Built-in providers (env, keyring, cache, pass) and configured vaults
// (keepass, pass, vault, env, keyring) implement this interface.
type SecretProvider interface {
	// Scheme returns the provider scheme/type (e.g., "keepass", "pass", "vault", "env", "keyring", "cache").
	Scheme() string

	// Initialize prepares the provider with backend-specific settings.
	// For stateless providers (env, keyring), this may be a no-op.
	// For stateful providers (keepass, vault), this authenticates or opens the store.
	Initialize(ctx context.Context, config ProviderConfig) error

	// GetSecret resolves a single secret value given a provider-specific location string.
	GetSecret(ctx context.Context, location string) (string, error)

	// Validate checks if the provider-specific configuration settings are valid.
	Validate(settings map[string]string) error
}

// ProviderConfig carries backend-specific initialization parameters.
type ProviderConfig struct {
	Settings          map[string]string
	Query             string
	ResolveReferences bool
}

// Entry is a flat raw field map returned by a vault provider.
// Canonical metadata fields ("title", "path", "uuid", "tags", "group") sit directly
// at the top level alongside raw secret attributes.
type Entry map[string]any

// Title returns the canonical entry title, if present.
func (e Entry) Title() string {
	if e == nil {
		return ""
	}
	if s, ok := e["title"].(string); ok {
		return s
	}
	return ""
}

// Path returns the canonical entry path, if present.
func (e Entry) Path() string {
	if e == nil {
		return ""
	}
	if s, ok := e["path"].(string); ok {
		return s
	}
	return ""
}

// UUID returns the canonical entry UUID, if present.
func (e Entry) UUID() string {
	if e == nil {
		return ""
	}
	if s, ok := e["uuid"].(string); ok {
		return s
	}
	return ""
}

// Tags returns a copy of the entry's tag slice, if present.
func (e Entry) Tags() []string {
	if e == nil {
		return nil
	}
	raw, ok := e["tags"]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		cp := make([]string, len(v))
		copy(cp, v)
		return cp
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// Group returns the entry's group metadata map ("name", "uuid", "path"), if present.
func (e Entry) Group() map[string]any {
	if e == nil {
		return nil
	}
	if gm, ok := e["group"].(map[string]any); ok {
		return gm
	}
	return nil
}

// Clone returns an independent deep copy of the Entry map.
func (e Entry) Clone() Entry {
	if e == nil {
		return nil
	}
	cp := make(Entry, len(e))
	for k, v := range e {
		cp[k] = CloneValue(v)
	}
	return cp
}

// CloneValue creates a deep copy of maps, slices, and scalar values.
func CloneValue(val any) any {
	if val == nil {
		return nil
	}
	switch v := val.(type) {
	case Entry:
		return v.Clone()
	case map[string]any:
		cp := make(map[string]any, len(v))
		for k, item := range v {
			cp[k] = CloneValue(item)
		}
		return cp
	case map[any]any:
		cp := make(map[any]any, len(v))
		for k, item := range v {
			cp[k] = CloneValue(item)
		}
		return cp
	case []any:
		cp := make([]any, len(v))
		for i, item := range v {
			cp[i] = CloneValue(item)
		}
		return cp
	case []string:
		cp := make([]string, len(v))
		copy(cp, v)
		return cp
	default:
		return v
	}
}

// SearchQuery defines provider-native filtering criteria for entry retrieval.
type SearchQuery struct {
	Title string
	Path  string
	Tags  []string
}

// SearchableProvider is implemented by read-only vault providers that expose
// their secret records as uniform flat Entry maps.
//
// Multi-field record providers (keepass, pass, vault) emit one Entry per record.
// Flat K/V providers (env, keyring) emit a single Entry containing all K/V pairs.
type SearchableProvider interface {
	// Search retrieves all raw entries matching the optional provider-native criteria.
	Search(ctx context.Context, query SearchQuery) ([]Entry, error)

	// GetEntry retrieves a single raw entry by location.
	GetEntry(ctx context.Context, location string) (Entry, error)
}

// ContextKey represents a custom type for context values to avoid collisions.
type ContextKey string

// ContextKeyTTL is the context key for specifying cache TTL duration.
const ContextKeyTTL ContextKey = "ttl"

// ContextKeyDepth is the context key for specifying recursion depth.
const ContextKeyDepth ContextKey = "depth"
