package engine

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
)

// expensiveSimulatedProvider simulates a slow remote or cryptographic backend
// (e.g. Argon2 KDBX key derivation or remote HashiCorp Vault network call).
type expensiveSimulatedProvider struct {
	calls atomic.Int64
	delay time.Duration
}

func (e *expensiveSimulatedProvider) Scheme() string { return "expensive" }
func (e *expensiveSimulatedProvider) Initialize(_ context.Context, _ provider.ProviderConfig) error {
	return nil
}
func (e *expensiveSimulatedProvider) GetSecret(_ context.Context, location string) (string, error) {
	e.calls.Add(1)
	time.Sleep(e.delay)
	return fmt.Sprintf("expensive_secret_%s", location), nil
}
func (e *expensiveSimulatedProvider) SetSecret(_ context.Context, _, _ string) error { return nil }
func (e *expensiveSimulatedProvider) DeleteSecret(_ context.Context, _ string) error { return nil }
func (e *expensiveSimulatedProvider) Validate(_ map[string]string) error             { return nil }

func BenchmarkCachingProvider(b *testing.B) {
	keyring.MockInit()
	tempDir := b.TempDir()
	b.Setenv("XDG_CACHE_HOME", tempDir)
	b.Setenv("HOME", tempDir)

	ctx := context.Background()

	b.Run("NoCache", func(b *testing.B) {
		p := &expensiveSimulatedProvider{delay: 1 * time.Millisecond}
		b.ResetTimer()
		for range b.N {
			_, _ = p.GetSecret(ctx, "db/password")
		}
	})

	b.Run("WithCache_KeyringMode", func(b *testing.B) {
		p := &expensiveSimulatedProvider{delay: 1 * time.Millisecond}
		cp, err := provider.NewCachingProvider(p, provider.CachingConfig{
			VaultName:     "bench_vault_keyring",
			TTL:           10 * time.Minute,
			Mode:          provider.CacheModeKeyring,
			KeyringPrefix: "bench_prefix",
		})
		if err != nil {
			b.Fatalf("NewCachingProvider failed: %v", err)
		}

		b.ResetTimer()
		for range b.N {
			_, _ = cp.GetSecret(ctx, "db/password")
		}
	})

	b.Run("WithCache_EncryptedDiskMode", func(b *testing.B) {
		p := &expensiveSimulatedProvider{delay: 1 * time.Millisecond}
		cp, err := provider.NewCachingProvider(p, provider.CachingConfig{
			VaultName:     "bench_vault_disk",
			TTL:           10 * time.Minute,
			Mode:          provider.CacheModeEncryptedDisk,
			KeyringPrefix: "bench_prefix",
		})
		if err != nil {
			b.Fatalf("NewCachingProvider failed: %v", err)
		}

		b.ResetTimer()
		for range b.N {
			_, _ = cp.GetSecret(ctx, "db/password")
		}
	})
}

func TestCachingIntegration_Orchestrator(t *testing.T) {
	keyring.MockInit()
	tempDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tempDir)
	t.Setenv("HOME", tempDir)

	t.Setenv("CLOAKENV_CACHED_API_KEY", "secret_api_key_value")
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"cached_vault": {
				Provider: "env",
				Cache: config.VaultCacheConfig{
					Enabled: true,
					TTL:     "500ms",
					Mode:    "keyring",
				},
			},
		},
		Envs: map[string][]config.VaultSourceConfig{
			"cached_env": {
				{
					Vault: "cached_vault",
					Rules: []config.RuleConfig{
						{Type: "pass", Include: []string{"CLOAKENV_CACHED_API_KEY"}},
					},
				},
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("NewOrchestrator failed: %v", err)
	}

	ctx := context.Background()

	// 1. Build env via cached vault
	env1, err := orch.EnvsBuilder().Build(ctx, "cached_env")
	if err != nil {
		t.Fatalf("EnvsBuilder.Build failed: %v", err)
	}
	if env1["CLOAKENV_CACHED_API_KEY"] != "secret_api_key_value" {
		t.Errorf("expected 'secret_api_key_value', got %q", env1["CLOAKENV_CACHED_API_KEY"])
	}

	// 2. Second build succeeds identically
	env2, err := orch.EnvsBuilder().Build(ctx, "cached_env")
	if err != nil || env2["CLOAKENV_CACHED_API_KEY"] != env1["CLOAKENV_CACHED_API_KEY"] {
		t.Errorf("second build failed: %v, env=%+v", err, env2)
	}

	// 3. ClearCache
	if err := orch.ClearCache(ctx); err != nil {
		t.Fatalf("ClearCache failed: %v", err)
	}

	// 4. PruneCache
	if _, err := orch.PruneCache(ctx); err != nil {
		t.Fatalf("PruneCache failed: %v", err)
	}
}
