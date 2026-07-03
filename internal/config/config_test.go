package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	tempDir := t.TempDir()
	mockHome := filepath.Join(tempDir, "home")
	if err := os.Mkdir(mockHome, 0755); err != nil {
		t.Fatalf("failed to create mock home: %v", err)
	}

	// Mock userHomeDir for hermetic testing
	oldUserHomeDir := userHomeDir
	userHomeDir = func() (string, error) {
		return mockHome, nil
	}
	defer func() { userHomeDir = oldUserHomeDir }()

	// 1. Test non-existent file
	nonExistentPath := filepath.Join(tempDir, "non-existent-file.yaml")
	cfg, err := Load(nonExistentPath)
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
	expectedPath := filepath.Join(mockHome, "secrets.kdbx")
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
	mockHome := filepath.Join(t.TempDir(), "home")

	oldUserHomeDir := userHomeDir
	userHomeDir = func() (string, error) {
		return mockHome, nil
	}
	defer func() { userHomeDir = oldUserHomeDir }()

	path, err := DefaultConfigPath()
	if err != nil {
		t.Fatalf("DefaultConfigPath failed: %v", err)
	}
	expected := filepath.Join(mockHome, ".config", "cloakenv", "config.yaml")
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestExpandHome(t *testing.T) {
	mockHome := filepath.Join(t.TempDir(), "home")

	oldUserHomeDir := userHomeDir
	userHomeDir = func() (string, error) {
		return mockHome, nil
	}
	defer func() { userHomeDir = oldUserHomeDir }()

	tests := []struct {
		input    string
		expected string
	}{
		{"~/test.txt", filepath.Join(mockHome, "test.txt")},
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
