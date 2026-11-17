package engine

import (
	"context"
	"fmt"
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
	o.providerManager.builtins["slow"] = sp
	o.providerManager.initializedBuiltins["slow"] = true

	explicit := make(map[string]string)
	for i := range 10 {
		explicit[fmt.Sprintf("KEY_%d", i)] = fmt.Sprintf("slow://loc_%d", i)
	}

	b.ResetTimer()
	for range b.N {
		_, err := o.BuildEnv(ctx, EnvConfig{Explicit: explicit})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSearch(b *testing.B) {
	entriesCount := 1000
	results := make([]provider.SearchResult, 0, entriesCount)
	for i := range entriesCount {
		results = append(results, provider.SearchResult{
			Provider: "search",
			Vault:    "bench",
			Path:     fmt.Sprintf("entry_%d", i),
			Entry: provider.Entry{
				Title: fmt.Sprintf("Title %d", i),
				Tags:  []string{fmt.Sprintf("tag_%d", i%10), "common"},
				Attributes: map[string]any{
					"attr1": fmt.Sprintf("val1_%d", i),
					"attr2": i,
				},
			},
		})
	}

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"bench": {
				Provider: "env",
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		b.Fatalf("failed to create orchestrator: %v", err)
	}
	if err := injectMockVault(orch, "bench", results); err != nil {
		b.Fatalf("injectMockVault failed: %v", err)
	}

	ctx := context.Background()
	query := `attr2 > 500 and "common" in tags`

	b.ResetTimer()
	for range b.N {
		_, err := orch.Search(ctx, query, nil)
		if err != nil {
			b.Fatalf("Search failed: %v", err)
		}
	}
}

func BenchmarkResolveArrayAttr(b *testing.B) {
	ctx := context.Background()
	cfg := &config.Config{
		Vaults: make(map[string]config.VaultConfig),
	}

	o, err := NewOrchestrator(cfg)
	if err != nil {
		b.Fatalf("failed to create orchestrator: %v", err)
	}

	sp := &slowProvider{delay: 1 * time.Microsecond}
	o.providerManager.builtins["slow"] = sp
	o.providerManager.initializedBuiltins["slow"] = true

	var arr []string
	for i := range 1000 {
		arr = append(arr, fmt.Sprintf("slow://loc_%d", i))
	}

	b.ResetTimer()
	for range b.N {
		_, err := o.resolver.ResolveAttrRecursive(ctx, arr, 0, "")
		if err != nil {
			b.Fatal(err)
		}
	}
}
