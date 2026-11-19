package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/warpcode/cloakenv/internal/audit"
	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
)

// Orchestrator coordinates declarative secret pipelines (envs, templates, autoload),
// redacted inspection, vault authentication, and cache lifecycle operations.
type Orchestrator struct {
	config            *config.Config
	providerManager   *ProviderManager
	envBuilder        *EnvBuilder
	ruleEngine        *RuleEngine
	envsBuilder       *EnvsBuilder
	templatesRenderer *TemplatesRenderer
	auditLogger       *audit.Logger
}

// NewOrchestrator creates a new orchestrator with the given config,
// registers built-in providers, and validates all vault and autoload configurations.
// Note: config.Load automatically precompiles autoload rules. Callers constructing
// a Config struct manually should call cfg.CompileAutoloadRules() before evaluation.
func NewOrchestrator(cfg *config.Config) (*Orchestrator, error) {
	kr := provider.NewOSKeyringProvider()
	env := provider.NewEnvProvider()
	cache := provider.NewCacheProvider()
	pass := provider.NewPassProvider()

	builtins := map[string]provider.SecretProvider{
		kr.Scheme():    kr,
		env.Scheme():   env,
		cache.Scheme(): cache,
		pass.Scheme():  pass,
	}

	pm := NewProviderManager(cfg, builtins, kr)

	// Validate vault configurations
	if cfg != nil {
		for vaultName, vault := range cfg.Vaults {
			if err := validateVaultConfig(vaultName, vault, builtins); err != nil {
				return nil, err
			}
		}

		// Validate autoload configuration rules
		for idx, rule := range cfg.Autoload {
			if strings.TrimSpace(rule.Match) == "" {
				return nil, fmt.Errorf("invalid config: autoload rule #%d is missing 'match'", idx+1)
			}
		}
	}

	var auditLogger *audit.Logger
	if cfg != nil && cfg.Audit.Enabled {
		var err error
		auditLogger, err = audit.NewLogger(audit.Config{
			Enabled: cfg.Audit.Enabled,
			LogPath: cfg.Audit.LogPath,
			Format:  cfg.Audit.Format,
		})
		if err != nil {
			return nil, err
		}
	}

	fetcher := &providerManagerEntryFetcher{pm: pm, auditLogger: auditLogger}
	ruleEngine := NewRuleEngine()
	envsBuilder := NewEnvsBuilderWithFetcher(cfg, fetcher, ruleEngine)
	templatesRenderer := NewTemplatesRendererWithFetcher(cfg, fetcher, ruleEngine, envsBuilder)
	envBuilder := NewEnvBuilder(cfg, envsBuilder, templatesRenderer, ruleEngine)

	return &Orchestrator{
		config:            cfg,
		providerManager:   pm,
		envBuilder:        envBuilder,
		ruleEngine:        ruleEngine,
		envsBuilder:       envsBuilder,
		templatesRenderer: templatesRenderer,
		auditLogger:       auditLogger,
	}, nil
}

// BuildEnvForCommand constructs the full environment block and evaluates config autoload rules.
func (o *Orchestrator) BuildEnvForCommand(ctx context.Context, cfg EnvConfig) ([]string, []string, error) {
	return o.envBuilder.BuildEnvForCommand(ctx, cfg)
}

// BuildEnvForCommandDetails constructs the full environment block and returns execution details.
func (o *Orchestrator) BuildEnvForCommandDetails(ctx context.Context, cfg EnvConfig) (*CommandEnvDetails, error) {
	return o.envBuilder.BuildEnvForCommandDetails(ctx, cfg)
}

// Keyring returns the built-in keyring provider for direct access.
func (o *Orchestrator) Keyring() *provider.OSKeyringProvider {
	return o.providerManager.Keyring()
}

// ClearCache clears all local cache files and vault caches.
func (o *Orchestrator) ClearCache(ctx context.Context) error {
	return o.providerManager.ClearCache(ctx)
}

// PruneCache removes expired cache entries from the standalone cache provider
// and across all configured vaults with caching enabled.
func (o *Orchestrator) PruneCache(ctx context.Context) (PruneStats, error) {
	return o.providerManager.PruneCache(ctx)
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

// Close flushes and closes any enabled audit logger.
func (o *Orchestrator) Close() error {
	if o == nil || o.auditLogger == nil {
		return nil
	}
	return o.auditLogger.Close()
}

// CheckAccess checks if a vault is active/accessible.
func (o *Orchestrator) CheckAccess(ctx context.Context, vaultName string) error {
	return o.providerManager.CheckAccess(ctx, vaultName)
}

func validateVaultConfig(vaultName string, vault config.VaultConfig, builtins map[string]provider.SecretProvider) error {
	if _, isBuiltin := builtins[vaultName]; isBuiltin {
		if (vaultName != "env" || vault.Provider != "env") &&
			(vaultName != "pass" || vault.Provider != "pass") &&
			(vaultName != "keyring" || vault.Provider != "keyring") {
			return fmt.Errorf("invalid config: vault name %q conflicts with built-in scheme", vaultName)
		}
	}
	if vaultName == "search" {
		return fmt.Errorf("invalid config: vault name %q conflicts with reserved scheme", vaultName)
	}

	switch vault.Provider {
	case "keepass":
		kp := provider.NewKeePassProvider()
		settings := map[string]string{
			"vault_path": vault.VaultPath,
		}
		if vault.KeyFile != "" {
			settings["key_file"] = vault.KeyFile
		}
		if err := kp.Validate(settings); err != nil {
			return fmt.Errorf("invalid config for vault %q: %w", vaultName, err)
		}
	case "pass":
		pp := provider.NewPassProvider()
		settings := map[string]string{
			"store_dir":  vault.VaultPath,
			"vault_path": vault.VaultPath,
		}
		if err := pp.Validate(settings); err != nil {
			return fmt.Errorf("invalid config for vault %q: %w", vaultName, err)
		}
	case "vault":
		vp := provider.NewVaultProvider()
		settings := map[string]string{
			"address":   vault.Address,
			"token":     vault.Token,
			"role_id":   vault.RoleID,
			"secret_id": vault.SecretID,
		}
		if err := vp.Validate(settings); err != nil {
			return fmt.Errorf("invalid config for vault %q: %w", vaultName, err)
		}
	case "env", "keyring":
		// Stateless built-in read-only providers require no vault_path settings.
	default:
		return fmt.Errorf("unsupported provider type %q for vault %q", vault.Provider, vaultName)
	}

	return nil
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

// EnvsBuilder returns the declarative `envs:` pipeline builder.
func (o *Orchestrator) EnvsBuilder() *EnvsBuilder {
	if o == nil {
		return nil
	}
	return o.envsBuilder
}

// TemplatesRenderer returns the declarative `templates:` renderer.
func (o *Orchestrator) TemplatesRenderer() *TemplatesRenderer {
	if o == nil {
		return nil
	}
	return o.templatesRenderer
}
