package provider

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

func TestCacheProvider(t *testing.T) {
	keyring.MockInit()
	ctx := context.Background()

	t.Run("Scheme", func(t *testing.T) {
		cp := NewCacheProvider()
		if cp.Scheme() != "cache" {
			t.Errorf("expected scheme 'cache', got %q", cp.Scheme())
		}
	})

	t.Run("Validate", func(t *testing.T) {
		cp := NewCacheProvider()

		tests := []struct {
			name     string
			settings map[string]string
		}{
			{
				name:     "nil settings",
				settings: nil,
			},
			{
				name:     "empty settings",
				settings: map[string]string{},
			},
			{
				name: "settings with keyring_prefix",
				settings: map[string]string{
					"keyring_prefix": "custom_prefix",
				},
			},
			{
				name: "settings with arbitrary keys",
				settings: map[string]string{
					"keyring_prefix": "custom_prefix",
					"extra_setting":  "value",
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if err := cp.Validate(tt.settings); err != nil {
					t.Errorf("Validate(%v) unexpected error = %v, want nil", tt.settings, err)
				}
			})
		}
	})

	t.Run("UninitializedBehavior", func(t *testing.T) {
		cp := NewCacheProvider()
		// Get before initialization should fail
		_, err := cp.GetSecret(ctx, "k")
		if err == nil {
			t.Errorf("expected error getting secret before initialization")
		}
		if err := cp.SetSecret(ctx, "k", "v"); err == nil {
			t.Errorf("expected error setting secret before initialization")
		}
	})

	t.Run("Initialize", func(t *testing.T) {
		cp := NewCacheProvider()
		cfg := ProviderConfig{
			Settings: map[string]string{
				"keyring_prefix": "cloakenv_test_prefix_init",
			},
		}
		if err := cp.Initialize(ctx, cfg); err != nil {
			t.Fatalf("Initialize failed: %v", err)
		}
		if cp.cacheDir == "" {
			t.Error("expected cacheDir to be set after Initialize")
		}

		if runtime.GOOS != "windows" {
			t.Run("HardensPreExistingPermissiveDirectory", func(t *testing.T) {
				tempUserCache := t.TempDir()
				t.Setenv("HOME", tempUserCache)
				t.Setenv("XDG_CACHE_HOME", tempUserCache)
				origUserCacheDir := userCacheDir
				userCacheDir = func() (string, error) {
					return tempUserCache, nil
				}
				t.Cleanup(func() {
					userCacheDir = origUserCacheDir
				})

				prefix := "cloakenv_test_permissive_dir"
				dir := filepath.Join(tempUserCache, prefix)
				if err := os.MkdirAll(dir, 0777); err != nil {
					t.Fatalf("failed to create permissive test directory: %v", err)
				}
				if err := os.Chmod(dir, 0777); err != nil {
					t.Fatalf("failed to chmod test directory to 0777: %v", err)
				}

				cpPerm := NewCacheProvider()
				permCfg := ProviderConfig{
					Settings: map[string]string{
						"keyring_prefix": prefix,
					},
				}
				if err := cpPerm.Initialize(ctx, permCfg); err != nil {
					t.Fatalf("Initialize failed: %v", err)
				}

				info, err := os.Stat(dir)
				if err != nil {
					t.Fatalf("failed to stat cache directory: %v", err)
				}
				if perm := info.Mode().Perm(); perm != 0700 {
					t.Errorf("expected hardened permissions 0700, got %o", perm)
				}
			})
		}
	})

	t.Run("SecretLifecycle", func(t *testing.T) {
		cp := NewCacheProvider()
		tempDir := t.TempDir()
		cfg := ProviderConfig{
			Settings: map[string]string{
				"keyring_prefix": "cloakenv_test_prefix_lifecycle",
			},
		}
		if err := cp.Initialize(ctx, cfg); err != nil {
			t.Fatalf("Initialize failed: %v", err)
		}
		cp.cacheDir = tempDir

		location := "test_key"
		secretVal := "secret_val"

		t.Run("GetUnset", func(t *testing.T) {
			_, err := cp.GetSecret(ctx, location)
			if err == nil {
				t.Errorf("expected error getting unset secret")
			}
		})

		t.Run("SetAndGet", func(t *testing.T) {
			if err := cp.SetSecret(ctx, location, secretVal); err != nil {
				t.Fatalf("SetSecret failed: %v", err)
			}

			// Verify permissions on the written cache file
			if runtime.GOOS != "windows" {
				filePath := cp.getCacheFilePath(location)
				info, err := os.Stat(filePath)
				if err != nil {
					t.Fatalf("failed to stat cache file: %v", err)
				}
				if perm := info.Mode().Perm(); perm != 0600 {
					t.Errorf("expected file permissions 0600, got %o", perm)
				}
			}

			// Get secret should succeed
			got, err := cp.GetSecret(ctx, location)
			if err != nil {
				t.Errorf("GetSecret failed: %v", err)
			}
			if got != secretVal {
				t.Errorf("expected %q, got %q", secretVal, got)
			}
		})

		t.Run("Expiration", func(t *testing.T) {
			// Set secret with immediate TTL
			ttlCtx := context.WithValue(ctx, ContextKeyTTL, time.Nanosecond) // Will expire immediately
			if err := cp.SetSecret(ttlCtx, "expire_key", "expire_val"); err != nil {
				t.Fatalf("SetSecret with TTL failed: %v", err)
			}
			// Wait 1ms to ensure expiration
			time.Sleep(time.Millisecond)

			// Get should fail on expired cache
			_, err := cp.GetSecret(ctx, "expire_key")
			if err == nil {
				t.Errorf("expected error getting expired secret")
			}
		})

		t.Run("Delete", func(t *testing.T) {
			// Delete secret
			if err := cp.DeleteSecret(ctx, location); err != nil {
				t.Fatalf("DeleteSecret failed: %v", err)
			}

			// Get deleted should fail
			_, err := cp.GetSecret(ctx, location)
			if err == nil {
				t.Errorf("expected error getting deleted secret")
			}
		})
	})

	t.Run("ClearCache", func(t *testing.T) {
		cp := NewCacheProvider()
		tempDir := t.TempDir()
		cfg := ProviderConfig{
			Settings: map[string]string{
				"keyring_prefix": "cloakenv_test_prefix_clear",
			},
		}
		if err := cp.Initialize(ctx, cfg); err != nil {
			t.Fatalf("Initialize failed: %v", err)
		}
		cp.cacheDir = tempDir

		// Populate again for ClearCache
		if err := cp.SetSecret(ctx, "k1", "v1"); err != nil {
			t.Fatalf("failed to set secret: %v", err)
		}
		if err := cp.SetSecret(ctx, "k2", "v2"); err != nil {
			t.Fatalf("failed to set secret: %v", err)
		}

		if err := cp.ClearCache(); err != nil {
			t.Fatalf("ClearCache failed: %v", err)
		}

		// Verify both are deleted
		files, _ := os.ReadDir(tempDir)
		if len(files) != 0 {
			t.Errorf("expected empty cache directory after ClearCache, got %d files", len(files))
		}
	})

	t.Run("PruneCache", func(t *testing.T) {
		t.Run("Uninitialized", func(t *testing.T) {
			cp := NewCacheProvider()
			_, _, err := cp.PruneCache()
			if err == nil {
				t.Errorf("expected error from uninitialized PruneCache")
			}
		})

		t.Run("PrunesOnlyExpired", func(t *testing.T) {
			cp := NewCacheProvider()
			tempDir := t.TempDir()
			cfg := ProviderConfig{
				Settings: map[string]string{
					"keyring_prefix": "cloakenv_test_prefix_prune",
				},
			}
			if err := cp.Initialize(ctx, cfg); err != nil {
				t.Fatalf("Initialize failed: %v", err)
			}
			cp.cacheDir = tempDir

			// 1. Valid entry with long TTL (1 hour)
			validCtx := context.WithValue(ctx, ContextKeyTTL, 1*time.Hour)
			if err := cp.SetSecret(validCtx, "valid_key", "valid_value"); err != nil {
				t.Fatalf("failed to set valid secret: %v", err)
			}

			// 2. Valid entry with no TTL (infinite)
			if err := cp.SetSecret(ctx, "infinite_key", "infinite_value"); err != nil {
				t.Fatalf("failed to set infinite secret: %v", err)
			}

			// 3. Expired entry (1 nanosecond TTL)
			expiredCtx := context.WithValue(ctx, ContextKeyTTL, 1*time.Nanosecond)
			if err := cp.SetSecret(expiredCtx, "expired_key_1", "expired_val_1"); err != nil {
				t.Fatalf("failed to set expired secret 1: %v", err)
			}
			if err := cp.SetSecret(expiredCtx, "expired_key_2", "expired_val_2"); err != nil {
				t.Fatalf("failed to set expired secret 2: %v", err)
			}

			time.Sleep(10 * time.Millisecond) // Ensure expiration

			count, reclaimed, err := cp.PruneCache()
			if err != nil {
				t.Fatalf("PruneCache failed: %v", err)
			}

			if count != 2 {
				t.Errorf("expected 2 pruned files, got %d", count)
			}
			if reclaimed <= 0 {
				t.Errorf("expected >0 bytes reclaimed, got %d", reclaimed)
			}

			// Valid keys should still be readable
			val1, err := cp.GetSecret(ctx, "valid_key")
			if err != nil || val1 != "valid_value" {
				t.Errorf("valid_key get failed: got %q, err %v", val1, err)
			}

			val2, err := cp.GetSecret(ctx, "infinite_key")
			if err != nil || val2 != "infinite_value" {
				t.Errorf("infinite_key get failed: got %q, err %v", val2, err)
			}

			// Expired keys should not exist in cache files
			files, err := os.ReadDir(tempDir)
			if err != nil {
				t.Fatalf("ReadDir failed: %v", err)
			}
			if len(files) != 2 {
				t.Errorf("expected exactly 2 remaining files in cache dir, got %d", len(files))
			}

			// Second prune should prune 0 files
			count2, reclaimed2, err := cp.PruneCache()
			if err != nil {
				t.Fatalf("second PruneCache failed: %v", err)
			}
			if count2 != 0 || reclaimed2 != 0 {
				t.Errorf("expected 0 files and 0 bytes on second prune, got count=%d, bytes=%d", count2, reclaimed2)
			}
		})
	})
}
