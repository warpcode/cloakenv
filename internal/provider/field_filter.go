package provider

import (
	"context"
	"fmt"
	"path"
	"strings"
)

// MatchGlob reports whether name matches the shell glob pattern.
// Slashes in pattern or name match wildcard '*' across boundary separators.
func MatchGlob(pattern, name string) bool {
	p := strings.ReplaceAll(pattern, "/", "\x01")
	n := strings.ReplaceAll(name, "/", "\x01")
	matched, err := path.Match(p, n)
	if err != nil {
		return false
	}
	return matched
}

// IsFieldAllowed returns true if the field name is permitted by includeFields and excludeFields rules.
func IsFieldAllowed(name string, includeFields, excludeFields []string) bool {
	if len(includeFields) > 0 {
		included := false
		for _, pattern := range includeFields {
			if MatchGlob(pattern, name) {
				included = true
				break
			}
		}
		if !included {
			return false
		}
	}

	if len(excludeFields) > 0 {
		for _, pattern := range excludeFields {
			if MatchGlob(pattern, name) {
				return false
			}
		}
	}

	return true
}

// FieldFilteringProvider wraps a SecretProvider to apply include_fields and
// exclude_fields glob filtering rules to entry attributes and secret requests.
type FieldFilteringProvider struct {
	inner         SecretProvider
	includeFields []string
	excludeFields []string
}

// NewFieldFilteringProvider creates a new FieldFilteringProvider wrapping inner.
func NewFieldFilteringProvider(inner SecretProvider, includeFields, excludeFields []string) *FieldFilteringProvider {
	return &FieldFilteringProvider{
		inner:         inner,
		includeFields: includeFields,
		excludeFields: excludeFields,
	}
}

// Inner returns the underlying wrapped SecretProvider.
func (f *FieldFilteringProvider) Inner() SecretProvider {
	return f.inner
}

// Scheme returns the scheme of the underlying provider.
func (f *FieldFilteringProvider) Scheme() string {
	return f.inner.Scheme()
}

// Initialize delegates initialization to the underlying provider.
func (f *FieldFilteringProvider) Initialize(ctx context.Context, cfg ProviderConfig) error {
	return f.inner.Initialize(ctx, cfg)
}

// GetSecret retrieves a secret from the underlying provider if the attribute is allowed by filtering rules.
func (f *FieldFilteringProvider) GetSecret(ctx context.Context, location string) (string, error) {
	attrName := extractAttributeName(ctx, f.inner, location)
	if !IsFieldAllowed(attrName, f.includeFields, f.excludeFields) {
		return "", fmt.Errorf("attribute %q is filtered out by vault configuration", attrName)
	}
	return f.inner.GetSecret(ctx, location)
}

// SetSecret writes a secret to the underlying provider if the attribute is allowed by filtering rules.
func (f *FieldFilteringProvider) SetSecret(ctx context.Context, location string, value string) error {
	attrName := extractAttributeName(ctx, f.inner, location)
	if !IsFieldAllowed(attrName, f.includeFields, f.excludeFields) {
		return fmt.Errorf("attribute %q is filtered out by vault configuration", attrName)
	}
	return f.inner.SetSecret(ctx, location, value)
}

// DeleteSecret removes a secret from the underlying provider if the attribute is allowed by filtering rules.
func (f *FieldFilteringProvider) DeleteSecret(ctx context.Context, location string) error {
	attrName := extractAttributeName(ctx, f.inner, location)
	if !IsFieldAllowed(attrName, f.includeFields, f.excludeFields) {
		return fmt.Errorf("attribute %q is filtered out by vault configuration", attrName)
	}
	return f.inner.DeleteSecret(ctx, location)
}

// Validate delegates settings validation to the underlying provider.
func (f *FieldFilteringProvider) Validate(settings map[string]string) error {
	return f.inner.Validate(settings)
}

// GetEntry retrieves an entry and filters its attributes based on include/exclude rules.
func (f *FieldFilteringProvider) GetEntry(ctx context.Context, location string) (Entry, error) {
	searchable, ok := f.inner.(SearchableProvider)
	if !ok {
		return Entry{}, fmt.Errorf("provider %q does not support structured entries", f.inner.Scheme())
	}

	entry, err := searchable.GetEntry(ctx, location)
	if err != nil {
		return Entry{}, err
	}

	entry.Attributes = f.filterAttributes(entry.Attributes)
	return entry, nil
}

// Search retrieves entries matching query and filters their attributes.
func (f *FieldFilteringProvider) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	searchable, ok := f.inner.(SearchableProvider)
	if !ok {
		return nil, fmt.Errorf("provider %q does not support searching", f.inner.Scheme())
	}

	results, err := searchable.Search(ctx, query)
	if err != nil {
		return nil, err
	}

	filteredResults := make([]SearchResult, len(results))
	for i, r := range results {
		r.Entry.Attributes = f.filterAttributes(r.Entry.Attributes)
		filteredResults[i] = r
	}

	return filteredResults, nil
}

// SupportsValueResolution implements ValueResolvableProvider if the inner provider supports it.
func (f *FieldFilteringProvider) SupportsValueResolution() bool {
	if vr, ok := f.inner.(ValueResolvableProvider); ok {
		return vr.SupportsValueResolution()
	}
	return false
}

func (f *FieldFilteringProvider) filterAttributes(attrs map[string]any) map[string]any {
	if attrs == nil {
		return nil
	}

	filtered := make(map[string]any)
	for k, v := range attrs {
		if IsFieldAllowed(k, f.includeFields, f.excludeFields) {
			filtered[k] = v
		}
	}
	return filtered
}

func extractAttributeName(ctx context.Context, inner SecretProvider, location string) string {
	if idx := strings.LastIndex(location, ":"); idx >= 0 {
		return location[idx+1:]
	}

	if searchable, ok := inner.(SearchableProvider); ok {
		if _, err := searchable.GetEntry(ctx, location); err == nil {
			return "Password"
		}
	}

	if idx := strings.LastIndex(location, "/"); idx >= 0 {
		return location[idx+1:]
	}

	if idx := strings.LastIndex(location, "."); idx >= 0 {
		return location[idx+1:]
	}

	return location
}
