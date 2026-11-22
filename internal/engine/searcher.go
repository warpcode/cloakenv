package engine

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	texttemplate "text/template"

	"github.com/warpcode/cloakenv/internal/provider"
)

// AttributeResolver defines the method to resolve nested attributes.
type AttributeResolver interface {
	ResolveAttrRecursive(ctx context.Context, val any, depth int, configKey string) (any, error)
}

// Searcher queries entries across searchable repositories using Go text/template
// boolean predicates or URL query parameters.
type Searcher struct {
	providers    *ProviderManager
	attrResolver AttributeResolver
	tmplCache    map[string]*texttemplate.Template
	cacheMu      sync.RWMutex
}

// NewSearcher creates a new Searcher.
func NewSearcher(pm *ProviderManager, attrResolver AttributeResolver) *Searcher {
	return &Searcher{
		providers:    pm,
		attrResolver: attrResolver,
		tmplCache:    make(map[string]*texttemplate.Template),
	}
}

func (s *Searcher) getSearchableProviders(ctx context.Context, repoScopes []string) (map[string]provider.SearchableProvider, error) {
	cfg := s.providers.Config()
	if cfg == nil {
		return nil, fmt.Errorf("no configuration loaded")
	}

	providersToSearch := make(map[string]provider.SearchableProvider)
	if len(repoScopes) > 0 {
		for _, repoScope := range repoScopes {
			if repoScope == "" {
				continue
			}
			p, _, err := s.providers.GetProvider(ctx, repoScope)
			if err != nil {
				return nil, err
			}
			if vaultConfig, hasVault := cfg.Vaults[repoScope]; hasVault {
				if vaultConfig.Searchable != nil && !*vaultConfig.Searchable {
					return nil, fmt.Errorf("vault %q is not searchable", repoScope)
				}
			}
			if searchable, ok := p.(provider.SearchableProvider); ok {
				providersToSearch[repoScope] = searchable
			} else {
				return nil, fmt.Errorf("vault %q does not support searching", repoScope)
			}
		}
	} else {
		for vaultName, vaultConfig := range cfg.Vaults {
			// When vaultName is "env", default searchable to false unless explicitly set to true.
			if vaultName == "env" {
				if vaultConfig.Searchable == nil || !*vaultConfig.Searchable {
					continue
				}
			} else if vaultConfig.Searchable != nil && !*vaultConfig.Searchable {
				continue
			}
			p, _, err := s.providers.GetProvider(ctx, vaultName)
			if err != nil {
				continue
			}
			if searchable, ok := p.(provider.SearchableProvider); ok {
				providersToSearch[vaultName] = searchable
			}
		}
	}

	return providersToSearch, nil
}

func (s *Searcher) resolveSearchResultAttributes(ctx context.Context, r provider.SearchResult, depth int) provider.SearchResult {
	if s.attrResolver == nil {
		return r
	}
	resolvedAttrs := make(map[string]any, len(r.Entry.Attributes))
	for k, v := range r.Entry.Attributes {
		res, err := s.attrResolver.ResolveAttrRecursive(ctx, v, depth+1, k)
		if err == nil {
			resolvedAttrs[k] = res
		} else {
			resolvedAttrs[k] = v
		}
	}
	r.Entry.Attributes = resolvedAttrs
	return r
}

func (s *Searcher) compileQueryTemplate(query string) (*texttemplate.Template, error) {
	s.cacheMu.RLock()
	t, ok := s.tmplCache[query]
	s.cacheMu.RUnlock()
	if ok {
		return t, nil
	}

	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if t, ok = s.tmplCache[query]; ok {
		return t, nil
	}

	parsed, err := texttemplate.New("query").Funcs(ruleTemplateFuncs()).Option("missingkey=error").Parse(query)
	if err != nil {
		return nil, fmt.Errorf("invalid query template: %w", err)
	}
	s.tmplCache[query] = parsed
	return parsed, nil
}

type queryCondition struct {
	key   string
	value string
}

func isTemplateQuery(query string) bool {
	return strings.Contains(query, "{{")
}

func parseQueryParams(queryPart string) ([]queryCondition, error) {
	var conds []queryCondition
	remaining := queryPart
	for len(remaining) > 0 {
		var part string
		part, remaining, _ = strings.Cut(remaining, "&")
		k, v, found := strings.Cut(part, "=")
		if !found {
			trimmedPart := strings.TrimSpace(part)
			if trimmedPart != "" {
				if strings.ContainsAny(trimmedPart, "<>") {
					return nil, fmt.Errorf("invalid search parameter %q: use Go template '{{ ... }}' syntax for expressions", part)
				}
				return nil, fmt.Errorf("invalid search parameter %q: expected key=value or Go template '{{ ... }}'", part)
			}
			continue
		}
		// Reject comparison operators around the first '=' delimiter (==, !=, <=, >=)
		// while allowing literal operator sequences inside values (e.g., token=abc== or notes=v>=2).
		rawKTrimmed := strings.TrimRight(k, " \t")
		if strings.HasPrefix(v, "=") ||
			strings.HasSuffix(rawKTrimmed, "!") ||
			strings.HasSuffix(rawKTrimmed, "<") ||
			strings.HasSuffix(rawKTrimmed, ">") ||
			strings.HasSuffix(rawKTrimmed, "=") {
			return nil, fmt.Errorf("invalid search parameter %q: use Go template '{{ ... }}' syntax for expressions", part)
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" {
			if v != "" {
				return nil, fmt.Errorf("invalid search parameter: %q", part)
			}
			continue
		}
		if decoded, err := url.PathUnescape(v); err == nil {
			v = decoded
		}
		switch k {
		case "tags", "_meta_.tags":
			tagRemaining := v
			for len(tagRemaining) > 0 {
				var tag string
				tag, tagRemaining, _ = strings.Cut(tagRemaining, ",")
				tag = strings.TrimSpace(tag)
				if tag != "" {
					conds = append(conds, queryCondition{key: "tags", value: tag})
				}
			}
		case "title", "path", "group":
			if v != "" {
				conds = append(conds, queryCondition{key: k, value: v})
			}
		default:
			if !isValidSearchKey(k) {
				return nil, fmt.Errorf("invalid search parameter: %q", k)
			}
			conds = append(conds, queryCondition{key: k, value: v})
		}
	}
	if len(conds) == 0 {
		return nil, fmt.Errorf("invalid search query: no query conditions specified")
	}
	return conds, nil
}

func buildSearchEntryEnv(r provider.SearchResult, fieldPolicy *provider.FieldPolicy, env, groupMap, metaMap map[string]any) {
	clear(env)
	env["title"] = r.Entry.Title
	tags := r.Entry.Tags
	if len(tags) == 0 {
		if v, ok := r.Entry.Attributes["tags"]; ok {
			if ts, ok := v.([]string); ok {
				tags = ts
			} else if ts, ok := v.([]any); ok {
				for _, item := range ts {
					if str, ok := item.(string); ok {
						tags = append(tags, str)
					}
				}
			}
		}
	}
	env["tags"] = tags
	env["path"] = r.Path
	env["uuid"] = r.Entry.UUID

	clear(groupMap)
	if r.Entry.Group != nil {
		groupMap["name"] = r.Entry.Group.Name
		groupMap["uuid"] = r.Entry.Group.UUID
		groupMap["path"] = r.Entry.Group.Path
	} else if gVal, ok := findAttrCaseFold(r.Entry.Attributes, "group"); ok && gVal != nil {
		if gm, isMap := gVal.(map[string]any); isMap {
			if n, ok := findAttrCaseFold(gm, "name"); ok && n != nil {
				groupMap["name"] = fmt.Sprint(n)
			} else {
				groupMap["name"] = ""
			}
			if p, ok := findAttrCaseFold(gm, "path"); ok && p != nil {
				groupMap["path"] = fmt.Sprint(p)
			} else {
				groupMap["path"] = groupMap["name"]
			}
			if u, ok := findAttrCaseFold(gm, "uuid"); ok && u != nil {
				groupMap["uuid"] = fmt.Sprint(u)
			} else {
				groupMap["uuid"] = ""
			}
		} else {
			groupMap["name"] = fmt.Sprint(gVal)
			groupMap["uuid"] = ""
			groupMap["path"] = fmt.Sprint(gVal)
		}
	} else {
		groupMap["name"] = ""
		groupMap["uuid"] = ""
		groupMap["path"] = ""
	}
	env["group"] = groupMap

	clear(metaMap)
	metaMap["provider"] = r.Provider
	metaMap["vault"] = r.Vault
	metaMap["tags"] = tags
	env["_meta_"] = metaMap

	for k, v := range r.Entry.Attributes {
		if k != "title" && k != "tags" && k != "path" && k != "_meta_" && (k != "uuid" || r.Entry.UUID == "") && k != "group" {
			env[k] = v
			lowerK := strings.ToLower(k)
			if lowerK != k && env[lowerK] == nil {
				env[lowerK] = v
			}
		}
	}

	if fieldPolicy != nil && len(fieldPolicy.MappingRules) > 0 {
		for _, rule := range fieldPolicy.MappingRules {
			if rule.CompiledRegex == nil {
				_ = rule.Compile()
			}
			if rule.CompiledRegex != nil {
				for k, v := range r.Entry.Attributes {
					if rule.CompiledRegex.MatchString(k) {
						tmpl := provider.ConvertBackslashGroups(rule.Key)
						newKey := rule.CompiledRegex.ReplaceAllString(k, tmpl)
						if newKey != "" && env[newKey] == nil {
							env[newKey] = v
						}
					}
				}
			}
		}
	}
}

func lookupNestedAttr(m map[string]any, dottedKey string) (any, bool) {
	parts := strings.Split(dottedKey, ".")
	var cur any = m
	for _, part := range parts {
		curMap, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if val, exists := curMap[part]; exists {
			cur = val
			continue
		}
		val, exists := findAttrCaseFold(curMap, part)
		if !exists {
			return nil, false
		}
		cur = val
	}
	return cur, true
}

func matchQueryConditions(conds []queryCondition, env, groupMap map[string]any) bool {
	for _, c := range conds {
		switch c.key {
		case "tags":
			tags, _ := env["tags"].([]string)
			found := false
			for _, t := range tags {
				if strings.EqualFold(t, c.value) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		case "title":
			title, _ := env["title"].(string)
			if !strings.Contains(title, c.value) {
				return false
			}
		case "path":
			path, _ := env["path"].(string)
			if !strings.Contains(path, c.value) {
				return false
			}
		case "group":
			name, _ := groupMap["name"].(string)
			path, _ := groupMap["path"].(string)
			if name != c.value && path != c.value {
				return false
			}
		default:
			val, ok := lookupNestedAttr(env, c.key)
			if !ok {
				return false
			}
			if val == nil {
				if c.value != "" {
					return false
				}
				continue
			}
			if fmt.Sprint(val) != c.value {
				return false
			}
		}
	}
	return true
}

func (s *Searcher) filterResultsByQuery(ctx context.Context, queryStr string, allResults []provider.SearchResult) ([]provider.SearchResult, error) {
	trimmed := strings.TrimSpace(queryStr)
	if trimmed == "" {
		return allResults, nil
	}

	fieldPolicy := provider.FieldPolicyFromContext(ctx)
	matchedResults := make([]provider.SearchResult, 0, len(allResults))
	env := make(map[string]any)
	groupMap := map[string]any{"name": "", "uuid": "", "path": ""}
	metaMap := map[string]any{"provider": "", "vault": "", "tags": []string{}}

	if isTemplateQuery(trimmed) {
		tmpl, err := s.compileQueryTemplate(trimmed)
		if err != nil {
			return nil, err
		}
		buf := bufPool.Get().(*bytes.Buffer)
		buf.Reset()
		defer func() {
			b := buf.Bytes()
			clear(b[:cap(b)])
			if buf.Cap() <= 64*1024 {
				buf.Reset()
				bufPool.Put(buf)
			}
		}()

		for _, r := range allResults {
			buildSearchEntryEnv(r, fieldPolicy, env, groupMap, metaMap)
			buf.Reset()
			if err := tmpl.Execute(buf, env); err != nil {
				// Entries in heterogeneous vaults may lack optional/entry-specific
				// attributes referenced by a query predicate; treat per-entry
				// missing-key errors as non-matching instead of aborting the search.
				if isMissingKeyError(err) {
					continue
				}
				return nil, fmt.Errorf("query execution failed: %w", err)
			}
			if strings.TrimSpace(buf.String()) == "true" {
				matchedResults = append(matchedResults, r)
			}
		}
		return matchedResults, nil
	}

	conds, err := parseQueryParams(trimmed)
	if err != nil {
		return nil, err
	}

	for _, r := range allResults {
		buildSearchEntryEnv(r, fieldPolicy, env, groupMap, metaMap)
		if matchQueryConditions(conds, env, groupMap) {
			matchedResults = append(matchedResults, r)
		}
	}
	return matchedResults, nil
}

// Search queries entries across searchable repositories using a Go template
// predicate or URL query parameters.
func (s *Searcher) Search(ctx context.Context, queryStr string, repoScopes []string) ([]provider.SearchResult, error) {
	return s.SearchRecursive(ctx, queryStr, repoScopes, 0)
}

// SearchRecursive executes the search tracking depth to prevent infinite recursion.
func (s *Searcher) SearchRecursive(ctx context.Context, queryStr string, repoScopes []string, depth int) ([]provider.SearchResult, error) {
	trimmed := strings.TrimSpace(queryStr)
	if trimmed != "" {
		if isTemplateQuery(trimmed) {
			if _, err := s.compileQueryTemplate(trimmed); err != nil {
				return nil, err
			}
		} else {
			if _, err := parseQueryParams(trimmed); err != nil {
				return nil, err
			}
		}
	}

	providersToSearch, err := s.getSearchableProviders(ctx, repoScopes)
	if err != nil {
		return nil, err
	}

	fieldPolicy := provider.FieldPolicyFromContext(ctx)
	cfg := s.providers.Config()

	var (
		allResults []provider.SearchResult
		resultsMu  sync.Mutex
		errOnce    sync.Once
		firstErr   error
		wg         sync.WaitGroup
	)

	gctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sem := make(chan struct{}, maxConcurrency)

	for name, searchable := range providersToSearch {
		wg.Add(1)
		go func(name string, searchable provider.SearchableProvider) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
			case <-gctx.Done():
				errOnce.Do(func() {
					firstErr = ctx.Err()
					cancel()
				})
				return
			}
			defer func() { <-sem }()

			results, err := searchable.Search(gctx, provider.SearchQuery{})
			if err != nil {
				errOnce.Do(func() {
					firstErr = fmt.Errorf("failed to retrieve entries from repo %q: %w", name, err)
					cancel()
				})
				return
			}

			var rootPrefixes []string
			if rpProvider, ok := searchable.(interface{ RootPrefixes() []string }); ok {
				rootPrefixes = rpProvider.RootPrefixes()
			}
			if len(rootPrefixes) == 0 && cfg != nil {
				if vc, ok := cfg.Vaults[name]; ok {
					if vc.EntitiesRootKey != "" && vc.EntitiesRootKey != "." {
						rootPrefixes = []string{vc.EntitiesRootKey}
					}
				}
			}

			vaultResults := make([]provider.SearchResult, 0, len(results))
			for _, r := range results {
				origTitle := r.Entry.Title
				origPath := r.Path
				if cfg != nil {
					if vc, ok := cfg.Vaults[name]; ok {
						if r.Provider == "" {
							r.Provider = vc.Provider
						}
						if r.Vault == "" {
							r.Vault = name
						}
						if len(vc.Mapping) > 0 {
							if !provider.IsDefaultFieldMappedOrIncluded("provider", vc.Mapping, vc.IncludeFields, vc.ExcludeFields, origPath, rootPrefixes, r.Entry, r.Provider, r.Vault) {
								r.Provider = ""
							}
							if !provider.IsDefaultFieldMappedOrIncluded("vault", vc.Mapping, vc.IncludeFields, vc.ExcludeFields, origPath, rootPrefixes, r.Entry, r.Provider, r.Vault) {
								r.Vault = ""
							}
							if !provider.IsDefaultFieldMappedOrIncluded("path", vc.Mapping, vc.IncludeFields, vc.ExcludeFields, origPath, rootPrefixes, r.Entry, r.Provider, r.Vault) {
								r.Path = ""
							}
						} else if len(vc.IncludeFields) > 0 || len(vc.ExcludeFields) > 0 {
							if !provider.IsDefaultFieldAllowed("provider", origPath, rootPrefixes, origTitle, vc.IncludeFields, vc.ExcludeFields) {
								r.Provider = ""
							}
							if !provider.IsDefaultFieldAllowed("vault", origPath, rootPrefixes, origTitle, vc.IncludeFields, vc.ExcludeFields) {
								r.Vault = ""
							}
							if !provider.IsDefaultFieldAllowed("path", origPath, rootPrefixes, origTitle, vc.IncludeFields, vc.ExcludeFields) {
								r.Path = ""
							}
						}
					}
				}
				if fieldPolicy != nil {
					var fpIncludes []string
					for _, layer := range fieldPolicy.IncludeLayers {
						fpIncludes = append(fpIncludes, layer...)
					}
					if len(fieldPolicy.MappingRules) > 0 {
						if !provider.IsDefaultFieldMappedOrIncluded("provider", fieldPolicy.MappingRules, fpIncludes, fieldPolicy.ExcludeFields, origPath, rootPrefixes, r.Entry, r.Provider, r.Vault) {
							r.Provider = ""
						}
						if !provider.IsDefaultFieldMappedOrIncluded("vault", fieldPolicy.MappingRules, fpIncludes, fieldPolicy.ExcludeFields, origPath, rootPrefixes, r.Entry, r.Provider, r.Vault) {
							r.Vault = ""
						}
						if !provider.IsDefaultFieldMappedOrIncluded("path", fieldPolicy.MappingRules, fpIncludes, fieldPolicy.ExcludeFields, origPath, rootPrefixes, r.Entry, r.Provider, r.Vault) {
							r.Path = ""
						}
					} else if len(fpIncludes) > 0 || len(fieldPolicy.ExcludeFields) > 0 {
						if !provider.IsDefaultFieldAllowed("provider", origPath, rootPrefixes, origTitle, fpIncludes, fieldPolicy.ExcludeFields) {
							r.Provider = ""
						}
						if !provider.IsDefaultFieldAllowed("vault", origPath, rootPrefixes, origTitle, fpIncludes, fieldPolicy.ExcludeFields) {
							r.Vault = ""
						}
						if !provider.IsDefaultFieldAllowed("path", origPath, rootPrefixes, origTitle, fpIncludes, fieldPolicy.ExcludeFields) {
							r.Path = ""
						}
					}
					r.Entry = fieldPolicy.ApplyToEntry(r.Entry, origPath, rootPrefixes)
				}
				vaultResults = append(vaultResults, s.resolveSearchResultAttributes(gctx, r, depth))
			}

			resultsMu.Lock()
			allResults = append(allResults, vaultResults...)
			resultsMu.Unlock()
		}(name, searchable)
	}

	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}

	slices.SortFunc(allResults, func(a, b provider.SearchResult) int {
		if c := cmp.Compare(a.Vault, b.Vault); c != 0 {
			return c
		}
		return cmp.Compare(a.Path, b.Path)
	})

	return s.filterResultsByQuery(ctx, trimmed, allResults)
}

func parseSearchURI(location string) (string, string, error) {
	trimmed := strings.TrimSpace(location)
	if trimmed == "" {
		return "", "", fmt.Errorf("invalid search URI: no query conditions specified")
	}

	var queryPart, attr string
	if isTemplateQuery(trimmed) {
		lastClose := strings.LastIndex(trimmed, "}}")
		if lastClose != -1 && lastClose+2 < len(trimmed) && trimmed[lastClose+2] == '/' {
			queryPart = trimmed[:lastClose+2]
			attr = trimmed[lastClose+3:]
		} else {
			queryPart = trimmed
			attr = "Password"
		}
	} else {
		lastSlash := strings.LastIndex(trimmed, "/")
		if lastSlash == -1 {
			queryPart = trimmed
			attr = "Password"
		} else {
			queryPart = trimmed[:lastSlash]
			attr = trimmed[lastSlash+1:]
		}
	}

	queryPart = strings.TrimSpace(queryPart)
	if queryPart == "" {
		return "", "", fmt.Errorf("invalid search URI: no query conditions specified")
	}
	if attr == "" {
		attr = "Password"
	}

	if !isTemplateQuery(queryPart) {
		if _, err := parseQueryParams(queryPart); err != nil {
			return "", "", err
		}
	}
	return queryPart, attr, nil
}

// findAttrCaseFold performs a deterministic case-insensitive attribute lookup.
// When several keys fold to the same name, the lexicographically smallest key
// wins so results are stable across runs despite randomized map iteration.
func findAttrCaseFold(attrs map[string]any, key string) (any, bool) {
	best := ""
	found := false
	for k := range attrs {
		if strings.EqualFold(k, key) && (!found || k < best) {
			best = k
			found = true
		}
	}
	if !found {
		return nil, false
	}
	return attrs[best], true
}

func isValidSearchKey(k string) bool {
	if k == "" {
		return false
	}
	startOfPart := true
	for i := range len(k) {
		c := k[i]
		if c == '.' {
			if startOfPart || i == len(k)-1 {
				return false
			}
			startOfPart = true
			continue
		}
		if startOfPart {
			if c != '_' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
				return false
			}
			startOfPart = false
		} else {
			if c != '_' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
				return false
			}
		}
	}
	return !startOfPart
}
