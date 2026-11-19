package runner

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMaterializeTemplates_AllFourModes(t *testing.T) {
	baseDir := t.TempDir()
	explicitEnvPath := filepath.Join(baseDir, "nested", "cfg.yaml")
	pathOnlyFile := filepath.Join(baseDir, "sub", "only.env")
	persistedFile := filepath.Join(baseDir, "keep", "persisted.json")

	deliveries := []TemplateDelivery{
		{
			TemplateName: "raw_env_tpl",
			EnvVar:       "RAW_CONFIG",
			Content:      "raw-secret-content",
		},
		{
			TemplateName: "auto_tmp_tpl",
			EnvPathVar:   "AUTO_CONFIG_PATH",
			Content:      "ephemeral-secret-content",
		},
		{
			TemplateName: "explicit_env_path_tpl",
			EnvPathVar:   "EXPLICIT_CONFIG_PATH",
			Path:         explicitEnvPath,
			Content:      "explicit-env-path-content",
		},
		{
			TemplateName: "path_only_tpl",
			Path:         pathOnlyFile,
			Content:      "path-only-content",
		},
		{
			TemplateName: "persisted_tpl",
			Path:         persistedFile,
			Persist:      true,
			Content:      `{"persisted":true}`,
		},
	}

	envOverrides, cleanup, err := MaterializeTemplates(deliveries)
	if err != nil {
		t.Fatalf("MaterializeTemplates failed: %v", err)
	}
	defer cleanup()

	// Mode 1: raw env content
	if got := envOverrides["RAW_CONFIG"]; got != "raw-secret-content" {
		t.Errorf("RAW_CONFIG = %q, want %q", got, "raw-secret-content")
	}

	// Mode 2: ephemeral file path
	autoPath := envOverrides["AUTO_CONFIG_PATH"]
	if autoPath == "" {
		t.Fatal("AUTO_CONFIG_PATH was not set")
	}
	if !strings.HasPrefix(filepath.Base(autoPath), EphemeralFilePrefix) {
		t.Errorf("ephemeral file %q does not have prefix %q", autoPath, EphemeralFilePrefix)
	}
	data, err := os.ReadFile(autoPath)
	if err != nil {
		t.Fatalf("failed reading ephemeral file %q: %v", autoPath, err)
	}
	if string(data) != "ephemeral-secret-content" {
		t.Errorf("ephemeral content = %q, want %q", string(data), "ephemeral-secret-content")
	}

	// Mode 3: env_path + path
	if got := envOverrides["EXPLICIT_CONFIG_PATH"]; got != explicitEnvPath {
		t.Errorf("EXPLICIT_CONFIG_PATH = %q, want %q", got, explicitEnvPath)
	}
	data, err = os.ReadFile(explicitEnvPath)
	if err != nil || string(data) != "explicit-env-path-content" {
		t.Errorf("explicit file = (%q, %v), want %q", string(data), err, "explicit-env-path-content")
	}

	// Mode 4: path only (no env var injected)
	if len(envOverrides) != 3 {
		t.Errorf("expected 3 env overrides (Modes 1-3), got %d: %v", len(envOverrides), envOverrides)
	}
	data, err = os.ReadFile(pathOnlyFile)
	if err != nil || string(data) != "path-only-content" {
		t.Errorf("path-only file = (%q, %v), want %q", string(data), err, "path-only-content")
	}

	// Verify 0600 file perms and 0700 parent dir perms on Unix
	if runtime.GOOS != "windows" {
		for _, fp := range []string{autoPath, explicitEnvPath, pathOnlyFile, persistedFile} {
			fi, statErr := os.Stat(fp)
			if statErr != nil {
				t.Fatalf("stat(%q) failed: %v", fp, statErr)
			}
			if perm := fi.Mode().Perm(); perm != 0600 {
				t.Errorf("file %q perm = %04o, want 0600", fp, perm)
			}
		}
		for _, dir := range []string{filepath.Dir(explicitEnvPath), filepath.Dir(pathOnlyFile), filepath.Dir(persistedFile)} {
			di, statErr := os.Stat(dir)
			if statErr != nil {
				t.Fatalf("stat dir(%q) failed: %v", dir, statErr)
			}
			if perm := di.Mode().Perm(); perm != 0700 {
				t.Errorf("parent dir %q perm = %04o, want 0700", dir, perm)
			}
		}
	}

	// Invoke cleanup: all non-persistent files must be removed; persistedFile must remain.
	cleanup()
	// Calling cleanup twice must be safe (idempotent).
	cleanup()

	for _, removed := range []string{autoPath, explicitEnvPath, pathOnlyFile} {
		if _, statErr := os.Lstat(removed); !os.IsNotExist(statErr) {
			t.Errorf("expected non-persistent file %q to be removed after cleanup, statErr=%v", removed, statErr)
		}
	}
	if _, statErr := os.Stat(persistedFile); statErr != nil {
		t.Errorf("expected persisted file %q to remain after cleanup, got err: %v", persistedFile, statErr)
	}
}

func TestWriteTemplateFile_SafetyChecks(t *testing.T) {
	baseDir := t.TempDir()

	t.Run("refuses to overwrite pre-existing file", func(t *testing.T) {
		existing := filepath.Join(baseDir, "existing.yaml")
		if err := os.WriteFile(existing, []byte("original"), 0600); err != nil {
			t.Fatalf("setup write failed: %v", err)
		}
		if _, err := WriteTemplateFile(existing, []byte("new")); err == nil {
			t.Fatal("expected error overwriting pre-existing file, got nil")
		}
		got, _ := os.ReadFile(existing)
		if string(got) != "original" {
			t.Errorf("existing file was modified: %q", string(got))
		}
	})

	if runtime.GOOS != "windows" {
		t.Run("refuses target symlink", func(t *testing.T) {
			realTarget := filepath.Join(baseDir, "real_target.yaml")
			linkPath := filepath.Join(baseDir, "link_target.yaml")
			if err := os.Symlink(realTarget, linkPath); err != nil {
				t.Fatalf("symlink failed: %v", err)
			}
			if _, err := WriteTemplateFile(linkPath, []byte("secret")); err == nil {
				t.Fatal("expected error writing through dangling symlink, got nil")
			}
			if _, err := os.Stat(realTarget); !os.IsNotExist(err) {
				t.Errorf("symlink target %q should not have been created", realTarget)
			}
		})

		t.Run("refuses symlink parent directory", func(t *testing.T) {
			realDir := filepath.Join(baseDir, "real_dir")
			if err := os.MkdirAll(realDir, 0700); err != nil {
				t.Fatalf("mkdir failed: %v", err)
			}
			linkDir := filepath.Join(baseDir, "link_dir")
			if err := os.Symlink(realDir, linkDir); err != nil {
				t.Fatalf("symlink dir failed: %v", err)
			}
			if _, err := WriteTemplateFile(filepath.Join(linkDir, "sub", "cfg.yaml"), []byte("secret")); err == nil {
				t.Fatal("expected error writing through symlink parent dir, got nil")
			}
		})
	}

	t.Run("rollback cleans previously materialized files on midway error", func(t *testing.T) {
		first := filepath.Join(baseDir, "first_ok.yaml")
		collision := filepath.Join(baseDir, "already_exists.yaml")
		if err := os.WriteFile(collision, []byte("keep"), 0600); err != nil {
			t.Fatalf("setup failed: %v", err)
		}
		_, _, err := MaterializeTemplates([]TemplateDelivery{
			{TemplateName: "t1", Path: first, Content: "secret1"},
			{TemplateName: "t2", Path: collision, Content: "secret2"},
		})
		if err == nil {
			t.Fatal("expected error on collision, got nil")
		}
		if _, statErr := os.Lstat(first); !os.IsNotExist(statErr) {
			t.Errorf("expected first file %q to be rolled back on error, statErr=%v", first, statErr)
		}
	})
}

func TestSecureZeroAndRemove_OverwritesWithZerosBeforeUnlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hard-link inode inspection after unlink is Unix-specific")
	}
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "secret.txt")
	hardLink := filepath.Join(dir, "inode_observer.txt")

	payload := []byte("top-secret-password-material-1234567890")
	if err := os.WriteFile(secretPath, payload, 0600); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	// Hard-link lets us inspect the underlying file bytes after SecureZeroAndRemove
	// unlinks secretPath, proving the bytes were actually overwritten with 0x00.
	if err := os.Link(secretPath, hardLink); err != nil {
		t.Fatalf("hard link failed: %v", err)
	}

	if err := SecureZeroAndRemove(secretPath); err != nil {
		t.Fatalf("SecureZeroAndRemove failed: %v", err)
	}
	if _, err := os.Lstat(secretPath); !os.IsNotExist(err) {
		t.Fatalf("expected %q to be unlinked, got err=%v", secretPath, err)
	}

	remaining, err := os.ReadFile(hardLink)
	if err != nil {
		t.Fatalf("failed reading observer hard link: %v", err)
	}
	wantZeros := bytes.Repeat([]byte{0x00}, len(payload))
	if !bytes.Equal(remaining, wantZeros) {
		t.Errorf("underlying bytes were not zeroed: got %x, want %x", remaining, wantZeros)
	}
}

func TestFindStaleEphemeralFilesInDirs(t *testing.T) {
	dir := t.TempDir()
	stale1 := filepath.Join(dir, EphemeralFilePrefix+"abc")
	stale2 := filepath.Join(dir, EphemeralFilePrefix+"xyz")
	unrelated := filepath.Join(dir, "other-file.tmp")
	subdir := filepath.Join(dir, EphemeralFilePrefix+"dir")

	for _, p := range []string{stale1, stale2, unrelated} {
		if err := os.WriteFile(p, []byte("x"), 0600); err != nil {
			t.Fatalf("write %q failed: %v", p, err)
		}
	}
	if err := os.MkdirAll(subdir, 0700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	found := FindStaleEphemeralFilesInDirs([]string{dir, dir})
	if len(found) != 2 || found[0] != stale1 || found[1] != stale2 {
		t.Errorf("FindStaleEphemeralFilesInDirs() = %v, want [%s %s]", found, stale1, stale2)
	}
}

func TestWriteTemplateFile_RejectsSymlinkedAncestorWithExistingSubdir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}
	base := t.TempDir()
	realTarget := filepath.Join(base, "real_target")
	existingSub := filepath.Join(realTarget, "existing_subdir")
	if err := os.MkdirAll(existingSub, 0o700); err != nil {
		t.Fatal(err)
	}
	symlinkDir := filepath.Join(base, "symlink_dir")
	if err := os.Symlink(realTarget, symlinkDir); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(symlinkDir, "existing_subdir", "secret.yaml")
	if _, err := WriteTemplateFile(target, []byte("SECRET=123")); err == nil {
		t.Fatalf("expected WriteTemplateFile to refuse writing through symlinked ancestor %q", target)
	}
	if _, err := os.Lstat(filepath.Join(existingSub, "secret.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("secret file was written despite symlinked ancestor")
	}
}

func TestMaterializeTemplates_RejectsEmptyDeliveryTargets(t *testing.T) {
	_, _, err := MaterializeTemplates([]TemplateDelivery{
		{TemplateName: "orphan", Content: "SECRET=1"},
	})
	if err == nil {
		t.Fatal("expected error when env, env_path, and path are all empty")
	}
}
