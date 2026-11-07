//go:build !windows

package audit

import "os"

func currentUID() int {
	return os.Getuid()
}
