package engine

import "github.com/warpcode/cloakenv/internal/provider"

// InjectMockVaultForTest injects a SearchProvider-backed mock vault into o for external engine_test tests.
func (o *Orchestrator) InjectMockVaultForTest(vaultName string, results []provider.SearchResult, rootPrefixes ...string) error {
	return injectMockVault(o, vaultName, results, rootPrefixes...)
}
