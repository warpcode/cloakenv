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
	"github.com/warpcode/cloakenv/internal/engine"
)

// TestHelperProcess is a fake command used by TestRunCommandExecution
// to verify that environment variables are passed down and masked on stdout.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "CLOAKENV_TEST") {
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
			args:     []string{"-E", "--"},
			wantExit: 1,
			wantErr:  "No command specified",
		},
		{
			name:     "Removed -e Flag Rejected",
			args:     []string{"-e", "A=B", "--", "echo", "1"},
			wantExit: 1,
			wantErr:  "unknown flag: -e",
		},
		{
			name:     "Removed -m Flag Rejected",
			args:     []string{"-m", "vault://entry", "--", "echo", "1"},
			wantExit: 1,
			wantErr:  "unknown flag: -m",
		},
		{
			name:     "Removed -t Flag Rejected",
			args:     []string{"-t", "file.env", "--", "echo", "1"},
			wantExit: 1,
			wantErr:  "unknown flag: -t",
		},
		{
			name:     "Removed -i Flag Rejected",
			args:     []string{"-i", "KEY", "--", "echo", "1"},
			wantExit: 1,
			wantErr:  "unknown flag: -i",
		},
		{
			name:     "Removed -f Flag Rejected",
			args:     []string{"-f", "env", "--", "echo", "1"},
			wantExit: 1,
			wantErr:  "unknown flag: -f",
		},
		{
			name:     "Removed --key-format Flag Rejected",
			args:     []string{"--key-format", "env", "--", "echo", "1"},
			wantExit: 1,
			wantErr:  "unknown flag: --key-format",
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

			_ = w.Close()
			var buf bytes.Buffer
			if _, err := io.Copy(&buf, r); err != nil {
				t.Errorf("failed to read from pipe: %v", err)
			}
			os.Stderr = oldStderr

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
	if os.Getenv("GO_WANT_RUN_SUBPROCESS") == "1" {
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
							"GO_WANT_HELPER_PROCESS": "1",
							"CLOAKENV_TEST_INJECTED": "injected_from_autoload",
						}),
					},
				},
			},
		}
		cfg.CompileAutoloadRules()

		exitCode := Run(args, cfg)
		os.Exit(exitCode)
	}

	t.Run("Positional arguments after dash dash execute normally", func(t *testing.T) {
		runArgs := []string{
			"--",
			os.Args[0], "-test.run=TestHelperProcess", "--", "CLOAKENV_ARG_POS=passed",
		}
		cmd := exec.Command(os.Args[0], "-test.run=TestRunCommandExecution")
		argsData, _ := json.Marshal(runArgs)
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
		if !strings.Contains(output, "CLOAKENV_ARG_POS=passed") {
			t.Errorf("Expected CLOAKENV_ARG_POS=passed, got:\n%s", output)
		}
	})

	t.Run("Empty environment with -E skips parent environment", func(t *testing.T) {
		t.Setenv("CLOAKENV_TEST_PARENT", "should_be_skipped")
		runArgs := []string{
			"-E",
			"--",
			os.Args[0], "-test.run=TestHelperProcess",
		}
		cmd := exec.Command(os.Args[0], "-test.run=TestRunCommandExecution")
		argsData, _ := json.Marshal(runArgs)
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
		wantHash := sha256.Sum256([]byte("injected_from_autoload"))
		if !strings.Contains(output, "SHA256:CLOAKENV_TEST_INJECTED="+hex.EncodeToString(wantHash[:])) {
			t.Errorf("Expected autoloaded var hash in output:\n%s", output)
		}
		if strings.Contains(output, "CLOAKENV_TEST_PARENT") {
			t.Errorf("Expected parent env variable to be skipped with -E, got:\n%s", output)
		}
	})
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

	t.Run("Autoloads config env for matching command and masks stdout", func(t *testing.T) {
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
	if os.Getenv("GO_WANT_RUN_DRYRUN_SUBPROCESS") == "1" {
		var args []string
		if err := json.Unmarshal([]byte(os.Getenv("RUN_ARGS")), &args); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to unmarshal RUN_ARGS: %v\n", err)
			os.Exit(1)
		}

		cfg := &config.Config{
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

	t.Run("Previews autoloaded transformations without leaking values", func(t *testing.T) {
		runArgs := []string{
			"--dry-run",
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
		if !strings.Contains(output, "[DRY RUN] Command execution preview:") {
			t.Errorf("Expected preview header, got:\n%s", output)
		}
		if !strings.Contains(output, "Command: expanded-tool subcommand --verbose") {
			t.Errorf("Expected transformed command line, got:\n%s", output)
		}
		for _, wantKey := range []string{"CLOAKENV_TEST_USER", "CLOAKENV_TEST_PASS"} {
			if !strings.Contains(output, wantKey) {
				t.Errorf("Expected key %s, got:\n%s", wantKey, output)
			}
		}
		if strings.Contains(output, "secret_admin_user") || strings.Contains(output, "super_secret_password_12345") {
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
		if !strings.Contains(output, "Injected variables: (none)") {
			t.Errorf("Expected 'Injected variables: (none)', got:\n%s", output)
		}
		if !strings.Contains(output, "Command: echo hello world") {
			t.Errorf("Expected 'Command: echo hello world', got:\n%s", output)
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

	tests := []struct {
		name         string
		runArgs      []string
		wantContains string
	}{
		{
			name:         "Short help flag -h before dash-dash",
			runArgs:      []string{"-h", "--", "echo", "hello"},
			wantContains: "cloakenv run",
		},
		{
			name:         "Long help flag --help without dash-dash",
			runArgs:      []string{"--help"},
			wantContains: "cloakenv run",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=TestRun_HelpFlag")
			argsData, _ := json.Marshal(tc.runArgs)
			cmd.Env = append(os.Environ(),
				"GO_WANT_RUN_HELP_SUBPROCESS=1",
				"RUN_ARGS="+string(argsData),
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("Subprocess failed: %v, output: %s", err, string(out))
			}
			if !strings.Contains(string(out), tc.wantContains) {
				t.Errorf("expected output to contain %q, got:\n%s", tc.wantContains, string(out))
			}
		})
	}
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
		t.Setenv("CLOAKENV_DELIVERY_API_KEY", "sk-delivery-super-secret-7777")
		cfg := &config.Config{
			Templates: map[string]config.TemplateConfig{
				"api_cfg": {
					Driver:   "template",
					Template: "token={{ (index .entries 0).API_KEY }}",
					Vaults: []config.VaultSourceConfig{
						{
							Vault: "env",
							Rules: []config.RuleConfig{
								{
									Type:  "match",
									Match: "^CLOAKENV_DELIVERY_API_KEY$",
									Key:   "API_KEY",
								},
							},
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

	if strings.Contains(output, "sk-delivery-super-secret-7777") {
		t.Errorf("SECURITY LEAK: template secret appeared unmasked in output:\n%s", output)
	}
	if !strings.Contains(output, "LEAK_RAW=<REDACTED>") || !strings.Contains(output, "LEAK_AUTO=<REDACTED>") {
		t.Errorf("expected echoed template contents to be masked as <REDACTED>, got:\n%s", output)
	}

	var autoPath string
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "AUTO_PATH=") {
			autoPath = strings.TrimPrefix(line, "AUTO_PATH=")
		}
	}
	if autoPath == "" {
		t.Fatalf("AUTO_PATH not reported in output:\n%s", output)
	}

	for _, p := range []string{autoPath, explicitFile, pathOnlyFile} {
		if _, statErr := os.Lstat(p); !os.IsNotExist(statErr) {
			t.Errorf("expected non-persistent template file %q to be deleted after exit, statErr=%v", p, statErr)
		}
	}

	data, readErr := os.ReadFile(persistedFile)
	if readErr != nil {
		t.Fatalf("expected persisted file %q to remain after exit, err=%v", persistedFile, readErr)
	}
	if string(data) != wantContent {
		t.Errorf("persisted file content = %q, want %q", string(data), wantContent)
	}
}

func TestPrintDryRunPreview_MasksSecretsInTemplatePath(t *testing.T) {
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = origStdout }()

	printDryRunPreview(
		[]string{"app", "--run"},
		[]string{"API_TOKEN"},
		[]engine.RenderedTemplateDelivery{
			{
				TemplateName: "cfg",
				Driver:       "yaml",
				EnvPathVar:   "CFG_PATH",
				Path:         "/tmp/super-secret-token-xyz.yaml",
			},
			{
				TemplateName: "direct",
				Driver:       "dotenv",
				Path:         "/tmp/direct-super-secret-token-xyz.env",
			},
		},
		[]string{"super-secret-token-xyz"},
	)

	_ = w.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()

	if strings.Contains(out, "super-secret-token-xyz") {
		t.Fatalf("dry-run preview leaked secret value in tpl.Path:\n%s", out)
	}
	if !strings.Contains(out, "/tmp/<REDACTED>.yaml") || !strings.Contains(out, "/tmp/direct-<REDACTED>.env") {
		t.Fatalf("expected masked paths in dry-run preview, got:\n%s", out)
	}
}
