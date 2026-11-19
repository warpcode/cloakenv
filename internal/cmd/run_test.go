package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

	// Dump specific environment variables and their SHA-256 digests so tests can verify
	// both actual child injection (via digest) and stdout masking of raw secret values.
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "CLOAKENV_TEST") || strings.HasPrefix(env, "TEST_TEMPLATE_") || strings.HasPrefix(env, "TEST_LITERAL_") {
			fmt.Println(env)
			if k, v, ok := strings.Cut(env, "="); ok {
				sum := sha256.Sum256([]byte(v))
				fmt.Printf("SHA256:%s=%s\n", k, hex.EncodeToString(sum[:]))
			}
		}
	}
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "CLOAKENV_ARG_") {
			fmt.Println(arg)
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
			args:     []string{"-e", `A={{ get "env://B" }}`, "--"},
			wantExit: 1,
			wantErr:  "No command specified",
		},
		{
			name:     "Invalid Key Format",
			args:     []string{"-f", "camel", "--", "echo", "1"},
			wantExit: 1,
			wantErr:  "invalid key format",
		},
		{
			name:     "Invalid -e Format",
			args:     []string{"-e", "INVALID_FORMAT", "--", "echo", "1"},
			wantExit: 1,
			wantErr:  "invalid -e format",
		},
		{
			name:     "Invalid -t Template File",
			args:     []string{"-t", "nonexistent_file.yaml", "--", "echo", "1"},
			wantExit: 1,
			wantErr:  "error parsing template file",
		},
		{
			name:     "Non-existent Command",
			args:     []string{"--", "this_command_should_not_exist_xyz123"},
			wantExit: 1,
			wantErr:  "this_command_should_not_exist_xyz123",
		},
		{
			name:     "Unknown Flag Before DashDash",
			args:     []string{"--whitelst", "FOO", "--", "echo", "1"},
			wantExit: 1,
			wantErr:  "unknown flag: --whitelst",
		},
		{
			name:     "Unknown Flag --dryrun",
			args:     []string{"--dryrun", "--", "echo", "1"},
			wantExit: 1,
			wantErr:  "unknown flag: --dryrun",
		},
		{
			name:     "Dry Run No Command",
			args:     []string{"--dry-run"},
			wantExit: 1,
			wantErr:  "No command specified",
		},
		{
			name:     "Unexpected Positional Argument Before DashDash",
			args:     []string{"unexpected_arg", "--", "echo", "1"},
			wantExit: 1,
			wantErr:  "unexpected argument: unexpected_arg",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Capture stderr
			oldStderr := os.Stderr
			defer func() { os.Stderr = oldStderr }()

			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("failed to create pipe: %v", err)
			}
			defer func() { _ = r.Close() }()
			defer func() { _ = w.Close() }()

			os.Stderr = w

			cfg := &config.Config{
				Vaults: make(map[string]config.VaultConfig),
			}

			exitCode := Run(tc.args, cfg)

			_ = w.Close() // Close write end so read can finish
			var buf bytes.Buffer
			if _, err := io.Copy(&buf, r); err != nil {
				t.Errorf("failed to read from pipe: %v", err)
			}
			os.Stderr = oldStderr // Restore early so test failure output isn't captured

			if exitCode != tc.wantExit {
				t.Errorf("expected exit code %d, got %d", tc.wantExit, exitCode)
			}

			if tc.wantErr != "" && !strings.Contains(buf.String(), tc.wantErr) {
				t.Errorf("expected stderr to contain %q, got %q", tc.wantErr, buf.String())
			}
		})
	}
}

func TestRunCommandExecution(t *testing.T) {
	// Re-exec the test binary if we are in the subprocess
	if os.Getenv("GO_WANT_RUN_SUBPROCESS") == "1" {
		var args []string
		if err := json.Unmarshal([]byte(os.Getenv("RUN_ARGS")), &args); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to unmarshal RUN_ARGS: %v\n", err)
			os.Exit(1)
		}

		cfg := &config.Config{
			Vaults: make(map[string]config.VaultConfig),
		}

		// Execute Run and exit with its return value
		exitCode := Run(args, cfg)
		os.Exit(exitCode)
	}

	// Create a temporary template file instead of depending on the external one
	tmpFile, err := os.CreateTemp(t.TempDir(), "test_template_*.env")
	if err != nil {
		t.Fatalf("Failed to create temp template file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	templateContent := `
TEST_TEMPLATE_A={{ get "env://SHOW_TEST_VAR_A" }}
TEST_TEMPLATE_B={{ get "env://SHOW_TEST_VAR_B" }}
TEST_LITERAL_VAL=literal_value_here
`
	if _, err := tmpFile.WriteString(templateContent); err != nil {
		t.Fatalf("Failed to write to temp template file: %v", err)
	}
	_ = tmpFile.Close()

	tests := []struct {
		name             string
		envVars          map[string]string
		runArgs          []string
		expectedOutput   []string
		unexpectedOutput []string
	}{
		{
			name: "Direct env var resolution via -e",
			envVars: map[string]string{
				"CLOAKENV_TEST_B": "value_from_env_b",
			},
			runArgs: []string{
				"-e", `CLOAKENV_TEST_A={{ get "env://CLOAKENV_TEST_B" }}`,
				"--",
				os.Args[0], "-test.run=TestHelperProcess",
			},
			expectedOutput: []string{
				"CLOAKENV_TEST_A=value_from_env_b",
			},
		},
		{
			name: "Positional arguments and flags after dash dash execute normally",
			envVars: map[string]string{
				"CLOAKENV_TEST_B": "value_from_env_b",
			},
			runArgs: []string{
				"-e", `CLOAKENV_TEST_A={{ get "env://CLOAKENV_TEST_B" }}`,
				"--",
				os.Args[0], "-test.run=TestHelperProcess", "--", "CLOAKENV_ARG_POS=passed",
			},
			expectedOutput: []string{
				"CLOAKENV_TEST_A=value_from_env_b",
				"CLOAKENV_ARG_POS=passed",
			},
		},
		{
			name: "Template resolution via -t",
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
		{
			name: "Empty environment with -E skips parent environment",
			envVars: map[string]string{
				"CLOAKENV_TEST_PARENT": "should_be_skipped",
			},
			runArgs: []string{
				"-E",
				"-e", "GO_WANT_HELPER_PROCESS=1",
				"-e", "CLOAKENV_TEST_EXPLICIT=explicit_val",
				"--",
				os.Args[0], "-test.run=TestHelperProcess",
			},
			expectedOutput: []string{
				"CLOAKENV_TEST_EXPLICIT=explicit_val",
			},
			unexpectedOutput: []string{
				"CLOAKENV_TEST_PARENT",
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
			for _, unexpected := range tc.unexpectedOutput {
				if strings.Contains(output, unexpected) {
					t.Errorf("Expected output NOT to contain %q, but got:\n%s", unexpected, output)
				}
			}
		})
	}
}

func TestRun_Autoload(t *testing.T) {
	if os.Getenv("GO_WANT_RUN_AUTOLOAD_SUBPROCESS") == "1" {
		var args []string
		if err := json.Unmarshal([]byte(os.Getenv("RUN_ARGS")), &args); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to unmarshal RUN_ARGS: %v\n", err)
			os.Exit(1)
		}

		cfg := &config.Config{
			Autoload: []config.AutoloadRule{
				{
					Match: "*TestHelperProcess*",
					Envs: []config.AutoloadEnvItem{
						config.InlineEnvItem(map[string]string{
							"CLOAKENV_TEST_AUTOLOAD_VAR": "autoloaded_secret_value",
						}),
					},
				},
			},
		}

		exitCode := Run(args, cfg)
		os.Exit(exitCode)
	}

	if os.Getenv("GO_WANT_RUN_REGEX_SUBPROCESS") == "1" {
		var args []string
		if err := json.Unmarshal([]byte(os.Getenv("RUN_ARGS")), &args); err != nil {
			os.Exit(1)
		}
		cfg := &config.Config{
			Autoload: []config.AutoloadRule{
				{
					Match:   `^my-alias\s+(.*)$`,
					Command: fmt.Sprintf("%q $1", os.Args[0]),
					Envs: []config.AutoloadEnvItem{
						config.InlineEnvItem(map[string]string{
							"CLOAKENV_TEST_REGEX_SECRET": "transformed_secret_val",
						}),
					},
				},
			},
		}
		cfg.CompileAutoloadRules()
		os.Exit(Run(args, cfg))
	}

	t.Run("Autoloads config vault for matching command", func(t *testing.T) {
		runArgs := []string{
			"--",
			os.Args[0], "-test.run=TestHelperProcess",
		}
		cmd := exec.Command(os.Args[0], "-test.run=TestRun_Autoload")
		argsData, _ := json.Marshal(runArgs)
		cmd.Env = append(os.Environ(),
			"GO_WANT_RUN_AUTOLOAD_SUBPROCESS=1",
			"RUN_ARGS="+string(argsData),
			"GO_WANT_HELPER_PROCESS=1",
		)

		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Subprocess failed: %v, output: %s", err, string(out))
		}

		output := string(out)
		wantHash := sha256.Sum256([]byte("autoloaded_secret_value"))
		expectedDigest := "SHA256:CLOAKENV_TEST_AUTOLOAD_VAR=" + hex.EncodeToString(wantHash[:])
		if !strings.Contains(output, expectedDigest) {
			t.Errorf("Expected child to receive real secret value (digest %q), got:\n%s", expectedDigest, output)
		}
		if !strings.Contains(output, "CLOAKENV_TEST_AUTOLOAD_VAR=<REDACTED>") {
			t.Errorf("Expected echoed autoload secret to be masked to <REDACTED>, got:\n%s", output)
		}
		if strings.Contains(output, "autoloaded_secret_value") {
			t.Errorf("SECURITY LEAK: raw autoload secret appeared in stdout:\n%s", output)
		}
	})

	t.Run("Disables autoload when --no-autoload flag is passed", func(t *testing.T) {
		runArgs := []string{
			"--no-autoload",
			"--",
			os.Args[0], "-test.run=TestHelperProcess",
		}
		cmd := exec.Command(os.Args[0], "-test.run=TestRun_Autoload")
		argsData, _ := json.Marshal(runArgs)
		cmd.Env = append(os.Environ(),
			"GO_WANT_RUN_AUTOLOAD_SUBPROCESS=1",
			"RUN_ARGS="+string(argsData),
			"GO_WANT_HELPER_PROCESS=1",
		)

		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Subprocess failed: %v, output: %s", err, string(out))
		}

		output := string(out)
		if strings.Contains(output, "CLOAKENV_TEST_AUTOLOAD_VAR=") {
			t.Errorf("Expected output NOT to contain CLOAKENV_TEST_AUTOLOAD_VAR with --no-autoload flag, but got:\n%s", output)
		}
	})

	t.Run("Disables autoload when deprecated hidden alias --skip-autoload flag is passed", func(t *testing.T) {
		runArgs := []string{
			"--skip-autoload",
			"--",
			os.Args[0], "-test.run=TestHelperProcess",
		}
		cmd := exec.Command(os.Args[0], "-test.run=TestRun_Autoload")
		argsData, _ := json.Marshal(runArgs)
		cmd.Env = append(os.Environ(),
			"GO_WANT_RUN_AUTOLOAD_SUBPROCESS=1",
			"RUN_ARGS="+string(argsData),
			"GO_WANT_HELPER_PROCESS=1",
		)

		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Subprocess failed: %v, output: %s", err, string(out))
		}

		output := string(out)
		if strings.Contains(output, "CLOAKENV_TEST_AUTOLOAD_VAR=") {
			t.Errorf("Expected output NOT to contain CLOAKENV_TEST_AUTOLOAD_VAR with --skip-autoload flag, but got:\n%s", output)
		}
	})

	t.Run("Autoloads regex match and transforms command", func(t *testing.T) {
		runArgs := []string{
			"--",
			"my-alias", "-test.run=TestHelperProcess",
		}
		cmd := exec.Command(os.Args[0], "-test.run=TestRun_Autoload")
		argsData, _ := json.Marshal(runArgs)
		cmd.Env = append(os.Environ(),
			"GO_WANT_RUN_REGEX_SUBPROCESS=1",
			"RUN_ARGS="+string(argsData),
			"GO_WANT_HELPER_PROCESS=1",
		)

		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Subprocess failed: %v, output: %s", err, string(out))
		}

		output := string(out)
		wantHash := sha256.Sum256([]byte("transformed_secret_val"))
		expectedDigest := "SHA256:CLOAKENV_TEST_REGEX_SECRET=" + hex.EncodeToString(wantHash[:])
		if !strings.Contains(output, expectedDigest) {
			t.Errorf("Expected child to receive real secret value (digest %q), got:\n%s", expectedDigest, output)
		}
		if !strings.Contains(output, "CLOAKENV_TEST_REGEX_SECRET=<REDACTED>") {
			t.Errorf("Expected echoed regex autoload secret to be masked to <REDACTED>, got:\n%s", output)
		}
		if strings.Contains(output, "transformed_secret_val") {
			t.Errorf("SECURITY LEAK: raw regex autoload secret appeared in stdout:\n%s", output)
		}
	})
}

func TestRun_DryRun(t *testing.T) {
	// Helper subprocess handler for dry-run tests
	if os.Getenv("GO_WANT_RUN_DRYRUN_SUBPROCESS") == "1" {
		var args []string
		if err := json.Unmarshal([]byte(os.Getenv("RUN_ARGS")), &args); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to unmarshal RUN_ARGS: %v\n", err)
			os.Exit(1)
		}

		dbPath := writeTestKDBX(t, "merge_vault", "testpass", testKDBXEntry{
			Title: "entry",
			Attributes: map[string]string{
				"CLOAKENV_TEST_ALLOWED": "secret_allowed_val",
				"CLOAKENV_TEST_FILTER":  "secret_filter_val",
			},
		})
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"merge_vault": {
					Provider:  "keepass",
					VaultPath: dbPath,
				},
			},
			Autoload: []config.AutoloadRule{
				{
					Match:   `^my-alias\s+(.*)$`,
					Command: "expanded-tool $1",
					Envs: []config.AutoloadEnvItem{
						config.InlineEnvItem(map[string]string{
							"CLOAKENV_TEST_USER": "secret_admin_user",
							"CLOAKENV_TEST_PASS": "super_secret_password_12345",
						}),
					},
				},
			},
		}
		cfg.CompileAutoloadRules()

		exitCode := Run(args, cfg)
		os.Exit(exitCode)
	}

	t.Run("Previews explicit mappings and command without executing", func(t *testing.T) {
		t.Setenv("CLOAKENV_SRC_VAR", "super_secret_value_xyz")

		runArgs := []string{
			"--dry-run",
			"-e", `CLOAKENV_TARGET={{ get "env://CLOAKENV_SRC_VAR" }}`,
			"--",
			os.Args[0], "-test.run=TestHelperProcess",
		}

		cmd := exec.Command(os.Args[0], "-test.run=TestRun_DryRun")
		argsData, _ := json.Marshal(runArgs)
		cmd.Env = append(os.Environ(),
			"GO_WANT_RUN_DRYRUN_SUBPROCESS=1",
			"RUN_ARGS="+string(argsData),
			"GO_WANT_HELPER_PROCESS=1",
		)

		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Subprocess failed: %v, output: %s", err, string(out))
		}

		output := string(out)

		// 1. Must display dry-run preview header and command line
		if !strings.Contains(output, "[DRY RUN] Command execution preview:") {
			t.Errorf("Expected preview header, got:\n%s", output)
		}
		expectedCmd := "Command: " + os.Args[0] + " -test.run=TestHelperProcess"
		if !strings.Contains(output, expectedCmd) {
			t.Errorf("Expected command line %q, got:\n%s", expectedCmd, output)
		}

		// 2. Must display injected variable names
		if !strings.Contains(output, "Injected variables:") {
			t.Errorf("Expected 'Injected variables:' section, got:\n%s", output)
		}
		if !strings.Contains(output, "CLOAKENV_TARGET") {
			t.Errorf("Expected injected variable name CLOAKENV_TARGET, got:\n%s", output)
		}

		// 3. Must NEVER execute the target process or leak secret values
		if strings.Contains(output, "super_secret_value_xyz") {
			t.Errorf("SECURITY LEAK: secret value was displayed in dry-run output:\n%s", output)
		}
		if strings.Contains(output, "CLOAKENV_TARGET=super_secret_value_xyz") {
			t.Errorf("Target process was executed despite --dry-run:\n%s", output)
		}
	})

	t.Run("Previews autoloaded transformations and merges with whitelist", func(t *testing.T) {
		runArgs := []string{
			"--dry-run",
			"-m", "merge_vault://entry",
			"-i", "CLOAKENV_TEST_ALLOWED",
			"--",
			"my-alias", "subcommand", "--verbose",
		}

		cmd := exec.Command(os.Args[0], "-test.run=TestRun_DryRun")
		argsData, _ := json.Marshal(runArgs)
		cmd.Env = append(os.Environ(),
			"GO_WANT_RUN_DRYRUN_SUBPROCESS=1",
			"RUN_ARGS="+string(argsData),
			"GO_WANT_HELPER_PROCESS=1",
		)

		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Subprocess failed: %v, output: %s", err, string(out))
		}

		output := string(out)

		// Transformed command line from autoload rule
		if !strings.Contains(output, "Command: expanded-tool subcommand --verbose") {
			t.Errorf("Expected transformed command line, got:\n%s", output)
		}

		// Autoload envs and whitelisted merge key must be shown
		for _, wantKey := range []string{"CLOAKENV_TEST_USER", "CLOAKENV_TEST_PASS", "CLOAKENV_TEST_ALLOWED"} {
			if !strings.Contains(output, wantKey) {
				t.Errorf("Expected key %s, got:\n%s", wantKey, output)
			}
		}

		// Filtered merge key must NOT be shown
		if strings.Contains(output, "CLOAKENV_TEST_FILTER") {
			t.Errorf("Expected CLOAKENV_TEST_FILTER to be filtered out by -i, but found in:\n%s", output)
		}

		// Secret values must NEVER be displayed
		if strings.Contains(output, "secret_admin_user") || strings.Contains(output, "super_secret_password_12345") || strings.Contains(output, "secret_allowed_val") || strings.Contains(output, "secret_filter_val") {
			t.Errorf("SECURITY LEAK: secret values displayed in dry-run output:\n%s", output)
		}
	})

	t.Run("Handles no injected variables", func(t *testing.T) {
		runArgs := []string{
			"--dry-run",
			"--",
			"echo", "hello", "world",
		}

		cmd := exec.Command(os.Args[0], "-test.run=TestRun_DryRun")
		argsData, _ := json.Marshal(runArgs)
		cmd.Env = append(os.Environ(),
			"GO_WANT_RUN_DRYRUN_SUBPROCESS=1",
			"RUN_ARGS="+string(argsData),
		)

		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Subprocess failed: %v, output: %s", err, string(out))
		}

		output := string(out)
		if !strings.Contains(output, "Command: echo hello world") {
			t.Errorf("Expected 'Command: echo hello world', got:\n%s", output)
		}
		if !strings.Contains(output, "Injected variables: (none)") {
			t.Errorf("Expected 'Injected variables: (none)', got:\n%s", output)
		}
	})

	t.Run("Handles positional command without dash-dash separator", func(t *testing.T) {
		runArgs := []string{
			"--dry-run",
			"echo", "plain_arg",
		}

		cmd := exec.Command(os.Args[0], "-test.run=TestRun_DryRun")
		argsData, _ := json.Marshal(runArgs)
		cmd.Env = append(os.Environ(),
			"GO_WANT_RUN_DRYRUN_SUBPROCESS=1",
			"RUN_ARGS="+string(argsData),
		)

		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Subprocess failed: %v, output: %s", err, string(out))
		}

		output := string(out)
		if !strings.Contains(output, "Command: echo plain_arg") {
			t.Errorf("Expected 'Command: echo plain_arg', got:\n%s", output)
		}
	})
}

func TestRun_HelpFlag(t *testing.T) {
	if os.Getenv("GO_WANT_RUN_HELP_SUBPROCESS") == "1" {
		var args []string
		if err := json.Unmarshal([]byte(os.Getenv("RUN_ARGS")), &args); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to unmarshal RUN_ARGS: %v\n", err)
			os.Exit(1)
		}

		cfg := &config.Config{}
		exitCode := Run(args, cfg)
		os.Exit(exitCode)
	}

	t.Run("Does not hijack child command help flags when -- is absent", func(t *testing.T) {
		runArgs := []string{
			"--dry-run", "echo", "--help",
		}

		cmd := exec.Command(os.Args[0], "-test.run=TestRun_HelpFlag")
		argsData, _ := json.Marshal(runArgs)
		cmd.Env = append(os.Environ(),
			"GO_WANT_RUN_HELP_SUBPROCESS=1",
			"RUN_ARGS="+string(argsData),
		)

		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Subprocess failed: %v, output: %s", err, string(out))
		}

		output := string(out)
		if !strings.Contains(output, "Command: echo --help") {
			t.Errorf("Expected 'Command: echo --help', got:\n%s", output)
		}
	})
}

func TestRun_TemplateDeliveryAndMasking(t *testing.T) {
	if os.Getenv("GO_WANT_DELIVERY_CHILD") == "1" && os.Getenv("GO_WANT_DELIVERY_RUNNER") != "1" {
		rawCfg := os.Getenv("CLOAKENV_RAW_CFG")
		autoPath := os.Getenv("CLOAKENV_AUTO_PATH")
		explicitPath := os.Getenv("CLOAKENV_EXPLICIT_PATH")
		pathOnly := os.Getenv("CLOAKENV_EXPECT_PATH_ONLY")
		persistPath := os.Getenv("CLOAKENV_EXPECT_PERSIST_PATH")

		sumRaw := sha256.Sum256([]byte(rawCfg))
		fmt.Printf("HASH_RAW=%s\n", hex.EncodeToString(sumRaw[:]))
		fmt.Printf("LEAK_RAW=%s\n", rawCfg)

		autoBytes, err := os.ReadFile(autoPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERR_READ_AUTO=%v\n", err)
			os.Exit(2)
		}
		sumAuto := sha256.Sum256(autoBytes)
		fmt.Printf("AUTO_PATH=%s\n", autoPath)
		fmt.Printf("HASH_AUTO=%s\n", hex.EncodeToString(sumAuto[:]))
		fmt.Printf("LEAK_AUTO=%s\n", string(autoBytes))

		explicitBytes, err := os.ReadFile(explicitPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERR_READ_EXPLICIT=%v\n", err)
			os.Exit(3)
		}
		sumExp := sha256.Sum256(explicitBytes)
		fmt.Printf("EXPLICIT_PATH=%s\n", explicitPath)
		fmt.Printf("HASH_EXPLICIT=%s\n", hex.EncodeToString(sumExp[:]))

		pathOnlyBytes, err := os.ReadFile(pathOnly)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERR_READ_PATH_ONLY=%v\n", err)
			os.Exit(4)
		}
		sumPO := sha256.Sum256(pathOnlyBytes)
		fmt.Printf("HASH_PATH_ONLY=%s\n", hex.EncodeToString(sumPO[:]))

		persistBytes, err := os.ReadFile(persistPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERR_READ_PERSIST=%v\n", err)
			os.Exit(5)
		}
		sumPer := sha256.Sum256(persistBytes)
		fmt.Printf("HASH_PERSIST=%s\n", hex.EncodeToString(sumPer[:]))
		os.Exit(0)
	}

	if os.Getenv("GO_WANT_DELIVERY_RUNNER") == "1" {
		_ = os.Unsetenv("GO_WANT_DELIVERY_RUNNER")
		baseDir := os.Getenv("CLOAKENV_DELIVERY_DIR")
		dbPath := writeTestKDBX(t, "secrets", "testpass", testKDBXEntry{
			Title: "API",
			Attributes: map[string]string{
				"API_KEY": "sk-delivery-super-secret-7777",
			},
		})
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"secrets": {
					Provider:  "keepass",
					VaultPath: dbPath,
				},
			},
			Templates: map[string]config.TemplateConfig{
				"api_cfg": {
					Driver:   "template",
					Template: "token={{ (index .entries 0).API_KEY }}",
					Vaults: []config.VaultSourceConfig{
						{
							Vault: "secrets",
							Rules: []config.RuleConfig{{Type: "pass"}},
						},
					},
				},
			},
			Autoload: []config.AutoloadRule{
				{
					Match: "*TestRun_TemplateDeliveryAndMasking*",
					Envs: []config.AutoloadEnvItem{
						config.InlineEnvItem(map[string]string{
							"APP_ENV": "staging",
						}),
					},
					Templates: []config.AutoloadTemplateConfig{
						{
							Template: "api_cfg",
							Env:      "CLOAKENV_RAW_CFG",
						},
						{
							Template: "api_cfg",
							EnvPath:  "CLOAKENV_AUTO_PATH",
						},
						{
							Template: "api_cfg",
							EnvPath:  "CLOAKENV_EXPLICIT_PATH",
							Path:     filepath.Join(baseDir, "explicit", "{{ .APP_ENV }}.cfg"),
						},
						{
							Template: "api_cfg",
							Path:     filepath.Join(baseDir, "pathonly", "{{ .APP_ENV }}.cfg"),
						},
						{
							Template: "api_cfg",
							Path:     filepath.Join(baseDir, "persisted", "{{ .APP_ENV }}.cfg"),
							Persist:  true,
						},
					},
				},
			},
		}
		cfg.CompileAutoloadRules()

		exitCode := Run([]string{
			"--",
			os.Args[0], "-test.run=^TestRun_TemplateDeliveryAndMasking$",
		}, cfg)
		os.Exit(exitCode)
	}

	baseDir := t.TempDir()
	explicitFile := filepath.Join(baseDir, "explicit", "staging.cfg")
	pathOnlyFile := filepath.Join(baseDir, "pathonly", "staging.cfg")
	persistedFile := filepath.Join(baseDir, "persisted", "staging.cfg")

	cmd := exec.Command(os.Args[0], "-test.run=^TestRun_TemplateDeliveryAndMasking$")
	cmd.Env = append(os.Environ(),
		"GO_WANT_DELIVERY_RUNNER=1",
		"GO_WANT_DELIVERY_CHILD=1",
		"CLOAKENV_DELIVERY_DIR="+baseDir,
		"CLOAKENV_EXPECT_PATH_ONLY="+pathOnlyFile,
		"CLOAKENV_EXPECT_PERSIST_PATH="+persistedFile,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("delivery runner failed: %v\noutput:\n%s", err, string(out))
	}

	output := string(out)
	wantContent := "token=sk-delivery-super-secret-7777"
	wantHash := sha256.Sum256([]byte(wantContent))
	wantHex := hex.EncodeToString(wantHash[:])

	for _, label := range []string{"HASH_RAW", "HASH_AUTO", "HASH_EXPLICIT", "HASH_PATH_ONLY", "HASH_PERSIST"} {
		wantLine := label + "=" + wantHex
		if !strings.Contains(output, wantLine) {
			t.Errorf("expected %q in output, got:\n%s", wantLine, output)
		}
	}

	// Secret and full rendered content must be masked when echoed by child.
	if strings.Contains(output, "sk-delivery-super-secret-7777") {
		t.Errorf("SECURITY LEAK: template secret appeared unmasked in output:\n%s", output)
	}
	if !strings.Contains(output, "LEAK_RAW=<REDACTED>") || !strings.Contains(output, "LEAK_AUTO=<REDACTED>") {
		t.Errorf("expected echoed template contents to be masked as <REDACTED>, got:\n%s", output)
	}

	// Extract AUTO_PATH to verify ephemeral file was unlinked on exit.
	var autoPath string
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "AUTO_PATH=") {
			autoPath = strings.TrimPrefix(line, "AUTO_PATH=")
		}
	}
	if autoPath == "" {
		t.Fatalf("AUTO_PATH not reported in output:\n%s", output)
	}

	// Non-persistent files must be zeroed and unlinked after exit.
	for _, p := range []string{autoPath, explicitFile, pathOnlyFile} {
		if _, statErr := os.Lstat(p); !os.IsNotExist(statErr) {
			t.Errorf("expected non-persistent template file %q to be deleted after exit, statErr=%v", p, statErr)
		}
	}

	// Persisted file must remain on disk with the expected content.
	data, readErr := os.ReadFile(persistedFile)
	if readErr != nil {
		t.Fatalf("expected persisted file %q to remain after exit, err=%v", persistedFile, readErr)
	}
	if string(data) != wantContent {
		t.Errorf("persisted file content = %q, want %q", string(data), wantContent)
	}
}
