package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
)

func TestCacheRouting(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		expectedCode   int
		expectedStdout string
		expectedStderr string
	}{
		{
			name:           "help flag without clear",
			args:           []string{"--help"},
			expectedCode:   0,
			expectedStdout: "Manage local encrypted cache",
			expectedStderr: "",
		},
		{
			name:           "no args",
			args:           []string{},
			expectedCode:   1,
			expectedStdout: "",
			expectedStderr: "Usage: cloakenv cache clear",
		},
		{
			name:           "invalid subcommand",
			args:           []string{"invalid"},
			expectedCode:   1,
			expectedStdout: "",
			expectedStderr: "Usage: cloakenv cache clear",
		},
		{
			name:           "clear subcommand with help",
			args:           []string{"clear", "--help"},
			expectedCode:   0,
			expectedStdout: "Clear all entries in the local encrypted cache",
			expectedStderr: "",
		},
		{
			name:           "clear subcommand extra args",
			args:           []string{"clear", "extra"},
			expectedCode:   1,
			expectedStdout: "",
			expectedStderr: "Usage: cloakenv cache clear",
		},
		{
			name:           "clear subcommand valid empty config",
			args:           []string{"clear"},
			expectedCode:   1,
			expectedStdout: "",
			// Should fail because of missing config or key ring init
			expectedStderr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldStdout := os.Stdout
			oldStderr := os.Stderr

			rOut, wOut, _ := os.Pipe()
			rErr, wErr, _ := os.Pipe()

			os.Stdout = wOut
			os.Stderr = wErr

			defer func() {
				os.Stdout = oldStdout
				os.Stderr = oldStderr
			}()

			cfg := &config.Config{}

			exitCode := Cache(tt.args, cfg)

			wOut.Close()
			wErr.Close()

			var bufOut, bufErr bytes.Buffer
			_, errOut := io.Copy(&bufOut, rOut)
			if errOut != nil {
				t.Fatalf("failed to copy stdout: %v", errOut)
			}
			_, errErr := io.Copy(&bufErr, rErr)
			if errErr != nil {
				t.Fatalf("failed to copy stderr: %v", errErr)
			}

			if exitCode != tt.expectedCode {
				t.Errorf("Cache() exit code = %d, want %d", exitCode, tt.expectedCode)
			}

			if tt.expectedStdout != "" && !strings.Contains(bufOut.String(), tt.expectedStdout) {
				t.Errorf("Cache() stdout = %q, want substring %q", bufOut.String(), tt.expectedStdout)
			}

			if tt.expectedStderr != "" && !strings.Contains(bufErr.String(), tt.expectedStderr) {
				t.Errorf("Cache() stderr = %q, want substring %q", bufErr.String(), tt.expectedStderr)
			}
		})
	}
}
