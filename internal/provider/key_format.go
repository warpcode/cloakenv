package provider

import (
	"context"
	"strings"

	"github.com/warpcode/cloakenv/internal/utils"
)

// KeyFormatProvider wraps a SecretProvider to apply key formatting (e.g. env, snake, kebab)
// to entry attribute names.
type KeyFormatProvider struct {
	underlying SecretProvider
	format     string
}

// Format returns the configured key format.
func (k *KeyFormatProvider) Format() string {
	return k.format
}

// Scheme returns the underlying provider's scheme.
func (k *KeyFormatProvider) Scheme() string {
	return k.underlying.Scheme()
}

// Initialize delegates initialization to the underlying provider.
func (k *KeyFormatProvider) Initialize(ctx context.Context, cfg ProviderConfig) error {
	return k.underlying.Initialize(ctx, cfg)
}

// SetSecret delegates secret writing to the underlying provider.
func (k *KeyFormatProvider) SetSecret(ctx context.Context, location, value string) error {
	return k.underlying.SetSecret(ctx, location, value)
}

// DeleteSecret delegates secret deletion to the underlying provider.
func (k *KeyFormatProvider) DeleteSecret(ctx context.Context, location string) error {
	return k.underlying.DeleteSecret(ctx, location)
}

// Validate delegates configuration validation to the underlying provider.
func (k *KeyFormatProvider) Validate(settings map[string]string) error {
	return k.underlying.Validate(settings)
}

// Underlying returns the wrapped SecretProvider.
func (k *KeyFormatProvider) Underlying() SecretProvider {
	return k.underlying
}

// GetSecret resolves a secret. It first queries the underlying provider with the requested
// location directly. If not found and the underlying provider is searchable, it checks whether
// the requested attribute matches a formatted attribute key.
func (k *KeyFormatProvider) GetSecret(ctx context.Context, location string) (string, error) {
	val, err := k.underlying.GetSecret(ctx, location)
	if err == nil {
		return val, nil
	}

	searchable, isSearchable := k.underlying.(SearchableProvider)
	if !isSearchable {
		return "", err
	}

	var entityLoc string
	var attrName string
	if strings.Contains(location, ":") {
		parts := strings.SplitN(location, ":", 2)
		entityLoc = parts[0]
		attrName = parts[1]
	} else {
		entityLoc = location
		attrName = location
	}

	rawEntry, entryErr := searchable.GetEntry(ctx, entityLoc)
	if entryErr != nil {
		return "", err
	}

	for origKey := range rawEntry.Attributes {
		if utils.FormatKeyWithFormat(origKey, k.format) == attrName {
			var origLocation string
			if strings.Contains(location, ":") {
				origLocation = entityLoc + ":" + origKey
			} else {
				origLocation = origKey
			}
			return k.underlying.GetSecret(ctx, origLocation)
		}
	}

	return "", err
}

func (k *KeyFormatProvider) formatAttributes(attrs map[string]any) map[string]any {
	if attrs == nil || k.format == "" || k.format == utils.KeyFormatNone {
		return attrs
	}
	formatted := make(map[string]any, len(attrs))
	for attrKey, val := range attrs {
		formatted[utils.FormatKeyWithFormat(attrKey, k.format)] = formatValue(val, k.format)
	}
	return formatted
}

func formatValue(val any, format string) any {
	if val == nil || format == "" || format == utils.KeyFormatNone {
		return val
	}
	if m, ok := val.(map[string]any); ok {
		newMap := make(map[string]any, len(m))
		for k, v := range m {
			newMap[utils.FormatKeyWithFormat(k, format)] = formatValue(v, format)
		}
		return newMap
	}
	if slice, ok := val.([]any); ok {
		newSlice := make([]any, len(slice))
		for i, elem := range slice {
			newSlice[i] = formatValue(elem, format)
		}
		return newSlice
	}
	return val
}

// SearchableKeyFormatProvider implements SearchableProvider when the underlying provider is searchable.
type SearchableKeyFormatProvider struct {
	KeyFormatProvider
}

// RootPrefixes delegates root prefix reporting if the underlying provider supports it.
func (s *SearchableKeyFormatProvider) RootPrefixes() []string {
	if rp, ok := s.underlying.(interface{ RootPrefixes() []string }); ok {
		return rp.RootPrefixes()
	}
	return nil
}

// GetEntryWithPath retrieves a structured entry and formats its attributes according to KeyFormat.
func (s *SearchableKeyFormatProvider) GetEntryWithPath(ctx context.Context, location string) (Entry, string, error) {
	if pWithPath, ok := s.underlying.(interface {
		GetEntryWithPath(ctx context.Context, location string) (Entry, string, error)
	}); ok {
		entry, path, err := pWithPath.GetEntryWithPath(ctx, location)
		if err != nil {
			return Entry{}, "", err
		}
		entry.Attributes = s.formatAttributes(entry.Attributes)
		return entry, path, nil
	}
	entry, err := s.GetEntry(ctx, location)
	return entry, location, err
}

// GetEntry retrieves a structured entry and formats its attributes according to KeyFormat.
func (s *SearchableKeyFormatProvider) GetEntry(ctx context.Context, location string) (Entry, error) {
	entry, err := s.underlying.(SearchableProvider).GetEntry(ctx, location)
	if err != nil {
		return Entry{}, err
	}
	entry.Attributes = s.formatAttributes(entry.Attributes)
	return entry, nil
}

// Search queries the underlying provider and formats attributes on all returned entries.
func (s *SearchableKeyFormatProvider) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	results, err := s.underlying.(SearchableProvider).Search(ctx, query)
	if err != nil {
		return nil, err
	}
	formattedResults := make([]SearchResult, len(results))
	for i, r := range results {
		r.Entry.Attributes = s.formatAttributes(r.Entry.Attributes)
		formattedResults[i] = r
	}
	return formattedResults, nil
}

// ValueResolvableKeyFormatProvider implements ValueResolvableProvider when underlying implements it.
type ValueResolvableKeyFormatProvider struct {
	KeyFormatProvider
}

// SupportsValueResolution delegates to the underlying ValueResolvableProvider.
func (v *ValueResolvableKeyFormatProvider) SupportsValueResolution() bool {
	if vr, ok := v.underlying.(ValueResolvableProvider); ok {
		return vr.SupportsValueResolution()
	}
	return false
}

// SearchableValueResolvableKeyFormatProvider implements both SearchableProvider and ValueResolvableProvider.
type SearchableValueResolvableKeyFormatProvider struct {
	SearchableKeyFormatProvider
}

// SupportsValueResolution delegates to the underlying ValueResolvableProvider.
func (s *SearchableValueResolvableKeyFormatProvider) SupportsValueResolution() bool {
	if vr, ok := s.underlying.(ValueResolvableProvider); ok {
		return vr.SupportsValueResolution()
	}
	return false
}

// NewKeyFormatProvider constructs a KeyFormatProvider wrapper preserving optional provider interfaces.
func NewKeyFormatProvider(p SecretProvider, format string) SecretProvider {
	if format == "" || format == utils.KeyFormatNone {
		return p
	}

	base := KeyFormatProvider{
		underlying: p,
		format:     format,
	}

	_, isSearchable := p.(SearchableProvider)
	_, isValueResolvable := p.(ValueResolvableProvider)

	if isSearchable && isValueResolvable {
		return &SearchableValueResolvableKeyFormatProvider{
			SearchableKeyFormatProvider: SearchableKeyFormatProvider{
				KeyFormatProvider: base,
			},
		}
	}
	if isSearchable {
		return &SearchableKeyFormatProvider{
			KeyFormatProvider: base,
		}
	}
	if isValueResolvable {
		return &ValueResolvableKeyFormatProvider{
			KeyFormatProvider: base,
		}
	}
	return &base
}
