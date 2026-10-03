package provider

import (
	"context"
	"fmt"
	"sort"
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
// then applies include_fields and exclude_fields glob filtering to unmapped keys using path-aware filtering.
// Mapped keys preserve their original values and are exempt from include_fields and exclude_fields filtering.
func ApplyMappingAndFilteringToAttributes(
	attrs map[string]any,
	rules []config.MappingRule,
	includeFields, excludeFields []string,
	entryPath string,
	rootPrefixes []string,
	entryTitle string,
) (map[string]any, error) {
	if attrs == nil {
		return nil, nil
	}

	compiledRules := make([]config.MappingRule, len(rules))
	for i, r := range rules {
		if r.CompiledRegex == nil {
			if err := r.Compile(); err != nil {
				return nil, fmt.Errorf("mapping rule match %q: %w", r.Match, err)
			}
		}
		compiledRules[i] = r
	}

	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	type mappedItem struct {
		key string
		val any
	}
	var mappedList []mappedItem
	mappedNewKeys := make(map[string]bool)
	unmappedAttrs := make(map[string]any)

	for _, k := range keys {
		v := attrs[k]
		valToUse := v

		childPath := k
		if entryPath != "" {
			childPath = entryPath + "." + k
		}

		if m, isMap := normalizeEntryMap(v); isMap {
			mappedSub, err := ApplyMappingAndFilteringToAttributes(m, compiledRules, includeFields, excludeFields, childPath, rootPrefixes, entryTitle)
			if err != nil {
				return nil, err
			}
			valToUse = mappedSub
		} else if sliceVal, isSlice := v.([]any); isSlice {
			newSlice := make([]any, len(sliceVal))
			for i, elem := range sliceVal {
				if elemMap, isMap := normalizeEntryMap(elem); isMap {
					mappedSub, err := ApplyMappingAndFilteringToAttributes(elemMap, compiledRules, includeFields, excludeFields, childPath, rootPrefixes, entryTitle)
					if err != nil {
						return nil, err
					}
					newSlice[i] = mappedSub
				} else {
					newSlice[i] = elem
				}
			}
			valToUse = newSlice
		}

		matched := false
		for i := range compiledRules {
			rule := &compiledRules[i]
			if rule.CompiledRegex != nil {
				loc := rule.CompiledRegex.FindStringSubmatchIndex(k)
				if loc != nil {
					template := convertBackslashGroups(rule.Key)
					res := rule.CompiledRegex.ExpandString(nil, template, k, loc)
					newKey := string(res)
					if newKey != "" {
						mappedList = append(mappedList, mappedItem{key: newKey, val: valToUse})
						mappedNewKeys[newKey] = true
						matched = true
						break
					}
				}
			}
		}

		if !matched {
			unmappedAttrs[k] = valToUse
		}
	}

	mappedAttrs := make(map[string]any, len(mappedList))
	for _, item := range mappedList {
		if _, exists := mappedAttrs[item.key]; !exists {
			mappedAttrs[item.key] = item.val
		}
	}

	// Drop colliding unmapped keys before recording exemption
	for k := range unmappedAttrs {
		if mappedNewKeys[k] {
			delete(unmappedAttrs, k)
		}
	}

	resultMap := make(map[string]any, len(mappedAttrs)+len(unmappedAttrs))
	for k, v := range mappedAttrs {
		resultMap[k] = v
	}

	if len(includeFields) == 0 && len(excludeFields) == 0 {
		for k, v := range unmappedAttrs {
			resultMap[k] = v
		}
		return resultMap, nil
	}

	unmappedEntry := Entry{
		Title:      entryTitle,
		Attributes: unmappedAttrs,
	}
	filteredUnmapped := FilterEntryWithPath(unmappedEntry, entryPath, rootPrefixes, includeFields, excludeFields)

	for k, v := range filteredUnmapped.Attributes {
		resultMap[k] = v
	}

	return resultMap, nil
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

// getRawEntryWithPath unwraps any FilteringProvider wrapper(s) to query the raw underlying provider for entry attributes.
func getRawEntryWithPath(p SecretProvider, ctx context.Context, location string) (Entry, string, error) {
	curr := p
	for {
		if inner, ok := curr.(interface{ Underlying() SecretProvider }); ok {
			curr = inner.Underlying()
		} else {
			break
		}
	}

	if pathProvider, ok := curr.(interface {
		GetEntryWithPath(ctx context.Context, location string) (Entry, string, error)
	}); ok {
		return pathProvider.GetEntryWithPath(ctx, location)
	} else if searchable, ok := curr.(SearchableProvider); ok {
		entry, err := searchable.GetEntry(ctx, location)
		return entry, location, err
	}
	return Entry{}, "", fmt.Errorf("provider %q does not support structured entries", p.Scheme())
}

// GetSecret resolves a secret value, querying mapped entry attributes first.
// If an entry is found, lookup is performed exclusively in the mapped+filtered view.
func (m *MappingProvider) GetSecret(ctx context.Context, location string) (string, error) {
	_, isSearchable := m.underlying.(SearchableProvider)

	if isSearchable {
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

		var rootPrefixes []string
		if rpProvider, ok := m.underlying.(interface{ RootPrefixes() []string }); ok {
			rootPrefixes = rpProvider.RootPrefixes()
		}

		if entityLoc != "" {
			rawEntry, authoritativePath, err := getRawEntryWithPath(m.underlying, ctx, entityLoc)
			if err == nil && rawEntry.Attributes != nil {
				mappedAttrs, mapErr := ApplyMappingAndFilteringToAttributes(
					rawEntry.Attributes,
					m.rules,
					m.includeFields,
					m.excludeFields,
					authoritativePath,
					rootPrefixes,
					rawEntry.Title,
				)
				if mapErr != nil {
					return "", mapErr
				}
				if attrName != "" {
					if val, ok := mappedAttrs[attrName]; ok {
						return serializeVal(val)
					}
				}
				if val, ok := mappedAttrs[location]; ok {
					return serializeVal(val)
				}
				if attrName != "" {
					if val, err := resolveDotPath(mappedAttrs, attrName); err == nil {
						return serializeVal(val)
					}
				}
				if val, err := resolveDotPath(mappedAttrs, location); err == nil {
					return serializeVal(val)
				}
				return "", fmt.Errorf("secret %q not found or excluded by vault configuration", location)
			}
		}

		// Also try single-entity entry (empty location)
		rawEntry, authoritativePath, err := getRawEntryWithPath(m.underlying, ctx, "")
		if err == nil && rawEntry.Attributes != nil {
			mappedAttrs, mapErr := ApplyMappingAndFilteringToAttributes(
				rawEntry.Attributes,
				m.rules,
				m.includeFields,
				m.excludeFields,
				authoritativePath,
				rootPrefixes,
				rawEntry.Title,
			)
			if mapErr != nil {
				return "", mapErr
			}
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
			if attrName != "" {
				if val, err := resolveDotPath(mappedAttrs, attrName); err == nil {
					return serializeVal(val)
				}
			}
			return "", fmt.Errorf("secret %q not found or excluded by vault configuration", location)
		}

		return "", fmt.Errorf("secret %q not found or excluded by vault configuration", location)
	}

	if len(m.includeFields) > 0 || len(m.excludeFields) > 0 {
		if !isFieldOrPathAllowed("", location, m.includeFields, m.excludeFields) {
			return "", fmt.Errorf("field %q is excluded by vault configuration", location)
		}
	}

	return m.underlying.GetSecret(ctx, location)
}

// SearchableMappingProvider implements SearchableProvider when the underlying provider is searchable.
type SearchableMappingProvider struct {
	MappingProvider
}

// RootPrefixes passes through root key prefixes from the underlying provider if supported.
func (s *SearchableMappingProvider) RootPrefixes() []string {
	if rpProvider, ok := s.underlying.(interface{ RootPrefixes() []string }); ok {
		return rpProvider.RootPrefixes()
	}
	return nil
}

// GetEntryWithPath retrieves a structured entry and applies mapping and filtering to its attributes.
func (s *SearchableMappingProvider) GetEntryWithPath(ctx context.Context, location string) (Entry, string, error) {
	rawEntry, authoritativePath, err := getRawEntryWithPath(s.underlying, ctx, location)
	if err != nil {
		return Entry{}, "", err
	}

	rootPrefixes := s.RootPrefixes()
	mappedAttrs, mapErr := ApplyMappingAndFilteringToAttributes(
		rawEntry.Attributes,
		s.rules,
		s.includeFields,
		s.excludeFields,
		authoritativePath,
		rootPrefixes,
		rawEntry.Title,
	)
	if mapErr != nil {
		return Entry{}, "", mapErr
	}

	rawEntry.Attributes = mappedAttrs
	return rawEntry, authoritativePath, nil
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

	rootPrefixes := s.RootPrefixes()
	mappedResults := make([]SearchResult, len(results))
	for i, r := range results {
		mappedAttrs, mapErr := ApplyMappingAndFilteringToAttributes(
			r.Entry.Attributes,
			s.rules,
			s.includeFields,
			s.excludeFields,
			r.Path,
			rootPrefixes,
			r.Entry.Title,
		)
		if mapErr != nil {
			return nil, mapErr
		}
		r.Entry.Attributes = mappedAttrs
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

// NewMappingProvider constructs a new mapping provider wrapper, precompiling rules and
// preserving optional interfaces (SearchableProvider, ValueResolvableProvider) when supported.
func NewMappingProvider(p SecretProvider, rules []config.MappingRule, includeFields, excludeFields []string) (SecretProvider, error) {
	compiledRules := make([]config.MappingRule, len(rules))
	for i, r := range rules {
		if err := r.Compile(); err != nil {
			return nil, fmt.Errorf("mapping rule match %q: %w", r.Match, err)
		}
		compiledRules[i] = r
	}

	base := MappingProvider{
		underlying:    p,
		rules:         compiledRules,
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
		}, nil
	}
	if isSearchable {
		return &SearchableMappingProvider{
			MappingProvider: base,
		}, nil
	}
	if isValResolvable {
		return &ValueResolvableMappingProvider{
			MappingProvider: base,
		}, nil
	}
	return &base, nil
}
