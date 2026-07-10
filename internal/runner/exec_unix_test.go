//go:build !windows
// +build !windows

package runner

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestRunCommand_Success(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		// In the helper process, we test RunCommand with echo.
		// If syscall.Exec succeeds, this process will become 'echo' and output "hello test",
		// then exit with 0.
		// If it fails, RunCommand will return 1, and we os.Exit with 1.
		args := []string{"echo", "hello test"}
		exitCode := RunCommand(args, os.Environ())
		os.Exit(exitCode)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestRunCommand_Success")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if !strings.Contains(out.String(), "hello test") {
		t.Errorf("Expected output to contain 'hello test', got %q", out.String())
	}
}

func TestRunCommand_NotFound(t *testing.T) {
	var stderr bytes.Buffer

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	exitCode := RunCommand([]string{"this-command-definitely-does-not-exist"}, os.Environ())

	w.Close()
	os.Stderr = oldStderr
	stderr.ReadFrom(r)

	if exitCode != 1 {
		t.Errorf("Expected exit code 1, got %d", exitCode)
	}

	if !strings.Contains(stderr.String(), "Command not found") {
		t.Errorf("Expected stderr to contain 'Command not found', got %q", stderr.String())
	}
}

func TestRunCommand_ExecFailure(t *testing.T) {
	// Create a temporary file that is executable but not a valid executable
	// This will pass exec.LookPath but fail syscall.Exec with ENOEXEC
	tmpFile, err := os.CreateTemp("", "invalid-exec-*")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write some garbage
	tmpFile.WriteString("not a binary")
	tmpFile.Close()

	// Make it executable
	os.Chmod(tmpFile.Name(), 0755)

	var stderr bytes.Buffer
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	exitCode := RunCommand([]string{tmpFile.Name()}, os.Environ())

	w.Close()
	os.Stderr = oldStderr
	stderr.ReadFrom(r)

	if exitCode != 1 {
		t.Errorf("Expected exit code 1, got %d", exitCode)
	}

	if !strings.Contains(stderr.String(), "Execution failed:") {
		t.Errorf("Expected stderr to contain 'Execution failed:', got %q", stderr.String())
	}
}
