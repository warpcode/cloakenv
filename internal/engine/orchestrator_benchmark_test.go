package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
)

func BenchmarkEnvsBuilder_QueryFilter(b *testing.B) {
	entries := make(map[string]provider.Entry, 1000)
	for i := range 1000 {
		key := fmt.Sprintf("entry_%d", i)
		entries[key] = provider.Entry{
			"title": fmt.Sprintf("Title %d", i),
			"path":  key,
			"tags":  []string{fmt.Sprintf("tag_%d", i%10), "common"},
			"attr1": fmt.Sprintf("val1_%d", i),
			"attr2": i,
		}
	}

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"bench": {Provider: "env"},
		},
		Envs: map[string][]config.VaultSourceConfig{
			"filtered": {
				{
					Vault: "bench",
					Query: `{{ and (gt .attr2 500) (hasTag "common" .tags) }}`,
					Rules: []config.RuleConfig{{Type: "pass", Include: []string{"attr1"}}},
				},
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		b.Fatalf("failed to create orchestrator: %v", err)
	}
	orch.providerManager.vaultCache["bench"] = newTestMemoryProvider(entries)

	ctx := context.Background()
	b.ResetTimer()
	for range b.N {
		if _, err := orch.EnvsBuilder().Build(ctx, "filtered"); err != nil {
			b.Fatalf("Build failed: %v", err)
		}
	}
}
