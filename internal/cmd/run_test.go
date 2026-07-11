package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
)

// TestHelperProcess is a fake command used by TestRunCommandExecution
// to verify that the environment variables are correctly passed down.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	// Dump specific environment variables to stdout so the parent can verify them
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "CLOAKENV_TEST") || strings.HasPrefix(env, "TEST_TEMPLATE_") || strings.HasPrefix(env, "TEST_LITERAL_") {
			fmt.Println(env)
		}
	}
	os.Exit(0)
}

func TestRun_Errors(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantExit int
		wantErr  string
	}{
		{
			name:     "Help Flag",
			args:     []string{"--help"},
			wantExit: 0,
		},
		{
			name:     "No Command",
			args:     []string{"-e", "A=env://B", "--"},
			wantExit: 1,
			wantErr:  "No command specified",
		},
		{
			name:     "Invalid -e Format",
			args:     []string{"-e", "INVALID_FORMAT", "--", "echo", "1"},
			wantExit: 1,
			wantErr:  "Invalid -e format",
		},
		{
			name:     "Invalid -t Template File",
			args:     []string{"-t", "nonexistent_file.yaml", "--", "echo", "1"},
			wantExit: 1,
			wantErr:  "Error parsing template file",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Capture stderr
			oldStderr := os.Stderr
			defer func() { os.Stderr = oldStderr }()

			r, w, _ := os.Pipe()
			defer r.Close()

			os.Stderr = w

			cfg := &config.Config{
				Vaults: make(map[string]config.VaultConfig),
			}

			exitCode := Run(tc.args, cfg)

			w.Close()
			var buf bytes.Buffer
			if _, err := io.Copy(&buf, r); err != nil {
				t.Fatalf("Failed to read from pipe: %v", err)
			}

			if exitCode != tc.wantExit {
				t.Errorf("Run() exit code = %d, want %d", exitCode, tc.wantExit)
			}

			if tc.wantErr != "" && !strings.Contains(buf.String(), tc.wantErr) {
				t.Errorf("Run() stderr output = %q, want containing %q", buf.String(), tc.wantErr)
			}
		})
	}
}

func TestRunCommandExecution(t *testing.T) {
	// Re-exec the test binary if we are in the subprocess
	if os.Getenv("GO_WANT_RUN_SUBPROCESS") == "1" {
		var args []string
		json.Unmarshal([]byte(os.Getenv("RUN_ARGS")), &args)

		cfg := &config.Config{
			Vaults: make(map[string]config.VaultConfig),
		}

		// Execute Run and exit with its return value
		exitCode := Run(args, cfg)
		os.Exit(exitCode)
	}

	// Create a temporary template file instead of depending on the external one
	tmpFile, err := os.CreateTemp("", "test_template_*.env")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	templateContent := `
TEST_TEMPLATE_A=env://SHOW_TEST_VAR_A
TEST_TEMPLATE_B=env://SHOW_TEST_VAR_B
TEST_LITERAL_VAL=literal_value_here
`
	if _, err := tmpFile.WriteString(templateContent); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	tests := []struct {
		name           string
		envVars        map[string]string // setup in parent before running subprocess
		runArgs        []string          // args for Run()
		expectedOutput []string          // strings expected in stdout
	}{
		{
			name: "Resolve with -e",
			envVars: map[string]string{
				"CLOAKENV_TEST_VAR": "test_val_e",
			},
			runArgs: []string{
				"-e", "CLOAKENV_TEST_INJECTED=env://CLOAKENV_TEST_VAR",
				"--",
				os.Args[0], "-test.run=TestHelperProcess",
			},
			expectedOutput: []string{
				"CLOAKENV_TEST_INJECTED=test_val_e",
			},
		},
		{
			name: "Resolve with -t template",
			envVars: map[string]string{
				"SHOW_TEST_VAR_A": "template_val_a",
				"SHOW_TEST_VAR_B": "template_val_b",
			},
			runArgs: []string{
				"-t", tmpFile.Name(),
				"--",
				os.Args[0], "-test.run=TestHelperProcess",
			},
			expectedOutput: []string{
				"TEST_TEMPLATE_A=template_val_a",
				"TEST_TEMPLATE_B=template_val_b",
				"TEST_LITERAL_VAL=literal_value_here",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.envVars {
				t.Setenv(k, v)
			}

			cmd := exec.Command(os.Args[0], "-test.run=TestRunCommandExecution")
			argsData, _ := json.Marshal(tc.runArgs)
			cmd.Env = append(os.Environ(),
				"GO_WANT_RUN_SUBPROCESS=1",
				"RUN_ARGS="+string(argsData),
				"GO_WANT_HELPER_PROCESS=1",
			)

			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("Subprocess failed: %v, output: %s", err, string(out))
			}

			output := string(out)
			for _, expected := range tc.expectedOutput {
				if !strings.Contains(output, expected) {
					t.Errorf("Expected output to contain %q, but got:\n%s", expected, output)
				}
			}
		})
	}
}
