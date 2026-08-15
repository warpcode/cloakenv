//go:build windows
// +build windows

package runner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// RunCommand wraps command execution on Windows using os/exec.Command.
// Windows does not support Unix-like execve/syscall.Exec, so we fall back
// to executing the subprocess and proxying standard descriptors.
func RunCommand(cmdArgs []string, env []string) int {
	if len(cmdArgs) == 0 {
		fmt.Fprintf(os.Stderr, "Command missing\n")
		return 1
	}

	commandName := cmdArgs[0]
	if commandName == "" || commandName == "." || commandName == ".." {
		fmt.Fprintf(os.Stderr, "Invalid command: %q\n", commandName)
		return 1
	}

	binary, err := exec.LookPath(commandName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Command not found: %v\n", err)
		return 1
	}

	absBinary, err := filepath.Abs(binary)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to resolve absolute path: %v\n", err)
		return 1
	}

	cmd := exec.Command(absBinary, cmdArgs[1:]...)
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
