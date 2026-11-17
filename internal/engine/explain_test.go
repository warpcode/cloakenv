package engine

import (
	"context"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
)

func TestExplainEntry_NestedKeyFormatting(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"custom": {
				Provider:  "env",
				KeyFormat: "kebab",
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("NewOrchestrator failed: %v", err)
	}

	if err := injectMockVault(orch, "custom", []provider.SearchResult{
		{
			Provider: "search",
			Vault:    "custom",
			Path:     "item",
			Entry: provider.Entry{
				Title: "item",
				Attributes: map[string]any{
					"server_config": map[string]any{
						"port_number": 8080,
					},
				},
			},
		},
	}); err != nil {
		t.Fatalf("injectMockVault failed: %v", err)
	}

	explanation, err := orch.ExplainEntry(ctx, "custom://item")
	if err != nil {
		t.Fatalf("ExplainEntry failed: %v", err)
	}

	if explanation.KeyFormatted.FormattedKeys["server_config"] != "server-config" {
		t.Errorf("expected server_config -> server-config, got %q", explanation.KeyFormatted.FormattedKeys["server_config"])
	}
	if explanation.KeyFormatted.FormattedKeys["server_config.port_number"] != "server-config.port-number" {
		t.Errorf("expected server_config.port_number -> server-config.port-number, got %q", explanation.KeyFormatted.FormattedKeys["server_config.port_number"])
	}
}
