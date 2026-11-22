//go:build windows
// +build windows

package runner

import (
	"fmt"
	"os"
	"syscall"
)

const fileFlagOpenReparsePoint = 0x00200000

func openFileNoFollowExclusive(path string, perm os.FileMode) (*os.File, error) {
	if _, err := os.Lstat(path); err == nil {
		return nil, fmt.Errorf("path already exists: %w", os.ErrExist)
	}
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm) //nolint:gosec // controlled template target path
}

func openFileWriteNoFollow(path string) (*os.File, error) {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(
		pathPtr,
		syscall.GENERIC_WRITE,
		0,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL|fileFlagOpenReparsePoint,
		0,
	)
	if err != nil {
		return nil, err
	}
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(h, &info); err != nil {
		_ = syscall.CloseHandle(h)
		return nil, err
	}
	if info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = syscall.CloseHandle(h)
		return nil, fmt.Errorf("refusing to follow symlink or reparse point %q", path)
	}
	if info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0 {
		_ = syscall.CloseHandle(h)
		return nil, fmt.Errorf("refusing to write to directory %q", path)
	}
	return os.NewFile(uintptr(h), path), nil
}
