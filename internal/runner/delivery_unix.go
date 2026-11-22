//go:build !windows
// +build !windows

package runner

import (
	"os"
	"syscall"
)

func openFileNoFollowExclusive(path string, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, perm) //nolint:gosec // controlled template target path
}

func openFileWriteNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|syscall.O_NOFOLLOW, 0) //nolint:gosec // controlled cleanup target path
}
