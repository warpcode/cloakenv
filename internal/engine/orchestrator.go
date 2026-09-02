package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
)

// Orchestrator coordinates secret resolution across multiple vaults and providers.
// Built-in schemes ("keyring", "env", "cache") are always available; configured vaults
// become additional schemes at runtime.
type Orchestrator struct {
	config          *config.Config
	providerManager *ProviderManager
	resolver        *Resolver
	searcher        *Searcher
	envBuilder      *EnvBuilder
}

// NewOrchestrator creates a new orchestrator with the given config,
// registers built-in providers, and validates all remote configurations.
// Note: config.Load automatically precompiles autoload rules. Callers constructing
// a Config struct manually should call cfg.CompileAutoloadRules() before evaluation.
func NewOrchestrator(cfg *config.Config) (*Orchestrator, error) {
	kr := provider.NewOSKeyringProvider()
	env := provider.NewEnvProvider()
	cache := provider.NewCacheProvider()

	builtins := map[string]provider.SecretProvider{
		kr.Scheme():    kr,
		env.Scheme():   env,
		cache.Scheme(): cache,
	}

	pm := NewProviderManager(cfg, builtins, kr)

	// Validate vault configurations
	if cfg != nil {
		for vaultName, vault := range cfg.Vaults {
			if _, isBuiltin := builtins[vaultName]; isBuiltin {
				return nil, fmt.Errorf("invalid config: vault name %q conflicts with built-in scheme", vaultName)
			}
			if vaultName == "search" {
				return nil, fmt.Errorf("invalid config: vault name %q conflicts with reserved scheme", vaultName)
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

		// Validate autoload configuration rules
		for idx, rule := range cfg.Autoload {
			if strings.TrimSpace(rule.Match) == "" {
				return nil, fmt.Errorf("invalid config: autoload rule #%d is missing 'match'", idx+1)
			}
		}
	}

	concurrencySem := make(chan struct{}, maxConcurrency)
	resolver := NewResolver(pm, concurrencySem)
	searcher := NewSearcher(pm, resolver)

	// Break the circular dependency by setting the search callback dynamically
	resolver.SetSearchFunc(func(ctx context.Context, expressionStr string, depth int) ([]provider.SearchResult, error) {
		return searcher.SearchRecursive(ctx, expressionStr, nil, depth)
	})

	envBuilder := NewEnvBuilder(cfg, resolver)

	o := &Orchestrator{
		config:          cfg,
		providerManager: pm,
		resolver:        resolver,
		searcher:        searcher,
		envBuilder:      envBuilder,
	}

	return o, nil
}

// Resolve takes a full value, expands any ${...} expressions, and
// returns the resolved secret value.
func (o *Orchestrator) Resolve(ctx context.Context, uri string) (string, error) {
	return o.resolver.Resolve(ctx, uri)
}

// ResolveWithKey takes a full value, expands any ${...} expressions, and
// includes the configKey in any failure messages if provided.
func (o *Orchestrator) ResolveWithKey(ctx context.Context, uri string, configKey string) (string, error) {
	return o.resolver.ResolveWithKey(ctx, uri, configKey)
}

// GetEntry retrieves a complete structured entry by location.
func (o *Orchestrator) GetEntry(ctx context.Context, uri string) (provider.Entry, error) {
	return o.resolver.GetEntry(ctx, uri)
}

// Search queries entries across searchable repositories using an expression.
func (o *Orchestrator) Search(ctx context.Context, expressionStr string, repoScopes []string) ([]provider.SearchResult, error) {
	return o.searcher.Search(ctx, expressionStr, repoScopes)
}

// BuildEnv constructs the full environment block without command autoloading.
func (o *Orchestrator) BuildEnv(ctx context.Context, explicit map[string]string, merges []string, whitelist []string, emptyEnv bool) ([]string, error) {
	return o.envBuilder.BuildEnv(ctx, explicit, merges, whitelist, emptyEnv)
}

// BuildEnvForCommand constructs the full environment block and evaluates config autoload rules.
func (o *Orchestrator) BuildEnvForCommand(ctx context.Context, cmdArgs []string, explicit map[string]string, merges []string, whitelist []string, emptyEnv bool) ([]string, []string, error) {
	return o.envBuilder.BuildEnvForCommand(ctx, cmdArgs, explicit, merges, whitelist, emptyEnv)
}

// Keyring returns the built-in keyring provider for direct access
// (used by the config subcommands).
func (o *Orchestrator) Keyring() *provider.OSKeyringProvider {
	return o.providerManager.Keyring()
}

// Write takes a full URI and writes the secret value to that location.
func (o *Orchestrator) Write(ctx context.Context, uri string, value string) error {
	return o.providerManager.Write(ctx, uri, value)
}

// Delete takes a full URI and removes the secret from the provider.
func (o *Orchestrator) Delete(ctx context.Context, uri string) error {
	return o.providerManager.Delete(ctx, uri)
}

// ClearCache clears all local cache files.
func (o *Orchestrator) ClearCache(ctx context.Context) error {
	return o.providerManager.ClearCache(ctx)
}

// Login triggers authentication setup for a vault/scheme.
func (o *Orchestrator) Login(ctx context.Context, vaultName string) error {
	return o.providerManager.Login(ctx, vaultName)
}

// Forget clears stored keyring credentials for a vault/scheme.
func (o *Orchestrator) Forget(ctx context.Context, vaultName string) error {
	return o.providerManager.Forget(ctx, vaultName)
}

// KeyringPrefix returns the active keyring prefix.
func (o *Orchestrator) KeyringPrefix() string {
	return o.config.KeyringPrefix()
}

// CheckAccess checks if a vault is active/accessible.
func (o *Orchestrator) CheckAccess(ctx context.Context, vaultName string) error {
	return o.providerManager.CheckAccess(ctx, vaultName)
}

// MatchRunAlias evaluates configured autoload/run alias rules against command arguments.
func (o *Orchestrator) MatchRunAlias(cmdArgs []string) (config.AutoloadRule, bool, error) {
	if o == nil {
		return config.AutoloadRule{}, false, nil
	}
	return MatchRunAlias(o.config, cmdArgs)
}

// IsRunAlias reports whether a command argument slice matches any configured autoload/run alias rule.
func (o *Orchestrator) IsRunAlias(cmdArgs []string) bool {
	if o == nil {
		return false
	}
	return IsRunAlias(o.config, cmdArgs)
}
