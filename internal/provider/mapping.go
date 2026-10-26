package provider

import (
	"bytes"
	"context"
	"fmt"
	"github.com/warpcode/cloakenv/internal/utils"
	"sort"
	"strings"
	"text/template"

	"github.com/warpcode/cloakenv/internal/config"
)

// ConvertBackslashGroups converts backslash capture group references (\1..\9)
// into Go regexp format ($1..$9).
func ConvertBackslashGroups(template string) string {
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

// HasMappedDescendant checks if val (map or slice) has any descendant key that matches one of the mapping rules.
func HasMappedDescendant(val any, rules []config.MappingRule) bool {
	if m, isMap := normalizeEntryMap(val); isMap {
		for k, v := range m {
			for _, r := range rules {
				if r.CompiledRegex != nil && r.CompiledRegex.MatchString(k) {
					return true
				}
			}
			if HasMappedDescendant(v, rules) {
				return true
			}
		}
	} else if slice, ok := val.([]any); ok {
		for _, elem := range slice {
			if HasMappedDescendant(elem, rules) {
				return true
			}
		}
	}
	return false
}

// ApplyMappingAndFilteringToAttributes transforms an attribute map by applying regex key mapping rules,
// then applies include_fields and exclude_fields glob filtering to unmapped attributes using path-aware filtering.
// Mapped fields always show regardless of include_fields or exclude_fields configured on the vault.

// renderMappingTemplate renders a text/template mapping key.
func renderMappingTemplate(templateStr string, rawEntry Entry, providerName string, vaultName string, entryPath string) (string, error) {
	funcs := template.FuncMap{
		"upper": strings.ToUpper,
		"lower": strings.ToLower,
		"snake": func(s string) string { return utils.FormatKeyWithFormat(s, utils.KeyFormatSnake) },
		"kebab": func(s string) string { return utils.FormatKeyWithFormat(s, utils.KeyFormatKebab) },
		"env":   func(s string) string { return utils.FormatKeyWithFormat(s, utils.KeyFormatEnv) },
	}

	tmpl, err := template.New("tmpl").Option("missingkey=error").Funcs(funcs).Parse(templateStr)
	if err != nil {
		return "", err
	}

	meta := map[string]any{
		"Title": rawEntry.Title, "title": rawEntry.Title,
		"Tags": rawEntry.Tags, "tags": rawEntry.Tags,
		"UUID": rawEntry.UUID, "uuid": rawEntry.UUID,
		"Path": entryPath, "path": entryPath,
		"Vault": vaultName, "vault": vaultName,
		"Provider": providerName, "provider": providerName,
	}
	if rawEntry.Group != nil {
		grp := map[string]any{
			"Name": rawEntry.Group.Name, "name": rawEntry.Group.Name,
			"UUID": rawEntry.Group.UUID, "uuid": rawEntry.Group.UUID,
			"Path": rawEntry.Group.Path, "path": rawEntry.Group.Path,
		}
		meta["Group"] = grp
		meta["group"] = grp
	} else {
		meta["Group"] = nil
		meta["group"] = nil
	}
	data := map[string]any{
		"Meta": meta,
		"meta": meta,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func ApplyMappingAndFilteringToAttributes(
	attrs map[string]any,
	rules []config.MappingRule,
	includeFields, excludeFields []string,
	entryPath string,
	rootPrefixes []string,
	rawEntry Entry,
	providerName string,
	vaultName string,
) (map[string]any, error) {
	filtered, _, err := applyMappingAndFiltering(attrs, rules, includeFields, excludeFields, entryPath, rootPrefixes, rawEntry, providerName, vaultName)
	return filtered, err
}

func applyMappingAndFiltering(
	attrs map[string]any,
	rules []config.MappingRule,
	includeFields, excludeFields []string,
	entryPath string,
	rootPrefixes []string,
	rawEntry Entry,
	providerName string,
	vaultName string,
) (map[string]any, bool, error) {
	if attrs == nil {
		return nil, false, nil
	}

	compiledRules := make([]config.MappingRule, len(rules))
	for i, r := range rules {
		if r.CompiledRegex == nil {
			if err := r.Compile(); err != nil {
				return nil, false, fmt.Errorf("mapping rule match %q: %w", r.Match, err)
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

	type itemInfo struct {
		key      string
		val      any
		isMapped bool
	}
	var items []itemInfo
	mappedNewKeys := make(map[string]bool)
	anyMapped := false

	for _, k := range keys {
		v := attrs[k]

		// Determine post-mapping key for k
		postKey := k
		matched := false
		for i := range compiledRules {
			rule := &compiledRules[i]
			if rule.CompiledRegex != nil && rule.CompiledRegex.MatchString(k) {
				templateStr := ConvertBackslashGroups(rule.Key)
				newKey := rule.CompiledRegex.ReplaceAllString(k, templateStr)
				if newKey != "" {
					if strings.Contains(newKey, "{{") && strings.Contains(newKey, "}}") {
						renderedKey, err := renderMappingTemplate(newKey, rawEntry, providerName, vaultName, entryPath)
						if err == nil {
							newKey = renderedKey
						} else {
							return nil, false, err
						}
					}
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
		childHasMapped := false
		if m, isMap := normalizeEntryMap(v); isMap {
			mappedSub, subHasMapped, err := applyMappingAndFiltering(m, compiledRules, includeFields, excludeFields, childPath, rootPrefixes, rawEntry, providerName, vaultName)
			if err != nil {
				return nil, false, err
			}
			valToUse = mappedSub
			childHasMapped = subHasMapped
		} else if sliceVal, isSlice := v.([]any); isSlice {
			newSlice := make([]any, len(sliceVal))
			for i, elem := range sliceVal {
				if elemMap, isMap := normalizeEntryMap(elem); isMap {
					mappedSub, subHasMapped, err := applyMappingAndFiltering(elemMap, compiledRules, includeFields, excludeFields, childPath, rootPrefixes, rawEntry, providerName, vaultName)
					if err != nil {
						return nil, false, err
					}
					newSlice[i] = mappedSub
					if subHasMapped {
						childHasMapped = true
					}
				} else {
					newSlice[i] = elem
				}
			}
			valToUse = newSlice
		}

		if matched {
			items = append(items, itemInfo{key: postKey, val: valToUse, isMapped: true})
			mappedNewKeys[postKey] = true
			anyMapped = true
		} else {
			items = append(items, itemInfo{key: k, val: valToUse, isMapped: childHasMapped})
			if childHasMapped {
				anyMapped = true
			}
		}
	}

	mappedAttrs := make(map[string]any)
	unmappedAttrs := make(map[string]any)

	for _, item := range items {
		// Collision handling: if a mapped target key collides with a raw unmapped attribute name,
		// drop the colliding mapped key so it cannot evict or substitute a real attribute.
		if mappedNewKeys[item.key] && rawUnmappedKeys[item.key] {
			continue
		}
		if item.isMapped {
			if _, exists := mappedAttrs[item.key]; !exists {
				mappedAttrs[item.key] = item.val
			}
		} else {
			if _, exists := unmappedAttrs[item.key]; !exists {
				unmappedAttrs[item.key] = item.val
			}
		}
	}

	var filteredUnmapped map[string]any
	if len(includeFields) == 0 && len(excludeFields) == 0 {
		filteredUnmapped = unmappedAttrs
	} else if len(unmappedAttrs) > 0 {
		postEntry := Entry{
			Title:      rawEntry.Title,
			Attributes: unmappedAttrs,
		}
		filteredPost := FilterEntryWithPath(postEntry, entryPath, rootPrefixes, includeFields, excludeFields)
		filteredUnmapped = filteredPost.Attributes
	}

	result := make(map[string]any, len(mappedAttrs)+len(filteredUnmapped))
	for k, v := range filteredUnmapped {
		result[k] = v
	}
	for k, v := range mappedAttrs {
		result[k] = v
	}

	return result, anyMapped, nil
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

		providerName, vaultName, _ := utils.ParseURI(location)
		if providerName == "" && m.underlying != nil {
			providerName = m.underlying.Scheme()
		}
		checkMappedAttrs := func(rawEntry Entry, authoritativePath string, mappedAttrs map[string]any, dotPath string) (string, bool, error) {
			if attrName != "" {
				if val, ok := mappedAttrs[attrName]; ok {
					sVal, err := serializeVal(val)
					return sVal, true, err
				}
				// Deterministic case-insensitive lookup on mappedAttrs so custom attributes
				// take precedence over entry metadata even when casing differs (e.g. :uuid vs UUID).
				keys := make([]string, 0, len(mappedAttrs))
				for k := range mappedAttrs {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					if strings.EqualFold(k, attrName) {
						sVal, err := serializeVal(mappedAttrs[k])
						return sVal, true, err
					}
				}
				if strings.EqualFold(attrName, "title") {
					allowed := false
					if len(m.rules) > 0 {
						allowed = isDefaultFieldMappedOrIncluded("title", m.rules, includeFields, excludeFields, authoritativePath, rootPrefixes, rawEntry, providerName, vaultName)
					} else {
						allowed = isDefaultFieldAllowed("title", authoritativePath, rootPrefixes, rawEntry.Title, includeFields, excludeFields)
					}
					if allowed && rawEntry.Title != "" {
						return rawEntry.Title, true, nil
					}
				}
				if strings.EqualFold(attrName, "tags") {
					allowed := false
					if len(m.rules) > 0 {
						allowed = isDefaultFieldMappedOrIncluded("tags", m.rules, includeFields, excludeFields, authoritativePath, rootPrefixes, rawEntry, providerName, vaultName)
					} else {
						allowed = isDefaultFieldAllowed("tags", authoritativePath, rootPrefixes, rawEntry.Title, includeFields, excludeFields)
					}
					if allowed && len(rawEntry.Tags) > 0 {
						sVal, err := serializeVal(rawEntry.Tags)
						return sVal, true, err
					}
				}
				if strings.EqualFold(attrName, "uuid") {
					allowed := false
					if len(m.rules) > 0 {
						allowed = isDefaultFieldMappedOrIncluded("uuid", m.rules, includeFields, excludeFields, authoritativePath, rootPrefixes, rawEntry, providerName, vaultName)
					} else {
						allowed = isDefaultFieldAllowed("uuid", authoritativePath, rootPrefixes, rawEntry.Title, includeFields, excludeFields)
					}
					if allowed && rawEntry.UUID != "" {
						return rawEntry.UUID, true, nil
					}
				}
				if strings.EqualFold(attrName, "group") || strings.EqualFold(attrName, "group_name") {
					allowed := false
					field := strings.ToLower(attrName)
					if len(m.rules) > 0 {
						allowed = isDefaultFieldMappedOrIncluded(field, m.rules, includeFields, excludeFields, authoritativePath, rootPrefixes, rawEntry, providerName, vaultName)
					} else {
						allowed = isDefaultFieldAllowed(field, authoritativePath, rootPrefixes, rawEntry.Title, includeFields, excludeFields)
					}
					if allowed && rawEntry.Group != nil && rawEntry.Group.Name != "" {
						return rawEntry.Group.Name, true, nil
					}
				}
				if strings.EqualFold(attrName, "group_uuid") {
					allowed := false
					if len(m.rules) > 0 {
						allowed = isDefaultFieldMappedOrIncluded("group_uuid", m.rules, includeFields, excludeFields, authoritativePath, rootPrefixes, rawEntry, providerName, vaultName)
					} else {
						allowed = isDefaultFieldAllowed("group_uuid", authoritativePath, rootPrefixes, rawEntry.Title, includeFields, excludeFields)
					}
					if allowed && rawEntry.Group != nil && rawEntry.Group.UUID != "" {
						return rawEntry.Group.UUID, true, nil
					}
				}
				if strings.EqualFold(attrName, "group_path") {
					allowed := false
					if len(m.rules) > 0 {
						allowed = isDefaultFieldMappedOrIncluded("group_path", m.rules, includeFields, excludeFields, authoritativePath, rootPrefixes, rawEntry, providerName, vaultName)
					} else {
						allowed = isDefaultFieldAllowed("group_path", authoritativePath, rootPrefixes, rawEntry.Title, includeFields, excludeFields)
					}
					if allowed && rawEntry.Group != nil && rawEntry.Group.Path != "" {
						return rawEntry.Group.Path, true, nil
					}
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
					rawEntry,
					providerName,
					vaultName,
				)
				if mapErr != nil {
					return "", mapErr
				}
				if val, ok, sErr := checkMappedAttrs(rawEntry, authoritativePath, mappedAttrs, ""); ok {
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
							rawEntry,
							providerName,
							vaultName,
						)
						if mapErr != nil {
							return "", mapErr
						}
						if val, ok, sErr := checkMappedAttrs(rawEntry, authoritativePath, mappedAttrs, nestedDotPath); ok {
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
						rawEntry,
						providerName,
						vaultName,
					)
					if mapErr != nil {
						return "", mapErr
					}
					if val, ok, sErr := checkMappedAttrs(rawEntry, authoritativePath, mappedAttrs, nestedDotPath); ok {
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
				rawEntry,
				providerName,
				vaultName,
			)
			if mapErr != nil {
				return "", mapErr
			}
			if val, ok, sErr := checkMappedAttrs(rawEntry, authoritativePath, mappedAttrs, ""); ok {
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

// IsDefaultFieldMappedOrIncluded checks whether a default metadata field
// ("title", "tags", "path", "vault", "provider") is produced/mapped by mapping rules
// or explicitly included in includeFields.
// Mapped fields always show regardless of include_fields or exclude_fields configured on the vault.
func IsDefaultFieldMappedOrIncluded(fieldName string, rules []config.MappingRule, includeFields, excludeFields []string, entryPath string, rootPrefixes []string, rawEntry Entry, providerName string, vaultName string) bool {
	fieldLower := strings.ToLower(fieldName)
	candidates := buildDefaultFieldCandidates(fieldName, entryPath, rootPrefixes, rawEntry.Title)

	for _, r := range rules {
		if r.CompiledRegex == nil {
			_ = r.Compile()
		}
		if r.CompiledRegex != nil {
			for _, c := range candidates {
				if r.CompiledRegex.MatchString(c) {
					templateStr := ConvertBackslashGroups(r.Key)
					newKey := r.CompiledRegex.ReplaceAllString(c, templateStr)
					if newKey != "" {
						if strings.Contains(newKey, "{{") && strings.Contains(newKey, "}}") {
							renderedKey, err := renderMappingTemplate(newKey, rawEntry, providerName, vaultName, entryPath)
							if err == nil {
								newKey = renderedKey
							} else {
								// Template evaluation failed, we just skip it as it might not be relevant
							}
						}
						if strings.EqualFold(newKey, fieldLower) {
							return true
						}
					}
				}
			}
		}
	}

	return IsDefaultFieldAllowed(fieldName, entryPath, rootPrefixes, rawEntry.Title, includeFields, excludeFields)
}

func isDefaultFieldMappedOrIncluded(fieldName string, rules []config.MappingRule, includeFields, excludeFields []string, entryPath string, rootPrefixes []string, rawEntry Entry, providerName string, vaultName string) bool {
	return IsDefaultFieldMappedOrIncluded(fieldName, rules, includeFields, excludeFields, entryPath, rootPrefixes, rawEntry, providerName, vaultName)
}

// GetEntryWithPath retrieves a structured entry and applies mapping and filtering to its attributes.
func (s *SearchableMappingProvider) GetEntryWithPath(ctx context.Context, location string) (Entry, string, error) {
	rawEntry, authoritativePath, err := getRawEntryWithPath(s.underlying, ctx, location)
	if err != nil {
		return Entry{}, "", err
	}

	includeFields, excludeFields := getFilteringParams(s.underlying)
	rootPrefixes := s.RootPrefixes()

	providerName, vaultName, _ := utils.ParseURI(location)
	if providerName == "" && s.underlying != nil {
		providerName = s.underlying.Scheme()
	}
	mappedAttrs, mapErr := ApplyMappingAndFilteringToAttributes(
		rawEntry.Attributes,
		s.rules,
		includeFields,
		excludeFields,
		authoritativePath,
		rootPrefixes,
		rawEntry,
		providerName,
		vaultName,
	)
	if mapErr != nil {
		return Entry{}, "", mapErr
	}

	rawEntry.Attributes = mappedAttrs

	if len(s.rules) > 0 {
		if !isDefaultFieldMappedOrIncluded("title", s.rules, includeFields, excludeFields, authoritativePath, rootPrefixes, rawEntry, providerName, vaultName) {
			rawEntry.Title = ""
		}
		if !isDefaultFieldMappedOrIncluded("tags", s.rules, includeFields, excludeFields, authoritativePath, rootPrefixes, rawEntry, providerName, vaultName) {
			rawEntry.Tags = nil
		}
		if !isDefaultFieldMappedOrIncluded("uuid", s.rules, includeFields, excludeFields, authoritativePath, rootPrefixes, rawEntry, providerName, vaultName) {
			rawEntry.UUID = ""
		}
		if !isDefaultFieldMappedOrIncluded("group", s.rules, includeFields, excludeFields, authoritativePath, rootPrefixes, rawEntry, providerName, vaultName) {
			rawEntry.Group = nil
		}
	} else {
		if !isDefaultFieldAllowed("title", authoritativePath, rootPrefixes, rawEntry.Title, includeFields, excludeFields) {
			rawEntry.Title = ""
		}
		if !isDefaultFieldAllowed("tags", authoritativePath, rootPrefixes, rawEntry.Title, includeFields, excludeFields) {
			rawEntry.Tags = nil
		}
		if !isDefaultFieldAllowed("uuid", authoritativePath, rootPrefixes, rawEntry.Title, includeFields, excludeFields) {
			rawEntry.UUID = ""
		}
		if !isDefaultFieldAllowed("group", authoritativePath, rootPrefixes, rawEntry.Title, includeFields, excludeFields) {
			rawEntry.Group = nil
		}
	}

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
		origTitle := r.Entry.Title
		origPath := r.Path

		mappedAttrs, mapErr := ApplyMappingAndFilteringToAttributes(
			r.Entry.Attributes,
			s.rules,
			includeFields,
			excludeFields,
			origPath,
			rootPrefixes,
			r.Entry,
			r.Provider,
			r.Vault,
		)
		if mapErr != nil {
			return nil, mapErr
		}
		r.Entry.Attributes = mappedAttrs

		if len(s.rules) > 0 {
			if !isDefaultFieldMappedOrIncluded("title", s.rules, includeFields, excludeFields, origPath, rootPrefixes, r.Entry, r.Provider, r.Vault) {
				r.Entry.Title = ""
			}
			if !isDefaultFieldMappedOrIncluded("tags", s.rules, includeFields, excludeFields, origPath, rootPrefixes, r.Entry, r.Provider, r.Vault) {
				r.Entry.Tags = nil
			}
			if !isDefaultFieldMappedOrIncluded("uuid", s.rules, includeFields, excludeFields, origPath, rootPrefixes, r.Entry, r.Provider, r.Vault) {
				r.Entry.UUID = ""
			}
			if !isDefaultFieldMappedOrIncluded("group", s.rules, includeFields, excludeFields, origPath, rootPrefixes, r.Entry, r.Provider, r.Vault) {
				r.Entry.Group = nil
			}
			if !isDefaultFieldMappedOrIncluded("path", s.rules, includeFields, excludeFields, origPath, rootPrefixes, r.Entry, r.Provider, r.Vault) {
				r.Path = ""
			}
			if !isDefaultFieldMappedOrIncluded("vault", s.rules, includeFields, excludeFields, origPath, rootPrefixes, r.Entry, r.Provider, r.Vault) {
				r.Vault = ""
			}
			if !isDefaultFieldMappedOrIncluded("provider", s.rules, includeFields, excludeFields, origPath, rootPrefixes, r.Entry, r.Provider, r.Vault) {
				r.Provider = ""
			}
		} else {
			if !isDefaultFieldAllowed("title", origPath, rootPrefixes, origTitle, includeFields, excludeFields) {
				r.Entry.Title = ""
			}
			if !isDefaultFieldAllowed("tags", origPath, rootPrefixes, origTitle, includeFields, excludeFields) {
				r.Entry.Tags = nil
			}
			if !isDefaultFieldAllowed("uuid", origPath, rootPrefixes, origTitle, includeFields, excludeFields) {
				r.Entry.UUID = ""
			}
			if !isDefaultFieldAllowed("group", origPath, rootPrefixes, origTitle, includeFields, excludeFields) {
				r.Entry.Group = nil
			}
			if !isDefaultFieldAllowed("path", origPath, rootPrefixes, origTitle, includeFields, excludeFields) {
				r.Path = ""
			}
			if !isDefaultFieldAllowed("vault", origPath, rootPrefixes, origTitle, includeFields, excludeFields) {
				r.Vault = ""
			}
			if !isDefaultFieldAllowed("provider", origPath, rootPrefixes, origTitle, includeFields, excludeFields) {
				r.Provider = ""
			}
		}

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
