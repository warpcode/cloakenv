// Package provider defines the SecretProvider interface for pluggable
// credential backends and the shared ProviderConfig type.
//
// Entry Processing Hierarchy (Filter Chain):
// Every entry is processed through a strict, deterministic 5-stage filter chain:
//   - Stage 1 — Raw Retrieval & Native Resolution: Load raw entry from backend
//     (resolving native placeholders/refs and URI references) or fold merge: sources.
//     Capture an immutable snapshot of the raw state as Entry.Original.
//   - Stage 2 — Field Filtering (include_fields -> exclude_fields): Filter candidate
//     attributes for the output projection (include_fields takes precedence over
//     exclude_fields; mapped fields are preserved for Stage 3).
//   - Stage 3 — Key Mapping (mapping): Apply regex capture groups and template
//     interpolation ({{ .meta.* }}, {{ .original.* }}) to transform matched keys.
//   - Stage 4 — Key Formatting (key_format): Transform output attribute keys
//     (env, snake, kebab, none).
//   - Stage 5 — Computed Attributes (attributes): Evaluate per-entry attributes:
//     templates using TemplateEngine, with access to both the transformed output
//     state (.attributes, .output.attributes) and immutable original state
//     (.original.attributes, .original.meta), injecting resulting keys into output.
package provider

import (
	"context"

	"github.com/warpcode/cloakenv/internal/config"
)

// SecretProvider is the core abstraction for all credential backends.
// Built-in providers (keyring://, env://, cache://, pass://) and remote-type providers
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
	Settings          map[string]string
	Attributes        map[string]any
	Entities          map[string]config.CustomEntityConfig
	SingleEntity      *bool
	EntityName        string
	Searchable        bool
	Tags              []string
	EntitiesRootKey   string
	SourceVaults      []string
	Query             string
	IncludeFields     []string
	ExcludeFields     []string
	Mapping           []config.MappingRule
	ResolveReferences bool
}

// GroupInfo captures hierarchical folder/group metadata for an entry.
type GroupInfo struct {
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
	UUID string `json:"uuid,omitempty" yaml:"uuid,omitempty"`
	Path string `json:"path,omitempty" yaml:"path,omitempty"`
}

// Entry represents a multi-secret credential record with metadata.
// It preserves an immutable snapshot of its raw, pre-transform state in Original
// across all filter chain stages (filtering, mapping, key formatting, and computed attributes).
type Entry struct {
	Title      string         `json:"title" yaml:"title"`
	Tags       []string       `json:"tags" yaml:"tags"`
	UUID       string         `json:"uuid,omitempty" yaml:"uuid,omitempty"`
	Group      *GroupInfo     `json:"group,omitempty" yaml:"group,omitempty"`
	Attributes map[string]any `json:"attributes" yaml:"attributes"`
	Original   *Entry         `json:"original,omitempty" yaml:"original,omitempty"`
}

// Clone returns an independent deep copy of the Entry with Original set to nil.
func (e Entry) Clone() Entry {
	clone := Entry{
		Title: e.Title,
		UUID:  e.UUID,
	}
	if e.Tags != nil {
		clone.Tags = make([]string, len(e.Tags))
		copy(clone.Tags, e.Tags)
	}
	if e.Group != nil {
		clone.Group = &GroupInfo{
			Name: e.Group.Name,
			UUID: e.Group.UUID,
			Path: e.Group.Path,
		}
	}
	if e.Attributes != nil {
		clone.Attributes = make(map[string]any, len(e.Attributes))
		for k, v := range e.Attributes {
			clone.Attributes[k] = CloneValue(v)
		}
	}
	return clone
}

// EnsureOriginal ensures that e.Original holds an immutable snapshot of e.
// If e.Original is already non-nil, it is retained; otherwise a deep clone of e is stored.
func (e Entry) EnsureOriginal() Entry {
	if e.Original == nil {
		orig := e.Clone()
		e.Original = &orig
	}
	return e
}

// CloneValue creates a deep copy of maps, slices, and scalar values.
func CloneValue(val any) any {
	if val == nil {
		return nil
	}
	switch v := val.(type) {
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

// BuildMetaMap constructs a normalized metadata map for an Entry.
func BuildMetaMap(e Entry, providerName, vaultName, entryPath string) map[string]any {
	meta := map[string]any{
		"Title":    e.Title,
		"title":    e.Title,
		"Tags":     e.Tags,
		"tags":     e.Tags,
		"UUID":     e.UUID,
		"uuid":     e.UUID,
		"Path":     entryPath,
		"path":     entryPath,
		"Vault":    vaultName,
		"vault":    vaultName,
		"Provider": providerName,
		"provider": providerName,
	}
	if e.Group != nil {
		grp := map[string]any{
			"Name": e.Group.Name,
			"name": e.Group.Name,
			"UUID": e.Group.UUID,
			"uuid": e.Group.UUID,
			"Path": e.Group.Path,
			"path": e.Group.Path,
		}
		meta["Group"] = grp
		meta["group"] = grp
	} else {
		meta["Group"] = nil
		meta["group"] = nil
	}
	return meta
}

// BuildTemplateData constructs the dual-state evaluation context (.original vs .output)
// for template interpolation in mapping rules, computed attributes, and composed entities.
func BuildTemplateData(entry Entry, providerName, vaultName, entryPath string) map[string]any {
	meta := BuildMetaMap(entry, providerName, vaultName, entryPath)

	origEntry := entry
	if entry.Original != nil {
		origEntry = *entry.Original
	}
	origMeta := BuildMetaMap(origEntry, providerName, vaultName, entryPath)

	origAttrs := origEntry.Attributes
	if origAttrs == nil {
		origAttrs = make(map[string]any)
	}
	outAttrs := entry.Attributes
	if outAttrs == nil {
		outAttrs = make(map[string]any)
	}

	originalData := map[string]any{
		"Meta":       origMeta,
		"meta":       origMeta,
		"_meta_":     origMeta,
		"Attributes": origAttrs,
		"attributes": origAttrs,
	}
	outputData := map[string]any{
		"Meta":       meta,
		"meta":       meta,
		"_meta_":     meta,
		"Attributes": outAttrs,
		"attributes": outAttrs,
	}

	return map[string]any{
		"Meta":       meta,
		"meta":       meta,
		"_meta_":     meta,
		"Attributes": outAttrs,
		"attributes": outAttrs,
		"Output":     outputData,
		"output":     outputData,
		"Original":   originalData,
		"original":   originalData,
	}
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

// WritableProvider is an interface implemented by secret providers that support
// writing and deleting secrets.
type WritableProvider interface {
	// SetSecret writes a secret value to the provider at the given location.
	SetSecret(ctx context.Context, location string, value string) error

	// DeleteSecret removes the secret at the given location from the provider.
	DeleteSecret(ctx context.Context, location string) error
}

// ContextKey represents a custom type for context values to avoid collisions.
type ContextKey string

// ContextKeyTTL is the context key for specifying cache TTL duration.
const ContextKeyTTL ContextKey = "ttl"

// ContextKeyDepth is the context key for specifying recursion depth.
const ContextKeyDepth ContextKey = "depth"

// ContextKeyRawKeys indicates that internal provider lookups should preserve unformatted raw keys.
const ContextKeyRawKeys ContextKey = "raw_keys"

// WithRawKeys returns a context signaling that providers should preserve raw attribute keys.
func WithRawKeys(ctx context.Context) context.Context {
	return context.WithValue(ctx, ContextKeyRawKeys, true)
}

// IsRawKeys checks whether the context requests unformatted raw attribute keys.
func IsRawKeys(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	raw, ok := ctx.Value(ContextKeyRawKeys).(bool)
	return ok && raw
}

type contextKeyFieldPolicy struct{}

// FieldPolicy holds include layers, exclude glob patterns, and mapping rules for attribute filtering.
type FieldPolicy struct {
	IncludeLayers [][]string
	ExcludeFields []string
	MappingRules  []config.MappingRule
}

// ApplyToEntry applies the composed field policy to an entry taking entryPath and rootPrefixes into account.
// Include layers are evaluated successively to enforce set intersection across all constraining layers.
// Attributes matching mapping rules are preserved and bypass include/exclude filters.
// Entry Title, Tags, UUID, and Group are preserved so search queries and entry matching can evaluate metadata.
func (fp *FieldPolicy) ApplyToEntry(entry Entry, entryPath string, rootPrefixes []string) Entry {
	if fp == nil {
		return entry
	}
	entry = entry.EnsureOriginal()
	origTitle := entry.Title
	origTags := entry.Tags
	origUUID := entry.UUID
	origGroup := entry.Group

	if len(fp.MappingRules) == 0 {
		current := entry
		if len(fp.IncludeLayers) > 0 {
			for _, layer := range fp.IncludeLayers {
				if len(layer) > 0 {
					current = FilterEntryWithPath(current, entryPath, rootPrefixes, layer, fp.ExcludeFields)
				}
			}
		} else if len(fp.ExcludeFields) > 0 {
			current = FilterEntryWithPath(current, entryPath, rootPrefixes, nil, fp.ExcludeFields)
		}
		current.Title = origTitle
		current.Tags = origTags
		current.UUID = origUUID
		current.Group = origGroup
		return current
	}

	compiledRules := make([]config.MappingRule, len(fp.MappingRules))
	for i, r := range fp.MappingRules {
		if r.CompiledRegex == nil {
			_ = r.Compile()
		}
		compiledRules[i] = r
	}

	mappedAttrs := make(map[string]any)
	unmappedAttrs := make(map[string]any)

	for k, v := range entry.Attributes {
		isMapped := false
		for _, r := range compiledRules {
			if r.CompiledRegex != nil && r.CompiledRegex.MatchString(k) {
				isMapped = true
				break
			}
		}
		if !isMapped && HasMappedDescendant(v, compiledRules) {
			isMapped = true
		}

		if isMapped {
			mappedAttrs[k] = v
		} else {
			unmappedAttrs[k] = v
		}
	}

	unmappedEntry := Entry{
		Title:      origTitle,
		Tags:       origTags,
		UUID:       origUUID,
		Group:      origGroup,
		Attributes: unmappedAttrs,
	}
	if len(fp.IncludeLayers) > 0 {
		for _, layer := range fp.IncludeLayers {
			if len(layer) > 0 {
				unmappedEntry = FilterEntryWithPath(unmappedEntry, entryPath, rootPrefixes, layer, fp.ExcludeFields)
			}
		}
	} else if len(fp.ExcludeFields) > 0 {
		unmappedEntry = FilterEntryWithPath(unmappedEntry, entryPath, rootPrefixes, nil, fp.ExcludeFields)
	}

	resultAttrs := make(map[string]any, len(mappedAttrs)+len(unmappedEntry.Attributes))
	for k, v := range unmappedEntry.Attributes {
		resultAttrs[k] = v
	}
	for k, v := range mappedAttrs {
		resultAttrs[k] = v
	}

	return Entry{
		Title:      origTitle,
		Tags:       origTags,
		UUID:       origUUID,
		Group:      origGroup,
		Attributes: resultAttrs,
		Original:   entry.Original,
	}
}

// WithFieldPolicy returns a context carrying include and exclude glob field policies.
// If ctx already carries a FieldPolicy, inherited and current policies are composed:
// active includes preserve layers to evaluate intersection, and excludes are unioned.
func WithFieldPolicy(ctx context.Context, includeFields, excludeFields []string) context.Context {
	return WithFieldPolicyAndMapping(ctx, includeFields, excludeFields, nil)
}

// WithFieldPolicyAndMapping returns a context carrying include, exclude, and mapping rules.
// If ctx already carries a FieldPolicy, inherited and current policies are composed:
// active includes preserve layers to evaluate intersection, excludes are unioned,
// and mapping rules are accumulated.
func WithFieldPolicyAndMapping(ctx context.Context, includeFields, excludeFields []string, mappingRules []config.MappingRule) context.Context {
	inherited := FieldPolicyFromContext(ctx)
	if inherited == nil {
		if len(includeFields) == 0 && len(excludeFields) == 0 && len(mappingRules) == 0 {
			return ctx
		}
		var layers [][]string
		if len(includeFields) > 0 {
			layers = append(layers, includeFields)
		}
		return context.WithValue(ctx, contextKeyFieldPolicy{}, &FieldPolicy{
			IncludeLayers: layers,
			ExcludeFields: excludeFields,
			MappingRules:  mappingRules,
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

	var effectiveMappingRules []config.MappingRule
	if len(inherited.MappingRules) > 0 {
		effectiveMappingRules = append(effectiveMappingRules, inherited.MappingRules...)
	}
	if len(mappingRules) > 0 {
		effectiveMappingRules = append(effectiveMappingRules, mappingRules...)
	}

	return context.WithValue(ctx, contextKeyFieldPolicy{}, &FieldPolicy{
		IncludeLayers: effectiveIncludeLayers,
		ExcludeFields: effectiveExcludes,
		MappingRules:  effectiveMappingRules,
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

// GetEntriesFunc resolves a URI into a slice of Entry objects.
type GetEntriesFunc func(ctx context.Context, uri string) ([]Entry, error)

// TemplateRenderFunc renders a string template against provided data.
type TemplateRenderFunc func(ctx context.Context, tmplStr string, data any) (string, error)
