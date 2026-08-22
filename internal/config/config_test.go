package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
    vault_path: ~/secrets.kdbx
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
autoload:
  - match: "aws"
    vaults:
      - "keepass"
    merge:
      - "custom_static"
    env:
      AWS_REGION: "env://REGION"
    whitelist:
      - "AWS_ACCESS_KEY_ID"
  - match: "litellm (.*)$"
    command: "uvx --with 'litellm[proxy]' litellm \\1"
    vaults:
      - "keepass"
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

	// Verify expandHome was called on vault_path
	expectedPath := filepath.Join(mockHome, "secrets.kdbx")
	if vault.VaultPath != expectedPath {
		t.Errorf("expected expanded path %q, got %q", expectedPath, vault.VaultPath)
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

	// Verify autoload rules parsing
	if len(cfg.Autoload) != 2 {
		t.Fatalf("expected 2 autoload rules, got %d", len(cfg.Autoload))
	}
	rule := cfg.Autoload[0]
	if rule.Match != "aws" {
		t.Errorf("expected match 'aws', got %q", rule.Match)
	}
	if len(rule.Vaults) != 1 || rule.Vaults[0] != "keepass" {
		t.Errorf("expected vaults ['keepass'], got %v", rule.Vaults)
	}
	if len(rule.Merge) != 1 || rule.Merge[0] != "custom_static" {
		t.Errorf("expected merge ['custom_static'], got %v", rule.Merge)
	}
	if rule.Env["AWS_REGION"] != "env://REGION" {
		t.Errorf("expected env AWS_REGION='env://REGION', got %q", rule.Env["AWS_REGION"])
	}
	if len(rule.Whitelist) != 1 || rule.Whitelist[0] != "AWS_ACCESS_KEY_ID" {
		t.Errorf("expected whitelist ['AWS_ACCESS_KEY_ID'], got %v", rule.Whitelist)
	}

	rule2 := cfg.Autoload[1]
	if rule2.Match != "litellm (.*)$" {
		t.Errorf("expected match 'litellm (.*)$', got %q", rule2.Match)
	}
	if rule2.Command != "uvx --with 'litellm[proxy]' litellm \\1" {
		t.Errorf("expected command template, got %q", rule2.Command)
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

func TestKeyringPrefix(t *testing.T) {
	defaultPath, _ := DefaultConfigPath()
	defaultAbs, _ := filepath.Abs(defaultPath)

	hashFunc := func(s string) string {
		h := sha256.Sum256([]byte(s))
		return hex.EncodeToString(h[:])[:10]
	}

	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{
			name: "nil config",
			cfg:  nil,
			want: "cloakenv",
		},
		{
			name: "empty config",
			cfg:  &Config{},
			want: "cloakenv",
		},
		{
			name: "custom prefix empty path",
			cfg: &Config{
				Keyring: KeyringConfig{Prefix: "custom_prefix"},
			},
			want: "custom_prefix",
		},
		{
			name: "default path no hash",
			cfg: &Config{
				ConfigPath: defaultAbs,
			},
			want: "cloakenv",
		},
		{
			name: "custom path with hash",
			cfg: &Config{
				ConfigPath: "/custom/path/config.yaml",
			},
			want: fmt.Sprintf("cloakenv_%s", hashFunc("/custom/path/config.yaml")),
		},
		{
			name: "custom prefix and custom path with hash",
			cfg: &Config{
				Keyring:    KeyringConfig{Prefix: "myprefix"},
				ConfigPath: "/custom/path/config.yaml",
			},
			want: fmt.Sprintf("myprefix_%s", hashFunc("/custom/path/config.yaml")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.KeyringPrefix()
			if got != tt.want {
				t.Errorf("Config.KeyringPrefix() = %v, want %v", got, tt.want)
			}
		})
	}
}
