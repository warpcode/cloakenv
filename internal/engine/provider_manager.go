package engine

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
)

// ProviderManager manages the initialization and lifecycle of all read-only secret providers.
type ProviderManager struct {
	config              *config.Config
	builtins            map[string]provider.SecretProvider
	initializedBuiltins map[string]bool
	vaultCache          map[string]provider.SecretProvider
	initLocks           map[string]*sync.Mutex
	keyring             *provider.OSKeyringProvider
	mu                  sync.Mutex
}

// NewProviderManager constructs a ProviderManager with built-in providers and OS keyring access.
func NewProviderManager(cfg *config.Config, builtins map[string]provider.SecretProvider, keyring *provider.OSKeyringProvider) *ProviderManager {
	return &ProviderManager{
		config:              cfg,
		builtins:            builtins,
		initializedBuiltins: make(map[string]bool),
		vaultCache:          make(map[string]provider.SecretProvider),
		initLocks:           make(map[string]*sync.Mutex),
		keyring:             keyring,
	}
}

// Config returns the configuration.
func (pm *ProviderManager) Config() *config.Config {
	return pm.config
}

// Keyring returns the OS keyring provider.
func (pm *ProviderManager) Keyring() *provider.OSKeyringProvider {
	return pm.keyring
}

// HasBuiltin reports whether the given scheme is a registered built-in provider.
func (pm *ProviderManager) HasBuiltin(scheme string) bool {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	_, ok := pm.builtins[scheme]
	return ok
}

// GetProvider retrieves and initializes a provider by scheme or vault name.
// Built-in stateless providers ("env", "keyring", "pass") resolve implicitly even when
// not explicitly declared in cfg.Vaults. If explicitly declared under cfg.Vaults,
// the configured vault settings (e.g., cache or query) are used.
func (pm *ProviderManager) GetProvider(ctx context.Context, scheme string) (provider.SecretProvider, bool, error) {
	pm.mu.Lock()
	cfg := pm.config
	p, isBuiltin := pm.builtins[scheme]
	pm.mu.Unlock()

	if isBuiltin {
		if (scheme == "env" || scheme == "pass" || scheme == "keyring") && cfg != nil {
			if _, ok := cfg.Vaults[scheme]; ok {
				vaultP, err := pm.getVaultProvider(ctx, scheme)
				if err != nil {
					return nil, false, err
				}
				return vaultP, false, nil
			}
		}
		if err := pm.EnsureInitialized(ctx, scheme, p); err != nil {
			return nil, true, err
		}
		return p, true, nil
	}

	p, err := pm.getVaultProvider(ctx, scheme)
	if err != nil {
		return nil, false, err
	}
	return p, false, nil
}

// initLock returns the per-key initialization mutex for the given vault or
// builtin scheme, creating it if necessary. Callers must hold pm.mu.
func (pm *ProviderManager) initLock(key string) *sync.Mutex {
	lk, ok := pm.initLocks[key]
	if !ok {
		lk = &sync.Mutex{}
		pm.initLocks[key] = lk
	}
	return lk
}

// getVaultProvider retrieves and initializes a vault provider, caching it for subsequent calls.
func (pm *ProviderManager) getVaultProvider(ctx context.Context, vaultName string) (provider.SecretProvider, error) {
	pm.mu.Lock()
	if p, ok := pm.vaultCache[vaultName]; ok {
		pm.mu.Unlock()
		return p, nil
	}
	if pm.config == nil {
		pm.mu.Unlock()
		return nil, fmt.Errorf("no configuration loaded")
	}
	vault, ok := pm.config.Vaults[vaultName]
	if !ok {
		pm.mu.Unlock()
		return nil, fmt.Errorf("unknown scheme or vault: %q (not a built-in and not defined in config)", vaultName)
	}
	initMu := pm.initLock(vaultName)
	pm.mu.Unlock()

	initMu.Lock()
	defer initMu.Unlock()

	pm.mu.Lock()
	if p, ok := pm.vaultCache[vaultName]; ok {
		pm.mu.Unlock()
		return p, nil
	}
	pm.mu.Unlock()

	p, err := pm.initVaultProvider(ctx, vaultName, vault)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize vault %q: %w", vaultName, err)
	}

	pm.mu.Lock()
	pm.vaultCache[vaultName] = p
	pm.mu.Unlock()
	return p, nil
}

// initVaultProvider creates and initializes a pure read-only provider for a configured vault,
// optionally wrapping it in CachingProvider when vault caching is enabled.
func (pm *ProviderManager) initVaultProvider(ctx context.Context, vaultName string, vault config.VaultConfig) (provider.SecretProvider, error) {
	var p provider.SecretProvider
	var err error

	switch vault.Provider {
	case "keepass":
		p, err = pm.initKeePass(ctx, vaultName, vault)
	case "pass":
		p, err = pm.initPass(ctx, vault)
	case "vault":
		p, err = pm.initVault(ctx, vault)
	case "env":
		p, err = pm.initEnv(ctx, vault)
	case "keyring":
		p, err = pm.initKeyring(ctx, vault)
	default:
		return nil, fmt.Errorf("unsupported provider type: %q", vault.Provider)
	}
	if err != nil {
		return nil, err
	}

	if vault.Cache.Enabled {
		ttl := time.Hour
		if vault.Cache.TTL != "" {
			parsedTTL, parseErr := time.ParseDuration(vault.Cache.TTL)
			if parseErr != nil {
				return nil, fmt.Errorf("invalid cache ttl %q: %w", vault.Cache.TTL, parseErr)
			}
			ttl = parsedTTL
		}
		mode := provider.CacheModeKeyring
		if vault.Cache.Mode == "encrypted_disk" {
			mode = provider.CacheModeEncryptedDisk
		}
		p, err = provider.NewCachingProvider(p, provider.CachingConfig{
			VaultName:     vaultName,
			TTL:           ttl,
			Mode:          mode,
			KeyringPrefix: pm.config.KeyringPrefix(),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to initialize cache wrapper for vault %q: %w", vaultName, err)
		}
	}

	return p, nil
}

func (pm *ProviderManager) initKeePass(ctx context.Context, vaultName string, vault config.VaultConfig) (provider.SecretProvider, error) {
	kp := provider.NewKeePassProvider()
	settings := map[string]string{
		"vault_path":         vault.VaultPath,
		"remote_name":        vaultName,
		"keyring_prefix":     pm.config.KeyringPrefix(),
		"resolve_references": fmt.Sprintf("%t", vault.ResolveReferences),
	}
	if vault.KeyFile != "" {
		settings["key_file"] = vault.KeyFile
	}
	err := kp.Initialize(ctx, provider.ProviderConfig{
		Settings:          settings,
		Query:             vault.Query,
		ResolveReferences: vault.ResolveReferences,
	})
	if err != nil {
		return nil, err
	}
	return kp, nil
}

func (pm *ProviderManager) initPass(ctx context.Context, vault config.VaultConfig) (provider.SecretProvider, error) {
	pp := provider.NewPassProvider()
	settings := map[string]string{
		"store_dir":  vault.VaultPath,
		"vault_path": vault.VaultPath,
	}
	err := pp.Initialize(ctx, provider.ProviderConfig{
		Settings: settings,
		Query:    vault.Query,
	})
	if err != nil {
		return nil, err
	}
	return pp, nil
}

func (pm *ProviderManager) initVault(ctx context.Context, vault config.VaultConfig) (provider.SecretProvider, error) {
	vp := provider.NewVaultProvider()
	settings := map[string]string{
		"address":    vault.Address,
		"token":      vault.Token,
		"role_id":    vault.RoleID,
		"secret_id":  vault.SecretID,
		"namespace":  vault.Namespace,
		"mount_path": vault.MountPath,
	}
	if vault.EngineVersion != 0 {
		settings["engine_version"] = strconv.Itoa(vault.EngineVersion)
	}
	err := vp.Initialize(ctx, provider.ProviderConfig{
		Settings: settings,
		Query:    vault.Query,
	})
	if err != nil {
		return nil, err
	}
	return vp, nil
}

func (pm *ProviderManager) initEnv(ctx context.Context, vault config.VaultConfig) (provider.SecretProvider, error) {
	ep := provider.NewEnvProvider()
	err := ep.Initialize(ctx, provider.ProviderConfig{
		Query: vault.Query,
	})
	if err != nil {
		return nil, err
	}
	return ep, nil
}

func (pm *ProviderManager) initKeyring(ctx context.Context, vault config.VaultConfig) (provider.SecretProvider, error) {
	// Share the manager's configured keyring store (important for tests using MockKeyringStore)
	// while using a per-vault provider instance for independent service/query settings.
	var kp *provider.OSKeyringProvider
	if pm.keyring != nil {
		kp = pm.keyring.CloneWithStore()
	} else {
		kp = provider.NewOSKeyringProvider()
	}
	settings := map[string]string{
		"keyring_prefix": pm.config.KeyringPrefix(),
	}
	err := kp.Initialize(ctx, provider.ProviderConfig{
		Settings: settings,
		Query:    vault.Query,
	})
	if err != nil {
		return nil, err
	}
	return kp, nil
}

// EnsureInitialized initializes a built-in provider if it hasn't been already.
func (pm *ProviderManager) EnsureInitialized(ctx context.Context, scheme string, p provider.SecretProvider) error {
	pm.mu.Lock()
	if pm.initializedBuiltins[scheme] {
		pm.mu.Unlock()
		return nil
	}
	if _, ok := pm.builtins[scheme]; !ok {
		pm.mu.Unlock()
		return fmt.Errorf("unknown built-in provider: %q", scheme)
	}
	initMu := pm.initLock("builtin:" + scheme)
	pm.mu.Unlock()

	initMu.Lock()
	defer initMu.Unlock()

	pm.mu.Lock()
	if pm.initializedBuiltins[scheme] {
		pm.mu.Unlock()
		return nil
	}
	settings := make(map[string]string)
	if (scheme == "cache" || scheme == "keyring") && pm.config != nil {
		settings["keyring_prefix"] = pm.config.KeyringPrefix()
	}
	pm.mu.Unlock()

	if err := p.Initialize(ctx, provider.ProviderConfig{Settings: settings}); err != nil {
		return fmt.Errorf("failed to initialize built-in provider %q: %w", scheme, err)
	}

	pm.mu.Lock()
	pm.initializedBuiltins[scheme] = true
	pm.mu.Unlock()
	return nil
}

// CheckAccess checks if a vault is active/accessible (i.e. can be successfully initialized).
func (pm *ProviderManager) CheckAccess(ctx context.Context, vaultName string) error {
	_, err := pm.getVaultProvider(ctx, vaultName)
	return err
}

// ClearCache clears all local cache files and keyring vault caches.
func (pm *ProviderManager) ClearCache(ctx context.Context) error {
	var errs []error

	p, isBuiltin, err := pm.GetProvider(ctx, "cache")
	if err != nil {
		errs = append(errs, fmt.Errorf("cache provider not registered or failed to initialize: %w", err))
	} else if !isBuiltin {
		errs = append(errs, fmt.Errorf("invalid cache provider type"))
	} else {
		cacheProv, ok := p.(interface{ ClearCache() error })
		if !ok {
			errs = append(errs, fmt.Errorf("invalid cache provider type"))
		} else if err := cacheProv.ClearCache(); err != nil {
			errs = append(errs, err)
		}
	}

	if pm.config != nil {
		for vaultName, vCfg := range pm.config.Vaults {
			if vCfg.Cache.Enabled {
				prov, getErr := pm.getVaultProvider(ctx, vaultName)
				if getErr != nil {
					continue
				}
				if clearer, ok := prov.(interface{ Clear() error }); ok {
					if cErr := clearer.Clear(); cErr != nil {
						errs = append(errs, fmt.Errorf("vault %q cache clear: %w", vaultName, cErr))
					}
				}
			}
		}
	}

	return errors.Join(errs...)
}

// PruneStats reports summary statistics of a cache prune operation.
type PruneStats struct {
	Count          int
	ReclaimedBytes int64
}

// PruneCache removes expired cache entries from the standalone cache provider
// and across all configured vaults with caching enabled.
func (pm *ProviderManager) PruneCache(ctx context.Context) (PruneStats, error) {
	var stats PruneStats
	var errs []error

	p, isBuiltin, err := pm.GetProvider(ctx, "cache")
	if err != nil {
		errs = append(errs, fmt.Errorf("cache provider not registered or failed to initialize: %w", err))
	} else if !isBuiltin {
		errs = append(errs, fmt.Errorf("invalid cache provider type"))
	} else {
		if cacheProv, ok := p.(interface {
			PruneCache() (int, int64, error)
		}); ok {
			count, bytesReclaimed, pruneErr := cacheProv.PruneCache()
			stats.Count += count
			stats.ReclaimedBytes += bytesReclaimed
			if pruneErr != nil {
				errs = append(errs, fmt.Errorf("cache provider prune: %w", pruneErr))
			}
		} else {
			errs = append(errs, fmt.Errorf("cache provider does not support pruning"))
		}
	}

	if pm.config != nil {
		for vaultName, vCfg := range pm.config.Vaults {
			if vCfg.Cache.Enabled {
				prov, getErr := pm.getVaultProvider(ctx, vaultName)
				if getErr != nil {
					continue
				}
				if pruner, ok := prov.(interface {
					Prune() (int, int64, error)
				}); ok {
					count, bytesReclaimed, pErr := pruner.Prune()
					stats.Count += count
					stats.ReclaimedBytes += bytesReclaimed
					if pErr != nil {
						errs = append(errs, fmt.Errorf("vault %q cache prune: %w", vaultName, pErr))
					}
				}
			}
		}
	}

	return stats, errors.Join(errs...)
}

// Login triggers authentication setup for a vault/scheme.
func (pm *ProviderManager) Login(ctx context.Context, vaultName string) error {
	if pm.config == nil {
		return fmt.Errorf("no configuration loaded")
	}
	vault, ok := pm.config.Vaults[vaultName]
	if !ok {
		return fmt.Errorf("unknown vault/scheme: %q", vaultName)
	}
	if vault.Provider != "keepass" {
		return fmt.Errorf("vault/scheme %q of type %q does not support authentication", vaultName, vault.Provider)
	}

	kp := provider.NewKeePassProvider()
	settings := map[string]string{
		"vault_path":         vault.VaultPath,
		"remote_name":        vaultName,
		"keyring_prefix":     pm.config.KeyringPrefix(),
		"force_prompt":       "true",
		"resolve_references": fmt.Sprintf("%t", vault.ResolveReferences),
	}
	if vault.KeyFile != "" {
		settings["key_file"] = vault.KeyFile
	}
	return kp.Initialize(ctx, provider.ProviderConfig{
		Settings:          settings,
		ResolveReferences: vault.ResolveReferences,
	})
}

// Forget clears stored keyring credentials for a vault/scheme.
func (pm *ProviderManager) Forget(_ context.Context, vaultName string) error {
	if pm.config == nil {
		return fmt.Errorf("no configuration loaded")
	}
	vault, ok := pm.config.Vaults[vaultName]
	if !ok {
		return fmt.Errorf("unknown vault/scheme: %q", vaultName)
	}
	if vault.Provider != "keepass" {
		return fmt.Errorf("vault/scheme %q of type %q does not support authentication", vaultName, vault.Provider)
	}
	prefix := pm.config.KeyringPrefix()
	account := "provider/" + vaultName
	return pm.keyring.DeleteRawSecret(prefix, account)
}
