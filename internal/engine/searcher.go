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

// SearchResult pairs a retrieved flat provider.Entry with its provider, vault, and path metadata.
type SearchResult struct {
	Provider string         `json:"provider" yaml:"provider"`
	Vault    string         `json:"vault" yaml:"vault"`
	Path     string         `json:"path" yaml:"path"`
	Entry    provider.Entry `json:"entry" yaml:"entry"`
}

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
			if searchable, ok := p.(provider.SearchableProvider); ok {
				providersToSearch[repoScope] = searchable
			} else {
				return nil, fmt.Errorf("vault %q does not support searching", repoScope)
			}
		}
	} else {
		for vaultName := range cfg.Vaults {
			if vaultName == "env" {
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

func (s *Searcher) resolveSearchResultAttributes(ctx context.Context, r SearchResult, depth int) SearchResult {
	if s.attrResolver == nil {
		return r
	}
	resolved := make(provider.Entry, len(r.Entry))
	for k, v := range r.Entry {
		if k == "title" || k == "path" || k == "uuid" || k == "tags" || k == "group" {
			resolved[k] = v
			continue
		}
		res, err := s.attrResolver.ResolveAttrRecursive(ctx, v, depth+1, k)
		if err == nil {
			resolved[k] = res
		} else {
			resolved[k] = v
		}
	}
	r.Entry = resolved
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

func buildSearchEntryEnv(r SearchResult, env, groupMap, metaMap map[string]any) {
	clear(env)
	env["title"] = r.Entry.Title()
	tags := r.Entry.Tags()
	env["tags"] = tags
	env["path"] = r.Path
	env["uuid"] = r.Entry.UUID()

	clear(groupMap)
	if grp := r.Entry.Group(); grp != nil {
		if n, ok := grp["name"]; ok {
			groupMap["name"] = fmt.Sprint(n)
		} else {
			groupMap["name"] = ""
		}
		if u, ok := grp["uuid"]; ok {
			groupMap["uuid"] = fmt.Sprint(u)
		} else {
			groupMap["uuid"] = ""
		}
		if p, ok := grp["path"]; ok {
			groupMap["path"] = fmt.Sprint(p)
		} else {
			groupMap["path"] = ""
		}
	} else if gVal, ok := findAttrCaseFold(r.Entry, "group"); ok && gVal != nil {
		if gm, isMap := gVal.(map[string]any); isMap {
			if n, ok := gm["name"]; ok {
				groupMap["name"] = fmt.Sprint(n)
			} else {
				groupMap["name"] = ""
			}
			if u, ok := gm["uuid"]; ok {
				groupMap["uuid"] = fmt.Sprint(u)
			} else {
				groupMap["uuid"] = ""
			}
			if p, ok := gm["path"]; ok {
				groupMap["path"] = fmt.Sprint(p)
			} else {
				groupMap["path"] = ""
			}
		} else {
			gs := fmt.Sprint(gVal)
			groupMap["name"] = gs
			groupMap["uuid"] = ""
			groupMap["path"] = gs
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

	for k, v := range r.Entry {
		if k != "title" && k != "tags" && k != "path" && k != "_meta_" && (k != "uuid" || r.Entry.UUID() == "") && k != "group" {
			env[k] = v
			lowerK := strings.ToLower(k)
			if lowerK != k && env[lowerK] == nil {
				env[lowerK] = v
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

func (s *Searcher) filterResultsByQuery(queryStr string, allResults []SearchResult) ([]SearchResult, error) {
	trimmed := strings.TrimSpace(queryStr)
	if trimmed == "" {
		return allResults, nil
	}

	matchedResults := make([]SearchResult, 0, len(allResults))
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
			buildSearchEntryEnv(r, env, groupMap, metaMap)
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
		buildSearchEntryEnv(r, env, groupMap, metaMap)
		if matchQueryConditions(conds, env, groupMap) {
			matchedResults = append(matchedResults, r)
		}
	}
	return matchedResults, nil
}

// Search queries entries across searchable repositories using a Go template
// predicate or URL query parameters.
func (s *Searcher) Search(ctx context.Context, queryStr string, repoScopes []string) ([]SearchResult, error) {
	return s.SearchRecursive(ctx, queryStr, repoScopes, 0)
}

// SearchRecursive executes the search tracking depth to prevent infinite recursion.
func (s *Searcher) SearchRecursive(ctx context.Context, queryStr string, repoScopes []string, depth int) ([]SearchResult, error) {
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

	cfg := s.providers.Config()

	var (
		allResults []SearchResult
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

			entries, err := searchable.Search(gctx, provider.SearchQuery{})
			if err != nil {
				errOnce.Do(func() {
					firstErr = fmt.Errorf("failed to retrieve entries from repo %q: %w", name, err)
					cancel()
				})
				return
			}

			provName := ""
			if cfg != nil {
				if vc, ok := cfg.Vaults[name]; ok {
					provName = vc.Provider
				}
			}
			if provName == "" {
				if sp, ok := searchable.(provider.SecretProvider); ok {
					provName = sp.Scheme()
				}
			}

			vaultResults := make([]SearchResult, 0, len(entries))
			for _, e := range entries {
				r := SearchResult{
					Provider: provName,
					Vault:    name,
					Path:     e.Path(),
					Entry:    e,
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

	slices.SortFunc(allResults, func(a, b SearchResult) int {
		if c := cmp.Compare(a.Vault, b.Vault); c != 0 {
			return c
		}
		return cmp.Compare(a.Path, b.Path)
	})

	return s.filterResultsByQuery(trimmed, allResults)
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
