package runner

import (
	"fmt"
	"os"
	"strings"
)

// validateCommand performs platform-agnostic checks on cmdArgs and env.
// It verifies that command args are non-empty, valid, and contain no null bytes.
func validateCommand(cmdArgs []string, env []string) int {
	if len(cmdArgs) == 0 {
		fmt.Fprintf(os.Stderr, "Command missing\n")
		return 1
	}

	commandName := cmdArgs[0]
	if commandName == "" || commandName == "." || commandName == ".." {
		fmt.Fprintf(os.Stderr, "Invalid command: %q\n", commandName)
		return 1
	}

	for i, arg := range cmdArgs {
		if strings.IndexByte(arg, 0) != -1 {
			fmt.Fprintf(os.Stderr, "Invalid argument at index %d: contains null byte\n", i)
			return 1
		}
	}

	for i, e := range env {
		if strings.IndexByte(e, 0) != -1 {
			key, _, _ := strings.Cut(e, "=")
			fmt.Fprintf(os.Stderr, "Invalid environment variable %q at index %d: contains null byte\n", key, i)
			return 1
		}
	}

	return 0
}
