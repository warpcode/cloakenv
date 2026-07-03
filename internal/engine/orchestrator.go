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

	"cloakenv/internal/config"
	"cloakenv/internal/provider"

	"github.com/expr-lang/expr"
)

// Orchestrator resolves secret URIs by dispatching to the appropriate
// SecretProvider based on the URI scheme. Built-in schemes (keyring://,
// env://) are always available. User-defined remote names from config
// become additional schemes at runtime.
type Orchestrator struct {
	config              *config.Config
	builtins            map[string]provider.SecretProvider
	initializedBuiltins map[string]bool
	remoteCache         map[string]provider.SecretProvider
	keyring             *provider.OSKeyringProvider
	mu                  sync.Mutex
}

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
		remoteCache:         make(map[string]provider.SecretProvider),
		keyring:             kr,
	}

	// Validate remote configurations
	for remoteName, remote := range cfg.Providers {
		switch remote.Provider {
		case "keepass":
			kp := provider.NewKeePassProvider()
			settings := map[string]string{
				"database_path": remote.DatabasePath,
			}
			if err := kp.Validate(settings); err != nil {
				return nil, fmt.Errorf("invalid config for remote %q: %w", remoteName, err)
			}
		case "yaml":
			yp := provider.NewYamlProvider()
			settings := map[string]string{
				"database_path": remote.DatabasePath,
				"entries_key":   remote.EntriesKey,
			}
			if err := yp.Validate(settings); err != nil {
				return nil, fmt.Errorf("invalid config for remote %q: %w", remoteName, err)
			}
		case "json":
			jp := provider.NewJsonProvider()
			settings := map[string]string{
				"database_path": remote.DatabasePath,
				"entries_key":   remote.EntriesKey,
			}
			if err := jp.Validate(settings); err != nil {
				return nil, fmt.Errorf("invalid config for remote %q: %w", remoteName, err)
			}
		default:
			return nil, fmt.Errorf("unsupported remote type %q for remote %q", remote.Provider, remoteName)
		}
	}

	return o, nil
}

// Resolve takes a full URI (e.g., "work://AI/OpenAI:Password") and
// returns the resolved secret value.
func (o *Orchestrator) Resolve(ctx context.Context, uri string) (string, error) {
	return o.resolveRecursive(ctx, uri, 0)
}

func (o *Orchestrator) resolveRecursive(ctx context.Context, uri string, depth int) (string, error) {
	if depth > 5 {
		return "", fmt.Errorf("infinite secret resolution recursion detected: reached max depth 5 resolving %q", uri)
	}

	scheme, location, err := parseURI(uri)
	if err != nil {
		// If not a valid URI (doesn't contain ://), treat it as a literal value
		return uri, nil
	}

	o.mu.Lock()
	_, isBuiltin := o.builtins[scheme]
	_, isRemote := o.config.Providers[scheme]
	o.mu.Unlock()

	if !isBuiltin && !isRemote && scheme != "search" {
		return uri, nil
	}

	// Handle search scheme dynamically
	if scheme == "search" {
		exprQuery, attr, err := parseSearchURI(location)
		if err != nil {
			return "", err
		}

		results, err := o.Search(ctx, exprQuery, nil)
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

		if str, ok := val.(string); ok {
			return o.resolveRecursive(ctx, str, depth+1)
		}
		return o.resolveRecursive(ctx, fmt.Sprintf("%v", val), depth+1)
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
		// Check user-defined remotes
		var getErr error
		val, getErr = o.resolveRemote(ctx, scheme, location)
		if getErr != nil {
			return "", getErr
		}
	}

	// If the value retrieved looks like a URI, recursively resolve it
	if strings.Contains(val, "://") {
		return o.resolveRecursive(ctx, val, depth+1)
	}

	return val, nil
}

// GetEntry retrieves a complete structured entry by location.
func (o *Orchestrator) GetEntry(ctx context.Context, uri string) (provider.Entry, error) {
	scheme, location, err := parseURI(uri)
	if err != nil {
		return provider.Entry{}, err
	}

	var p provider.SecretProvider
	if builtin, ok := o.builtins[scheme]; ok {
		if err := o.ensureInitialized(ctx, scheme, builtin); err != nil {
			return provider.Entry{}, err
		}
		p = builtin
	} else {
		var getErr error
		p, getErr = o.getRemoteProvider(ctx, scheme)
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

	// Recursively resolve all attributes that contain secret URIs
	resolvedAttrs := make(map[string]any)
	for k, v := range entry.Attributes {
		resolvedVal, err := o.resolveAttrRecursive(ctx, v, 0)
		if err != nil {
			return provider.Entry{}, fmt.Errorf("failed to resolve attribute %q: %w", k, err)
		}
		resolvedAttrs[k] = resolvedVal
	}
	entry.Attributes = resolvedAttrs

	return entry, nil
}

func (o *Orchestrator) resolveAttrRecursive(ctx context.Context, val any, depth int) (any, error) {
	if depth > 5 {
		return nil, errors.New("max recursion depth reached resolving attribute")
	}

	switch typedVal := val.(type) {
	case string:
		if strings.Contains(typedVal, "://") {
			resolved, err := o.resolveRecursive(ctx, typedVal, depth)
			if err != nil {
				return nil, err
			}
			return resolved, nil
		}
		return typedVal, nil
	case []string:
		resolvedSlice := make([]string, len(typedVal))
		for i, v := range typedVal {
			res, err := o.resolveAttrRecursive(ctx, v, depth)
			if err != nil {
				return nil, err
			}
			if str, ok := res.(string); ok {
				resolvedSlice[i] = str
			} else {
				resolvedSlice[i] = fmt.Sprintf("%v", res)
			}
		}
		return resolvedSlice, nil
	case []any:
		resolvedSlice := make([]any, len(typedVal))
		for i, v := range typedVal {
			res, err := o.resolveAttrRecursive(ctx, v, depth)
			if err != nil {
				return nil, err
			}
			resolvedSlice[i] = res
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
			p, err := o.getRemoteProvider(ctx, repoScope)
			if err != nil {
				return nil, err
			}
			if searchable, ok := p.(provider.SearchableProvider); ok {
				providersToSearch[repoScope] = searchable
			} else {
				return nil, fmt.Errorf("repository %q does not support searching", repoScope)
			}
		}
	} else {
		for remoteName := range o.config.Providers {
			p, err := o.getRemoteProvider(ctx, remoteName)
			if err != nil {
				continue
			}
			if searchable, ok := p.(provider.SearchableProvider); ok {
				providersToSearch[remoteName] = searchable
			}
		}
	}

	return providersToSearch, nil
}

func (o *Orchestrator) resolveSearchResultAttributes(ctx context.Context, r provider.SearchResult) provider.SearchResult {
	// Recursively resolve attributes
	resolvedAttrs := make(map[string]any)
	for k, v := range r.Entry.Attributes {
		res, err := o.resolveAttrRecursive(ctx, v, 0)
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

	program, err := expr.Compile(expressionStr, expr.Env(sampleEnv), expr.AllowUndefinedVariables())
	if err != nil {
		return nil, fmt.Errorf("invalid query expression: %w", err)
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
			r.Repository = name
			allResults = append(allResults, o.resolveSearchResultAttributes(ctx, r))
		}
	}

	return o.filterResultsByExpression(expressionStr, allResults)
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
func (o *Orchestrator) BuildEnv(ctx context.Context, explicit map[string]string, fileEnv map[string]string, whitelist []string, forwardParent bool) ([]string, error) {
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
		whitelistSet[k] = true
	}
	hasWhitelist := len(whitelist) > 0

	// finalEnv maps key -> value
	finalEnv := make(map[string]string)

	// - A. Parent env is least important (lowest priority)
	// If forwardParent is true, we copy all parent env first.
	if forwardParent {
		for k, v := range parentEnvMap {
			finalEnv[k] = v
		}
	}

	// - B. File env (-f next)
	// We only include keys from fileEnv. If whitelist is specified, we filter fileEnv by whitelist.
	for k, uri := range fileEnv {
		if hasWhitelist && !whitelistSet[k] {
			continue
		}
		val, err := o.Resolve(ctx, uri)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve file env %s=%s: %w", k, uri, err)
		}
		finalEnv[k] = val
	}

	// - C. Whitelist filters parent env if forwardParent is NOT set
	// If forwardParent is false, any key in whitelist that is in parentEnv (and not overwritten by fileEnv) is copied.
	if !forwardParent && hasWhitelist {
		for k := range whitelistSet {
			// If already set by fileEnv, it is already in finalEnv. Otherwise:
			if _, exists := finalEnv[k]; !exists {
				if val, existsInParent := parentEnvMap[k]; existsInParent {
					finalEnv[k] = val
				}
			}
		}
	}

	// - D. Explicit mappings (-e highest priority)
	// These are never filtered by whitelist and overwrite everything.
	for k, uri := range explicit {
		val, err := o.Resolve(ctx, uri)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve explicit env %s=%s: %w", k, uri, err)
		}
		finalEnv[k] = val
	}

	// Convert finalEnv map to []string slice in "KEY=VALUE" format
	var result []string
	for k, v := range finalEnv {
		result = append(result, fmt.Sprintf("%s=%s", k, v))
	}

	return result, nil
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

	// Check user-defined remotes
	p, err := o.getRemoteProvider(ctx, scheme)
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

	// Check user-defined remotes
	p, err := o.getRemoteProvider(ctx, scheme)
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

// resolveRemote handles URIs whose scheme matches a user-defined remote name.
func (o *Orchestrator) resolveRemote(ctx context.Context, remoteName, location string) (string, error) {
	p, err := o.getRemoteProvider(ctx, remoteName)
	if err != nil {
		return "", err
	}
	return p.GetSecret(ctx, location)
}

// getRemoteProvider retrieves and initializes a remote provider, caching it for subsequent calls.
func (o *Orchestrator) getRemoteProvider(ctx context.Context, remoteName string) (provider.SecretProvider, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	// Check the cache first
	if p, ok := o.remoteCache[remoteName]; ok {
		return p, nil
	}

	// Look up the remote in config
	remote, ok := o.config.Providers[remoteName]
	if !ok {
		return nil, fmt.Errorf("unknown scheme or remote: %q (not a built-in and not defined in config)", remoteName)
	}

	// Initialize the provider for this remote type
	p, err := o.initRemoteProvider(ctx, remoteName, remote)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize remote %q: %w", remoteName, err)
	}

	// Cache for subsequent lookups within the same run
	o.remoteCache[remoteName] = p
	return p, nil
}

// initRemoteProvider creates and initializes a provider for a configured remote.
// Currently supports the "keepass", "yaml" and "json" types.
func (o *Orchestrator) initRemoteProvider(ctx context.Context, remoteName string, remote config.ProviderConfig) (provider.SecretProvider, error) {
	switch remote.Provider {
	case "keepass":
		return o.initKeePass(ctx, remoteName, remote)
	case "yaml":
		return o.initYaml(ctx, remoteName, remote)
	case "json":
		return o.initJson(ctx, remoteName, remote)
	default:
		return nil, fmt.Errorf("unsupported remote type: %q", remote.Provider)
	}
}

// initKeePass bootstraps a KeePass provider using settings.
func (o *Orchestrator) initKeePass(ctx context.Context, remoteName string, remote config.ProviderConfig) (provider.SecretProvider, error) {
	kp := provider.NewKeePassProvider()
	err := kp.Initialize(ctx, provider.ProviderConfig{
		Settings: map[string]string{
			"database_path":  remote.DatabasePath,
			"remote_name":    remoteName,
			"keyring_prefix": o.KeyringPrefix(),
		},
	})
	if err != nil {
		return nil, err
	}

	return kp, nil
}

// initYaml bootstraps a YAML provider using settings.
func (o *Orchestrator) initYaml(ctx context.Context, remoteName string, remote config.ProviderConfig) (provider.SecretProvider, error) {
	yp := provider.NewYamlProvider()
	err := yp.Initialize(ctx, provider.ProviderConfig{
		Settings: map[string]string{
			"database_path": remote.DatabasePath,
			"entries_key":   remote.EntriesKey,
		},
	})
	if err != nil {
		return nil, err
	}

	return yp, nil
}

// initJson bootstraps a JSON provider using settings.
func (o *Orchestrator) initJson(ctx context.Context, remoteName string, remote config.ProviderConfig) (provider.SecretProvider, error) {
	jp := provider.NewJsonProvider()
	err := jp.Initialize(ctx, provider.ProviderConfig{
		Settings: map[string]string{
			"database_path": remote.DatabasePath,
			"entries_key":   remote.EntriesKey,
		},
	})
	if err != nil {
		return nil, err
	}

	return jp, nil
}

// Login triggers authentication setup for a remote/scheme.
func (o *Orchestrator) Login(ctx context.Context, remoteName string) error {
	remote, ok := o.config.Providers[remoteName]
	if !ok {
		return fmt.Errorf("unknown remote/scheme: %q", remoteName)
	}

	if remote.Provider != "keepass" {
		return fmt.Errorf("remote/scheme %q of type %q does not support authentication", remoteName, remote.Provider)
	}

	kp := provider.NewKeePassProvider()
	return kp.Initialize(ctx, provider.ProviderConfig{
		Settings: map[string]string{
			"database_path":  remote.DatabasePath,
			"remote_name":    remoteName,
			"keyring_prefix": o.KeyringPrefix(),
			"force_prompt":   "true",
		},
	})
}

// Forget clears stored keyring credentials for a remote/scheme.
func (o *Orchestrator) Forget(ctx context.Context, remoteName string) error {
	remote, ok := o.config.Providers[remoteName]
	if !ok {
		return fmt.Errorf("unknown remote/scheme: %q", remoteName)
	}

	if remote.Provider != "keepass" {
		return fmt.Errorf("remote/scheme %q of type %q does not support authentication", remoteName, remote.Provider)
	}

	prefix := o.KeyringPrefix()
	account := "provider/" + remoteName
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
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("malformed URI: %q (expected scheme://location)", uri)
	}
	return parts[0], parts[1], nil
}
