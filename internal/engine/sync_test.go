package engine

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
	"github.com/zalando/go-keyring"
)

type mockWritableProvider struct {
	mu      sync.Mutex
	secrets map[string]string
	scheme  string
}

func newMockWritableProvider(scheme string) *mockWritableProvider {
	return &mockWritableProvider{
		scheme:  scheme,
		secrets: make(map[string]string),
	}
}

func (m *mockWritableProvider) Scheme() string {
	return m.scheme
}

func (m *mockWritableProvider) Initialize(_ context.Context, _ provider.ProviderConfig) error {
	return nil
}

func (m *mockWritableProvider) GetSecret(_ context.Context, location string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	val, ok := m.secrets[location]
	if !ok {
		return "", fmt.Errorf("secret not found: %s", location)
	}
	return val, nil
}

func (m *mockWritableProvider) SetSecret(_ context.Context, location string, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.secrets[location] = value
	return nil
}

func (m *mockWritableProvider) DeleteSecret(_ context.Context, location string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.secrets, location)
	return nil
}

func (m *mockWritableProvider) Validate(_ map[string]string) error {
	return nil
}

func TestOrchestrator_Sync(t *testing.T) {
	keyring.MockInit()

	ctx := context.Background()

	t.Run("read-only target returns diagnostic error", func(t *testing.T) {
		cfg := &config.Config{}
		orch, err := NewOrchestrator(cfg)
		if err != nil {
			t.Fatalf("NewOrchestrator failed: %v", err)
		}

		t.Setenv("SRC_KEY", "someval")
		// env:// is a read-only provider
		_, err = orch.Sync(ctx, "env://SRC_KEY", "env://TARGET_KEY", SyncOptions{})
		if err == nil {
			t.Fatal("expected error syncing to read-only target, got nil")
		}
		if !strings.Contains(err.Error(), "read-only or does not support writing") {
			t.Errorf("expected read-only error message, got: %v", err)
		}
	})

	t.Run("sync single secret to keyring", func(t *testing.T) {
		cfg := &config.Config{}
		orch, err := NewOrchestrator(cfg)
		if err != nil {
			t.Fatalf("NewOrchestrator failed: %v", err)
		}

		t.Setenv("SRC_VAR", "supersecret123")

		// 1. Dry run
		summary, err := orch.Sync(ctx, "env://SRC_VAR", "keyring://app/token", SyncOptions{DryRun: true})
		if err != nil {
			t.Fatalf("Sync dry-run failed: %v", err)
		}
		if summary.Added != 1 || len(summary.Diffs) != 1 {
			t.Fatalf("expected 1 added in dry run, got %+v", summary)
		}
		if summary.Diffs[0].Key != "SRC_VAR" || summary.Diffs[0].Status != "added" {
			t.Errorf("unexpected diff: %+v", summary.Diffs[0])
		}

		// Verify target was not written in dry run
		_, err = orch.Keyring().GetSecret(ctx, "app/token")
		if err == nil {
			t.Error("expected secret to not exist in keyring after dry run")
		}

		// 2. Real run (should add)
		summary, err = orch.Sync(ctx, "env://SRC_VAR", "keyring://app/token", SyncOptions{DryRun: false})
		if err != nil {
			t.Fatalf("Sync failed: %v", err)
		}
		if summary.Added != 1 {
			t.Errorf("expected 1 added, got %d", summary.Added)
		}

		val, err := orch.Keyring().GetSecret(ctx, "app/token")
		if err != nil || val != "supersecret123" {
			t.Fatalf("keyring value mismatch: %v, got %q", err, val)
		}

		// 3. Second run with same value (should skip unchanged)
		summary, err = orch.Sync(ctx, "env://SRC_VAR", "keyring://app/token", SyncOptions{DryRun: false})
		if err != nil {
			t.Fatalf("Sync second run failed: %v", err)
		}
		if summary.Skipped != 1 || summary.Added != 0 || summary.Updated != 0 {
			t.Errorf("expected 1 skipped, got %+v", summary)
		}

		// 4. Update value
		t.Setenv("SRC_VAR", "newsecret456")
		summary, err = orch.Sync(ctx, "env://SRC_VAR", "keyring://app/token", SyncOptions{DryRun: false})
		if err != nil {
			t.Fatalf("Sync update run failed: %v", err)
		}
		if summary.Updated != 1 || summary.Added != 0 || summary.Skipped != 0 {
			t.Errorf("expected 1 updated, got %+v", summary)
		}

		val, err = orch.Keyring().GetSecret(ctx, "app/token")
		if err != nil || val != "newsecret456" {
			t.Fatalf("keyring updated value mismatch: %v, got %q", err, val)
		}
	})

	t.Run("sync custom_vault with filtering (tags, prefix, regex)", func(t *testing.T) {
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"mock_src": {
					Provider: "custom_vault",
					Entities: map[string]config.CustomEntityConfig{
						"api": {
							Title: "API Service",
							Tags:  []string{"sync:keyring", "env:prod"},
							Attributes: map[string]any{
								"KEY":    "api_key_val",
								"SECRET": "api_secret_val",
							},
						},
						"db": {
							Title: "DB Service",
							Tags:  []string{"internal"},
							Attributes: map[string]any{
								"PASSWORD": "db_pass_val",
							},
						},
					},
				},
			},
		}

		orch, err := NewOrchestrator(cfg)
		if err != nil {
			t.Fatalf("NewOrchestrator failed: %v", err)
		}

		// 1. Filter by tag: only "api" entity matches "sync:keyring"
		summary, err := orch.Sync(ctx, "mock_src://", "keyring://work", SyncOptions{
			Tags:   []string{"sync:keyring"},
			DryRun: true,
		})
		if err != nil {
			t.Fatalf("Sync failed: %v", err)
		}
		if summary.Added != 2 {
			t.Fatalf("expected 2 added (KEY, SECRET from api), got %d: %+v", summary.Added, summary.Diffs)
		}

		// 2. Filter by prefix: only keys starting with "api/K"
		summary, err = orch.Sync(ctx, "mock_src://", "keyring://work", SyncOptions{
			Prefix: "api/K",
			DryRun: true,
		})
		if err != nil {
			t.Fatalf("Sync failed: %v", err)
		}
		if summary.Added != 1 || summary.Diffs[0].Key != "api/KEY" {
			t.Fatalf("expected 1 added for prefix api/K, got %+v", summary)
		}

		// 3. Filter by regex: only keys matching ".*SECRET$"
		summary, err = orch.Sync(ctx, "mock_src://", "keyring://work", SyncOptions{
			Regex:  ".*SECRET$",
			DryRun: true,
		})
		if err != nil {
			t.Fatalf("Sync failed: %v", err)
		}
		if summary.Added != 1 || summary.Diffs[0].Key != "api/SECRET" {
			t.Fatalf("expected 1 added for regex .*SECRET$, got %+v", summary)
		}
	})

	t.Run("zero secret leakage guarantee", func(t *testing.T) {
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"leak_src": {
					Provider: "custom_vault",
					Entities: map[string]config.CustomEntityConfig{
						"creds": {
							Title: "Credentials",
							Attributes: map[string]any{
								"TOKEN": "super_sensitive_token_do_not_leak",
							},
						},
					},
				},
			},
		}

		orch, err := NewOrchestrator(cfg)
		if err != nil {
			t.Fatalf("NewOrchestrator failed: %v", err)
		}

		summary, err := orch.Sync(ctx, "leak_src://", "keyring://app", SyncOptions{DryRun: true})
		if err != nil {
			t.Fatalf("Sync failed: %v", err)
		}

		// Verify that string representation of summary / diffs does not contain secret value
		summaryStr := fmt.Sprintf("%+v", summary)
		if strings.Contains(summaryStr, "super_sensitive_token_do_not_leak") {
			t.Errorf("SECURITY VIOLATION: summary leaked secret value: %s", summaryStr)
		}
	})
}
