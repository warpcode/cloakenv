package engine

import (
	"context"
	"fmt"
	"testing"
	"time"

	"cloakenv/internal/config"
	"cloakenv/internal/provider"
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
		Providers: make(map[string]config.ProviderConfig),
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

	// 10 keys with 1ms delay simulates a moderate KeePass workload.
	// Adjust N and delay to match real-world usage.
	fileEnv := make(map[string]string)
	for i := 0; i < 10; i++ {
		fileEnv[fmt.Sprintf("KEY_%d", i)] = fmt.Sprintf("slow://loc_%d", i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := o.BuildEnv(ctx, nil, fileEnv, nil, false)
		if err != nil {
			b.Fatal(err)
		}
	}
}
