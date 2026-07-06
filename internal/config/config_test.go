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
	if len(cfg.Vaults) != 0 {
		t.Errorf("expected 0 vaults, got %d", len(cfg.Vaults))
	}

	// 2. Test valid YAML file
	yamlContent := `
cache:
  default_ttl: 1h
keyring:
  prefix: test-
vaults:
  keepass:
    provider: keepass
    database_path: ~/secrets.kdbx
    entities_root_key: entries
    searchable: false
    single_entity: false
  custom_static:
    provider: custom_vault
    single_entity: true
    entity_name: "Static Vault"
    tags: [tag1, tag2]
    attributes:
      secret_key: secret_val
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
	vault, ok := cfg.Vaults["keepass"]
	if !ok {
		t.Fatal("expected 'keepass' vault to exist")
	}
	if vault.Provider != "keepass" {
		t.Errorf("expected provider 'keepass', got %q", vault.Provider)
	}

	// Verify expandHome was called on database_path
	expectedPath := filepath.Join(mockHome, "secrets.kdbx")
	if vault.DatabasePath != expectedPath {
		t.Errorf("expected expanded path %q, got %q", expectedPath, vault.DatabasePath)
	}

	if vault.Searchable == nil || *vault.Searchable != false {
		t.Error("expected searchable to be false")
	}

	if vault.SingleEntity == nil || *vault.SingleEntity != false {
		t.Error("expected single_entity to be false")
	}

	// Verify custom static vault parsing
	staticVault, ok := cfg.Vaults["custom_static"]
	if !ok {
		t.Fatal("expected 'custom_static' vault to exist")
	}
	if staticVault.Provider != "custom_vault" {
		t.Errorf("expected provider 'custom_vault', got %q", staticVault.Provider)
	}
	if staticVault.SingleEntity == nil || *staticVault.SingleEntity != true {
		t.Error("expected custom_static single_entity to be true")
	}
	if staticVault.EntityName != "Static Vault" {
		t.Errorf("expected entity_name 'Static Vault', got %q", staticVault.EntityName)
	}
	if len(staticVault.Tags) != 2 || staticVault.Tags[0] != "tag1" || staticVault.Tags[1] != "tag2" {
		t.Errorf("expected tags [tag1, tag2], got %v", staticVault.Tags)
	}
	if val, ok := staticVault.Attributes["secret_key"]; !ok || val != "secret_val" {
		t.Errorf("expected attributes to contain secret_key=secret_val, got %v", staticVault.Attributes)
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
		{"~config", "~config"},
		{"~/", mockHome},
		{"~/.config/test.yaml", filepath.Join(mockHome, ".config", "test.yaml")},
	}

	for _, tt := range tests {
		got := expandHome(tt.input)
		if got != tt.expected {
			t.Errorf("expandHome(%q) = %q, expected %q", tt.input, got, tt.expected)
		}
	}
}
