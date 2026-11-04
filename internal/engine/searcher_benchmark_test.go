package engine

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
)

func BenchmarkParseSearchURI(b *testing.B) {
	uri := "tags=auth:ssh,env:prod,deprecated&title=bastion&path=servers/ssh/Password"
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		_, _, err := parseSearchURI(uri)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMultiVaultSearch(b *testing.B) {
	vaultCount := 8
	vaults := make(map[string]config.VaultConfig, vaultCount)
	for i := range vaultCount {
		vaults[fmt.Sprintf("v%d", i)] = config.VaultConfig{Provider: "custom_vault"}
	}
	cfg := &config.Config{Vaults: vaults}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		b.Fatalf("failed to create orchestrator: %v", err)
	}

	for i := range vaultCount {
		vName := fmt.Sprintf("v%d", i)
		orch.providerManager.vaultCache[vName] = &delayedSearchableProvider{
			delay: 100 * time.Microsecond,
			items: []provider.SearchResult{
				{
					Vault: vName,
					Path:  fmt.Sprintf("path_%d", i),
					Entry: provider.Entry{
						Title: fmt.Sprintf("Title %d", i),
						Tags:  []string{"tag1", "common"},
					},
				},
			},
		}
	}

	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		results, err := orch.Search(ctx, `"common" in tags`, nil)
		if err != nil {
			b.Fatal(err)
		}
		if len(results) != vaultCount {
			b.Fatalf("expected %d results, got %d", vaultCount, len(results))
		}
	}
}
