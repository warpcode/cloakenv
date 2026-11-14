package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
)

func TestDoctorCmd(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)
	t.Setenv("HOME", tmpDir)
	t.Setenv("LocalAppData", tmpDir)

	// Create dummy vault file
	vaultPath := filepath.Join(tmpDir, "vault.kdbx")
	err := os.WriteFile(vaultPath, []byte("dummy"), 0600)
	if err != nil {
		t.Fatalf("Failed to create vault file: %v", err)
	}

	cfg := &config.Config{
		ConfigPath: filepath.Join(tmpDir, "config.yaml"),
		Vaults: map[string]config.VaultConfig{
			"valid_keepass": {
				Provider:  "keepass",
				VaultPath: vaultPath,
			},
			"invalid_keepass": {
				Provider:  "keepass",
				VaultPath: filepath.Join(tmpDir, "does-not-exist.kdbx"),
			},
		},
	}

	var stdout, stderr bytes.Buffer
	exitCode := DoctorCmd([]string{}, cfg, &stdout, &stderr)

	if exitCode != 1 {
		t.Errorf("Expected exit code 1 due to invalid_keepass, got %d", exitCode)
	}

	outStr := stdout.String()
	if !strings.Contains(outStr, "Cloakenv Doctor Diagnostic Report") {
		t.Errorf("Expected report header, got: %s", outStr)
	}

	if !strings.Contains(outStr, "[OK] Vault file exists and is accessible") {
		t.Errorf("Expected OK for valid_keepass, got: %s", outStr)
	}

	if !strings.Contains(outStr, "[ERROR] Vault file not accessible") {
		t.Errorf("Expected ERROR for invalid_keepass, got: %s", outStr)
	}
}

func TestDoctorCmd_Help(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cfg := &config.Config{}

	exitCode := DoctorCmd([]string{"--help"}, cfg, &stdout, &stderr)

	if exitCode != 0 {
		t.Errorf("Expected exit code 0 for help, got %d", exitCode)
	}

	outStr := stdout.String()
	if !strings.Contains(outStr, "Usage:\n  cloakenv doctor [--strict]") {
		t.Errorf("Expected doctor usage output, got: %s", outStr)
	}
	if !strings.Contains(outStr, "--strict") {
		t.Errorf("Expected --strict flag documented in help output, got: %s", outStr)
	}
}

func TestDoctorCmd_ConfigValidationAndStrict(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tempDir)
	t.Setenv("HOME", tempDir)
	t.Setenv("LocalAppData", tempDir)

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

	unknownKeyYAML := `
cache:
  default_ttl: "10m"
unknown_key: "error"
`
	unknownKeyPath := filepath.Join(tempDir, "unknown_key.yaml")
	if err := os.WriteFile(unknownKeyPath, []byte(unknownKeyYAML), 0600); err != nil {
		t.Fatalf("failed to write unknown-key config: %v", err)
	}

	warningYAML := `
vaults:
  empty_search:
    provider: "search"
    query: '"env:prod" in tags'
`
	warningPath := filepath.Join(tempDir, "warning.yaml")
	if err := os.WriteFile(warningPath, []byte(warningYAML), 0600); err != nil {
		t.Fatalf("failed to write warning config: %v", err)
	}

	badSyntaxYAML := "invalid: yaml: :"
	badSyntaxPath := filepath.Join(tempDir, "bad_syntax.yaml")
	if err := os.WriteFile(badSyntaxPath, []byte(badSyntaxYAML), 0600); err != nil {
		t.Fatalf("failed to write bad syntax config: %v", err)
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
			name:     "valid config passes",
			args:     []string{},
			cfgPath:  validPath,
			wantExit: 0,
			wantOut:  "Configuration is valid.",
		},
		{
			name:     "valid config passes in strict mode",
			args:     []string{"--strict"},
			cfgPath:  validPath,
			wantExit: 0,
			wantOut:  "Configuration is valid.",
		},
		{
			name:     "non-strict with unknown key passes",
			args:     []string{},
			cfgPath:  unknownKeyPath,
			wantExit: 0,
			wantOut:  "Configuration is valid.",
		},
		{
			name:       "strict with unknown key fails",
			args:       []string{"--strict"},
			cfgPath:    unknownKeyPath,
			wantExit:   1,
			wantErrOut: "schema validation error",
		},
		{
			name:     "non-strict with validation warning passes",
			args:     []string{},
			cfgPath:  warningPath,
			wantExit: 0,
			wantOut:  "[WARN]",
		},
		{
			name:     "strict with validation warning fails",
			args:     []string{"--strict"},
			cfgPath:  warningPath,
			wantExit: 1,
			wantOut:  "strict mode enabled",
		},
		{
			name:       "invalid yaml syntax fails",
			args:       []string{},
			cfgPath:    badSyntaxPath,
			wantExit:   1,
			wantErrOut: "yaml syntax error",
		},
		{
			name:       "unexpected positional argument fails",
			args:       []string{"foobar"},
			cfgPath:    validPath,
			wantExit:   1,
			wantErrOut: "Unexpected arguments for 'doctor': [foobar]",
		},
		{
			name:       "unknown flag fails",
			args:       []string{"--bogus"},
			cfgPath:    validPath,
			wantExit:   1,
			wantErrOut: "unknown flag: --bogus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := config.Load(tt.cfgPath)
			if err != nil {
				cfg = &config.Config{ConfigPath: tt.cfgPath}
			}
			var stdout, stderr bytes.Buffer
			code := DoctorCmd(tt.args, cfg, &stdout, &stderr)
			if code != tt.wantExit {
				t.Fatalf("DoctorCmd(%v) exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s",
					tt.args, code, tt.wantExit, stdout.String(), stderr.String())
			}
			if tt.wantOut != "" && !strings.Contains(stdout.String(), tt.wantOut) {
				t.Errorf("expected stdout to contain %q, got:\n%s", tt.wantOut, stdout.String())
			}
			if tt.wantErrOut != "" && !strings.Contains(stderr.String(), tt.wantErrOut) {
				t.Errorf("expected stderr to contain %q, got:\n%s", tt.wantErrOut, stderr.String())
			}
		})
	}
}

func TestDoctorCmd_PassProviderDependencyCheck(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tempDir)
	t.Setenv("HOME", tempDir)
	t.Setenv("LocalAppData", tempDir)
	t.Setenv("PATH", t.TempDir())

	storeDir := filepath.Join(tempDir, ".password-store")
	if err := os.MkdirAll(storeDir, 0700); err != nil {
		t.Fatalf("failed to create pass store dir: %v", err)
	}

	cfgPath := filepath.Join(tempDir, "config.yaml")
	cfgYAML := "vaults:\n  pass_store:\n    provider: \"pass\"\n    vault_path: " + filepath.ToSlash(storeDir) + "\n"
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// Non-strict warns when gpg is missing from PATH and returns 0
	var stdout, stderr bytes.Buffer
	if code := DoctorCmd([]string{}, cfg, &stdout, &stderr); code != 0 {
		t.Fatalf("expected exit 0 when gpg missing without --strict, got %d\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "[OK] Password store directory exists and is accessible") {
		t.Errorf("expected OK pass store dir message, got:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "[WARN] 'gpg' binary not found in PATH") {
		t.Errorf("expected WARN for missing gpg binary, got:\n%s", stdout.String())
	}

	// Strict fails on the missing gpg binary warning
	stdout.Reset()
	stderr.Reset()
	if code := DoctorCmd([]string{"--strict"}, cfg, &stdout, &stderr); code != 1 {
		t.Fatalf("expected exit 1 when gpg missing with --strict, got %d\nstdout:\n%s", code, stdout.String())
	}
}
