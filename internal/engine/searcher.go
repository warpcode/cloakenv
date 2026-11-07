package engine

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/warpcode/cloakenv/internal/provider"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
	"github.com/expr-lang/expr/vm"
)

// AttributeResolver defines the method to resolve nested attributes.
type AttributeResolver interface {
	ResolveAttrRecursive(ctx context.Context, val any, depth int, configKey string) (any, error)
}

// Searcher queries entries across searchable repositories using an expression.
type Searcher struct {
	providers    *ProviderManager
	attrResolver AttributeResolver
	programCache map[string]*vm.Program
	cacheMu      sync.RWMutex
}

// NewSearcher creates a new Searcher.
func NewSearcher(pm *ProviderManager, attrResolver AttributeResolver) *Searcher {
	return &Searcher{
		providers:    pm,
		attrResolver: attrResolver,
		programCache: make(map[string]*vm.Program),
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
			if vaultConfig.Provider == "search" {
				continue
			}
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
	if s.providers.Config() != nil {
		if vaultConfig, hasVault := s.providers.Config().Vaults[r.Vault]; hasVault && !vaultConfig.ResolveValues {
			return r
		}
	}
	// Recursively resolve attributes
	resolvedAttrs := make(map[string]any)
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

func (s *Searcher) filterResultsByExpression(ctx context.Context, expressionStr string, allResults []provider.SearchResult) ([]provider.SearchResult, error) {
	if expressionStr == "" {
		return allResults, nil
	}

	var program *vm.Program
	var ok bool
	var err error

	s.cacheMu.RLock()
	program, ok = s.programCache[expressionStr]
	s.cacheMu.RUnlock()

	if !ok {
		s.cacheMu.Lock()
		program, ok = s.programCache[expressionStr]
		if !ok {
			if err = validateExpression(expressionStr); err != nil {
				s.cacheMu.Unlock()
				return nil, err
			}

			sampleEnv := map[string]any{
				"title": "",
				"tags":  []string{},
				"path":  "",
				"uuid":  "",
				"group": map[string]any{"name": "", "uuid": "", "path": ""},
				"_meta_": map[string]any{
					"provider": "",
					"vault":    "",
					"tags":     []string{},
				},
			}

			program, err = expr.Compile(expressionStr, expr.Env(sampleEnv), expr.AllowUndefinedVariables())
			if err != nil {
				s.cacheMu.Unlock()
				return nil, fmt.Errorf("invalid query expression: %w", err)
			}

			s.programCache[expressionStr] = program
		}
		s.cacheMu.Unlock()
	}

	fieldPolicy := provider.FieldPolicyFromContext(ctx)

	var matchedResults []provider.SearchResult
	env := make(map[string]any)
	// Optimization: reuse groupMap and metaMap across search result iterations using clear()
	// to avoid allocating a new map for every candidate entry during query evaluation.
	groupMap := map[string]any{"name": "", "uuid": "", "path": ""}
	metaMap := map[string]any{"provider": "", "vault": "", "tags": []string{}}
	for _, r := range allResults {
		clear(env)
		env["title"] = r.Entry.Title
		tags := r.Entry.Tags
		if len(tags) == 0 {
			if v, ok := r.Entry.Attributes["tags"]; ok {
				if ts, ok := v.([]string); ok {
					tags = ts
				} else if ts, ok := v.([]any); ok {
					for _, item := range ts {
						if s, ok := item.(string); ok {
							tags = append(tags, s)
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
				// Structured group attribute (e.g. YAML/JSON entries defining
				// group as {name: ..., path: ..., uuid: ...}): extract fields
				// case-insensitively instead of stringifying the whole map.
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
							template := provider.ConvertBackslashGroups(rule.Key)
							newKey := rule.CompiledRegex.ReplaceAllString(k, template)
							if newKey != "" && env[newKey] == nil {
								env[newKey] = v
							}
						}
					}
				}
			}
		}

		output, err := expr.Run(program, env)
		if err != nil {
			// Skip entries that fail query evaluation (e.g. key missing or type mismatch)
			continue
		}

		matched, ok := output.(bool)
		if !ok || !matched {
			continue
		}

		matchedResults = append(matchedResults, r)
	}

	return matchedResults, nil
}

// Search queries entries across searchable repositories using an expression.
func (s *Searcher) Search(ctx context.Context, expressionStr string, repoScopes []string) ([]provider.SearchResult, error) {
	return s.SearchRecursive(ctx, expressionStr, repoScopes, 0)
}

// SearchRecursive executes the search tracking depth to prevent infinite recursion.
func (s *Searcher) SearchRecursive(ctx context.Context, expressionStr string, repoScopes []string, depth int) ([]provider.SearchResult, error) {
	exprQuery, err := convertToExpr(expressionStr)
	if err != nil {
		return nil, err
	}
	expressionStr = exprQuery

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

	// Sort deterministically so consumers selecting the first match (e.g.
	// search:// URIs resolving results[0]) get stable, reproducible results
	// regardless of Go's randomized map iteration order over vaults and
	// provider-internal entity maps.
	// Optimization: slices.SortFunc avoids the reflection and closure conversion
	// overhead of sort.Slice.
	slices.SortFunc(allResults, func(a, b provider.SearchResult) int {
		if c := cmp.Compare(a.Vault, b.Vault); c != 0 {
			return c
		}
		return cmp.Compare(a.Path, b.Path)
	})

	return s.filterResultsByExpression(ctx, expressionStr, allResults)
}

func validateExpression(expressionStr string) error {
	tree, err := parser.Parse(expressionStr)
	if err != nil {
		return fmt.Errorf("failed to parse expression: %w", err)
	}

	var validationErr error
	ast.Walk(&tree.Node, &visitor{err: &validationErr})
	return validationErr
}

type visitor struct {
	err *error
}

func (v *visitor) Visit(node *ast.Node) {
	if *v.err != nil {
		return
	}

	switch n := (*node).(type) {
	case *ast.IdentifierNode,
		*ast.IntegerNode,
		*ast.FloatNode,
		*ast.BoolNode,
		*ast.StringNode,
		*ast.BytesNode,
		*ast.ConstantNode,
		*ast.UnaryNode,
		*ast.BinaryNode,
		*ast.ArrayNode,
		*ast.MapNode,
		*ast.PairNode,
		*ast.NilNode,
		*ast.SliceNode,
		*ast.BuiltinNode,
		*ast.PredicateNode,
		*ast.PointerNode,
		*ast.ConditionalNode,
		*ast.ChainNode,
		*ast.VariableDeclaratorNode,
		*ast.SequenceNode:
		// Safe node types are allowed
	case *ast.CallNode:
		*v.err = fmt.Errorf("function calls are not allowed in search expressions")
	case *ast.MemberNode:
		if n.Method {
			*v.err = fmt.Errorf("method calls are not allowed in search expressions")
		}
	default:
		*v.err = fmt.Errorf("expression node type %T is not allowed", *node)
	}
}

func hasExprWordOperator(query, op string) bool {
	qLen := len(query)
	opLen := len(op)
	idx := 0
	for {
		i := strings.Index(query[idx:], op)
		if i == -1 {
			return false
		}
		pos := idx + i
		idx = pos + opLen

		// Check before pos: must not be a word char (a-z, A-Z, 0-9, _)
		if pos > 0 {
			prev := query[pos-1]
			if (prev >= 'a' && prev <= 'z') || (prev >= 'A' && prev <= 'Z') || (prev >= '0' && prev <= '9') || prev == '_' {
				continue
			}
			// Must not be preceded by '=' (skipping spaces), which indicates a URL parameter value like key=in
			trimPrev := strings.TrimRight(query[:pos], " \t")
			if strings.HasSuffix(trimPrev, "=") {
				continue
			}
		}

		// Check after pos+opLen: must not be a word char
		if pos+opLen < qLen {
			next := query[pos+opLen]
			if (next >= 'a' && next <= 'z') || (next >= 'A' && next <= 'Z') || (next >= '0' && next <= '9') || next == '_' {
				continue
			}
			// Must not be followed by '=' (skipping spaces), which indicates a URL parameter key like in=val
			trimNext := strings.TrimLeft(query[pos+opLen:], " \t")
			if strings.HasPrefix(trimNext, "=") {
				continue
			}
		}

		return true
	}
}

// isExprQuery checks if the query string contains Expr operators or syntax.
func isExprQuery(query string) bool {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return false
	}

	if strings.Contains(trimmed, "==") ||
		strings.Contains(trimmed, "!=") ||
		strings.Contains(trimmed, "<=") ||
		strings.Contains(trimmed, ">=") ||
		strings.Contains(trimmed, "&&") ||
		strings.Contains(trimmed, "||") ||
		strings.Contains(trimmed, "<") ||
		strings.Contains(trimmed, ">") {
		return true
	}

	if hasExprWordOperator(trimmed, "in") ||
		hasExprWordOperator(trimmed, "IN") ||
		hasExprWordOperator(trimmed, "contains") ||
		hasExprWordOperator(trimmed, "matches") ||
		hasExprWordOperator(trimmed, "startsWith") ||
		hasExprWordOperator(trimmed, "endsWith") ||
		hasExprWordOperator(trimmed, "and") ||
		hasExprWordOperator(trimmed, "or") ||
		hasExprWordOperator(trimmed, "not") {
		return true
	}

	// If the string contains a single '=' (not part of ==, !=, <=, >=) outside any Expr operators,
	// it is a URL query parameter string, avoiding unnecessary parser.Parse invocations.
	if hasSingleEqual(trimmed) {
		return false
	}

	// If parser.Parse succeeds on the trimmed expression, it is valid Expr syntax.
	if _, err := parser.Parse(trimmed); err == nil {
		return true
	}

	return false
}

func hasSingleEqual(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			if (i == 0 || (s[i-1] != '=' && s[i-1] != '!' && s[i-1] != '<' && s[i-1] != '>')) &&
				(i+1 == len(s) || s[i+1] != '=') {
				return true
			}
		}
	}
	return false
}

// convertToExpr automatically detects whether query is an Expr expression or URL query parameters
// and normalizes it to a valid Expr expression string.
func convertToExpr(query string) (string, error) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return "", nil
	}

	if isExprQuery(trimmed) {
		if err := validateExpression(trimmed); err != nil {
			return "", err
		}
		return trimmed, nil
	}

	return parseQueryParamsToExpr(trimmed)
}

func parseQueryParamsToExpr(queryPart string) (string, error) {
	// Optimization: construct the expression query directly with a strings.Builder
	// and iterate via strings.Cut rather than allocating intermediate slices with
	// strings.Split, strings.Join, and strconv.Quote.
	var b strings.Builder
	b.Grow(len(queryPart) * 2)

	// Quote scratch buffer to avoid allocations from strconv.Quote.
	var quoteBuf []byte
	hasConditions := false

	remaining := queryPart
	for len(remaining) > 0 {
		var part string
		part, remaining, _ = strings.Cut(remaining, "&")

		k, v, found := strings.Cut(part, "=")
		if !found {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" {
			if v != "" {
				// "=value" carries no parameter key; reject it like main did
				// instead of silently dropping user input.
				return "", fmt.Errorf("invalid search parameter: %q", part)
			}
			continue
		}
		// Percent-decode values so reserved URI characters can be queried:
		// %2F -> "/", %26 -> "&", %25 -> "%". Values with invalid escapes are
		// kept verbatim.
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
					if hasConditions {
						b.WriteString(" and ")
					}
					quoteBuf = strconv.AppendQuote(quoteBuf[:0], tag)
					b.Write(quoteBuf)
					b.WriteString(" in tags")
					hasConditions = true
				}
			}
		case "title":
			if v != "" {
				if hasConditions {
					b.WriteString(" and ")
				}
				b.WriteString("title contains ")
				quoteBuf = strconv.AppendQuote(quoteBuf[:0], v)
				b.Write(quoteBuf)
				hasConditions = true
			}
		case "path":
			if v != "" {
				if hasConditions {
					b.WriteString(" and ")
				}
				b.WriteString("path contains ")
				quoteBuf = strconv.AppendQuote(quoteBuf[:0], v)
				b.Write(quoteBuf)
				hasConditions = true
			}
		case "group":
			if v != "" {
				if hasConditions {
					b.WriteString(" and ")
				}
				b.WriteString("(group.name == ")
				quoteBuf = strconv.AppendQuote(quoteBuf[:0], v)
				b.Write(quoteBuf)
				b.WriteString(" or group.path == ")
				b.Write(quoteBuf)
				b.WriteString(")")
				hasConditions = true
			}
		default:
			if !isValidSearchKey(k) {
				return "", fmt.Errorf("invalid search parameter: %q", k)
			}
			if hasConditions {
				b.WriteString(" and ")
			}
			if v == "" {
				// "key=" is an explicit empty-string condition so empty
				// attribute values remain queryable.
				b.WriteString(k)
				b.WriteString(` == ""`)
			} else if n, err := strconv.ParseInt(v, 10, 64); err == nil && strconv.FormatInt(n, 10) == v {
				b.WriteString("(")
				b.WriteString(k)
				b.WriteString(" == ")
				quoteBuf = strconv.AppendQuote(quoteBuf[:0], v)
				b.Write(quoteBuf)
				b.WriteString(" or ")
				b.WriteString(k)
				b.WriteString(" == ")
				b.WriteString(v)
				b.WriteString(")")
			} else if v == "true" || v == "false" {
				b.WriteString("(")
				b.WriteString(k)
				b.WriteString(" == ")
				quoteBuf = strconv.AppendQuote(quoteBuf[:0], v)
				b.Write(quoteBuf)
				b.WriteString(" or ")
				b.WriteString(k)
				b.WriteString(" == ")
				b.WriteString(v)
				b.WriteString(")")
			} else {
				b.WriteString(k)
				b.WriteString(" == ")
				quoteBuf = strconv.AppendQuote(quoteBuf[:0], v)
				b.Write(quoteBuf)
			}
			hasConditions = true
		}
	}

	if !hasConditions {
		return "", fmt.Errorf("invalid search URI: no query conditions specified")
	}

	return b.String(), nil
}

func parseSearchURI(location string) (string, string, error) {
	lastSlash := strings.LastIndex(location, "/")
	var queryPart, attr string
	if lastSlash == -1 {
		queryPart = location
		attr = "Password"
	} else {
		candQuery := location[:lastSlash]
		candAttr := location[lastSlash+1:]

		// If the entire location is a valid Expr query (which might contain "/" inside a string literal),
		// and candQuery is not a valid Expr query, treat the entire location as the query.
		if isExprQuery(location) && validateExpression(location) == nil && (!isExprQuery(candQuery) || validateExpression(candQuery) != nil) {
			queryPart = location
			attr = "Password"
		} else {
			queryPart = candQuery
			attr = candAttr
		}
	}

	if strings.TrimSpace(queryPart) == "" {
		return "", "", fmt.Errorf("invalid search URI: no query conditions specified")
	}

	if attr == "" {
		attr = "Password"
	}

	exprQuery, err := convertToExpr(queryPart)
	if err != nil {
		return "", "", err
	}

	return exprQuery, attr, nil
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
