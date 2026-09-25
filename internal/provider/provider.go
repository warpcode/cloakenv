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
	Settings        map[string]string
	Attributes      map[string]any
	Entities        map[string]map[string]any
	SingleEntity    *bool
	EntityName      string
	Searchable      bool
	Tags            []string
	EntitiesRootKey string
	SourceVaults    []string
	Query           string
	IncludeFields   []string
	ExcludeFields   []string
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

// SearchResult wraps a found entry with provider type, vault name, and location details.
type SearchResult struct {
	Provider string `json:"provider" yaml:"provider"`
	Vault    string `json:"vault" yaml:"vault"`
	Path     string `json:"path" yaml:"path"`
	Entry    Entry  `json:"entry" yaml:"entry"`
}

// SearchableProvider is implemented by providers that support searching and entry retrieval.
type SearchableProvider interface {
	// Search retrieves all entries matching the query criteria.
	Search(ctx context.Context, query SearchQuery) ([]SearchResult, error)

	// GetEntry retrieves a complete structured entry by location.
	GetEntry(ctx context.Context, location string) (Entry, error)
}

// ValueResolvableProvider is implemented by providers that support the
// resolve_values config flag. When a provider implements this interface,
// the engine gates URI resolution of attribute values on the flag; when it
// does not, the engine resolves URI values unconditionally (legacy default).
type ValueResolvableProvider interface {
	// SupportsValueResolution returns true, confirming the provider honours
	// the resolve_values config option.
	SupportsValueResolution() bool
}

// ContextKey represents a custom type for context values to avoid collisions.
type ContextKey string

// ContextKeyTTL is the context key for specifying cache TTL duration.
const ContextKeyTTL ContextKey = "ttl"

// ContextKeyDepth is the context key for specifying recursion depth.
const ContextKeyDepth ContextKey = "depth"

type contextKeyFieldPolicy struct{}

// FieldPolicy holds include layers and exclude glob patterns for attribute filtering.
type FieldPolicy struct {
	IncludeLayers [][]string
	IncludeFields []string
	ExcludeFields []string
}

// ApplyToEntry applies the composed field policy to an entry taking entryPath and rootPrefixes into account.
// Include layers are evaluated successively to enforce set intersection across all constraining layers.
func (fp *FieldPolicy) ApplyToEntry(entry Entry, entryPath string, rootPrefixes []string) Entry {
	if fp == nil {
		return entry
	}
	current := entry
	for _, layer := range fp.IncludeLayers {
		if len(layer) > 0 {
			current = FilterEntryWithPath(current, entryPath, rootPrefixes, layer, nil)
		}
	}
	if len(fp.ExcludeFields) > 0 {
		current = FilterEntryWithPath(current, entryPath, rootPrefixes, nil, fp.ExcludeFields)
	}
	return current
}

// WithFieldPolicy returns a context carrying include and exclude glob field policies.
// If ctx already carries a FieldPolicy, inherited and current policies are composed:
// active includes preserve layers to evaluate intersection, and excludes are unioned.
func WithFieldPolicy(ctx context.Context, includeFields, excludeFields []string) context.Context {
	inherited := FieldPolicyFromContext(ctx)
	if inherited == nil {
		if len(includeFields) == 0 && len(excludeFields) == 0 {
			return ctx
		}
		var layers [][]string
		if len(includeFields) > 0 {
			layers = append(layers, includeFields)
		}
		return context.WithValue(ctx, contextKeyFieldPolicy{}, &FieldPolicy{
			IncludeLayers: layers,
			IncludeFields: includeFields,
			ExcludeFields: excludeFields,
		})
	}

	var effectiveExcludes []string
	seenEx := make(map[string]bool)
	for _, p := range inherited.ExcludeFields {
		if !seenEx[p] {
			seenEx[p] = true
			effectiveExcludes = append(effectiveExcludes, p)
		}
	}
	for _, p := range excludeFields {
		if !seenEx[p] {
			seenEx[p] = true
			effectiveExcludes = append(effectiveExcludes, p)
		}
	}

	var effectiveIncludeLayers [][]string
	if len(inherited.IncludeLayers) > 0 {
		effectiveIncludeLayers = append(effectiveIncludeLayers, inherited.IncludeLayers...)
	}
	if len(includeFields) > 0 {
		effectiveIncludeLayers = append(effectiveIncludeLayers, includeFields)
	}

	var effectiveIncludes []string
	if len(effectiveIncludeLayers) == 1 {
		effectiveIncludes = effectiveIncludeLayers[0]
	}

	return context.WithValue(ctx, contextKeyFieldPolicy{}, &FieldPolicy{
		IncludeLayers: effectiveIncludeLayers,
		IncludeFields: effectiveIncludes,
		ExcludeFields: effectiveExcludes,
	})
}

// FieldPolicyFromContext retrieves the FieldPolicy from ctx, if present.
func FieldPolicyFromContext(ctx context.Context) *FieldPolicy {
	if ctx == nil {
		return nil
	}
	if fp, ok := ctx.Value(contextKeyFieldPolicy{}).(*FieldPolicy); ok {
		return fp
	}
	return nil
}
