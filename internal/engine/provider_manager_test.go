package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
)

func TestCheckAccess(t *testing.T) {
	ctx := context.Background()

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"valid_vault": {
				Provider: "env",
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	// We'll configure a failing vault using a provider that doesn't exist
	// which will cause initVaultProvider to return an "unsupported provider type" error.
	orch.config.Vaults["failing_vault"] = config.VaultConfig{
		Provider: "unsupported_provider",
	}

	tests := []struct {
		name      string
		vaultName string
		wantErr   string
	}{
		{
			name:      "valid_vault",
			vaultName: "valid_vault",
			wantErr:   "",
		},
		{
			name:      "unknown_vault",
			vaultName: "nonexistent_vault",
			wantErr:   "unknown scheme or vault",
		},
		{
			name:      "failing_vault",
			vaultName: "failing_vault",
			wantErr:   "unsupported provider type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := orch.CheckAccess(ctx, tt.vaultName)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestProviderManager_PassProvider(t *testing.T) {
	tmpDir := t.TempDir()
	gpgFile := filepath.Join(tmpDir, "development", "db.gpg")
	if err := os.MkdirAll(filepath.Dir(gpgFile), 0o700); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	content := []byte("secretpass123\nusername: myuser\n")
	if err := os.WriteFile(gpgFile, content, 0o600); err != nil {
		t.Fatalf("failed to write dummy gpg file: %v", err)
	}

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"pass_vault": {
				Provider:  "pass",
				VaultPath: tmpDir,
			},
		},
	}

	pm := NewProviderManager(cfg, nil, nil)
	ctx := context.Background()

	p, isBuiltin, err := pm.GetProvider(ctx, "pass_vault")
	if err != nil {
		t.Fatalf("GetProvider(pass_vault) error = %v", err)
	}
	if isBuiltin {
		t.Errorf("pass_vault reported as builtin, expected false")
	}

	// Unwrap filtering or decorator if needed to set decrypt function
	curr := p
	for {
		if inner, ok := curr.(interface {
			Underlying() provider.SecretProvider
		}); ok {
			curr = inner.Underlying()
		} else {
			break
		}
	}

	if passProv, ok := curr.(*provider.PassProvider); ok {
		passProv.SetDecryptFunc(func(_ context.Context, filePath string) ([]byte, error) {
			return content, nil
		})
	} else {
		t.Fatalf("expected *provider.PassProvider, got %T", curr)
	}

	// Test GetSecret with password and username
	pw, err := p.GetSecret(ctx, "development/db")
	if err != nil {
		t.Fatalf("GetSecret(development/db) error = %v", err)
	}
	if got, want := pw, "secretpass123"; got != want {
		t.Errorf("GetSecret(development/db) = %q, want %q", got, want)
	}

	user, err := p.GetSecret(ctx, "development/db:username")
	if err != nil {
		t.Fatalf("GetSecret(development/db:username) error = %v", err)
	}
	if got, want := user, "myuser"; got != want {
		t.Errorf("GetSecret(development/db:username) = %q, want %q", got, want)
	}
}

func TestClearCache(t *testing.T) {
	keyring.MockInit()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())

	cfg := &config.Config{}
	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	t.Run("missing cache provider", func(t *testing.T) {
		origCache := orch.providerManager.builtins["cache"]
		delete(orch.providerManager.builtins, "cache")
		defer func() { orch.providerManager.builtins["cache"] = origCache }()

		err := orch.ClearCache(context.Background())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "cache provider not registered") {
			t.Errorf("expected error message to contain 'cache provider not registered', got %q", err.Error())
		}
	})

	t.Run("initialization failure", func(t *testing.T) {
		origCache := orch.providerManager.builtins["cache"]
		orch.providerManager.builtins["cache"] = &failInitProvider{}
		defer func() { orch.providerManager.builtins["cache"] = origCache }()
		delete(orch.providerManager.initializedBuiltins, "cache")

		err := orch.ClearCache(context.Background())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "init failed") {
			t.Errorf("expected error message to contain 'init failed', got %q", err.Error())
		}
	})

	t.Run("invalid cache provider type", func(t *testing.T) {
		origCache := orch.providerManager.builtins["cache"]
		orch.providerManager.builtins["cache"] = provider.NewEnvProvider()
		defer func() { orch.providerManager.builtins["cache"] = origCache }()
		delete(orch.providerManager.initializedBuiltins, "cache")

		err := orch.ClearCache(context.Background())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "invalid cache provider type") {
			t.Errorf("expected error message to contain 'invalid cache provider type', got %q", err.Error())
		}
	})

	t.Run("successful clear cache with mock", func(t *testing.T) {
		origCache := orch.providerManager.builtins["cache"]
		mockCache := &mockCacheProvider{}
		orch.providerManager.builtins["cache"] = mockCache
		defer func() { orch.providerManager.builtins["cache"] = origCache }()
		delete(orch.providerManager.initializedBuiltins, "cache")

		err := orch.ClearCache(context.Background())
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !mockCache.clearCalled {
			t.Errorf("expected mock cache to have clearCalled = true")
		}
	})
}

func TestProviderManagerUnknownSchemeDoesNotAllocateLock(t *testing.T) {
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"known_vault": {
				Provider: "env",
			},
		},
	}
	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	ctx := context.Background()
	_, _, err = orch.providerManager.GetProvider(ctx, "nonexistent_vault")
	if err == nil {
		t.Fatal("expected error for nonexistent vault, got nil")
	}

	orch.providerManager.mu.Lock()
	lockCount := len(orch.providerManager.initLocks)
	orch.providerManager.mu.Unlock()

	if lockCount != 0 {
		t.Errorf("expected 0 initLocks allocated for unknown vault, got %d", lockCount)
	}
}

type countingInitProvider struct {
	initCount atomic.Int64
}

func (p *countingInitProvider) Scheme() string { return "counting" }
func (p *countingInitProvider) Initialize(_ context.Context, _ provider.ProviderConfig) error {
	p.initCount.Add(1)
	return nil
}
func (p *countingInitProvider) Validate(_ map[string]string) error { return nil }
func (p *countingInitProvider) GetSecret(_ context.Context, _ string) (string, error) {
	return "val", nil
}
func (p *countingInitProvider) SetSecret(_ context.Context, _, _ string) error { return nil }
func (p *countingInitProvider) DeleteSecret(_ context.Context, _ string) error { return nil }

func TestProviderManagerConcurrentInitialization(t *testing.T) {
	t.Run("builtin concurrent initialization race", func(t *testing.T) {
		counting := &countingInitProvider{}
		builtins := map[string]provider.SecretProvider{
			"counting": counting,
		}
		pm := NewProviderManager(&config.Config{}, builtins, nil)
		ctx := context.Background()

		const numWorkers = 50
		var wg sync.WaitGroup
		wg.Add(numWorkers)

		for range numWorkers {
			go func() {
				defer wg.Done()
				p, isBuiltin, err := pm.GetProvider(ctx, "counting")
				if err != nil {
					t.Errorf("GetProvider failed: %v", err)
					return
				}
				if !isBuiltin {
					t.Errorf("expected isBuiltin to be true")
				}
				if p != provider.SecretProvider(counting) {
					t.Errorf("expected provider instance %p, got %p", counting, p)
				}
			}()
		}

		wg.Wait()

		if count := counting.initCount.Load(); count != 1 {
			t.Errorf("expected Initialize to be called exactly 1 time, got %d", count)
		}
	})

	t.Run("vault concurrent initialization race", func(t *testing.T) {
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"test_vault": {
					Provider: "env",
				},
			},
		}
		orch, err := NewOrchestrator(cfg)
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}
		ctx := context.Background()

		const numWorkers = 50
		var wg sync.WaitGroup
		wg.Add(numWorkers)
		providers := make([]provider.SecretProvider, numWorkers)

		for i := range numWorkers {
			idx := i
			go func() {
				defer wg.Done()
				p, isBuiltin, err := orch.providerManager.GetProvider(ctx, "test_vault")
				if err != nil {
					t.Errorf("GetProvider failed: %v", err)
					return
				}
				if isBuiltin {
					t.Errorf("expected isBuiltin to be false")
				}
				providers[idx] = p
			}()
		}

		wg.Wait()

		first := providers[0]
		if first == nil {
			t.Fatal("first provider is nil")
		}
		for i, p := range providers {
			if p != first {
				t.Errorf("worker %d got different provider instance (%p vs %p)", i, p, first)
			}
		}
	})
}

func TestProviderManager_IsolatedKeyringVaultInstances(t *testing.T) {
	store := provider.NewMockKeyringStore()
	_ = store.Set("cloakenv", "KEY_ALPHA", "alpha-val")
	_ = store.Set("cloakenv", "KEY_BETA", "beta-val")

	sharedKR := provider.NewOSKeyringProviderWithStore(store)
	cfg := &config.Config{
		Keyring: config.KeyringConfig{Prefix: "cloakenv"},
		Vaults: map[string]config.VaultConfig{
			"kr_alpha": {
				Provider: "keyring",
				Query:    "KEY_ALPHA",
			},
			"kr_beta": {
				Provider: "keyring",
				Query:    "KEY_BETA",
			},
		},
	}
	pm := NewProviderManager(cfg, map[string]provider.SecretProvider{"keyring": sharedKR}, sharedKR)
	ctx := context.Background()

	pAlpha, isBuiltin, err := pm.GetProvider(ctx, "kr_alpha")
	if err != nil || isBuiltin {
		t.Fatalf("GetProvider(kr_alpha) = (%v, %v, %v)", pAlpha, isBuiltin, err)
	}
	pBeta, isBuiltin, err := pm.GetProvider(ctx, "kr_beta")
	if err != nil || isBuiltin {
		t.Fatalf("GetProvider(kr_beta) = (%v, %v, %v)", pBeta, isBuiltin, err)
	}
	pBuiltin, isBuiltin, err := pm.GetProvider(ctx, "keyring")
	if err != nil || !isBuiltin {
		t.Fatalf("GetProvider(keyring) = (%v, %v, %v)", pBuiltin, isBuiltin, err)
	}
	if pAlpha == pBeta || pAlpha == pBuiltin || pBeta == pBuiltin {
		t.Fatalf("expected isolated OSKeyringProvider instances across vaults and builtin")
	}
	entriesB, err := pBeta.(provider.SearchableProvider).Search(ctx, provider.SearchQuery{})
	if err != nil || len(entriesB) != 1 || entriesB[0]["KEY_BETA"] != "beta-val" {
		t.Fatalf("unexpected kr_beta entries: %+v err=%v", entriesB, err)
	}
	entriesA, err := pAlpha.(provider.SearchableProvider).Search(ctx, provider.SearchQuery{})
	if err != nil || len(entriesA) != 1 || entriesA[0]["KEY_ALPHA"] != "alpha-val" {
		t.Fatalf("unexpected kr_alpha entries (may have been overwritten by kr_beta): %+v err=%v", entriesA, err)
	}
}

func TestProviderManagerClearAndPruneCacheOfflineVault(t *testing.T) {
	keyring.MockInit()
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmpHome, ".cache"))

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"offline_kp": {
				Provider:  "keepass",
				VaultPath: filepath.Join(tmpHome, "does-not-exist.kdbx"),
				Cache: config.VaultCacheConfig{
					Enabled: true,
					Mode:    provider.CacheModeEncryptedDisk,
					TTL:     "10m",
				},
			},
		},
	}

	// Pre-populate encrypted disk cache for offline_kp via a mock searchable backend
	// so we can verify: (1) cached Search succeeds lazily without unlocking KeePass,
	// and (2) ClearCache clears the cache without requiring KeePass initialization.
	seedBackend := newTestMemoryProvider(map[string]provider.Entry{
		"prod/db": {"title": "db", "path": "prod/db", "password": "cached-secret"},
	})
	seedCache, err := provider.NewCachingProvider(seedBackend, provider.CachingConfig{
		VaultName:     "offline_kp",
		TTL:           10 * 60 * 1e9,
		Mode:          provider.CacheModeEncryptedDisk,
		KeyringPrefix: cfg.KeyringPrefix(),
	})
	if err != nil {
		t.Fatalf("NewCachingProvider seed failed: %v", err)
	}
	if _, err := seedCache.(provider.SearchableProvider).Search(context.Background(), provider.SearchQuery{}); err != nil {
		t.Fatalf("seed Search failed: %v", err)
	}

	pm := NewProviderManager(cfg, map[string]provider.SecretProvider{"cache": provider.NewCacheProvider()}, nil)

	// 1. Cached Search hit must NOT try to unlock missing KeePass DB.
	p, _, err := pm.GetProvider(context.Background(), "offline_kp")
	if err != nil {
		t.Fatalf("GetProvider for cached vault failed: %v", err)
	}
	entries, err := p.(provider.SearchableProvider).Search(context.Background(), provider.SearchQuery{})
	if err != nil {
		t.Fatalf("cached Search on offline vault failed: %v", err)
	}
	if len(entries) != 1 || entries[0]["password"] != "cached-secret" {
		t.Fatalf("unexpected cached entries: %v", entries)
	}

	// 2. CheckAccess must still detect that the underlying KeePass file is unreachable.
	if err := pm.CheckAccess(context.Background(), "offline_kp"); err == nil {
		t.Fatalf("expected CheckAccess to fail for missing KeePass file")
	}

	// 3. ClearCache must clear the offline vault's cache without initializing KeePass.
	if err := pm.ClearCache(context.Background()); err != nil {
		t.Fatalf("ClearCache failed: %v", err)
	}

	// 4. After ClearCache, Search must now miss cache and fail trying to open missing KeePass DB.
	if _, err := p.(provider.SearchableProvider).Search(context.Background(), provider.SearchQuery{}); err == nil {
		t.Fatalf("expected Search to fail after ClearCache on offline vault")
	}
}
