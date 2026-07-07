//go:build windows
// +build windows

package runner

import (
	"fmt"
	"os"
	"os/exec"
)

// RunCommand wraps command execution on Windows using os/exec.Command.
// Windows does not support Unix-like execve/syscall.Exec, so we fall back
// to executing the subprocess and proxying standard descriptors.
func RunCommand(cmdArgs []string, env []string) int {
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "Execution failed: %v\n", err)
		return 1
	}

	return 0
}
