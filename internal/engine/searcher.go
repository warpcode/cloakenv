package engine

import (
	"context"
	"fmt"
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

			if vaultConfig, hasVault := s.providers.Config().Vaults[repoScope]; hasVault {
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
		for vaultName, vaultConfig := range s.providers.Config().Vaults {
			if vaultConfig.Searchable != nil && !*vaultConfig.Searchable {
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

func (s *Searcher) filterResultsByExpression(expressionStr string, allResults []provider.SearchResult) ([]provider.SearchResult, error) {
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
		if err = validateExpression(expressionStr); err != nil {
			return nil, err
		}

		sampleEnv := map[string]any{
			"title": "",
			"tags":  []string{},
			"path":  "",
		}

		program, err = expr.Compile(expressionStr, expr.Env(sampleEnv), expr.AllowUndefinedVariables())
		if err != nil {
			return nil, fmt.Errorf("invalid query expression: %w", err)
		}

		s.cacheMu.Lock()
		s.programCache[expressionStr] = program
		s.cacheMu.Unlock()
	}

	var matchedResults []provider.SearchResult
	for _, r := range allResults {
		env := map[string]any{
			"title": r.Entry.Title,
			"tags":  r.Entry.Tags,
			"path":  r.Path,
		}
		for k, v := range r.Entry.Attributes {
			env[k] = v
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
	providersToSearch, err := s.getSearchableProviders(ctx, repoScopes)
	if err != nil {
		return nil, err
	}

	var allResults []provider.SearchResult
	for name, searchable := range providersToSearch {
		results, err := searchable.Search(ctx, provider.SearchQuery{})
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve entries from repo %q: %w", name, err)
		}

		for _, r := range results {
			r.Provider = s.providers.Config().Vaults[name].Provider
			r.Vault = name
			allResults = append(allResults, s.resolveSearchResultAttributes(ctx, r, depth))
		}
	}

	return s.filterResultsByExpression(expressionStr, allResults)
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

func parseSearchURI(location string) (string, string, error) {
	lastSlash := strings.LastIndex(location, "/")
	var queryPart, attr string
	if lastSlash == -1 {
		queryPart = location
		attr = "Password"
	} else {
		queryPart = location[:lastSlash]
		attr = location[lastSlash+1:]
	}

	var conditions []string
	parts := strings.Split(queryPart, "&")
	for _, part := range parts {
		k, v, found := strings.Cut(part, "=")
		if !found {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		switch k {
		case "tags":
			tags := strings.Split(v, ",")
			for _, tag := range tags {
				tag = strings.TrimSpace(tag)
				if tag != "" {
					conditions = append(conditions, fmt.Sprintf("%q in tags", tag))
				}
			}
		case "title":
			if v != "" {
				conditions = append(conditions, fmt.Sprintf("title contains %q", v))
			}
		case "path":
			if v != "" {
				conditions = append(conditions, fmt.Sprintf("path contains %q", v))
			}
		}
	}

	if len(conditions) == 0 {
		return "", "", fmt.Errorf("invalid search URI: no query conditions specified")
	}

	return strings.Join(conditions, " and "), attr, nil
}
