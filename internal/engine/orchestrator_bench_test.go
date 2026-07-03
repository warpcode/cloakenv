package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"cloakenv/internal/config"
)

func BenchmarkSearch(b *testing.B) {
	// Create temp dir
	tempDir, err := os.MkdirTemp("", "cloakenv-bench")
	if err != nil {
		b.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a large entries.yaml
	entriesCount := 1000
	yamlContent := "entries:\n"
	for i := 0; i < entriesCount; i++ {
		yamlContent += fmt.Sprintf(`  entry_%d:
    tags:
      - tag_%d
      - common
    title: "Title %d"
    attr1: "val1_%d"
    attr2: %d
`, i, i%10, i, i, i)
	}

	yamlPath := filepath.Join(tempDir, "entries.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		b.Fatalf("failed to write temp yaml file: %v", err)
	}

	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"bench": {
				Provider:     "yaml",
				DatabasePath: yamlPath,
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		b.Fatalf("failed to create orchestrator: %v", err)
	}

	ctx := context.Background()
	query := `attr2 > 500 and "common" in tags`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := orch.Search(ctx, query, nil)
		if err != nil {
			b.Fatalf("Search failed: %v", err)
		}
	}
}
