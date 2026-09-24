package provider

import (
	"context"
	"fmt"
	"path"
	"strings"
)

// matchPattern checks if name matches the glob pattern using path.Match.
// If the pattern is invalid syntax, it falls back to exact equality comparison.
func matchPattern(pattern, name string) bool {
	matched, err := path.Match(pattern, name)
	if err != nil {
		return pattern == name
	}
	return matched
}

// IsFieldAllowed determines whether a field name is permitted under the given
// include_fields and exclude_fields glob patterns.
//
// Rules:
// 1. If includeFields is non-empty, fieldName must match at least one include pattern.
// 2. If excludeFields is non-empty, fieldName must not match any exclude pattern.
func IsFieldAllowed(fieldName string, includeFields, excludeFields []string) bool {
	if len(includeFields) > 0 {
		matched := false
		for _, pattern := range includeFields {
			if matchPattern(pattern, fieldName) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	if len(excludeFields) > 0 {
		for _, pattern := range excludeFields {
			if matchPattern(pattern, fieldName) {
				return false
			}
		}
	}

	return true
}

// isFieldOrPathAllowed checks if a field is permitted under include and exclude glob patterns,
// matching against both the leaf attribute name and the full path.
func isFieldOrPathAllowed(fullPath, leafAttr string, includeFields, excludeFields []string) bool {
	if len(excludeFields) > 0 {
		for _, pattern := range excludeFields {
			if matchPattern(pattern, leafAttr) || (fullPath != "" && matchPattern(pattern, fullPath)) {
				return false
			}
		}
	}

	if len(includeFields) > 0 {
		matched := false
		for _, pattern := range includeFields {
			if matchPattern(pattern, leafAttr) || (fullPath != "" && matchPattern(pattern, fullPath)) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	return true
}

// FilterAttributes filters a map of entry attributes according to includeFields
// and excludeFields glob patterns, returning a new map with only allowed keys.
func FilterAttributes(attrs map[string]any, includeFields, excludeFields []string) map[string]any {
	if attrs == nil {
		return nil
	}
	if len(includeFields) == 0 && len(excludeFields) == 0 {
		return attrs
	}

	filtered := make(map[string]any, len(attrs))
	for k, v := range attrs {
		if IsFieldAllowed(k, includeFields, excludeFields) {
			filtered[k] = v
		}
	}
	return filtered
}

// FilterEntry applies field filtering to an Entry's Attributes map.
func FilterEntry(entry Entry, includeFields, excludeFields []string) Entry {
	if len(includeFields) == 0 && len(excludeFields) == 0 {
		return entry
	}
	entry.Attributes = FilterAttributes(entry.Attributes, includeFields, excludeFields)
	return entry
}

// FilteringProvider wraps a SecretProvider to enforce include_fields and exclude_fields
// glob attribute filtering for GetSecret calls.
type FilteringProvider struct {
	underlying    SecretProvider
	includeFields []string
	excludeFields []string
}

// Scheme delegates to the underlying provider.
func (f *FilteringProvider) Scheme() string {
	return f.underlying.Scheme()
}

// Initialize delegates to the underlying provider.
func (f *FilteringProvider) Initialize(ctx context.Context, config ProviderConfig) error {
	return f.underlying.Initialize(ctx, config)
}

// SetSecret delegates to the underlying provider.
func (f *FilteringProvider) SetSecret(ctx context.Context, location, value string) error {
	return f.underlying.SetSecret(ctx, location, value)
}

// DeleteSecret delegates to the underlying provider.
func (f *FilteringProvider) DeleteSecret(ctx context.Context, location string) error {
	return f.underlying.DeleteSecret(ctx, location)
}

// Validate delegates to the underlying provider.
func (f *FilteringProvider) Validate(settings map[string]string) error {
	return f.underlying.Validate(settings)
}

// Underlying returns the wrapped SecretProvider.
func (f *FilteringProvider) Underlying() SecretProvider {
	return f.underlying
}

// SearchableFilteringProvider implements SearchableProvider when the underlying provider is searchable.
type SearchableFilteringProvider struct {
	FilteringProvider
}

// GetEntry retrieves a structured entry and filters its attributes according to configured glob rules.
func (s *SearchableFilteringProvider) GetEntry(ctx context.Context, location string) (Entry, error) {
	searchable, ok := s.underlying.(SearchableProvider)
	if !ok {
		return Entry{}, fmt.Errorf("provider %q does not support structured entries", s.Scheme())
	}

	entry, err := searchable.GetEntry(ctx, location)
	if err != nil {
		return Entry{}, err
	}

	return FilterEntry(entry, s.includeFields, s.excludeFields), nil
}

// Search retrieves matching entries and filters all result entry attributes according to configured glob rules.
func (s *SearchableFilteringProvider) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	searchable, ok := s.underlying.(SearchableProvider)
	if !ok {
		return nil, fmt.Errorf("provider %q does not support searching", s.Scheme())
	}

	results, err := searchable.Search(ctx, query)
	if err != nil {
		return nil, err
	}

	filteredResults := make([]SearchResult, len(results))
	for i, r := range results {
		r.Entry = FilterEntry(r.Entry, s.includeFields, s.excludeFields)
		filteredResults[i] = r
	}

	return filteredResults, nil
}

// ValueResolvableFilteringProvider implements ValueResolvableProvider when underlying implements it.
type ValueResolvableFilteringProvider struct {
	FilteringProvider
}

// SupportsValueResolution delegates to the underlying provider.
func (v *ValueResolvableFilteringProvider) SupportsValueResolution() bool {
	if vr, ok := v.underlying.(ValueResolvableProvider); ok {
		return vr.SupportsValueResolution()
	}
	return false
}

// SearchableValueResolvableFilteringProvider implements both SearchableProvider and ValueResolvableProvider.
type SearchableValueResolvableFilteringProvider struct {
	SearchableFilteringProvider
}

// SupportsValueResolution delegates to the underlying provider.
func (s *SearchableValueResolvableFilteringProvider) SupportsValueResolution() bool {
	if vr, ok := s.underlying.(ValueResolvableProvider); ok {
		return vr.SupportsValueResolution()
	}
	return false
}

// NewFilteringProvider constructs a new filtering provider wrapper, preserving optional
// interfaces (SearchableProvider, ValueResolvableProvider) only when the underlying provider
// implements them.
func NewFilteringProvider(p SecretProvider, includeFields, excludeFields []string) SecretProvider {
	base := FilteringProvider{
		underlying:    p,
		includeFields: includeFields,
		excludeFields: excludeFields,
	}

	_, isSearchable := p.(SearchableProvider)
	_, isValResolvable := p.(ValueResolvableProvider)

	if isSearchable && isValResolvable {
		return &SearchableValueResolvableFilteringProvider{
			SearchableFilteringProvider: SearchableFilteringProvider{
				FilteringProvider: base,
			},
		}
	}
	if isSearchable {
		return &SearchableFilteringProvider{
			FilteringProvider: base,
		}
	}
	if isValResolvable {
		return &ValueResolvableFilteringProvider{
			FilteringProvider: base,
		}
	}
	return &base
}

// GetSecret checks if the requested attribute is permitted by field filters before returning the secret.
// Dispatch is performed by provider scheme to prevent filter bypasses across different path semantics.
func (f *FilteringProvider) GetSecret(ctx context.Context, location string) (string, error) {
	switch f.underlying.Scheme() {
	case "keepass":
		// Format: "[group/]entry[:attribute]". Omitted or empty selector defaults to "Password".
		effectiveAttr := "Password"
		if strings.Contains(location, ":") {
			parts := strings.SplitN(location, ":", 2)
			if parts[1] != "" {
				effectiveAttr = parts[1]
			}
		}
		leafAttr := effectiveAttr
		if lastDot := strings.LastIndex(effectiveAttr, "."); lastDot >= 0 {
			leafAttr = effectiveAttr[lastDot+1:]
		}
		if !isFieldOrPathAllowed(effectiveAttr, leafAttr, f.includeFields, f.excludeFields) {
			return "", fmt.Errorf("field %q is excluded by vault configuration", leafAttr)
		}
		return f.underlying.GetSecret(ctx, location)

	case "custom_vault":
		// Format: "entity[:attribute]". Bare location defaults to "Password".
		effectiveAttr := "Password"
		if strings.Contains(location, ":") {
			parts := strings.SplitN(location, ":", 2)
			effectiveAttr = parts[1]
		}
		if effectiveAttr == "" {
			return "", fmt.Errorf("field %q is excluded by vault configuration", "")
		}
		leafAttr := effectiveAttr
		if lastDot := strings.LastIndex(effectiveAttr, "."); lastDot >= 0 {
			leafAttr = effectiveAttr[lastDot+1:]
		}
		if !isFieldOrPathAllowed(effectiveAttr, leafAttr, f.includeFields, f.excludeFields) {
			return "", fmt.Errorf("field %q is excluded by vault configuration", leafAttr)
		}
		return f.underlying.GetSecret(ctx, location)

	case "search":
		// Stored search provider:
		// Attributes can be resolved case-insensitively, so look up the canonical key first.
		var canonicalField string
		var requestedField string

		if strings.Contains(location, ":") {
			parts := strings.SplitN(location, ":", 2)
			entryLoc := parts[0]
			attrName := parts[1]
			requestedField = attrName

			if searchable, ok := f.underlying.(SearchableProvider); ok {
				if entry, err := searchable.GetEntry(ctx, entryLoc); err == nil {
					for k := range entry.Attributes {
						if strings.EqualFold(k, attrName) {
							canonicalField = k
							break
						}
					}
					if canonicalField == "" {
						if strings.EqualFold(attrName, "title") {
							canonicalField = "Title"
						} else if strings.EqualFold(attrName, "tags") {
							canonicalField = "Tags"
						}
					}
				}
			}
			if canonicalField == "" {
				canonicalField = attrName
			}
		} else {
			requestedField = location
			if searchable, ok := f.underlying.(SearchableProvider); ok {
				if entry, err := searchable.GetEntry(ctx, location); err == nil {
					for k := range entry.Attributes {
						if strings.EqualFold(k, location) {
							canonicalField = k
							break
						}
					}
				}
			}
			if canonicalField == "" {
				// Bare location did not match any attribute name; defaults to "Password"
				canonicalField = "Password"
			}
		}

		leafCanonical := canonicalField
		if lastDot := strings.LastIndex(canonicalField, "."); lastDot >= 0 {
			leafCanonical = canonicalField[lastDot+1:]
		}
		if !isFieldOrPathAllowed(canonicalField, leafCanonical, f.includeFields, f.excludeFields) {
			return "", fmt.Errorf("field %q is excluded by vault configuration", leafCanonical)
		}

		if requestedField != "" && requestedField != canonicalField {
			leafRequested := requestedField
			if lastDot := strings.LastIndex(requestedField, "."); lastDot >= 0 {
				leafRequested = requestedField[lastDot+1:]
			}
			if !isFieldOrPathAllowed(requestedField, leafRequested, f.includeFields, f.excludeFields) {
				return "", fmt.Errorf("field %q is excluded by vault configuration", leafRequested)
			}
		}

		return f.underlying.GetSecret(ctx, location)

	case "yaml", "json":
		return f.getStaticSecret(ctx, location)

	default:
		// Generic fallback
		if strings.Contains(location, ":") {
			parts := strings.SplitN(location, ":", 2)
			attrName := parts[1]
			if attrName == "" {
				attrName = "Password"
			}
			leafAttr := attrName
			if lastDot := strings.LastIndex(attrName, "."); lastDot >= 0 {
				leafAttr = attrName[lastDot+1:]
			}
			if !isFieldOrPathAllowed(attrName, leafAttr, f.includeFields, f.excludeFields) {
				return "", fmt.Errorf("field %q is excluded by vault configuration", leafAttr)
			}
			return f.underlying.GetSecret(ctx, location)
		}

		if strings.Contains(location, ".") {
			leafAttr := location[strings.LastIndex(location, ".")+1:]
			if !isFieldOrPathAllowed(location, leafAttr, f.includeFields, f.excludeFields) {
				return "", fmt.Errorf("field %q is excluded by vault configuration", leafAttr)
			}
		} else {
			if !IsFieldAllowed(location, f.includeFields, f.excludeFields) && !IsFieldAllowed("Password", f.includeFields, f.excludeFields) {
				return "", fmt.Errorf("field %q is excluded by vault configuration", location)
			}
		}
		return f.underlying.GetSecret(ctx, location)
	}
}

// getStaticSecret resolves static dot-path secrets and enforces field filtering before delegating.
// Root-keyed and nested paths are resolved and filtered using the underlying provider's serializer.
func (f *FilteringProvider) getStaticSecret(ctx context.Context, location string) (string, error) {
	spAccessor, ok := f.underlying.(interface{ getStaticProvider() *staticProvider })
	if !ok {
		leafAttr := location
		if lastDot := strings.LastIndex(location, "."); lastDot >= 0 {
			leafAttr = location[lastDot+1:]
		}
		if !isFieldOrPathAllowed(location, leafAttr, f.includeFields, f.excludeFields) {
			return "", fmt.Errorf("field %q is excluded by vault configuration", leafAttr)
		}
		return f.underlying.GetSecret(ctx, location)
	}

	sp := spAccessor.getStaticProvider()

	var rawVal any
	var err error

	if sp.singleEntity {
		entry, ok := sp.entries[""]
		if !ok {
			return "", fmt.Errorf("%s provider: single entity not found", sp.scheme)
		}
		rawVal, err = resolveDotPath(entry.Attributes, location)
	} else {
		if sp.rawContent == nil {
			return "", fmt.Errorf("%s provider: not initialized or empty database", sp.scheme)
		}
		rawVal, err = resolveDotPath(sp.rawContent, location)
		if err != nil && sp.entitiesRootKey != "" && sp.entitiesRootKey != "." && !strings.HasPrefix(location, sp.entitiesRootKey+".") {
			if val2, err2 := resolveDotPath(sp.rawContent, sp.entitiesRootKey+"."+location); err2 == nil {
				rawVal = val2
				err = nil
			}
		}
	}

	if err != nil {
		return "", fmt.Errorf("%s provider: failed to resolve path %q: %w", sp.scheme, location, err)
	}

	// If rawVal is a map (an entire entity or nested object), filter its attributes
	if m, ok := normalizeEntryMap(rawVal); ok {
		filteredMap := FilterAttributes(m, f.includeFields, f.excludeFields)
		return sp.serialize(filteredMap)
	}

	// For scalar attributes, determine path candidates
	leafAttr := location
	if lastDot := strings.LastIndex(location, "."); lastDot >= 0 {
		leafAttr = location[lastDot+1:]
	}

	var strippedPath string
	if !sp.singleEntity && sp.entitiesRootKey != "" && sp.entitiesRootKey != "." {
		if strings.HasPrefix(location, sp.entitiesRootKey+".") {
			strippedPath = strings.TrimPrefix(location, sp.entitiesRootKey+".")
		}
	}

	var entityAttr string
	pathToCheck := strippedPath
	if pathToCheck == "" {
		pathToCheck = location
	}
	if !sp.singleEntity {
		if dotIdx := strings.Index(pathToCheck, "."); dotIdx >= 0 {
			entityAttr = pathToCheck[dotIdx+1:]
		}
	}

	if len(f.excludeFields) > 0 {
		for _, pattern := range f.excludeFields {
			if matchPattern(pattern, leafAttr) ||
				matchPattern(pattern, location) ||
				(strippedPath != "" && matchPattern(pattern, strippedPath)) ||
				(entityAttr != "" && matchPattern(pattern, entityAttr)) {
				return "", fmt.Errorf("field %q is excluded by vault configuration", leafAttr)
			}
		}
	}

	if len(f.includeFields) > 0 {
		matched := false
		for _, pattern := range f.includeFields {
			if matchPattern(pattern, leafAttr) ||
				matchPattern(pattern, location) ||
				(strippedPath != "" && matchPattern(pattern, strippedPath)) ||
				(entityAttr != "" && matchPattern(pattern, entityAttr)) {
				matched = true
				break
			}
		}
		if !matched {
			return "", fmt.Errorf("field %q is excluded by vault configuration", leafAttr)
		}
	}

	return sp.serialize(rawVal)
}
