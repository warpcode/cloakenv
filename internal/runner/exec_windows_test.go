//go:build windows

package runner

import (
	"os"
	"testing"
)

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0) // Ensure we don't run other tests in the helper process

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

	tests := []struct {
		name     string
		cmdArgs  []string
		wantCode int
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
			gotCode := RunCommand(tt.cmdArgs, env)
			if gotCode != tt.wantCode {
				t.Errorf("RunCommand() = %v, want %v", gotCode, tt.wantCode)
			}
		})
	}
}
