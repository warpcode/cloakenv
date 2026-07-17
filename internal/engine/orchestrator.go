// Package engine implements the core URI routing and secret resolution
// orchestrator for cloakenv.
package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
	"github.com/warpcode/cloakenv/internal/utils"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
	"github.com/expr-lang/expr/vm"
	"gopkg.in/yaml.v3"
)

// Orchestrator resolves secret URIs by dispatching to the appropriate
// SecretProvider based on the URI scheme. Built-in schemes (keyring://,
// env://) are always available. User-defined remote names from config
// become additional schemes at runtime.
type Orchestrator struct {
	config              *config.Config
	builtins            map[string]provider.SecretProvider
	initializedBuiltins map[string]bool
	vaultCache          map[string]provider.SecretProvider
	keyring             *provider.OSKeyringProvider
	concurrencySem      chan struct{}
	programCache        map[string]*vm.Program
	cacheMu             sync.RWMutex
	mu                  sync.Mutex
}

const maxConcurrency = 16

// NewOrchestrator creates a new orchestrator with the given config,
// registers built-in providers, and validates all remote configurations.
func NewOrchestrator(cfg *config.Config) (*Orchestrator, error) {
	kr := provider.NewOSKeyringProvider()
	env := provider.NewEnvProvider()
	cache := provider.NewCacheProvider()

	o := &Orchestrator{
		config: cfg,
		builtins: map[string]provider.SecretProvider{
			kr.Scheme():    kr,
			env.Scheme():   env,
			cache.Scheme(): cache,
		},
		initializedBuiltins: make(map[string]bool),
		vaultCache:          make(map[string]provider.SecretProvider),
		programCache:        make(map[string]*vm.Program),
		keyring:             kr,
		concurrencySem:      make(chan struct{}, maxConcurrency),
	}

	// Validate vault configurations
	for vaultName, vault := range cfg.Vaults {
		if _, isBuiltin := o.builtins[vaultName]; isBuiltin {
			return nil, fmt.Errorf("invalid config: vault name %q conflicts with built-in scheme", vaultName)
		}

		// If resolve_values is set, ask the provider whether it supports it.
		if vault.ResolveValues {
			p, err := newBareProvider(vault.Provider)
			if err != nil {
				return nil, fmt.Errorf("invalid config for vault %q: %w", vaultName, err)
			}
			if _, ok := p.(provider.ValueResolvableProvider); !ok {
				return nil, fmt.Errorf("invalid config for vault %q: provider %q does not support resolve_values", vaultName, vault.Provider)
			}
		}

		switch vault.Provider {
		case "keepass":
			if vault.SingleEntity != nil && *vault.SingleEntity {
				return nil, fmt.Errorf("invalid config for vault %q: keepass provider cannot be configured as a single-entity vault", vaultName)
			}
			kp := provider.NewKeePassProvider()
			settings := map[string]string{
				"vault_path": vault.VaultPath,
			}
			if err := kp.Validate(settings); err != nil {
				return nil, fmt.Errorf("invalid config for vault %q: %w", vaultName, err)
			}
		case "yaml":
			yp := provider.NewYamlProvider()
			settings := map[string]string{
				"vault_path": vault.VaultPath,
			}
			if err := yp.Validate(settings); err != nil {
				return nil, fmt.Errorf("invalid config for vault %q: %w", vaultName, err)
			}
		case "json":
			jp := provider.NewJsonProvider()
			settings := map[string]string{
				"vault_path": vault.VaultPath,
			}
			if err := jp.Validate(settings); err != nil {
				return nil, fmt.Errorf("invalid config for vault %q: %w", vaultName, err)
			}
		case "custom_vault":
			// custom_vault is statically defined in config, so it is always valid.
		default:
			return nil, fmt.Errorf("unsupported provider type %q for vault %q", vault.Provider, vaultName)
		}
	}

	return o, nil
}

// Resolve takes a full value, expands any ${...} expressions, and
// returns the resolved secret value.
func (o *Orchestrator) Resolve(ctx context.Context, uri string) (string, error) {
	return o.ResolveWithKey(ctx, uri, "")
}

// ResolveWithKey takes a full value, expands any ${...} expressions, and
// includes the configKey in any failure messages if provided.
func (o *Orchestrator) ResolveWithKey(ctx context.Context, uri string, configKey string) (string, error) {
	return o.expandString(ctx, uri, 0, configKey)
}

func (o *Orchestrator) expandString(ctx context.Context, s string, depth int, configKey string) (string, error) {
	if depth > 5 {
		return "", fmt.Errorf("infinite secret resolution recursion detected: reached max depth 5")
	}

	var sb strings.Builder
	i := 0
	n := len(s)
	for i < n {
		if i+1 < n && s[i] == '$' && s[i+1] == '$' {
			sb.WriteByte('$')
			i += 2
			continue
		}
		if i+1 < n && s[i] == '$' && s[i+1] == '{' {
			// Find matching '}'
			start := i + 2
			end := -1
			for j := start; j < n; j++ {
				if s[j] == '}' {
					end = j
					break
				}
			}
			if end == -1 {
				keyPart := ""
				if configKey != "" {
					keyPart = fmt.Sprintf(" in configuration key %q", configKey)
				}
				return "", fmt.Errorf("unclosed expansion syntax '${...}'%s", keyPart)
			}

			innerText := s[start:end]
			if strings.Contains(innerText, "${") {
				keyPart := ""
				if configKey != "" {
					keyPart = fmt.Sprintf(" in configuration key %q", configKey)
				}
				return "", fmt.Errorf("nested expansions are not supported%s: %q", keyPart, s)
			}

			resolved, err := o.resolveSingleURI(ctx, innerText, depth, configKey)
			if err != nil {
				keyPart := ""
				if configKey != "" {
					keyPart = fmt.Sprintf(" in configuration key %q", configKey)
				}
				return "", fmt.Errorf("failed to resolve expansion %q%s: %w", innerText, keyPart, err)
			}

			sb.WriteString(resolved)
			i = end + 1
			continue
		}
		sb.WriteByte(s[i])
		i++
	}
	return sb.String(), nil
}

func (o *Orchestrator) resolveSingleURI(ctx context.Context, uri string, depth int, configKey string) (string, error) {
	scheme, location, err := parseURI(uri)
	if err != nil {
		return "", fmt.Errorf("invalid URI format: %w", err)
	}

	o.mu.Lock()
	_, isBuiltin := o.builtins[scheme]
	_, isVault := o.config.Vaults[scheme]
	o.mu.Unlock()

	if !isBuiltin && !isVault && scheme != "search" {
		return "", fmt.Errorf("unknown scheme or vault: %q", scheme)
	}

	// Handle search scheme dynamically
	if scheme == "search" {
		exprQuery, attr, err := parseSearchURI(location)
		if err != nil {
			return "", err
		}

		results, err := o.searchRecursive(ctx, exprQuery, nil, depth+1)
		if err != nil {
			return "", fmt.Errorf("search URI evaluation failed: %w", err)
		}

		if len(results) == 0 {
			return "", fmt.Errorf("no secrets found matching search query %q", exprQuery)
		}

		matchedEntry := results[0].Entry
		val, ok := matchedEntry.Attributes[attr]
		if !ok {
			return "", fmt.Errorf("attribute %q not found in matched entry %q", attr, matchedEntry.Title)
		}

		var valStr string
		if str, ok := val.(string); ok {
			valStr = str
		} else {
			valStr = fmt.Sprintf("%v", val)
		}
		return o.expandString(ctx, valStr, depth+1, configKey)
	}

	var val string
	// Check built-in providers first
	if p, ok := o.builtins[scheme]; ok {
		if err := o.ensureInitialized(ctx, scheme, p); err != nil {
			return "", err
		}
		var getErr error
		val, getErr = p.GetSecret(ctx, location)
		if getErr != nil {
			return "", getErr
		}
	} else {
		// Check user-defined vaults
		var getErr error
		val, getErr = o.resolveVault(ctx, scheme, location)
		if getErr != nil {
			return "", getErr
		}
	}

	shouldResolve := true
	if _, isBuiltin := o.builtins[scheme]; !isBuiltin {
		// For vault providers: ask the cached provider whether it gates
		// value resolution. If it implements ValueResolvableProvider it
		// expects the engine to honour the resolve_values flag; otherwise
		// we fall through and resolve unconditionally (legacy behaviour).
		o.mu.Lock()
		p, cached := o.vaultCache[scheme]
		o.mu.Unlock()

		if cached {
			if _, ok := p.(provider.ValueResolvableProvider); ok {
				shouldResolve = o.config.Vaults[scheme].ResolveValues
			}
		}
	}

	if shouldResolve {
		return o.expandString(ctx, val, depth+1, configKey)
	}
	return val, nil
}

// GetEntry retrieves a complete structured entry by location.
// If the URI contains an attribute selector (e.g. "kp://Group/Entry:Password"),
// a synthetic single-key entry is returned rather than the full entry — this
// allows -m URIs to inject a single named attribute rather than all fields.
func (o *Orchestrator) GetEntry(ctx context.Context, uri string) (provider.Entry, error) {
	return o.getEntryRecursive(ctx, uri, 0)
}

func (o *Orchestrator) getEntryRecursive(ctx context.Context, uri string, depth int) (provider.Entry, error) {
	if depth > 5 {
		return provider.Entry{}, fmt.Errorf("infinite secret resolution recursion detected: reached max depth 5 resolving entry %q", uri)
	}

	scheme, location, err := parseURI(uri)
	if err != nil {
		return provider.Entry{}, err
	}

	// If the location contains an attribute selector, resolve the single value
	// and return a synthetic entry with that one key.
	if attrIdx := strings.LastIndex(location, ":"); attrIdx >= 0 {
		attrName := location[attrIdx+1:]
		if attrName != "" {
			val, err := o.resolveSingleURI(ctx, uri, 0, "")
			if err != nil {
				return provider.Entry{}, err
			}
			return provider.Entry{
				Attributes: map[string]any{attrName: val},
			}, nil
		}
	}

	var p provider.SecretProvider
	if builtin, ok := o.builtins[scheme]; ok {
		if err := o.ensureInitialized(ctx, scheme, builtin); err != nil {
			return provider.Entry{}, err
		}
		p = builtin
	} else {
		var getErr error
		p, getErr = o.getVaultProvider(ctx, scheme)
		if getErr != nil {
			return provider.Entry{}, getErr
		}
	}

	searchable, ok := p.(provider.SearchableProvider)
	if !ok {
		return provider.Entry{}, fmt.Errorf("provider %q does not support structured entries", scheme)
	}

	entry, err := searchable.GetEntry(ctx, location)
	if err != nil {
		return provider.Entry{}, err
	}

	// Determine whether attribute-value URIs should be resolved for this vault.
	// For user-defined vaults that implement ValueResolvableProvider the
	// resolve_values flag gates resolution; for builtins we always resolve.
	shouldResolveAttrs := true
	if _, isBuiltin := o.builtins[scheme]; !isBuiltin {
		o.mu.Lock()
		cachedP, cached := o.vaultCache[scheme]
		o.mu.Unlock()
		if cached {
			if _, ok := cachedP.(provider.ValueResolvableProvider); ok {
				shouldResolveAttrs = o.config.Vaults[scheme].ResolveValues
			}
		}
	}

	// Recursively resolve all attributes that contain secret URIs
	resolvedAttrs := make(map[string]any)
	for k, v := range entry.Attributes {
		if !shouldResolveAttrs {
			resolvedAttrs[k] = v
			continue
		}
		resolvedVal, err := o.resolveAttrRecursive(ctx, v, depth+1, k)
		if err != nil {
			return provider.Entry{}, fmt.Errorf("failed to resolve attribute %q: %w", k, err)
		}
		resolvedAttrs[k] = resolvedVal
	}
	entry.Attributes = resolvedAttrs

	return entry, nil
}

func (o *Orchestrator) resolveAttrRecursive(ctx context.Context, val any, depth int, configKey string) (any, error) {
	if depth > 5 {
		return nil, errors.New("max recursion depth reached resolving attribute")
	}

	switch typedVal := val.(type) {
	case string:
		return o.expandString(ctx, typedVal, depth+1, configKey)
	case []string:
		resolvedSlice := make([]string, len(typedVal))
		var wg sync.WaitGroup
		var firstErr error
		var mu sync.Mutex

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

	LoopStr:
		for i, v := range typedVal {
			select {
			case o.concurrencySem <- struct{}{}:
				wg.Add(1)
				go func(i int, v string) {
					defer wg.Done()
					defer func() { <-o.concurrencySem }()

					select {
					case <-ctx.Done():
						return
					default:
					}

					res, err := o.resolveAttrRecursive(ctx, v, depth, configKey)
					if err != nil {
						mu.Lock()
						if firstErr == nil {
							firstErr = err
							cancel()
						}
						mu.Unlock()
						return
					}
					if str, ok := res.(string); ok {
						resolvedSlice[i] = str
					} else {
						resolvedSlice[i] = fmt.Sprintf("%v", res)
					}
				}(i, v)
			default:
				// Fallback to sequential if concurrency limit is reached
				if ctx.Err() != nil {
					break LoopStr
				}
				res, err := o.resolveAttrRecursive(ctx, v, depth, configKey)
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
						cancel()
					}
					mu.Unlock()
					break LoopStr
				}
				if str, ok := res.(string); ok {
					resolvedSlice[i] = str
				} else {
					resolvedSlice[i] = fmt.Sprintf("%v", res)
				}
			}
		}
		wg.Wait()
		if firstErr != nil {
			return nil, firstErr
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return resolvedSlice, nil
	case []any:
		resolvedSlice := make([]any, len(typedVal))
		var wg sync.WaitGroup
		var firstErr error
		var mu sync.Mutex

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

	LoopAny:
		for i, v := range typedVal {
			select {
			case o.concurrencySem <- struct{}{}:
				wg.Add(1)
				go func(i int, v any) {
					defer wg.Done()
					defer func() { <-o.concurrencySem }()

					select {
					case <-ctx.Done():
						return
					default:
					}

					res, err := o.resolveAttrRecursive(ctx, v, depth, configKey)
					if err != nil {
						mu.Lock()
						if firstErr == nil {
							firstErr = err
							cancel()
						}
						mu.Unlock()
						return
					}
					resolvedSlice[i] = res
				}(i, v)
			default:
				// Fallback to sequential if concurrency limit is reached
				if ctx.Err() != nil {
					break LoopAny
				}
				res, err := o.resolveAttrRecursive(ctx, v, depth, configKey)
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
						cancel()
					}
					mu.Unlock()
					break LoopAny
				}
				resolvedSlice[i] = res
			}
		}
		wg.Wait()
		if firstErr != nil {
			return nil, firstErr
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return resolvedSlice, nil
	default:
		return val, nil
	}
}

func (o *Orchestrator) getSearchableProviders(ctx context.Context, repoScopes []string) (map[string]provider.SearchableProvider, error) {
	providersToSearch := make(map[string]provider.SearchableProvider)

	if len(repoScopes) > 0 {
		for _, repoScope := range repoScopes {
			if repoScope == "" {
				continue
			}

			var p provider.SecretProvider
			var vaultConfig config.VaultConfig
			var hasVault bool

			if builtin, ok := o.builtins[repoScope]; ok {
				if err := o.ensureInitialized(ctx, repoScope, builtin); err != nil {
					return nil, err
				}
				p = builtin
			} else {
				var err error
				p, err = o.getVaultProvider(ctx, repoScope)
				if err != nil {
					return nil, err
				}
				vaultConfig, hasVault = o.config.Vaults[repoScope]
			}

			if hasVault && vaultConfig.Searchable != nil && !*vaultConfig.Searchable {
				return nil, fmt.Errorf("vault %q is not searchable", repoScope)
			}
			if searchable, ok := p.(provider.SearchableProvider); ok {
				providersToSearch[repoScope] = searchable
			} else {
				return nil, fmt.Errorf("vault %q does not support searching", repoScope)
			}
		}
	} else {
		for vaultName, vaultConfig := range o.config.Vaults {
			if vaultConfig.Searchable != nil && !*vaultConfig.Searchable {
				continue
			}
			p, err := o.getVaultProvider(ctx, vaultName)
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

func (o *Orchestrator) resolveSearchResultAttributes(ctx context.Context, r provider.SearchResult, depth int) provider.SearchResult {
	// Recursively resolve attributes
	resolvedAttrs := make(map[string]any)
	for k, v := range r.Entry.Attributes {
		res, err := o.resolveAttrRecursive(ctx, v, depth+1, k)
		if err == nil {
			resolvedAttrs[k] = res
		} else {
			resolvedAttrs[k] = v
		}
	}
	r.Entry.Attributes = resolvedAttrs
	return r
}

func (o *Orchestrator) filterResultsByExpression(expressionStr string, allResults []provider.SearchResult) ([]provider.SearchResult, error) {
	if expressionStr == "" {
		return allResults, nil
	}

	var program *vm.Program
	var ok bool
	var err error

	o.cacheMu.RLock()
	program, ok = o.programCache[expressionStr]
	o.cacheMu.RUnlock()

	if !ok {
		if err = validateExpression(expressionStr); err != nil {
			return nil, err
		}

		// Union all attribute keys for compilation type checking
		unionAttrs := make(map[string]any)
		for _, r := range allResults {
			for k, v := range r.Entry.Attributes {
				unionAttrs[k] = v
			}
		}
		sampleEnv := map[string]any{
			"title": "",
			"tags":  []string{},
			"path":  "",
		}
		for k, v := range unionAttrs {
			sampleEnv[k] = v
		}

		program, err = expr.Compile(expressionStr, expr.Env(sampleEnv), expr.AllowUndefinedVariables())
		if err != nil {
			return nil, fmt.Errorf("invalid query expression: %w", err)
		}

		o.cacheMu.Lock()
		o.programCache[expressionStr] = program
		o.cacheMu.Unlock()
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
func (o *Orchestrator) Search(ctx context.Context, expressionStr string, repoScopes []string) ([]provider.SearchResult, error) {
	return o.searchRecursive(ctx, expressionStr, repoScopes, 0)
}

func (o *Orchestrator) searchRecursive(ctx context.Context, expressionStr string, repoScopes []string, depth int) ([]provider.SearchResult, error) {
	providersToSearch, err := o.getSearchableProviders(ctx, repoScopes)
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
			r.Provider = o.config.Vaults[name].Provider
			r.Vault = name
			allResults = append(allResults, o.resolveSearchResultAttributes(ctx, r, depth))
		}
	}

	return o.filterResultsByExpression(expressionStr, allResults)
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
	case *ast.CallNode:
		*v.err = fmt.Errorf("function calls are not allowed in search expressions")
	case *ast.MemberNode:
		if n.Method {
			*v.err = fmt.Errorf("method calls are not allowed in search expressions")
		}
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

// BuildEnv constructs the full environment block.
func (o *Orchestrator) BuildEnv(ctx context.Context, explicit map[string]string, merges []string, whitelist []string) ([]string, error) {
	// 1. Get parent environment
	parentEnvMap := make(map[string]string)
	for _, envStr := range os.Environ() {
		k, v, ok := strings.Cut(envStr, "=")
		if ok && k != "" {
			parentEnvMap[k] = v
		}
	}

	// whitelistSet is a helper to quickly check if a key is whitelisted
	whitelistSet := make(map[string]bool)
	for _, k := range whitelist {
		whitelistSet[utils.FormatKey(k)] = true
	}
	hasWhitelist := len(whitelist) > 0

	// We will resolve/load the merge sources in parallel.
	type loadedSource struct {
		keys map[string]string
	}
	loaded := make([]loadedSource, len(merges))
	var wg sync.WaitGroup
	var errOnce sync.Once
	var firstErr error

	for i, m := range merges {
		wg.Add(1)
		go func(idx int, uri string) {
			defer wg.Done()
			keys := make(map[string]string)
			entry, err := o.GetEntry(ctx, uri)
			if err != nil {
				errOnce.Do(func() {
					firstErr = fmt.Errorf("failed to get entry %s: %w", uri, err)
				})
				return
			}
			for k, v := range entry.Attributes {
				kLower := strings.ToLower(k)
				if kLower == "title" || kLower == "tags" {
					continue
				}
				formattedKey := utils.FormatKey(k)
				if hasWhitelist && !whitelistSet[formattedKey] {
					continue
				}
				strVal, err := serializeValHelper(v)
				if err != nil {
					errOnce.Do(func() {
						firstErr = fmt.Errorf("failed to serialize attribute %q in entry %s: %w", k, uri, err)
					})
					return
				}
				keys[formattedKey] = strVal
			}
			loaded[idx] = loadedSource{keys: keys}
		}(i, m)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}

	// finalEnv maps key -> value, initialized with parent environment (always forwarded)
	finalEnv := make(map[string]string)
	for k, v := range parentEnvMap {
		finalEnv[k] = v
	}

	// Merge sources sequentially: later overrides earlier
	for _, src := range loaded {
		for k, v := range src.keys {
			finalEnv[k] = v
		}
	}

	// - Explicit mappings (-e highest priority)
	// These are never filtered by whitelist and overwrite everything.
	if len(explicit) > 0 {
		var explicitWg sync.WaitGroup
		var explicitErrOnce sync.Once
		var explicitErr error
		var mu sync.Mutex

		for k, uri := range explicit {
			explicitWg.Add(1)
			go func(k, uri string) {
				defer explicitWg.Done()
				val, err := o.ResolveWithKey(ctx, uri, k)
				if err != nil {
					explicitErrOnce.Do(func() {
						explicitErr = fmt.Errorf("failed to resolve explicit env %s=%s: %w", k, uri, err)
					})
					return
				}
				mu.Lock()
				finalEnv[utils.FormatKey(k)] = val
				mu.Unlock()
			}(k, uri)
		}
		explicitWg.Wait()
		if explicitErr != nil {
			return nil, explicitErr
		}
	}

	// Convert finalEnv map to []string slice in "KEY=VALUE" format
	var result []string
	for k, v := range finalEnv {
		result = append(result, fmt.Sprintf("%s=%s", k, v))
	}

	return result, nil
}

func serializeValHelper(val any) (string, error) {
	switch v := val.(type) {
	case string:
		return v, nil
	case []any, map[string]any, map[any]any, []string:
		data, err := yaml.Marshal(v)
		if err != nil {
			return "", err
		}
		return strings.TrimSuffix(string(data), "\n"), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

// Keyring returns the built-in keyring provider for direct access
// (used by the config subcommands).
func (o *Orchestrator) Keyring() *provider.OSKeyringProvider {
	return o.keyring
}

// Write takes a full URI (e.g., "keyring://service/account" or "cache://my_key") and
// writes the secret value to that location.
func (o *Orchestrator) Write(ctx context.Context, uri string, value string) error {
	scheme, location, err := parseURI(uri)
	if err != nil {
		return err
	}

	// Check built-in providers first
	if p, ok := o.builtins[scheme]; ok {
		if err := o.ensureInitialized(ctx, scheme, p); err != nil {
			return err
		}
		return p.SetSecret(ctx, location, value)
	}

	// Check user-defined vaults
	p, err := o.getVaultProvider(ctx, scheme)
	if err != nil {
		return err
	}
	return p.SetSecret(ctx, location, value)
}

// Delete takes a full URI (e.g., "keyring://service/account" or "cache://my_key") and
// removes the secret from the provider.
func (o *Orchestrator) Delete(ctx context.Context, uri string) error {
	scheme, location, err := parseURI(uri)
	if err != nil {
		return err
	}

	// Check built-in providers first
	if p, ok := o.builtins[scheme]; ok {
		if err := o.ensureInitialized(ctx, scheme, p); err != nil {
			return err
		}
		return p.DeleteSecret(ctx, location)
	}

	// Check user-defined vaults
	p, err := o.getVaultProvider(ctx, scheme)
	if err != nil {
		return err
	}
	return p.DeleteSecret(ctx, location)
}

// ClearCache clears all local cache files.
func (o *Orchestrator) ClearCache(ctx context.Context) error {
	p, ok := o.builtins["cache"]
	if !ok {
		return fmt.Errorf("cache provider not registered")
	}
	if err := o.ensureInitialized(ctx, "cache", p); err != nil {
		return err
	}

	cacheProv, ok := p.(*provider.CacheProvider)
	if !ok {
		return fmt.Errorf("invalid cache provider type")
	}
	return cacheProv.ClearCache()
}

func (o *Orchestrator) ensureInitialized(ctx context.Context, scheme string, p provider.SecretProvider) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.initializedBuiltins[scheme] {
		return nil
	}

	settings := make(map[string]string)
	if scheme == "cache" {
		settings["keyring_prefix"] = o.KeyringPrefix()
	}

	if err := p.Initialize(ctx, provider.ProviderConfig{Settings: settings}); err != nil {
		return err
	}
	o.initializedBuiltins[scheme] = true
	return nil
}

// resolveVault handles URIs whose scheme matches a user-defined vault name.
func (o *Orchestrator) resolveVault(ctx context.Context, vaultName, location string) (string, error) {
	p, err := o.getVaultProvider(ctx, vaultName)
	if err != nil {
		return "", err
	}
	return p.GetSecret(ctx, location)
}

// getVaultProvider retrieves and initializes a vault provider, caching it for subsequent calls.
func (o *Orchestrator) getVaultProvider(ctx context.Context, vaultName string) (provider.SecretProvider, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	// Check the cache first
	if p, ok := o.vaultCache[vaultName]; ok {
		return p, nil
	}

	// Look up the vault in config
	vault, ok := o.config.Vaults[vaultName]
	if !ok {
		return nil, fmt.Errorf("unknown scheme or vault: %q (not a built-in and not defined in config)", vaultName)
	}

	// Initialize the provider for this vault type
	p, err := o.initVaultProvider(ctx, vaultName, vault)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize vault %q: %w", vaultName, err)
	}

	// Cache for subsequent lookups within the same run
	o.vaultCache[vaultName] = p
	return p, nil
}

// initVaultProvider creates and initializes a provider for a configured vault.
// Currently supports the "keepass", "yaml", "json" and "custom_vault" types.
func (o *Orchestrator) initVaultProvider(ctx context.Context, vaultName string, vault config.VaultConfig) (provider.SecretProvider, error) {
	switch vault.Provider {
	case "keepass":
		return o.initKeePass(ctx, vaultName, vault)
	case "yaml":
		return o.initYaml(ctx, vaultName, vault)
	case "json":
		return o.initJson(ctx, vaultName, vault)
	case "custom_vault":
		return o.initCustomVault(ctx, vaultName, vault)
	default:
		return nil, fmt.Errorf("unsupported provider type: %q", vault.Provider)
	}
}

// initKeePass bootstraps a KeePass provider using settings.
func (o *Orchestrator) initKeePass(ctx context.Context, vaultName string, vault config.VaultConfig) (provider.SecretProvider, error) {
	kp := provider.NewKeePassProvider()
	err := kp.Initialize(ctx, provider.ProviderConfig{
		Settings: map[string]string{
			"vault_path":     vault.VaultPath,
			"remote_name":    vaultName,
			"keyring_prefix": o.KeyringPrefix(),
		},
		SingleEntity:    vault.SingleEntity,
		EntityName:      vault.EntityName,
		Searchable:      vault.Searchable == nil || *vault.Searchable,
		Tags:            vault.Tags,
		EntitiesRootKey: vault.EntitiesRootKey,
	})
	if err != nil {
		return nil, err
	}

	return kp, nil
}

// initYaml bootstraps a YAML provider using settings.
func (o *Orchestrator) initYaml(ctx context.Context, vaultName string, vault config.VaultConfig) (provider.SecretProvider, error) {
	yp := provider.NewYamlProvider()
	err := yp.Initialize(ctx, provider.ProviderConfig{
		Settings: map[string]string{
			"vault_path": vault.VaultPath,
			"vault_name": vaultName,
		},
		SingleEntity:    vault.SingleEntity,
		EntityName:      vault.EntityName,
		Searchable:      vault.Searchable == nil || *vault.Searchable,
		Tags:            vault.Tags,
		EntitiesRootKey: vault.EntitiesRootKey,
	})
	if err != nil {
		return nil, err
	}

	return yp, nil
}

// initJson bootstraps a JSON provider using settings.
func (o *Orchestrator) initJson(ctx context.Context, vaultName string, vault config.VaultConfig) (provider.SecretProvider, error) {
	jp := provider.NewJsonProvider()
	err := jp.Initialize(ctx, provider.ProviderConfig{
		Settings: map[string]string{
			"vault_path": vault.VaultPath,
			"vault_name": vaultName,
		},
		SingleEntity:    vault.SingleEntity,
		EntityName:      vault.EntityName,
		Searchable:      vault.Searchable == nil || *vault.Searchable,
		Tags:            vault.Tags,
		EntitiesRootKey: vault.EntitiesRootKey,
	})
	if err != nil {
		return nil, err
	}

	return jp, nil
}

// initCustomVault bootstraps a CustomVault provider using config settings.
func (o *Orchestrator) initCustomVault(ctx context.Context, vaultName string, vault config.VaultConfig) (provider.SecretProvider, error) {
	cp := provider.NewCustomVaultProvider()
	err := cp.Initialize(ctx, provider.ProviderConfig{
		Settings: map[string]string{
			"vault_name": vaultName,
		},
		Entities: vault.Entities,
	})
	if err != nil {
		return nil, err
	}
	return cp, nil
}

// Login triggers authentication setup for a vault/scheme.
func (o *Orchestrator) Login(ctx context.Context, vaultName string) error {
	vault, ok := o.config.Vaults[vaultName]
	if !ok {
		return fmt.Errorf("unknown vault/scheme: %q", vaultName)
	}

	if vault.Provider != "keepass" {
		return fmt.Errorf("vault/scheme %q of type %q does not support authentication", vaultName, vault.Provider)
	}

	kp := provider.NewKeePassProvider()
	return kp.Initialize(ctx, provider.ProviderConfig{
		Settings: map[string]string{
			"vault_path":     vault.VaultPath,
			"remote_name":    vaultName,
			"keyring_prefix": o.KeyringPrefix(),
			"force_prompt":   "true",
		},
		SingleEntity:    vault.SingleEntity,
		EntityName:      vault.EntityName,
		Searchable:      vault.Searchable == nil || *vault.Searchable,
		Tags:            vault.Tags,
		EntitiesRootKey: vault.EntitiesRootKey,
	})
}

// Forget clears stored keyring credentials for a vault/scheme.
func (o *Orchestrator) Forget(ctx context.Context, vaultName string) error {
	vault, ok := o.config.Vaults[vaultName]
	if !ok {
		return fmt.Errorf("unknown vault/scheme: %q", vaultName)
	}

	if vault.Provider != "keepass" {
		return fmt.Errorf("vault/scheme %q of type %q does not support authentication", vaultName, vault.Provider)
	}

	prefix := o.KeyringPrefix()
	account := "provider/" + vaultName
	return o.keyring.DeleteRawSecret(prefix, account)
}

// KeyringPrefix returns the active keyring prefix. If using a custom config file,
// it computes a 10-character SHA-256 hash of its absolute path and appends it.
func (o *Orchestrator) KeyringPrefix() string {
	prefix := o.config.Keyring.Prefix
	if prefix == "" {
		prefix = "cloakenv"
	}

	if o.config.ConfigPath == "" {
		return prefix
	}

	defaultPath, err := config.DefaultConfigPath()
	if err != nil {
		return prefix
	}

	defaultAbs, err := filepath.Abs(defaultPath)
	if err != nil || o.config.ConfigPath == defaultAbs {
		return prefix
	}

	h := sha256.Sum256([]byte(o.config.ConfigPath))
	hashStr := hex.EncodeToString(h[:])[:10]
	return fmt.Sprintf("cloakenv_%s", hashStr)
}

// parseURI splits "scheme://location" into its components.
func parseURI(uri string) (string, string, error) {
	parts := strings.SplitN(uri, "://", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", "", fmt.Errorf("malformed URI: %q (expected scheme://location)", uri)
	}
	return parts[0], parts[1], nil
}

// CheckAccess checks if a vault is active/accessible (i.e. can be successfully initialized).
func (o *Orchestrator) CheckAccess(ctx context.Context, vaultName string) error {
	_, err := o.getVaultProvider(ctx, vaultName)
	return err
}

// newBareProvider creates an uninitialized provider instance for capability
// probing (e.g. interface assertions) without performing any I/O or
// initialization. It mirrors the type switch in initVaultProvider.
func newBareProvider(providerType string) (provider.SecretProvider, error) {
	switch providerType {
	case "keepass":
		return provider.NewKeePassProvider(), nil
	case "yaml":
		return provider.NewYamlProvider(), nil
	case "json":
		return provider.NewJsonProvider(), nil
	case "custom_vault":
		return provider.NewCustomVaultProvider(), nil
	default:
		return nil, fmt.Errorf("unsupported provider type %q", providerType)
	}
}
