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

	t.Run("GetCacheFilePath", func(t *testing.T) {
		cp := NewCacheProvider()
		cp.cacheDir = "/tmp/cache"
		path := cp.getCacheFilePath("test_location")
		expected := filepath.Join("/tmp/cache", "ced718b23002817631bd4add6547c4e9a587c699cf0755db57d2b967fc490590")
		if path != expected {
			t.Errorf("getCacheFilePath(%q) = %q, want %q", "test_location", path, expected)
		}
	})
}

func BenchmarkGetCacheFilePath(b *testing.B) {
	cp := NewCacheProvider()
	cp.cacheDir = "/tmp/cache"
	location := "production_db_password_key"

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = cp.getCacheFilePath(location)
	}
}
