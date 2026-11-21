package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
	"github.com/warpcode/cloakenv/internal/utils"
)

type mockVaultProvider struct {
	vaultName    string
	results      []provider.SearchResult
	rootPrefixes []string
}

func (m *mockVaultProvider) Scheme() string { return "search" }
func (m *mockVaultProvider) Initialize(_ context.Context, _ provider.ProviderConfig) error {
	return nil
}
func (m *mockVaultProvider) SetSecret(_ context.Context, _, _ string) error { return nil }
func (m *mockVaultProvider) DeleteSecret(_ context.Context, _ string) error { return nil }
func (m *mockVaultProvider) Validate(_ map[string]string) error             { return nil }
func (m *mockVaultProvider) RootPrefixes() []string                         { return m.rootPrefixes }

func (m *mockVaultProvider) Search(_ context.Context, _ provider.SearchQuery) ([]provider.SearchResult, error) {
	out := make([]provider.SearchResult, len(m.results))
	copy(out, m.results)
	return out, nil
}

func (m *mockVaultProvider) GetEntryWithPath(_ context.Context, location string) (provider.Entry, string, error) {
	for _, r := range m.results {
		if r.Path == location || r.Entry.Title == location {
			return r.Entry, r.Path, nil
		}
	}
	if len(m.results) == 1 && (location == "" || location == "default") {
		return m.results[0].Entry, m.results[0].Path, nil
	}
	return provider.Entry{}, "", fmt.Errorf("entry %q not found", location)
}

func (m *mockVaultProvider) GetEntry(ctx context.Context, location string) (provider.Entry, error) {
	e, _, err := m.GetEntryWithPath(ctx, location)
	return e, err
}

func (m *mockVaultProvider) lookupAttr(r provider.SearchResult, attr string) (string, any, bool) {
	if attr == "" {
		attr = "Password"
	}
	switch strings.ToLower(attr) {
	case "title":
		return "title", r.Entry.Title, true
	case "tags":
		return "tags", strings.Join(r.Entry.Tags, ","), true
	case "path":
		return "path", r.Path, true
	case "vault":
		return "vault", r.Vault, true
	case "provider":
		return "provider", r.Provider, true
	case "uuid":
		return "uuid", r.Entry.UUID, true
	}
	if v, ok := r.Entry.Attributes[attr]; ok {
		return attr, v, true
	}
	for k, v := range r.Entry.Attributes {
		if strings.EqualFold(k, attr) {
			return k, v, true
		}
	}
	return "", nil, false
}

func (m *mockVaultProvider) GetSecretWithRaw(_ context.Context, location string) (string, any, string, string, error) {
	if entryLoc, attr, ok := strings.Cut(location, ":"); ok {
		var matches []provider.SearchResult
		for _, r := range m.results {
			if r.Path == entryLoc || r.Entry.Title == entryLoc {
				matches = append(matches, r)
			}
		}
		if len(matches) == 0 && len(m.results) == 1 && (entryLoc == "" || entryLoc == "default") {
			matches = m.results
		}
		for _, r := range matches {
			if ck, v, found := m.lookupAttr(r, attr); found {
				return ck, v, r.Path, r.Entry.Title, nil
			}
		}
		if len(matches) == 0 {
			return "", nil, "", "", fmt.Errorf("entry %q not found", entryLoc)
		}
		return "", nil, "", "", fmt.Errorf("attribute %q not found in %q", attr, entryLoc)
	}
	if len(m.results) == 1 {
		attr := location
		if location == "default" || location == m.results[0].Path || location == m.results[0].Entry.Title {
			attr = "Password"
		}
		if ck, v, found := m.lookupAttr(m.results[0], attr); found {
			return ck, v, m.results[0].Path, m.results[0].Entry.Title, nil
		}
	}
	for _, r := range m.results {
		if r.Path == location || r.Entry.Title == location {
			if ck, v, found := m.lookupAttr(r, "Password"); found {
				return ck, v, r.Path, r.Entry.Title, nil
			}
		}
	}
	return "", nil, "", "", fmt.Errorf("secret %q not found", location)
}

func (m *mockVaultProvider) GetSecret(ctx context.Context, location string) (string, error) {
	_, v, _, _, err := m.GetSecretWithRaw(ctx, location)
	if err != nil {
		return "", err
	}
	return utils.SerializeYAMLValue(v)
}

// injectMockVault creates an in-memory vault populated with results,
// applies any Filtering, Mapping, KeyFormat, Attributes, and Caching decorators defined on
// orch.config.Vaults[vaultName], and stores it in orch.providerManager.vaultCache[vaultName].
func injectMockVault(orch *Orchestrator, vaultName string, results []provider.SearchResult, rootPrefixes ...string) error {
	var p provider.SecretProvider = &mockVaultProvider{
		vaultName:    vaultName,
		results:      results,
		rootPrefixes: rootPrefixes,
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
