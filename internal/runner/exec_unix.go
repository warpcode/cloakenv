//go:build !windows
// +build !windows

package runner

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// RunCommand wraps command execution on Unix systems using syscall.Exec.
// This completely replaces the current process with the target subprocess,
// ensuring the child process directly inherits standard input (stdin),
// standard output/error, PID, terminal control, and signal handling.
func RunCommand(cmdArgs []string, env []string) int {
	binary, err := exec.LookPath(cmdArgs[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Command not found: %v\n", err)
		return 1
	}

	err = syscall.Exec(binary, cmdArgs, env)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Execution failed: %v\n", err)
		return 1
	}
	return 0
}
