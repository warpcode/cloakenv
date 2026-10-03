package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/warpcode/cloakenv/internal/config"
)

// convertBackslashGroups converts backslash capture group references (\1..\9)
// into Go regexp format ($1..$9).
func convertBackslashGroups(template string) string {
	var sb strings.Builder
	for i := 0; i < len(template); i++ {
		if template[i] == '\\' && i+1 < len(template) && template[i+1] >= '0' && template[i+1] <= '9' {
			sb.WriteByte('$')
			sb.WriteByte(template[i+1])
			i++
			continue
		}
		sb.WriteByte(template[i])
	}
	return sb.String()
}

// ApplyMappingAndFilteringToAttributes transforms an attribute map by applying regex key mapping rules,
// then applies include_fields and exclude_fields glob filtering to unmapped keys.
// Mapped keys preserve their original values and are exempt from include_fields and exclude_fields filtering.
func ApplyMappingAndFilteringToAttributes(attrs map[string]any, rules []config.MappingRule, includeFields, excludeFields []string) map[string]any {
	if attrs == nil {
		return nil
	}

	// 1. Apply mapping
	mappedMap := make(map[string]any, len(attrs))
	mappedKeys := make(map[string]bool)

	for k, v := range attrs {
		valToUse := v
		if m, isMap := normalizeEntryMap(v); isMap {
			valToUse = ApplyMappingAndFilteringToAttributes(m, rules, includeFields, excludeFields)
		} else if sliceVal, isSlice := v.([]any); isSlice {
			newSlice := make([]any, len(sliceVal))
			for i, elem := range sliceVal {
				if elemMap, isMap := normalizeEntryMap(elem); isMap {
					newSlice[i] = ApplyMappingAndFilteringToAttributes(elemMap, rules, includeFields, excludeFields)
				} else {
					newSlice[i] = elem
				}
			}
			valToUse = newSlice
		}

		matched := false
		for i := range rules {
			rule := &rules[i]
			if rule.CompiledRegex == nil {
				_ = rule.Compile()
			}
			if rule.CompiledRegex != nil && rule.CompiledRegex.MatchString(k) {
				template := convertBackslashGroups(rule.Key)
				newKey := rule.CompiledRegex.ReplaceAllString(k, template)
				if newKey != "" {
					mappedMap[newKey] = valToUse
					mappedKeys[newKey] = true
					matched = true
					break
				}
			}
		}

		if !matched {
			mappedMap[k] = valToUse
		}
	}

	// 2. Apply filtering to unmapped keys if includeFields or excludeFields are specified
	if len(includeFields) == 0 && len(excludeFields) == 0 {
		return mappedMap
	}

	filteredMap := make(map[string]any, len(mappedMap))
	for k, v := range mappedMap {
		if mappedKeys[k] {
			// Mapped fields are included by default and are not filtered by include_fields / exclude_fields
			filteredMap[k] = v
		} else {
			// Unmapped fields are subject to glob filtering
			if isFieldOrPathAllowed("", k, includeFields, excludeFields) {
				filteredMap[k] = v
			}
		}
	}

	return filteredMap
}

// MappingProvider wraps a SecretProvider to perform regex key mapping and field filtering on entry attributes.
type MappingProvider struct {
	underlying    SecretProvider
	rules         []config.MappingRule
	includeFields []string
	excludeFields []string
}

// Scheme delegates to the underlying provider.
func (m *MappingProvider) Scheme() string {
	return m.underlying.Scheme()
}

// Initialize delegates to the underlying provider.
func (m *MappingProvider) Initialize(ctx context.Context, cfg ProviderConfig) error {
	return m.underlying.Initialize(ctx, cfg)
}

// SetSecret delegates to the underlying provider.
func (m *MappingProvider) SetSecret(ctx context.Context, location, value string) error {
	return m.underlying.SetSecret(ctx, location, value)
}

// DeleteSecret delegates to the underlying provider.
func (m *MappingProvider) DeleteSecret(ctx context.Context, location string) error {
	return m.underlying.DeleteSecret(ctx, location)
}

// Validate delegates to the underlying provider.
func (m *MappingProvider) Validate(settings map[string]string) error {
	return m.underlying.Validate(settings)
}

// Underlying returns the wrapped SecretProvider.
func (m *MappingProvider) Underlying() SecretProvider {
	return m.underlying
}

// GetSecret resolves a secret value, querying mapped entry attributes first before delegating.
func (m *MappingProvider) GetSecret(ctx context.Context, location string) (string, error) {
	if searchable, ok := m.underlying.(SearchableProvider); ok {
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

		if entityLoc != "" {
			entry, err := searchable.GetEntry(ctx, entityLoc)
			if err == nil && entry.Attributes != nil {
				mappedAttrs := ApplyMappingAndFilteringToAttributes(entry.Attributes, m.rules, m.includeFields, m.excludeFields)
				if attrName != "" {
					if val, ok := mappedAttrs[attrName]; ok {
						return serializeVal(val)
					}
				}
				if val, ok := mappedAttrs[location]; ok {
					return serializeVal(val)
				}
				if val, err := resolveDotPath(mappedAttrs, attrName); err == nil {
					return serializeVal(val)
				}
				if val, err := resolveDotPath(mappedAttrs, location); err == nil {
					return serializeVal(val)
				}
			}
		}

		// Also try single-entity entry (empty location)
		entry, err := searchable.GetEntry(ctx, "")
		if err == nil && entry.Attributes != nil {
			mappedAttrs := ApplyMappingAndFilteringToAttributes(entry.Attributes, m.rules, m.includeFields, m.excludeFields)
			if val, ok := mappedAttrs[location]; ok {
				return serializeVal(val)
			}
			if attrName != "" {
				if val, ok := mappedAttrs[attrName]; ok {
					return serializeVal(val)
				}
			}
			if val, err := resolveDotPath(mappedAttrs, location); err == nil {
				return serializeVal(val)
			}
		}
	}

	return m.underlying.GetSecret(ctx, location)
}

// SearchableMappingProvider implements SearchableProvider when the underlying provider is searchable.
type SearchableMappingProvider struct {
	MappingProvider
}

// GetEntryWithPath retrieves a structured entry and applies mapping and filtering to its attributes.
func (s *SearchableMappingProvider) GetEntryWithPath(ctx context.Context, location string) (Entry, string, error) {
	var entry Entry
	var authoritativePath string
	var err error

	if pathProvider, ok := s.underlying.(interface {
		GetEntryWithPath(ctx context.Context, location string) (Entry, string, error)
	}); ok {
		entry, authoritativePath, err = pathProvider.GetEntryWithPath(ctx, location)
		if err != nil {
			return Entry{}, "", err
		}
	} else if searchable, ok := s.underlying.(SearchableProvider); ok {
		entry, err = searchable.GetEntry(ctx, location)
		if err != nil {
			return Entry{}, "", err
		}
		authoritativePath = location
	} else {
		return Entry{}, "", fmt.Errorf("provider %q does not support structured entries", s.Scheme())
	}

	entry.Attributes = ApplyMappingAndFilteringToAttributes(entry.Attributes, s.rules, s.includeFields, s.excludeFields)
	return entry, authoritativePath, nil
}

// GetEntry retrieves a structured entry and applies mapping and filtering to its attributes.
func (s *SearchableMappingProvider) GetEntry(ctx context.Context, location string) (Entry, error) {
	entry, _, err := s.GetEntryWithPath(ctx, location)
	return entry, err
}

// Search retrieves matching entries and applies mapping and filtering to all result entry attributes.
func (s *SearchableMappingProvider) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	searchable, ok := s.underlying.(SearchableProvider)
	if !ok {
		return nil, fmt.Errorf("provider %q does not support searching", s.Scheme())
	}

	results, err := searchable.Search(ctx, query)
	if err != nil {
		return nil, err
	}

	mappedResults := make([]SearchResult, len(results))
	for i, r := range results {
		r.Entry.Attributes = ApplyMappingAndFilteringToAttributes(r.Entry.Attributes, s.rules, s.includeFields, s.excludeFields)
		mappedResults[i] = r
	}

	return mappedResults, nil
}

// ValueResolvableMappingProvider implements ValueResolvableProvider when underlying implements it.
type ValueResolvableMappingProvider struct {
	MappingProvider
}

// SupportsValueResolution delegates to the underlying provider.
func (v *ValueResolvableMappingProvider) SupportsValueResolution() bool {
	if vr, ok := v.underlying.(ValueResolvableProvider); ok {
		return vr.SupportsValueResolution()
	}
	return false
}

// SearchableValueResolvableMappingProvider implements both SearchableProvider and ValueResolvableProvider.
type SearchableValueResolvableMappingProvider struct {
	SearchableMappingProvider
}

// SupportsValueResolution delegates to the underlying provider.
func (s *SearchableValueResolvableMappingProvider) SupportsValueResolution() bool {
	if vr, ok := s.underlying.(ValueResolvableProvider); ok {
		return vr.SupportsValueResolution()
	}
	return false
}

// NewMappingProvider constructs a new mapping provider wrapper, preserving optional
// interfaces (SearchableProvider, ValueResolvableProvider) when the underlying provider implements them.
func NewMappingProvider(p SecretProvider, rules []config.MappingRule, includeFields, excludeFields []string) SecretProvider {
	base := MappingProvider{
		underlying:    p,
		rules:         rules,
		includeFields: includeFields,
		excludeFields: excludeFields,
	}

	_, isSearchable := p.(SearchableProvider)
	_, isValResolvable := p.(ValueResolvableProvider)

	if isSearchable && isValResolvable {
		return &SearchableValueResolvableMappingProvider{
			SearchableMappingProvider: SearchableMappingProvider{
				MappingProvider: base,
			},
		}
	}
	if isSearchable {
		return &SearchableMappingProvider{
			MappingProvider: base,
		}
	}
	if isValResolvable {
		return &ValueResolvableMappingProvider{
			MappingProvider: base,
		}
	}
	return &base
}
