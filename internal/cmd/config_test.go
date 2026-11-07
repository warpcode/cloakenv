package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigCmd(t *testing.T) {
	tempDir := t.TempDir()

	validYAML := `
cache:
  default_ttl: "10m"
keyring:
  prefix: "test"
`
	validPath := filepath.Join(tempDir, "config.yaml")
	if err := os.WriteFile(validPath, []byte(validYAML), 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	invalidYAML := `
cache:
  default_ttl: "10m"
unknown_key: "error"
`
	invalidPath := filepath.Join(tempDir, "invalid.yaml")
	if err := os.WriteFile(invalidPath, []byte(invalidYAML), 0600); err != nil {
		t.Fatalf("failed to write invalid config: %v", err)
	}

	tests := []struct {
		name       string
		args       []string
		cfgPath    string
		wantExit   int
		wantOut    string
		wantErrOut string
	}{
		{
			name:     "no args",
			args:     []string{},
			wantExit: 1,
			wantOut:  "cloakenv config <subcommand>",
		},
		{
			name:     "help flag",
			args:     []string{"--help"},
			wantExit: 0,
			wantOut:  "cloakenv config <subcommand>",
		},
		{
			name:     "check valid config",
			args:     []string{"check"},
			cfgPath:  validPath,
			wantExit: 0,
			wantOut:  "Configuration is valid.",
		},
		{
			name:     "check non-strict with unknown key",
			args:     []string{"check"},
			cfgPath:  invalidPath,
			wantExit: 0,
			wantOut:  "Configuration is valid.",
		},
		{
			name:       "check strict with unknown key",
			args:       []string{"check", "--strict"},
			cfgPath:    invalidPath,
			wantExit:   1,
			wantErrOut: "schema validation error",
		},
		{
			name:     "check help",
			args:     []string{"check", "--help"},
			cfgPath:  validPath,
			wantExit: 0,
			wantOut:  "Usage:\n  cloakenv config check [--strict]",
		},
		{
			name:       "unknown subcommand",
			args:       []string{"foobar"},
			cfgPath:    validPath,
			wantExit:   1,
			wantErrOut: "Unknown config subcommand: foobar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := ConfigCmd(tt.args, tt.cfgPath, &stdout, &stderr)
			if code != tt.wantExit {
				t.Fatalf("ConfigCmd(%v) exit code = %d, want %d (stderr: %s)", tt.args, code, tt.wantExit, stderr.String())
			}
			if tt.wantOut != "" && !strings.Contains(stdout.String(), tt.wantOut) {
				t.Errorf("expected stdout to contain %q, got: %s", tt.wantOut, stdout.String())
			}
			if tt.wantErrOut != "" && !strings.Contains(stderr.String(), tt.wantErrOut) {
				t.Errorf("expected stderr to contain %q, got: %s", tt.wantErrOut, stderr.String())
			}
		})
	}
}
