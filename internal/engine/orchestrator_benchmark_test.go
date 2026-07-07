package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
)

type slowProvider struct {
	delay time.Duration
}

func (p *slowProvider) Scheme() string { return "slow" }
func (p *slowProvider) Initialize(ctx context.Context, cfg provider.ProviderConfig) error {
	if d, ok := cfg.Settings["delay"]; ok {
		dur, _ := time.ParseDuration(d)
		p.delay = dur
	}
	return nil
}
func (p *slowProvider) GetSecret(ctx context.Context, location string) (string, error) {
	time.Sleep(p.delay)
	return "value-" + location, nil
}
func (p *slowProvider) SetSecret(ctx context.Context, location, value string) error { return nil }
func (p *slowProvider) DeleteSecret(ctx context.Context, location string) error     { return nil }
func (p *slowProvider) Validate(settings map[string]string) error                   { return nil }

func BenchmarkBuildEnv(b *testing.B) {
	ctx := context.Background()

	// Create Orchestrator with empty config to avoid validation errors.
	// Empty providers map: NewOrchestrator requires a non-nil map but no providers are needed for this benchmark.
	cfg := &config.Config{
		Vaults: make(map[string]config.VaultConfig),
	}

	o, err := NewOrchestrator(cfg)
	if err != nil {
		b.Fatalf("failed to create orchestrator: %v", err)
	}

	// Inject slow provider. Direct field access to builtins and initializedBuiltins
	// is intentional as this is a same-package test.
	sp := &slowProvider{delay: 1 * time.Millisecond}
	o.builtins["slow"] = sp
	o.initializedBuiltins["slow"] = true

	explicit := make(map[string]string)
	for i := 0; i < 10; i++ {
		explicit[fmt.Sprintf("KEY_%d", i)] = fmt.Sprintf("slow://loc_%d", i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := o.BuildEnv(ctx, explicit, nil, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

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
		Vaults: map[string]config.VaultConfig{
			"bench": {
				Provider:  "yaml",
				VaultPath: yamlPath,
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
