package engine

import (
	"context"
	"strings"
	"time"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
)

// mockRootedSearchProvider wraps a SearchProvider and exposes RootPrefixes for tests.
type mockRootedSearchProvider struct {
	*provider.SearchProvider
	rootPrefixes []string
}

func (m *mockRootedSearchProvider) RootPrefixes() []string {
	return m.rootPrefixes
}

// injectMockVault creates a SearchProvider-backed in-memory vault populated with results,
// applies any Filtering, Mapping, KeyFormat, Attributes, and Caching decorators defined on
// orch.config.Vaults[vaultName], and stores it in orch.providerManager.vaultCache[vaultName].
func injectMockVault(orch *Orchestrator, vaultName string, results []provider.SearchResult, rootPrefixes ...string) error {
	ctx := context.Background()
	sp := provider.NewSearchProvider()
	if err := sp.Initialize(ctx, provider.ProviderConfig{
		Settings: map[string]string{
			"vault_name": vaultName,
		},
	}); err != nil {
		return err
	}
	sp.SetSearchExecutor(func(_ context.Context, _ string, _ []string, _ int) ([]provider.SearchResult, error) {
		out := make([]provider.SearchResult, len(results))
		copy(out, results)
		return out, nil
	})

	var p provider.SecretProvider = sp
	if len(rootPrefixes) > 0 {
		p = &mockRootedSearchProvider{
			SearchProvider: sp,
			rootPrefixes:   rootPrefixes,
		}
	}

	if orch.config != nil {
		if vault, ok := orch.config.Vaults[vaultName]; ok {
			var err error
			p, err = decorateTestVaultProvider(orch.providerManager, vaultName, vault, p)
			if err != nil {
				return err
			}
		}
	}

	orch.providerManager.mu.Lock()
	orch.providerManager.vaultCache[vaultName] = p
	orch.providerManager.mu.Unlock()
	return nil
}

func decorateTestVaultProvider(pm *ProviderManager, vaultName string, vault config.VaultConfig, p provider.SecretProvider) (provider.SecretProvider, error) {
	if len(vault.IncludeFields) > 0 || len(vault.ExcludeFields) > 0 {
		p = provider.NewFilteringProvider(p, vault.IncludeFields, vault.ExcludeFields)
	}
	pm.mu.Lock()
	renderTemplate := pm.renderTemplateFunc
	pm.mu.Unlock()
	if len(vault.Mapping) > 0 {
		var err error
		p, err = provider.NewMappingProvider(p, vault.Mapping)
		if err != nil {
			return nil, err
		}
		if renderTemplate != nil {
			if tr, ok := p.(interface {
				SetRenderTemplateFunc(provider.TemplateRenderFunc)
			}); ok {
				tr.SetRenderTemplateFunc(renderTemplate)
			}
		}
	}
	kf := vault.KeyFormat
	if pm.config != nil && pm.config.KeyFormatOverride != "" {
		kf = pm.config.KeyFormatOverride
	}
	if kf != "" && kf != "none" {
		p = provider.NewKeyFormatProvider(p, kf)
	}
	if len(vault.Attributes) > 0 {
		p = provider.NewAttributesProvider(p, vault.Attributes)
		if renderTemplate != nil {
			if tr, ok := p.(interface {
				SetRenderTemplateFunc(provider.TemplateRenderFunc)
			}); ok {
				tr.SetRenderTemplateFunc(renderTemplate)
			}
		}
	}
	if vault.Cache.Enabled {
		ttl := 5 * time.Minute
		if vault.Cache.TTL != "" {
			if parsedTTL, parseErr := time.ParseDuration(vault.Cache.TTL); parseErr == nil {
				ttl = parsedTTL
			}
		}
		mode := provider.CacheModeKeyring
		if vault.Cache.Mode != "" {
			mode = strings.ToLower(vault.Cache.Mode)
		}
		var err error
		p, err = provider.NewCachingProvider(p, provider.CachingConfig{
			VaultName:     vaultName,
			TTL:           ttl,
			Mode:          mode,
			KeyringPrefix: pm.config.KeyringPrefix(),
		})
		if err != nil {
			return nil, err
		}
	}
	return p, nil
}
