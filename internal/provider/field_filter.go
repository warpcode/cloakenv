package provider

import (
	"context"
	"fmt"
	"path"
	"strings"
)

// FieldFilter manages include and exclude glob patterns for vault field filtering.
type FieldFilter struct {
	IncludeFields []string
	ExcludeFields []string
}

// NewFieldFilter returns a new FieldFilter instance.
func NewFieldFilter(include, exclude []string) *FieldFilter {
	if len(include) == 0 && len(exclude) == 0 {
		return nil
	}
	return &FieldFilter{
		IncludeFields: include,
		ExcludeFields: exclude,
	}
}

// IsEmpty reports whether the filter contains no include or exclude rules.
func (ff *FieldFilter) IsEmpty() bool {
	return ff == nil || (len(ff.IncludeFields) == 0 && len(ff.ExcludeFields) == 0)
}

// Validate checks whether all include and exclude glob patterns are syntactically valid.
func (ff *FieldFilter) Validate() error {
	if ff == nil {
		return nil
	}
	for _, pattern := range ff.IncludeFields {
		if _, err := path.Match(pattern, ""); err != nil {
			return fmt.Errorf("invalid glob pattern %q in include_fields: %w", pattern, err)
		}
	}
	for _, pattern := range ff.ExcludeFields {
		if _, err := path.Match(pattern, ""); err != nil {
			return fmt.Errorf("invalid glob pattern %q in exclude_fields: %w", pattern, err)
		}
	}
	return nil
}

// IsAllowed reports whether fieldName passes the configured include and exclude rules.
func (ff *FieldFilter) IsAllowed(fieldName string) bool {
	if ff == nil || ff.IsEmpty() {
		return true
	}

	if len(ff.IncludeFields) > 0 {
		matched := false
		for _, pattern := range ff.IncludeFields {
			if matchFieldGlob(pattern, fieldName) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	if len(ff.ExcludeFields) > 0 {
		for _, pattern := range ff.ExcludeFields {
			if matchFieldGlob(pattern, fieldName) {
				return false
			}
		}
	}

	return true
}

// FilterAttributes returns a new map containing only the attributes allowed by the filter.
func (ff *FieldFilter) FilterAttributes(attrs map[string]any) map[string]any {
	if ff == nil || ff.IsEmpty() || attrs == nil {
		return attrs
	}
	filtered := make(map[string]any, len(attrs))
	for k, v := range attrs {
		if ff.IsAllowed(k) {
			filtered[k] = v
		}
	}
	return filtered
}

func matchFieldGlob(pattern, name string) bool {
	cleanPattern := strings.ReplaceAll(pattern, "/", "\x00")
	cleanName := strings.ReplaceAll(name, "/", "\x00")
	matched, err := path.Match(cleanPattern, cleanName)
	if err != nil {
		return pattern == name
	}
	return matched
}

// FilteredProvider wraps a SecretProvider to enforce field inclusion and exclusion filtering.
type FilteredProvider struct {
	provider     SecretProvider
	filter       *FieldFilter
	vaultName    string
	singleEntity bool
}

// NewFilteredProvider wraps a provider with the given FieldFilter.
func NewFilteredProvider(p SecretProvider, filter *FieldFilter, vaultName string, singleEntity bool) *FilteredProvider {
	return &FilteredProvider{
		provider:     p,
		filter:       filter,
		vaultName:    vaultName,
		singleEntity: singleEntity,
	}
}

// Scheme returns the underlying provider's scheme.
func (fp *FilteredProvider) Scheme() string {
	return fp.provider.Scheme()
}

// Initialize initializes the underlying provider.
func (fp *FilteredProvider) Initialize(ctx context.Context, cfg ProviderConfig) error {
	return fp.provider.Initialize(ctx, cfg)
}

// GetSecret checks if the requested attribute is allowed, then retrieves the secret.
func (fp *FilteredProvider) GetSecret(ctx context.Context, location string) (string, error) {
	attr := extractAttributeName(location, fp.singleEntity)
	if !fp.filter.IsAllowed(attr) {
		return "", fmt.Errorf("attribute %q in vault %q is filtered out by vault configuration", attr, fp.vaultName)
	}
	return fp.provider.GetSecret(ctx, location)
}

// SetSecret checks if the requested attribute is allowed, then sets the secret.
func (fp *FilteredProvider) SetSecret(ctx context.Context, location string, value string) error {
	attr := extractAttributeName(location, fp.singleEntity)
	if !fp.filter.IsAllowed(attr) {
		return fmt.Errorf("attribute %q in vault %q is filtered out by vault configuration", attr, fp.vaultName)
	}
	return fp.provider.SetSecret(ctx, location, value)
}

// DeleteSecret checks if the requested attribute is allowed, then deletes the secret.
func (fp *FilteredProvider) DeleteSecret(ctx context.Context, location string) error {
	attr := extractAttributeName(location, fp.singleEntity)
	if !fp.filter.IsAllowed(attr) {
		return fmt.Errorf("attribute %q in vault %q is filtered out by vault configuration", attr, fp.vaultName)
	}
	return fp.provider.DeleteSecret(ctx, location)
}

// Validate validates backend settings.
func (fp *FilteredProvider) Validate(settings map[string]string) error {
	return fp.provider.Validate(settings)
}

// SupportsValueResolution implements ValueResolvableProvider if the underlying provider supports it.
func (fp *FilteredProvider) SupportsValueResolution() bool {
	if vr, ok := fp.provider.(ValueResolvableProvider); ok {
		return vr.SupportsValueResolution()
	}
	return false
}

// GetEntry retrieves a complete structured entry by location and filters its attributes.
func (fp *FilteredProvider) GetEntry(ctx context.Context, location string) (Entry, error) {
	searchable, ok := fp.provider.(SearchableProvider)
	if !ok {
		return Entry{}, fmt.Errorf("provider %q does not support structured entries", fp.provider.Scheme())
	}
	entry, err := searchable.GetEntry(ctx, location)
	if err != nil {
		return Entry{}, err
	}
	entry.Attributes = fp.filter.FilterAttributes(entry.Attributes)
	return entry, nil
}

// Search queries entries using SearchQuery and filters entry attributes in results.
func (fp *FilteredProvider) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	searchable, ok := fp.provider.(SearchableProvider)
	if !ok {
		return nil, fmt.Errorf("provider %q does not support searching", fp.provider.Scheme())
	}
	results, err := searchable.Search(ctx, query)
	if err != nil {
		return nil, err
	}
	for i := range results {
		results[i].Entry.Attributes = fp.filter.FilterAttributes(results[i].Entry.Attributes)
	}
	return results, nil
}

// extractAttributeName parses a location string to determine the attribute name being accessed.
func extractAttributeName(location string, singleEntity bool) string {
	if location == "" {
		return "Password"
	}

	if singleEntity {
		if idx := strings.Index(location, "."); idx >= 0 {
			return location[:idx]
		}
		return location
	}

	if idx := strings.Index(location, ":"); idx >= 0 {
		attr := location[idx+1:]
		if attr != "" {
			return attr
		}
		return "Password"
	}

	if idx := strings.Index(location, "."); idx >= 0 {
		attr := location[idx+1:]
		if attrIdx := strings.Index(attr, "."); attrIdx >= 0 {
			return attr[:attrIdx]
		}
		if attr != "" {
			return attr
		}
		return "Password"
	}

	return "Password"
}
