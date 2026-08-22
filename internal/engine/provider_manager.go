package engine

import (
	"context"
	"fmt"
	"sync"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
	"github.com/warpcode/cloakenv/internal/utils"
)

// ProviderManager manages the initialization and lifecycle of all secret providers.
type ProviderManager struct {
	config              *config.Config
	builtins            map[string]provider.SecretProvider
	initializedBuiltins map[string]bool
	vaultCache          map[string]provider.SecretProvider
	initLocks           map[string]*sync.Mutex
	keyring             *provider.OSKeyringProvider
	mu                  sync.Mutex
}

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

// GetProvider retrieves and initializes a provider by scheme.
// Returns the provider, and a boolean indicating if it is a built-in provider.
func (pm *ProviderManager) GetProvider(ctx context.Context, scheme string) (provider.SecretProvider, bool, error) {
	pm.mu.Lock()
	p, isBuiltin := pm.builtins[scheme]
	pm.mu.Unlock()

	if isBuiltin {
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
//
// Initialization performs blocking work (file I/O, keyring access, and for
// KeePass possibly an interactive master-password prompt), so it runs under a
// per-vault lock instead of the global mutex. This keeps unrelated provider
// lookups responsive while one vault is being initialized, while still
// guaranteeing each vault is initialized exactly once.
func (pm *ProviderManager) getVaultProvider(ctx context.Context, vaultName string) (provider.SecretProvider, error) {
	// Fast path: check the cache under the global lock.
	pm.mu.Lock()
	if p, ok := pm.vaultCache[vaultName]; ok {
		pm.mu.Unlock()
		return p, nil
	}
	initMu := pm.initLock(vaultName)
	pm.mu.Unlock()

	// Serialize only identical vaults during blocking initialization.
	initMu.Lock()
	defer initMu.Unlock()

	// Double-check: another goroutine may have initialized this vault while we
	// waited on the per-vault lock.
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
	pm.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown scheme or vault: %q (not a built-in and not defined in config)", vaultName)
	}

	// Initialize the provider for this vault type (blocking; outside pm.mu).
	p, err := pm.initVaultProvider(ctx, vaultName, vault)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize vault %q: %w", vaultName, err)
	}

	// Cache for subsequent lookups within the same run
	pm.mu.Lock()
	pm.vaultCache[vaultName] = p
	pm.mu.Unlock()
	return p, nil
}

// initVaultProvider creates and initializes a provider for a configured vault.
func (pm *ProviderManager) initVaultProvider(ctx context.Context, vaultName string, vault config.VaultConfig) (provider.SecretProvider, error) {
	switch vault.Provider {
	case "keepass":
		return pm.initKeePass(ctx, vaultName, vault)
	case "yaml":
		return pm.initYaml(ctx, vaultName, vault)
	case "json":
		return pm.initJson(ctx, vaultName, vault)
	case "custom_vault":
		return pm.initCustomVault(ctx, vaultName, vault)
	default:
		return nil, fmt.Errorf("unsupported provider type: %q", vault.Provider)
	}
}

// initKeePass bootstraps a KeePass provider using settings.
func (pm *ProviderManager) initKeePass(ctx context.Context, vaultName string, vault config.VaultConfig) (provider.SecretProvider, error) {
	kp := provider.NewKeePassProvider()
	err := kp.Initialize(ctx, provider.ProviderConfig{
		Settings: map[string]string{
			"vault_path":     vault.VaultPath,
			"remote_name":    vaultName,
			"keyring_prefix": pm.config.KeyringPrefix(),
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
func (pm *ProviderManager) initYaml(ctx context.Context, vaultName string, vault config.VaultConfig) (provider.SecretProvider, error) {
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
func (pm *ProviderManager) initJson(ctx context.Context, vaultName string, vault config.VaultConfig) (provider.SecretProvider, error) {
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
func (pm *ProviderManager) initCustomVault(ctx context.Context, vaultName string, vault config.VaultConfig) (provider.SecretProvider, error) {
	cp := provider.NewCustomVaultProvider()
	entities := vault.Entities
	if entities == nil {
		entities = make(map[string]map[string]any)
	}
	if (vault.SingleEntity != nil && *vault.SingleEntity) || len(vault.Attributes) > 0 {
		entityName := vault.EntityName
		if entityName == "" {
			entityName = vaultName
		}
		attrs := make(map[string]any)
		for k, v := range vault.Attributes {
			attrs[k] = v
		}
		if len(vault.Tags) > 0 {
			attrs["tags"] = vault.Tags
		}
		attrs["title"] = entityName
		entities[""] = attrs
		entities[entityName] = attrs
		entities["default"] = attrs
	}

	err := cp.Initialize(ctx, provider.ProviderConfig{
		Settings: map[string]string{
			"vault_name": vaultName,
		},
		Entities: entities,
	})
	if err != nil {
		return nil, err
	}
	return cp, nil
}

// EnsureInitialized initializes a built-in provider if it hasn't been already.
//
// Like getVaultProvider, initialization runs under a per-scheme lock so that
// blocking work (e.g. cache key generation with keyring I/O) does not hold the
// global mutex and stall unrelated lookups.
func (pm *ProviderManager) EnsureInitialized(ctx context.Context, scheme string, p provider.SecretProvider) error {
	pm.mu.Lock()
	if pm.initializedBuiltins[scheme] {
		pm.mu.Unlock()
		return nil
	}
	initMu := pm.initLock("builtin:" + scheme)
	pm.mu.Unlock()

	initMu.Lock()
	defer initMu.Unlock()

	// Double-check after acquiring the per-scheme lock.
	pm.mu.Lock()
	if pm.initializedBuiltins[scheme] {
		pm.mu.Unlock()
		return nil
	}
	settings := make(map[string]string)
	if scheme == "cache" {
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

// Write takes a full URI and writes the secret value to that location.
func (pm *ProviderManager) Write(ctx context.Context, uri string, value string) error {
	scheme, location, err := utils.ParseURI(uri)
	if err != nil {
		return err
	}

	p, _, err := pm.GetProvider(ctx, scheme)
	if err != nil {
		return err
	}
	return p.SetSecret(ctx, location, value)
}

// Delete takes a full URI and removes the secret from the provider.
func (pm *ProviderManager) Delete(ctx context.Context, uri string) error {
	scheme, location, err := utils.ParseURI(uri)
	if err != nil {
		return err
	}

	p, _, err := pm.GetProvider(ctx, scheme)
	if err != nil {
		return err
	}
	return p.DeleteSecret(ctx, location)
}

// ClearCache clears all local cache files.
func (pm *ProviderManager) ClearCache(ctx context.Context) error {
	p, isBuiltin, err := pm.GetProvider(ctx, "cache")
	if err != nil {
		return fmt.Errorf("cache provider not registered or failed to initialize: %w", err)
	}
	if !isBuiltin {
		return fmt.Errorf("invalid cache provider type")
	}

	cacheProv, ok := p.(interface {
		ClearCache() error
	})
	if !ok {
		return fmt.Errorf("invalid cache provider type")
	}
	return cacheProv.ClearCache()
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
	return kp.Initialize(ctx, provider.ProviderConfig{
		Settings: map[string]string{
			"vault_path":     vault.VaultPath,
			"remote_name":    vaultName,
			"keyring_prefix": pm.config.KeyringPrefix(),
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
func (pm *ProviderManager) Forget(ctx context.Context, vaultName string) error {
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

// newBareProvider creates an uninitialized provider instance for capability
// probing (e.g. interface assertions) without performing any I/O or
// initialization.
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
