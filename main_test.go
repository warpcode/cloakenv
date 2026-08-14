package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestHelperProcess runs main() if invoked as a helper process.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_MAIN_PROCESS") != "1" {
		return
	}

	// Read arguments from GO_MAIN_ARGS and construct os.Args
	argsStr := os.Getenv("GO_MAIN_ARGS")
	var args []string
	if argsStr != "" {
		args = strings.Split(argsStr, "\x00")
	}

	os.Args = append([]string{"cloakenv"}, args...)

	main()

	os.Exit(0)
}

func TestMainArgsParsing(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantExit int
		wantOut  string
		wantErr  string
	}{
		{
			name:     "no arguments",
			args:     []string{},
			wantExit: 1,
			wantOut:  "",
			wantErr:  "cloakenv \u2014 pluggable secret orchestrator",
		},
		{
			name:     "help flag",
			args:     []string{"--help"},
			wantExit: 0,
			wantOut:  "cloakenv \u2014 pluggable secret orchestrator",
			wantErr:  "",
		},
		{
			name:     "short help flag",
			args:     []string{"-h"},
			wantExit: 0,
			wantOut:  "cloakenv \u2014 pluggable secret orchestrator",
			wantErr:  "",
		},
		{
			name:     "unknown command",
			args:     []string{"unknown"},
			wantExit: 1,
			wantOut:  "",
			wantErr:  "Unknown command: unknown",
		},
		{
			name:     "config flag no arg",
			args:     []string{"-c"},
			wantExit: 1,
			wantOut:  "",
			wantErr:  "Error: -c flag requires a config path argument",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess")
			cmd.Env = append(os.Environ(), "GO_WANT_MAIN_PROCESS=1")
			if len(tt.args) > 0 {
				cmd.Env = append(cmd.Env, "GO_MAIN_ARGS="+strings.Join(tt.args, "\x00"))
			}

			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()

			var exitCode int
			if err != nil {
				if exitError, ok := err.(*exec.ExitError); ok {
					exitCode = exitError.ExitCode()
				} else {
					t.Fatalf("failed to run command: %v", err)
				}
			} else {
				exitCode = 0
			}

			if exitCode != tt.wantExit {
				t.Errorf("want exit %d, got %d. stderr: %s", tt.wantExit, exitCode, stderr.String())
			}
			if tt.wantOut != "" && !strings.Contains(stdout.String(), tt.wantOut) {
				t.Errorf("want stdout to contain %q, got %q", tt.wantOut, stdout.String())
			}
			if tt.wantErr != "" && !strings.Contains(stderr.String(), tt.wantErr) {
				t.Errorf("want stderr to contain %q, got %q", tt.wantErr, stderr.String())
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	tempDir := t.TempDir()

	validPath := filepath.Join(tempDir, "valid.yaml")
	yamlContent := `
cache:
  default_ttl: 2h
keyring:
  prefix: test-prefix-
`
	if err := os.WriteFile(validPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write temp config file: %v", err)
	}

	invalidPath := filepath.Join(tempDir, "invalid.yaml")
	if err := os.WriteFile(invalidPath, []byte("invalid: yaml: :"), 0644); err != nil {
		t.Fatalf("failed to write invalid yaml file: %v", err)
	}

	nonExistentPath := filepath.Join(tempDir, "does-not-exist.yaml")

	originalConfigPath := customConfigPath
	defer func() { customConfigPath = originalConfigPath }()

	tests := []struct {
		name       string
		configPath string
		wantErr    bool
		wantTTL    string
		wantPrefix string
		skipCheck  bool
	}{
		{
			name:       "Valid custom config path",
			configPath: validPath,
			wantErr:    false,
			wantTTL:    "2h",
			wantPrefix: "test-prefix-",
		},
		{
			name:       "Invalid custom config path",
			configPath: invalidPath,
			wantErr:    true,
		},
		{
			name:       "Non-existent custom config path",
			configPath: nonExistentPath,
			wantErr:    false,
		},
		{
			name:       "Empty custom config path uses default",
			configPath: "",
			skipCheck:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			customConfigPath = tt.configPath
			cfg, err := loadConfig()

			if tt.skipCheck {
				return
			}

			if (err != nil) != tt.wantErr {
				t.Fatalf("loadConfig() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if cfg == nil {
					t.Fatal("expected non-nil config")
				}
				if tt.wantTTL != "" && cfg.Cache.DefaultTTL != tt.wantTTL {
					t.Errorf("expected default_ttl %q, got %q", tt.wantTTL, cfg.Cache.DefaultTTL)
				}
				if tt.wantPrefix != "" && cfg.Keyring.Prefix != tt.wantPrefix {
					t.Errorf("expected prefix %q, got %q", tt.wantPrefix, cfg.Keyring.Prefix)
				}
			}
		})
	}
}
