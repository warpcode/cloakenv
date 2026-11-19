//go:build windows
// +build windows

package runner

import (
	"fmt"
	"os"
)

func openFileNoFollowExclusive(path string, perm os.FileMode) (*os.File, error) {
	if _, err := os.Lstat(path); err == nil {
		return nil, fmt.Errorf("path already exists: %w", os.ErrExist)
	}
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm) //nolint:gosec // controlled template target path
}

func openFileWriteNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY, 0) //nolint:gosec // controlled cleanup target path
}
