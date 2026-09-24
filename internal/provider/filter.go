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

// isFieldAuthorized checks if a field is permitted under include and exclude glob patterns,
// matching against leaf attribute name and all candidate paths.
func isFieldAuthorized(leafAttr string, candidates []string, includeFields, excludeFields []string) bool {
	if len(excludeFields) > 0 {
		for _, pattern := range excludeFields {
			if matchPattern(pattern, leafAttr) {
				return false
			}
			for _, c := range candidates {
				if c != "" && matchPattern(pattern, c) {
					return false
				}
			}
		}
	}

	if len(includeFields) > 0 {
		matched := false
		for _, pattern := range includeFields {
			if matchPattern(pattern, leafAttr) {
				matched = true
				break
			}
			for _, c := range candidates {
				if c != "" && matchPattern(pattern, c) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if !matched {
			return false
		}
	}

	return true
}

func isContainerDirectlyIncluded(leafAttr string, candidates []string, includeFields []string) bool {
	if len(includeFields) == 0 {
		return true
	}
	for _, pattern := range includeFields {
		if matchPattern(pattern, leafAttr) {
			return true
		}
		for _, c := range candidates {
			if c != "" && matchPattern(pattern, c) {
				return true
			}
		}
	}
	return false
}

// projectRecursive performs path-aware recursive canonical projection on maps and slices.
func projectRecursive(val any, pfx string, prefixes []string, includeFields, excludeFields []string, parentIncluded bool) (any, bool) {
	m, isMap := normalizeEntryMap(val)
	if isMap {
		filtered := make(map[string]any, len(m))
		for k, v := range m {
			kLeaf := k
			if lastDot := strings.LastIndex(k, "."); lastDot >= 0 {
				kLeaf = k[lastDot+1:]
			}
			childPfx := k
			if pfx != "" {
				childPfx = pfx + "." + k
			}
			var childCandidates []string
			childCandidates = append(childCandidates, childPfx, k)
			for _, p := range prefixes {
				if p != "" {
					childCandidates = append(childCandidates, p+"."+k)
					if pfx != "" {
						childCandidates = append(childCandidates, p+"."+pfx+"."+k)
					}
				}
			}
			for i, c := range childCandidates {
				childCandidates[i] = normalizeDotPath(c)
			}

			// Add ancestor path segments so subtree-exclude and subtree-include patterns apply.
			var ancestorCandidates []string
			for _, c := range childCandidates {
				if c == "" {
					continue
				}
				segments := strings.Split(c, ".")
				for i := 1; i < len(segments); i++ {
					ancestor := strings.Join(segments[:i], ".")
					ancestorCandidates = append(ancestorCandidates, ancestor)
				}
				slashParts := strings.Split(c, "/")
				for i := 1; i < len(slashParts); i++ {
					ancestorCandidates = append(ancestorCandidates, strings.Join(slashParts[:i], "/"))
				}
			}

			allCandidates := make([]string, 0, len(childCandidates)+len(ancestorCandidates))
			allCandidates = append(allCandidates, childCandidates...)
			allCandidates = append(allCandidates, ancestorCandidates...)

			// Check exclude_fields first
			childExcluded := false
			if len(excludeFields) > 0 {
				for _, pattern := range excludeFields {
					if matchPattern(pattern, kLeaf) {
						childExcluded = true
						break
					}
					for _, c := range allCandidates {
						if c != "" && matchPattern(pattern, c) {
							childExcluded = true
							break
						}
					}
					if childExcluded {
						break
					}
				}
			}
			if childExcluded {
				continue
			}

			childDirectlyIncluded := parentIncluded || isContainerDirectlyIncluded(kLeaf, allCandidates, includeFields)

			// If child is a map:
			if _, ok := normalizeEntryMap(v); ok {
				projChild, hasAllowed := projectRecursive(v, childPfx, prefixes, includeFields, excludeFields, childDirectlyIncluded)
				if hasAllowed {
					filtered[k] = projChild
				}
				continue
			}

			// If child is a slice:
			if childSlice, ok := v.([]any); ok {
				projSlice, hasAllowed := projectSliceRecursive(childSlice, childPfx, prefixes, includeFields, excludeFields, childDirectlyIncluded)
				if hasAllowed {
					filtered[k] = projSlice
				}
				continue
			}

			// Scalar value
			if childDirectlyIncluded || isFieldAuthorized(kLeaf, allCandidates, includeFields, excludeFields) {
				filtered[k] = v
			}
		}

		if len(filtered) > 0 || len(includeFields) == 0 || parentIncluded {
			return filtered, true
		}
		return nil, false
	}

	return val, true
}

func projectSliceRecursive(slice []any, pfx string, prefixes []string, includeFields, excludeFields []string, parentIncluded bool) ([]any, bool) {
	filtered := make([]any, 0, len(slice))
	for _, item := range slice {
		if _, ok := normalizeEntryMap(item); ok {
			projItem, hasAllowed := projectRecursive(item, pfx, prefixes, includeFields, excludeFields, parentIncluded)
			if hasAllowed {
				filtered = append(filtered, projItem)
			}
		} else if itemSlice, ok := item.([]any); ok {
			projSlice, hasAllowed := projectSliceRecursive(itemSlice, pfx, prefixes, includeFields, excludeFields, parentIncluded)
			if hasAllowed {
				filtered = append(filtered, projSlice)
			}
		} else {
			if parentIncluded || len(includeFields) == 0 {
				filtered = append(filtered, item)
			}
		}
	}
	if len(filtered) > 0 || len(includeFields) == 0 || parentIncluded {
		return filtered, true
	}
	return nil, false
}

// FilterAttributes filters a map of entry attributes according to includeFields
// and excludeFields glob patterns, returning a new map with only allowed keys.
// Path-aware recursive projection is applied to nested maps and slices.
func FilterAttributes(attrs map[string]any, includeFields, excludeFields []string) map[string]any {
	return FilterAttributesWithPrefixes(attrs, nil, includeFields, excludeFields)
}

// FilterAttributesWithPrefixes applies path-aware recursive projection to attrs.
func FilterAttributesWithPrefixes(attrs map[string]any, prefixes []string, includeFields, excludeFields []string) map[string]any {
	if attrs == nil {
		return nil
	}
	if len(includeFields) == 0 && len(excludeFields) == 0 {
		return attrs
	}
	proj, _ := projectRecursive(attrs, "", prefixes, includeFields, excludeFields, len(includeFields) == 0)
	if m, ok := proj.(map[string]any); ok {
		return m
	}
	return make(map[string]any)
}

// FilterEntry applies field filtering to an Entry's Attributes map.
func FilterEntry(entry Entry, includeFields, excludeFields []string) Entry {
	return FilterEntryWithPath(entry, entry.Title, nil, includeFields, excludeFields)
}

// FilterEntryWithPath applies path-aware recursive field filtering to an Entry's Attributes map,
// taking entry path and root prefixes into account.
func FilterEntryWithPath(entry Entry, entryPath string, rootPrefixes []string, includeFields, excludeFields []string) Entry {
	if len(includeFields) == 0 && len(excludeFields) == 0 {
		return entry
	}
	var prefixes []string
	cleanPath := normalizeDotPath(entryPath)
	if cleanPath != "" {
		prefixes = append(prefixes, cleanPath)
	}
	for _, rp := range rootPrefixes {
		cleanRP := normalizeDotPath(rp)
		if cleanRP != "" {
			prefixes = append(prefixes, cleanRP)
			if cleanPath != "" {
				prefixes = append(prefixes, normalizeDotPath(cleanRP+"."+cleanPath))
			}
		}
	}
	if entry.Title != "" && entry.Title != cleanPath {
		prefixes = append(prefixes, entry.Title)
	}
	entry.Attributes = FilterAttributesWithPrefixes(entry.Attributes, prefixes, includeFields, excludeFields)
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

func (s *SearchableFilteringProvider) rootPrefixes() []string {
	if spAccessor, ok := s.underlying.(interface{ getStaticProvider() *staticProvider }); ok {
		sp := spAccessor.getStaticProvider()
		if sp != nil && sp.entitiesRootKey != "" && sp.entitiesRootKey != "." {
			return []string{sp.entitiesRootKey}
		}
	}
	return nil
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

	return FilterEntryWithPath(entry, location, s.rootPrefixes(), s.includeFields, s.excludeFields), nil
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

	rootPrefixes := s.rootPrefixes()
	filteredResults := make([]SearchResult, len(results))
	for i, r := range results {
		r.Entry = FilterEntryWithPath(r.Entry, r.Path, rootPrefixes, s.includeFields, s.excludeFields)
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
		entryPath := location
		effectiveAttr := "Password"
		if strings.Contains(location, ":") {
			parts := strings.SplitN(location, ":", 2)
			entryPath = parts[0]
			if parts[1] != "" {
				effectiveAttr = parts[1]
			}
		}
		leafAttr := effectiveAttr
		if lastDot := strings.LastIndex(effectiveAttr, "."); lastDot >= 0 {
			leafAttr = effectiveAttr[lastDot+1:]
		}

		var candidates []string
		candidates = append(candidates, effectiveAttr, location)
		if entryPath != "" {
			candidates = append(candidates, entryPath, entryPath+"."+effectiveAttr, entryPath+":"+effectiveAttr)
			cleanEntryPath := normalizeDotPath(entryPath)
			if cleanEntryPath != "" && cleanEntryPath != entryPath {
				candidates = append(candidates, cleanEntryPath, cleanEntryPath+"."+effectiveAttr)
			}
			slashParts := strings.Split(entryPath, "/")
			for i := 1; i < len(slashParts); i++ {
				candidates = append(candidates, strings.Join(slashParts[:i], "/"))
			}
			fullField := entryPath + "." + effectiveAttr
			segments := strings.Split(fullField, ".")
			for i := 1; i < len(segments); i++ {
				candidates = append(candidates, strings.Join(segments[:i], "."))
			}
		}

		if !isFieldAuthorized(leafAttr, candidates, f.includeFields, f.excludeFields) {
			return "", fmt.Errorf("field %q is excluded by vault configuration", leafAttr)
		}
		return f.underlying.GetSecret(ctx, location)

	case "custom_vault":
		// Format: "entity[:attribute]". Bare location defaults to "Password".
		effectiveAttr := "Password"
		entityLoc := location
		if strings.Contains(location, ":") {
			parts := strings.SplitN(location, ":", 2)
			entityLoc = parts[0]
			effectiveAttr = parts[1]
		}
		if effectiveAttr == "" {
			return "", fmt.Errorf("field %q is excluded by vault configuration", "")
		}
		leafAttr := effectiveAttr
		if lastDot := strings.LastIndex(effectiveAttr, "."); lastDot >= 0 {
			leafAttr = effectiveAttr[lastDot+1:]
		}

		var candidates []string
		candidates = append(candidates, effectiveAttr, location)
		if entityLoc != "" {
			candidates = append(candidates, entityLoc, entityLoc+"."+effectiveAttr, entityLoc+":"+effectiveAttr)
			cleanLoc := normalizeDotPath(entityLoc)
			if cleanLoc != "" && cleanLoc != entityLoc {
				candidates = append(candidates, cleanLoc, cleanLoc+"."+effectiveAttr)
			}
			dotParts := strings.Split(entityLoc, ".")
			for i := 1; i < len(dotParts); i++ {
				candidates = append(candidates, strings.Join(dotParts[:i], "."))
			}
			slashParts := strings.Split(entityLoc, "/")
			for i := 1; i < len(slashParts); i++ {
				candidates = append(candidates, strings.Join(slashParts[:i], "/"))
			}
			fullField := entityLoc + "." + effectiveAttr
			segments := strings.Split(fullField, ".")
			for i := 1; i < len(segments); i++ {
				candidates = append(candidates, strings.Join(segments[:i], "."))
			}
		}

		if !isFieldAuthorized(leafAttr, candidates, f.includeFields, f.excludeFields) {
			return "", fmt.Errorf("field %q is excluded by vault configuration", leafAttr)
		}
		return f.underlying.GetSecret(ctx, location)

	case "search":
		// Stored search provider:
		// Attributes can be resolved case-insensitively, so resolve the exact result, canonical key, and result path first.
		var canonicalField string
		var val string
		var resultPath string
		var resolved bool

		if csp, ok := f.underlying.(interface {
			GetSecretWithKey(ctx context.Context, location string) (string, string, string, error)
		}); ok {
			key, v, p, err := csp.GetSecretWithKey(ctx, location)
			if err != nil {
				return "", err
			}
			canonicalField = key
			val = v
			resultPath = p
			resolved = true
		} else if csp, ok := f.underlying.(interface {
			GetSecretWithKey(ctx context.Context, location string) (string, string, error)
		}); ok {
			key, v, err := csp.GetSecretWithKey(ctx, location)
			if err != nil {
				return "", err
			}
			canonicalField = key
			val = v
			resolved = true
		} else {
			// Fallback for mocks / custom SearchableProvider implementations
			if strings.Contains(location, ":") {
				parts := strings.SplitN(location, ":", 2)
				entryLoc := parts[0]
				attrName := parts[1]
				resultPath = entryLoc

				if searchable, ok := f.underlying.(SearchableProvider); ok {
					if entry, err := searchable.GetEntry(ctx, entryLoc); err == nil {
						if key, found := getEntryAttributeKey(entry, attrName); found {
							canonicalField = key
						}
					}
				}
				if canonicalField == "" {
					canonicalField = attrName
				}
			} else {
				resultPath = location
				if searchable, ok := f.underlying.(SearchableProvider); ok {
					if entry, err := searchable.GetEntry(ctx, location); err == nil {
						if key, found := getEntryAttributeKey(entry, location); found {
							canonicalField = key
						}
					}
				}
				if canonicalField == "" {
					canonicalField = "Password"
				}
			}
		}

		leafCanonical := canonicalField
		if lastDot := strings.LastIndex(canonicalField, "."); lastDot >= 0 {
			leafCanonical = canonicalField[lastDot+1:]
		}

		var candidates []string
		candidates = append(candidates, canonicalField, location)
		if resultPath != "" {
			candidates = append(candidates, resultPath, resultPath+"."+canonicalField, resultPath+":"+canonicalField)
			cleanResPath := normalizeDotPath(resultPath)
			if cleanResPath != "" && cleanResPath != resultPath {
				candidates = append(candidates, cleanResPath, cleanResPath+"."+canonicalField)
			}
			dotParts := strings.Split(resultPath, ".")
			for i := 1; i < len(dotParts); i++ {
				candidates = append(candidates, strings.Join(dotParts[:i], "."))
			}
			slashParts := strings.Split(resultPath, "/")
			for i := 1; i < len(slashParts); i++ {
				candidates = append(candidates, strings.Join(slashParts[:i], "/"))
			}
			fullField := resultPath + "." + canonicalField
			segments := strings.Split(fullField, ".")
			for i := 1; i < len(segments); i++ {
				candidates = append(candidates, strings.Join(segments[:i], "."))
			}
		}

		if !isFieldAuthorized(leafCanonical, candidates, f.includeFields, f.excludeFields) {
			return "", fmt.Errorf("field %q is excluded by vault configuration", leafCanonical)
		}

		if resolved {
			return val, nil
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

// normalizeDotPath strips empty components from a dot-separated path (e.g. "a..b" -> "a.b").
func normalizeDotPath(path string) string {
	parts := strings.Split(path, ".")
	var cleanParts []string
	for _, p := range parts {
		if p != "" {
			cleanParts = append(cleanParts, p)
		}
	}
	return strings.Join(cleanParts, ".")
}

// getStaticSecret resolves static dot-path secrets and enforces field filtering before delegating.
// Root-keyed and nested paths are resolved and authorized against effective canonical paths
// before serialization, preserving the underlying provider's serializer.
func (f *FilteringProvider) getStaticSecret(ctx context.Context, location string) (string, error) {
	cleanLoc := normalizeDotPath(location)

	spAccessor, ok := f.underlying.(interface{ getStaticProvider() *staticProvider })
	if !ok {
		leafAttr := cleanLoc
		if lastDot := strings.LastIndex(cleanLoc, "."); lastDot >= 0 {
			leafAttr = cleanLoc[lastDot+1:]
		}
		if !isFieldOrPathAllowed(cleanLoc, leafAttr, f.includeFields, f.excludeFields) {
			return "", fmt.Errorf("field %q is excluded by vault configuration", leafAttr)
		}
		return f.underlying.GetSecret(ctx, location)
	}

	sp := spAccessor.getStaticProvider()

	var rawVal any
	var err error
	canonicalPath := cleanLoc

	if sp.singleEntity {
		entry, ok := sp.entries[""]
		if !ok {
			return "", fmt.Errorf("%s provider: single entity not found", sp.scheme)
		}
		rawVal, err = resolveDotPath(entry.Attributes, cleanLoc)
	} else {
		if sp.rawContent == nil {
			return "", fmt.Errorf("%s provider: not initialized or empty database", sp.scheme)
		}
		val, err1 := resolveDotPath(sp.rawContent, cleanLoc)
		if err1 == nil {
			rawVal = val
			canonicalPath = cleanLoc
		} else if sp.entitiesRootKey != "" && sp.entitiesRootKey != "." && !strings.HasPrefix(cleanLoc, sp.entitiesRootKey+".") {
			rootPrefixed := sp.entitiesRootKey + "." + cleanLoc
			val2, err2 := resolveDotPath(sp.rawContent, rootPrefixed)
			if err2 == nil {
				rawVal = val2
				canonicalPath = rootPrefixed
			} else {
				err = err1
			}
		} else {
			err = err1
		}
	}

	if err != nil {
		return "", fmt.Errorf("%s provider: failed to resolve path %q: %w", sp.scheme, location, err)
	}

	leafAttr := canonicalPath
	if lastDot := strings.LastIndex(canonicalPath, "."); lastDot >= 0 {
		leafAttr = canonicalPath[lastDot+1:]
	}

	var strippedPath string
	if !sp.singleEntity && sp.entitiesRootKey != "" && sp.entitiesRootKey != "." {
		if strings.HasPrefix(canonicalPath, sp.entitiesRootKey+".") {
			strippedPath = strings.TrimPrefix(canonicalPath, sp.entitiesRootKey+".")
		}
	}

	var entityAttr string
	pathToCheck := strippedPath
	if pathToCheck == "" {
		pathToCheck = canonicalPath
	}
	if !sp.singleEntity {
		if dotIdx := strings.Index(pathToCheck, "."); dotIdx >= 0 {
			entityAttr = pathToCheck[dotIdx+1:]
		}
	}

	containerCandidates := []string{canonicalPath, strippedPath, entityAttr, cleanLoc, location}
	var cleanCandidates []string
	for _, c := range containerCandidates {
		if c != "" {
			cleanCandidates = append(cleanCandidates, normalizeDotPath(c))
		}
	}

	// Add ancestor path segments so subtree-exclude patterns apply to nested paths.
	// e.g. exclude_fields: ["db"] blocks GetSecret("db.username").
	// We add ancestors from both canonicalPath and strippedPath to cover root-prefixed and rootless forms.
	for _, basePath := range []string{canonicalPath, strippedPath} {
		if basePath == "" {
			continue
		}
		segments := strings.Split(basePath, ".")
		for i := 1; i < len(segments); i++ {
			ancestor := strings.Join(segments[:i], ".")
			cleanCandidates = append(cleanCandidates, ancestor)
		}
	}

	// Check if container itself is excluded
	if len(f.excludeFields) > 0 {
		for _, pattern := range f.excludeFields {
			if matchPattern(pattern, leafAttr) {
				return "", fmt.Errorf("field %q is excluded by vault configuration", leafAttr)
			}
			for _, c := range cleanCandidates {
				if matchPattern(pattern, c) {
					return "", fmt.Errorf("field %q is excluded by vault configuration", leafAttr)
				}
			}
		}
	}

	// If rawVal is a map (an entire entity or nested object), evaluate allowed descendants independently
	if m, ok := normalizeEntryMap(rawVal); ok {
		containerIncluded := len(f.includeFields) == 0
		if !containerIncluded {
			for _, pattern := range f.includeFields {
				if matchPattern(pattern, leafAttr) {
					containerIncluded = true
					break
				}
				for _, c := range cleanCandidates {
					if matchPattern(pattern, c) {
						containerIncluded = true
						break
					}
				}
				if containerIncluded {
					break
				}
			}
		}

		proj, _ := projectRecursive(m, "", cleanCandidates, f.includeFields, f.excludeFields, containerIncluded)
		filteredMap, _ := proj.(map[string]any)
		if len(f.includeFields) > 0 && !containerIncluded && len(filteredMap) == 0 {
			return "", fmt.Errorf("field %q is excluded by vault configuration", leafAttr)
		}
		return sp.serialize(filteredMap)
	}

	// If rawVal is a slice, route through projectSliceRecursive for field filtering.
	if rawSlice, ok := rawVal.([]any); ok {
		containerIncluded := len(f.includeFields) == 0
		if !containerIncluded {
			for _, pattern := range f.includeFields {
				if matchPattern(pattern, leafAttr) {
					containerIncluded = true
					break
				}
				for _, c := range cleanCandidates {
					if matchPattern(pattern, c) {
						containerIncluded = true
						break
					}
				}
				if containerIncluded {
					break
				}
			}
		}

		projSlice, hasAllowed := projectSliceRecursive(rawSlice, "", cleanCandidates, f.includeFields, f.excludeFields, containerIncluded)
		if len(f.includeFields) > 0 && !containerIncluded && !hasAllowed {
			return "", fmt.Errorf("field %q is excluded by vault configuration", leafAttr)
		}
		return sp.serialize(projSlice)
	}

	if !isFieldAuthorized(leafAttr, cleanCandidates, f.includeFields, f.excludeFields) {
		return "", fmt.Errorf("field %q is excluded by vault configuration", leafAttr)
	}

	return sp.serialize(rawVal)
}
