//go:build !windows
// +build !windows

package runner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// RunCommand wraps command execution on Unix systems using syscall.Exec.
// This completely replaces the current process with the target subprocess,
// ensuring the child process directly inherits standard input (stdin),
// standard output/error, PID, terminal control, and signal handling.
func RunCommand(cmdArgs []string, env []string) int {
	if len(cmdArgs) == 0 {
		fmt.Fprintf(os.Stderr, "Command missing\n")
		return 1
	}

	cmd := cmdArgs[0]
	if cmd == "" || cmd == "." || cmd == ".." {
		fmt.Fprintf(os.Stderr, "Invalid command: %q\n", cmd)
		return 1
	}

	binary, err := exec.LookPath(cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Command not found: %v\n", err)
		return 1
	}

	absBinary, err := filepath.Abs(binary)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to resolve absolute path: %v\n", err)
		return 1
	}

	err = syscall.Exec(absBinary, cmdArgs, env)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Execution failed: %v\n", err)
		return 1
	}
	return 0
}
