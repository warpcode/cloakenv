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
// then applies include_fields and exclude_fields glob filtering to the mapped attributes using path-aware filtering.
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

	rawUnmappedKeys := make(map[string]bool)
	for _, k := range keys {
		matched := false
		for i := range compiledRules {
			if compiledRules[i].CompiledRegex != nil && compiledRules[i].CompiledRegex.MatchString(k) {
				matched = true
				break
			}
		}
		if !matched {
			rawUnmappedKeys[k] = true
		}
	}

	type mappedItem struct {
		key string
		val any
	}
	var mappedList []mappedItem
	mappedNewKeys := make(map[string]bool)

	for _, k := range keys {
		v := attrs[k]

		// Determine post-mapping key for k
		postKey := k
		matched := false
		for i := range compiledRules {
			rule := &compiledRules[i]
			if rule.CompiledRegex != nil && rule.CompiledRegex.MatchString(k) {
				template := convertBackslashGroups(rule.Key)
				newKey := rule.CompiledRegex.ReplaceAllString(k, template)
				if newKey != "" {
					postKey = newKey
					matched = true
					break
				}
			}
		}

		childPath := postKey
		if entryPath != "" {
			childPath = entryPath + "." + postKey
		}

		valToUse := v
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

		if matched {
			mappedList = append(mappedList, mappedItem{key: postKey, val: valToUse})
			mappedNewKeys[postKey] = true
		} else {
			mappedList = append(mappedList, mappedItem{key: k, val: valToUse})
		}
	}

	postMapAttrs := make(map[string]any, len(mappedList))
	for _, item := range mappedList {
		// Collision handling: if a mapped target key collides with a raw unmapped attribute name,
		// drop the colliding mapped key so it cannot evict or substitute a real attribute.
		if mappedNewKeys[item.key] && rawUnmappedKeys[item.key] {
			continue
		}
		if _, exists := postMapAttrs[item.key]; !exists {
			postMapAttrs[item.key] = item.val
		}
	}

	if len(includeFields) == 0 && len(excludeFields) == 0 {
		return postMapAttrs, nil
	}

	postEntry := Entry{
		Title:      entryTitle,
		Attributes: postMapAttrs,
	}
	filteredPost := FilterEntryWithPath(postEntry, entryPath, rootPrefixes, includeFields, excludeFields)
	return filteredPost.Attributes, nil
}

// MappingProvider wraps a SecretProvider to perform regex key mapping on entry attributes.
type MappingProvider struct {
	underlying SecretProvider
	rules      []config.MappingRule
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

// getFilteringParams extracts include_fields and exclude_fields if the underlying provider is a FilteringProvider.
func getFilteringParams(p SecretProvider) ([]string, []string) {
	curr := p
	for {
		if fp, ok := curr.(*FilteringProvider); ok {
			return fp.includeFields, fp.excludeFields
		}
		if sfp, ok := curr.(*SearchableFilteringProvider); ok {
			return sfp.includeFields, sfp.excludeFields
		}
		if vfp, ok := curr.(*ValueResolvableFilteringProvider); ok {
			return vfp.includeFields, vfp.excludeFields
		}
		if svfp, ok := curr.(*SearchableValueResolvableFilteringProvider); ok {
			return svfp.includeFields, svfp.excludeFields
		}
		if inner, ok := curr.(interface{ Underlying() SecretProvider }); ok {
			curr = inner.Underlying()
		} else {
			break
		}
	}
	return nil, nil
}

// GetSecret resolves a secret value, querying mapped entry attributes first.
// If an entry is found, lookup is performed exclusively in the mapped+filtered view.
func (m *MappingProvider) GetSecret(ctx context.Context, location string) (string, error) {
	includeFields, excludeFields := getFilteringParams(m.underlying)

	_, isSearchable := m.underlying.(SearchableProvider)

	if isSearchable {
		var entityLoc string
		var attrName string
		var hasColon bool
		if strings.Contains(location, ":") {
			parts := strings.SplitN(location, ":", 2)
			entityLoc = parts[0]
			attrName = parts[1]
			hasColon = true
		} else {
			entityLoc = location
			attrName = location
		}

		var rootPrefixes []string
		if rpProvider, ok := m.underlying.(interface{ RootPrefixes() []string }); ok {
			rootPrefixes = rpProvider.RootPrefixes()
		}

		checkMappedAttrs := func(mappedAttrs map[string]any, dotPath string) (string, bool, error) {
			if attrName != "" {
				if val, ok := mappedAttrs[attrName]; ok {
					sVal, err := serializeVal(val)
					return sVal, true, err
				}
			}
			if val, ok := mappedAttrs[location]; ok {
				sVal, err := serializeVal(val)
				return sVal, true, err
			}
			if attrName != "" {
				if val, err := resolveDotPath(mappedAttrs, attrName); err == nil {
					sVal, sErr := serializeVal(val)
					return sVal, true, sErr
				}
			}
			if dotPath != "" {
				if val, err := resolveDotPath(mappedAttrs, dotPath); err == nil {
					sVal, sErr := serializeVal(val)
					return sVal, true, sErr
				}
			}
			if val, err := resolveDotPath(mappedAttrs, location); err == nil {
				sVal, sErr := serializeVal(val)
				return sVal, true, sErr
			}
			if !hasColon {
				if val, ok := mappedAttrs["Password"]; ok {
					sVal, err := serializeVal(val)
					return sVal, true, err
				}
				// Whole-entity container resolution for static providers (yaml, json) when !hasColon
				scheme := m.underlying.Scheme()
				if scheme == "yaml" || scheme == "json" {
					if val, err := resolveDotPath(mappedAttrs, entityLoc); err == nil {
						sVal, sErr := serializeVal(val)
						return sVal, true, sErr
					}
					if len(mappedAttrs) > 0 {
						sVal, err := serializeVal(mappedAttrs)
						if err == nil {
							return sVal, true, nil
						}
						return "", true, err
					}
				}
			}
			return "", false, nil
		}

		if entityLoc != "" {
			rawEntry, authoritativePath, err := getRawEntryWithPath(m.underlying, ctx, entityLoc)
			if err == nil && rawEntry.Attributes != nil {
				// Reject if returned entry path/title does not match requested entity location
				if authoritativePath != "" && !strings.EqualFold(authoritativePath, entityLoc) && !strings.EqualFold(rawEntry.Title, entityLoc) {
					return "", fmt.Errorf("secret %q not found or excluded by vault configuration", location)
				}

				mappedAttrs, mapErr := ApplyMappingAndFilteringToAttributes(
					rawEntry.Attributes,
					m.rules,
					includeFields,
					excludeFields,
					authoritativePath,
					rootPrefixes,
					rawEntry.Title,
				)
				if mapErr != nil {
					return "", mapErr
				}
				if val, ok, sErr := checkMappedAttrs(mappedAttrs, ""); ok {
					if sErr != nil {
						return "", sErr
					}
					return val, nil
				}
				return "", fmt.Errorf("secret %q not found or excluded by vault configuration", location)
			}

			// Try stripping root prefixes (e.g. "hosts.ssh_host.hostname" -> "ssh_host.hostname")
			for _, rp := range rootPrefixes {
				if rp != "" && strings.HasPrefix(entityLoc, rp+".") {
					stripped := strings.TrimPrefix(entityLoc, rp+".")
					entityName := stripped
					nestedDotPath := ""
					if dotIdx := strings.Index(stripped, "."); dotIdx >= 0 {
						entityName = stripped[:dotIdx]
						nestedDotPath = stripped[dotIdx+1:]
					}
					if attrName != "" && hasColon {
						if nestedDotPath != "" {
							nestedDotPath = nestedDotPath + "." + attrName
						} else {
							nestedDotPath = attrName
						}
					}
					rawEntry, authoritativePath, err := getRawEntryWithPath(m.underlying, ctx, entityName)
					if err == nil && rawEntry.Attributes != nil {
						mappedAttrs, mapErr := ApplyMappingAndFilteringToAttributes(
							rawEntry.Attributes,
							m.rules,
							includeFields,
							excludeFields,
							authoritativePath,
							rootPrefixes,
							rawEntry.Title,
						)
						if mapErr != nil {
							return "", mapErr
						}
						if val, ok, sErr := checkMappedAttrs(mappedAttrs, nestedDotPath); ok {
							if sErr != nil {
								return "", sErr
							}
							return val, nil
						}
						return "", fmt.Errorf("secret %q not found or excluded by vault configuration", location)
					}
				}
			}

			// Multi-entity dot path resolution without root prefix (e.g. "ssh_host.hostname" -> entity "ssh_host", path "hostname")
			if dotIdx := strings.Index(entityLoc, "."); dotIdx >= 0 {
				baseEntity := entityLoc[:dotIdx]
				nestedDotPath := entityLoc[dotIdx+1:]
				if attrName != "" && hasColon {
					nestedDotPath = nestedDotPath + "." + attrName
				}
				rawEntry, authoritativePath, err := getRawEntryWithPath(m.underlying, ctx, baseEntity)
				if err == nil && rawEntry.Attributes != nil {
					mappedAttrs, mapErr := ApplyMappingAndFilteringToAttributes(
						rawEntry.Attributes,
						m.rules,
						includeFields,
						excludeFields,
						authoritativePath,
						rootPrefixes,
						rawEntry.Title,
					)
					if mapErr != nil {
						return "", mapErr
					}
					if val, ok, sErr := checkMappedAttrs(mappedAttrs, nestedDotPath); ok {
						if sErr != nil {
							return "", sErr
						}
						return val, nil
					}
					return "", fmt.Errorf("secret %q not found or excluded by vault configuration", location)
				}
			}
		}

		// Single-entity entry (empty location)
		rawEntry, authoritativePath, err := getRawEntryWithPath(m.underlying, ctx, "")
		if err == nil && rawEntry.Attributes != nil {
			mappedAttrs, mapErr := ApplyMappingAndFilteringToAttributes(
				rawEntry.Attributes,
				m.rules,
				includeFields,
				excludeFields,
				authoritativePath,
				rootPrefixes,
				rawEntry.Title,
			)
			if mapErr != nil {
				return "", mapErr
			}
			if val, ok, sErr := checkMappedAttrs(mappedAttrs, ""); ok {
				if sErr != nil {
					return "", sErr
				}
				return val, nil
			}
			return "", fmt.Errorf("secret %q not found or excluded by vault configuration", location)
		}

		return "", fmt.Errorf("secret %q not found or excluded by vault configuration", location)
	}

	if len(includeFields) > 0 || len(excludeFields) > 0 {
		if !isFieldOrPathAllowed("", location, includeFields, excludeFields) {
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

	includeFields, excludeFields := getFilteringParams(s.underlying)
	rootPrefixes := s.RootPrefixes()
	mappedAttrs, mapErr := ApplyMappingAndFilteringToAttributes(
		rawEntry.Attributes,
		s.rules,
		includeFields,
		excludeFields,
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
	// Query raw provider for search results
	curr := s.underlying
	for {
		if inner, ok := curr.(interface{ Underlying() SecretProvider }); ok {
			curr = inner.Underlying()
		} else {
			break
		}
	}

	searchable, ok := curr.(SearchableProvider)
	if !ok {
		return nil, fmt.Errorf("provider %q does not support searching", s.Scheme())
	}

	results, err := searchable.Search(ctx, query)
	if err != nil {
		return nil, err
	}

	includeFields, excludeFields := getFilteringParams(s.underlying)
	rootPrefixes := s.RootPrefixes()
	mappedResults := make([]SearchResult, len(results))
	for i, r := range results {
		mappedAttrs, mapErr := ApplyMappingAndFilteringToAttributes(
			r.Entry.Attributes,
			s.rules,
			includeFields,
			excludeFields,
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
func NewMappingProvider(p SecretProvider, rules []config.MappingRule) (SecretProvider, error) {
	compiledRules := make([]config.MappingRule, len(rules))
	for i, r := range rules {
		if err := r.Compile(); err != nil {
			return nil, fmt.Errorf("mapping rule match %q: %w", r.Match, err)
		}
		compiledRules[i] = r
	}

	base := MappingProvider{
		underlying: p,
		rules:      compiledRules,
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
