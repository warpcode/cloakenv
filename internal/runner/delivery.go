package runner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
)

// EphemeralFilePrefix is the filename prefix used for ephemeral template files
// created in /dev/shm or os.TempDir(), allowing `cloakenv doctor` to detect
// stale files left behind by crashed or SIGKILL'd runs.
const EphemeralFilePrefix = "cloakenv-ephemeral-"

// TemplateDelivery describes a single rendered autoload template to materialize
// into the child environment and/or filesystem.
type TemplateDelivery struct {
	TemplateName string
	EnvVar       string // Mode 1: env: VAR (raw content in memory, no file)
	EnvPathVar   string // Mode 2 & 3: env_path: VAR
	Path         string // Mode 3 & 4: resolved file path (empty for Mode 2 auto temp file)
	Persist      bool   // If true, skip zero-on-exit cleanup for the written file
	Content      string
}

// MaterializeTemplates executes all configured template deliveries:
//   - Mode 1 (EnvVar only): injects raw content directly into envOverrides.
//   - Mode 2 (EnvPathVar only): creates a 0600 ephemeral file in /dev/shm (Linux) or os.TempDir().
//   - Mode 3 (EnvPathVar + Path): creates a 0600 file at Path (0700 parent dirs, no symlinks, no overwrite) and sets EnvPathVar.
//   - Mode 4 (Path only): creates a 0600 file at Path without injecting an env var.
//
// The returned cleanup function is idempotent and zeroes (0x00) plus unlinks every
// non-persistent file created. If any delivery fails, previously created non-persistent
// files are cleaned up immediately before returning the error.
func MaterializeTemplates(deliveries []TemplateDelivery) (map[string]string, func(), error) {
	envOverrides := make(map[string]string)
	var cleanupPaths []string

	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			for i := len(cleanupPaths) - 1; i >= 0; i-- {
				_ = SecureZeroAndRemove(cleanupPaths[i])
			}
		})
	}

	for _, d := range deliveries {
		// Mode 1: raw env var in memory
		if d.EnvVar != "" {
			envOverrides[d.EnvVar] = d.Content
			continue
		}

		var writtenPath string
		var err error
		if d.Path == "" {
			// Mode 2: ephemeral file in /dev/shm (Linux) or os.TempDir()
			writtenPath, err = WriteEphemeralFile([]byte(d.Content))
		} else {
			// Mode 3 & 4: explicit target path
			writtenPath, err = WriteTemplateFile(d.Path, []byte(d.Content))
		}
		if err != nil {
			cleanup()
			return nil, func() {}, fmt.Errorf("failed to materialize template %q: %w", d.TemplateName, err)
		}

		if !d.Persist {
			cleanupPaths = append(cleanupPaths, writtenPath)
		}
		if d.EnvPathVar != "" {
			envOverrides[d.EnvPathVar] = writtenPath
		}
	}

	return envOverrides, cleanup, nil
}

// WriteEphemeralFile writes content to a newly created 0600 temporary file.
// On Linux it prefers /dev/shm when available and writable, falling back to os.TempDir().
func WriteEphemeralFile(content []byte) (string, error) {
	var f *os.File
	var err error
	if runtime.GOOS == "linux" {
		if info, statErr := os.Stat("/dev/shm"); statErr == nil && info.IsDir() {
			f, err = os.CreateTemp("/dev/shm", EphemeralFilePrefix+"*")
		}
	}
	if f == nil {
		f, err = os.CreateTemp("", EphemeralFilePrefix+"*")
	}
	if err != nil {
		return "", fmt.Errorf("failed to create ephemeral file: %w", err)
	}

	absPath, absErr := filepath.Abs(f.Name())
	if absErr != nil {
		absPath = f.Name()
	}

	if err := f.Chmod(0600); err != nil {
		_ = f.Close()
		_ = SecureZeroAndRemove(absPath)
		return "", fmt.Errorf("failed to set 0600 permissions on ephemeral file: %w", err)
	}
	if err := writeAll(f, content); err != nil {
		_ = f.Close()
		_ = SecureZeroAndRemove(absPath)
		return "", fmt.Errorf("failed to write ephemeral file: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = SecureZeroAndRemove(absPath)
		return "", fmt.Errorf("failed to sync ephemeral file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = SecureZeroAndRemove(absPath)
		return "", fmt.Errorf("failed to close ephemeral file: %w", err)
	}
	return absPath, nil
}

// WriteTemplateFile writes content to targetPath with strict filesystem safety:
//   - Rejects empty paths and null bytes.
//   - Refuses to overwrite any pre-existing file or follow symlinks at targetPath or its parent directory.
//   - Creates missing parent directories with 0700 permissions.
//   - Creates the target file with 0600 permissions using exclusive open flags.
func WriteTemplateFile(targetPath string, content []byte) (string, error) {
	if strings.TrimSpace(targetPath) == "" {
		return "", fmt.Errorf("template target path cannot be empty")
	}
	if strings.ContainsRune(targetPath, '\x00') {
		return "", fmt.Errorf("template target path contains null byte")
	}

	expanded := targetPath
	if expanded == "~" || strings.HasPrefix(expanded, "~/") || strings.HasPrefix(expanded, "~\\") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to resolve home directory for template path: %w", err)
		}
		if expanded == "~" {
			expanded = home
		} else {
			expanded = filepath.Join(home, expanded[2:])
		}
	}

	absPath, err := filepath.Abs(filepath.Clean(expanded))
	if err != nil {
		return "", fmt.Errorf("failed to resolve template path %q: %w", targetPath, err)
	}

	// Refuse any pre-existing file, directory, or symlink (including dangling symlinks).
	if info, lstatErr := os.Lstat(absPath); lstatErr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("refusing to write template through symlink %q", absPath)
		}
		return "", fmt.Errorf("refusing to overwrite pre-existing file %q", absPath)
	} else if !errors.Is(lstatErr, os.ErrNotExist) {
		return "", fmt.Errorf("cannot inspect template target path %q: %w", absPath, lstatErr)
	}

	// Verify the immediate parent (and any newly traversed existing parent component)
	// is not a symlink before creating missing directories with 0700.
	parentDir := filepath.Dir(absPath)
	if err := rejectSymlinkParent(parentDir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(parentDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create parent directory for %q: %w", absPath, err)
	}
	// Re-check parentDir after MkdirAll to guard against symlink races.
	if info, lstatErr := os.Lstat(parentDir); lstatErr != nil {
		return "", fmt.Errorf("failed to inspect parent directory %q: %w", parentDir, lstatErr)
	} else if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("refusing to write template through symlink directory %q", parentDir)
	}

	f, err := openFileNoFollowExclusive(absPath, 0600)
	if err != nil {
		return "", fmt.Errorf("failed to create template file %q: %w", absPath, err)
	}

	if err := writeAll(f, content); err != nil {
		_ = f.Close()
		_ = SecureZeroAndRemove(absPath)
		return "", fmt.Errorf("failed to write template file %q: %w", absPath, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = SecureZeroAndRemove(absPath)
		return "", fmt.Errorf("failed to sync template file %q: %w", absPath, err)
	}
	if err := f.Close(); err != nil {
		_ = SecureZeroAndRemove(absPath)
		return "", fmt.Errorf("failed to close template file %q: %w", absPath, err)
	}
	return absPath, nil
}

// rejectSymlinkParent walks upward from dir until it reaches the first existing
// directory and rejects it if it is a symlink. This blocks attacks where a user
// path points through a symlinked directory (e.g., linkdir/sub/secret.yaml)
// without breaking OS-level root symlinks like /var -> /private/var on macOS.
func rejectSymlinkParent(dir string) error {
	cur := filepath.Clean(dir)
	for {
		info, err := os.Lstat(cur)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("refusing to write template through symlink directory %q", cur)
			}
			if !info.IsDir() {
				return fmt.Errorf("parent path %q exists and is not a directory", cur)
			}
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("cannot inspect parent directory %q: %w", cur, err)
		}
		next := filepath.Dir(cur)
		if next == cur {
			return nil
		}
		cur = next
	}
}

// SecureZeroAndRemove overwrites the file at path with zero bytes (0x00), syncs it,
// and unlinks it. It is best-effort against filesystem CoW/journaling and never follows symlinks.
func SecureZeroAndRemove(path string) error {
	if path == "" {
		return nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		// Never follow symlinks when wiping; unlink only if present.
		return os.Remove(path)
	}
	if info.IsDir() {
		return fmt.Errorf("refusing to zero directory %q", path)
	}

	f, openErr := openFileWriteNoFollow(path)
	if openErr == nil {
		size := info.Size()
		if size > 0 {
			zeros := make([]byte, 4096)
			remaining := size
			for remaining > 0 {
				chunk := zeros
				if int64(len(chunk)) > remaining {
					chunk = zeros[:remaining]
				}
				if _, wErr := f.Write(chunk); wErr != nil {
					break
				}
				remaining -= int64(len(chunk))
			}
		}
		_ = f.Sync()
		_ = f.Close()
	}

	if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
		return rmErr
	}
	return nil
}

// FindStaleEphemeralFiles inspects the standard ephemeral directories (/dev/shm
// on Linux and os.TempDir()) for leftover cloakenv ephemeral files from aborted runs.
func FindStaleEphemeralFiles() []string {
	dirs := make([]string, 0, 2)
	if runtime.GOOS == "linux" {
		dirs = append(dirs, "/dev/shm")
	}
	if tmp := os.TempDir(); tmp != "" && (len(dirs) == 0 || dirs[0] != tmp) {
		dirs = append(dirs, tmp)
	}
	return FindStaleEphemeralFilesInDirs(dirs)
}

// FindStaleEphemeralFilesInDirs returns sorted absolute paths of regular files
// matching EphemeralFilePrefix inside the given directories.
func FindStaleEphemeralFilesInDirs(dirs []string) []string {
	seen := make(map[string]struct{})
	var stale []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), EphemeralFilePrefix) {
				continue
			}
			fullPath := filepath.Join(dir, entry.Name())
			if _, ok := seen[fullPath]; ok {
				continue
			}
			seen[fullPath] = struct{}{}
			stale = append(stale, fullPath)
		}
	}
	slices.Sort(stale)
	return stale
}
