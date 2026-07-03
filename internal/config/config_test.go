package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	// 1. Test non-existent file
	cfg, err := Load("non-existent-file.yaml")
	if err != nil {
		t.Fatalf("Load failed for non-existent file: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config for non-existent file")
	}
	if len(cfg.Providers) != 0 {
		t.Errorf("expected 0 providers, got %d", len(cfg.Providers))
	}

	// 2. Test valid YAML file
	tempDir, err := os.MkdirTemp("", "cloakenv-test-config")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	yamlContent := `
cache:
  default_ttl: 1h
keyring:
  prefix: test-
providers:
  keepass:
    provider: keepass
    database_path: ~/secrets.kdbx
    entries_key: entries
`
	configPath := filepath.Join(tempDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write temp config file: %v", err)
	}

	cfg, err = Load(configPath)
	if err != nil {
		t.Fatalf("Load failed for valid YAML: %v", err)
	}

	if cfg.Cache.DefaultTTL != "1h" {
		t.Errorf("expected default_ttl '1h', got %q", cfg.Cache.DefaultTTL)
	}
	if cfg.Keyring.Prefix != "test-" {
		t.Errorf("expected prefix 'test-', got %q", cfg.Keyring.Prefix)
	}
	prov, ok := cfg.Providers["keepass"]
	if !ok {
		t.Fatal("expected 'keepass' provider to exist")
	}
	if prov.Provider != "keepass" {
		t.Errorf("expected provider 'keepass', got %q", prov.Provider)
	}

	// Verify expandHome was called on database_path
	home, _ := os.UserHomeDir()
	expectedPath := filepath.Join(home, "secrets.kdbx")
	if prov.DatabasePath != expectedPath {
		t.Errorf("expected expanded path %q, got %q", expectedPath, prov.DatabasePath)
	}

	// 3. Test invalid YAML file
	invalidPath := filepath.Join(tempDir, "invalid.yaml")
	if err := os.WriteFile(invalidPath, []byte("invalid: yaml: :"), 0644); err != nil {
		t.Fatalf("failed to write invalid yaml file: %v", err)
	}
	_, err = Load(invalidPath)
	if err == nil {
		t.Error("expected error for invalid YAML, got nil")
	}
}

func TestDefaultConfigPath(t *testing.T) {
	path, err := DefaultConfigPath()
	if err != nil {
		t.Fatalf("DefaultConfigPath failed: %v", err)
	}
	if !strings.HasSuffix(path, filepath.Join(".config", "cloakenv", "config.yaml")) {
		t.Errorf("unexpected default config path: %q", path)
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("skipping TestExpandHome: could not determine home directory")
	}

	tests := []struct {
		input    string
		expected string
	}{
		{"~/test.txt", filepath.Join(home, "test.txt")},
		{"/abs/path/test.txt", "/abs/path/test.txt"},
		{"rel/path/test.txt", "rel/path/test.txt"},
		{"~", "~"}, // only ~/ is expanded according to implementation
	}

	for _, tt := range tests {
		got := expandHome(tt.input)
		if got != tt.expected {
			t.Errorf("expandHome(%q) = %q, expected %q", tt.input, got, tt.expected)
		}
	}
}
