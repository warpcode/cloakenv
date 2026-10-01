//go:build windows
// +build windows

package runner

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}
	if len(args) == 0 {
		os.Exit(2)
	}

	cmd := args[0]
	switch cmd {
	case "success":
		os.Exit(0)
	case "fail":
		os.Exit(42)
	default:
		os.Exit(1)
	}
}

func TestRunCommand(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("Could not get executable path: %v", err)
	}

	tempDir := t.TempDir()
	batPath := filepath.Join(tempDir, "test.bat")
	cmdPath := filepath.Join(tempDir, "test.cmd")

	if err := os.WriteFile(batPath, []byte(""), 0755); err != nil {
		t.Fatalf("Failed to create dummy bat file: %v", err)
	}
	if err := os.WriteFile(cmdPath, []byte(""), 0755); err != nil {
		t.Fatalf("Failed to create dummy cmd file: %v", err)
	}
	batUpperPath := filepath.Join(tempDir, "test.BAT")
	if err := os.WriteFile(batUpperPath, []byte(""), 0755); err != nil {
		t.Fatalf("Failed to create dummy BAT file: %v", err)
	}

	tests := []struct {
		name       string
		cmdArgs    []string
		wantCode   int
		wantStderr string
	}{
		{
			name:     "success",
			cmdArgs:  []string{exe, "-test.run=TestHelperProcess", "--", "success"},
			wantCode: 0,
		},
		{
			name:     "failure",
			cmdArgs:  []string{exe, "-test.run=TestHelperProcess", "--", "fail"},
			wantCode: 42,
		},
		{
			name:     "not_found",
			cmdArgs:  []string{"this-command-does-not-exist-123456789"},
			wantCode: 1,
		},
		{
			name:       "bat_blocked",
			cmdArgs:    []string{batPath, "hello"},
			wantCode:   1,
			wantStderr: "is blocked due to security risks\n",
		},
		{
			name:       "cmd_blocked",
			cmdArgs:    []string{cmdPath, "hello"},
			wantCode:   1,
			wantStderr: "is blocked due to security risks\n",
		},
		{
			name:       "bat_blocked_uppercase",
			cmdArgs:    []string{batUpperPath, "hello"},
			wantCode:   1,
			wantStderr: "is blocked due to security risks\n",
		},
		{
			name:       "bat_blocked_trailing_dot",
			cmdArgs:    []string{batPath + ".", "hello"},
			wantCode:   1,
			wantStderr: "is blocked due to security risks\n",
		},
		{
			name:       "bat_blocked_trailing_space",
			cmdArgs:    []string{batPath + " ", "hello"},
			wantCode:   1,
			wantStderr: "is blocked due to security risks\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Redirect os.Stderr to capture output
			oldStderr := os.Stderr
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("Failed to create pipe: %v", err)
			}
			os.Stderr = w

			env := append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
			gotCode := RunCommand(tt.cmdArgs, env)

			// Restore os.Stderr and read captured output
			w.Close()
			os.Stderr = oldStderr

			var buf bytes.Buffer
			if _, err := io.Copy(&buf, r); err != nil {
				t.Fatalf("Failed to read from pipe: %v", err)
			}
			gotStderr := buf.String()

			if gotCode != tt.wantCode {
				t.Errorf("RunCommand() = %v, want %v", gotCode, tt.wantCode)
			}

			if tt.wantStderr != "" && !strings.Contains(gotStderr, tt.wantStderr) {
				t.Errorf("RunCommand() stderr = %q, want to contain %q", gotStderr, tt.wantStderr)
			}
		})
	}
}
