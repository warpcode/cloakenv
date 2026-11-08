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

	if !strings.Contains(stdout.String(), "Usage:") {
		t.Errorf("Expected help output, got: %s", stdout.String())
	}
}
