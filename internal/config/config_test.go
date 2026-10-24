package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
			name: "custom path no isolate no hash",
			cfg: &Config{
				ConfigPath: "/custom/path/config.yaml",
			},
			want: "cloakenv",
		},
		{
			name: "custom path with isolate and hash",
			cfg: &Config{
				Keyring:    KeyringConfig{Isolate: true},
				ConfigPath: "/custom/path/config.yaml",
			},
			want: fmt.Sprintf("cloakenv_%s", hashFunc("/custom/path/config.yaml")),
		},
		{
			name: "custom prefix and custom path with isolate and hash",
			cfg: &Config{
				Keyring:    KeyringConfig{Prefix: "myprefix", Isolate: true},
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

func TestLoad_Templates(t *testing.T) {
	tempDir := t.TempDir()

	// Create a template file in a subdirectory
	tmplDir := filepath.Join(tempDir, "templates")
	if err := os.Mkdir(tmplDir, 0755); err != nil {
		t.Fatalf("failed to create tmplDir: %v", err)
	}
	tmplPath := filepath.Join(tmplDir, "model.tmpl")
	fileContent := "MODEL_BLOCK={{ .attributes.Model }}"
	if err := os.WriteFile(tmplPath, []byte(fileContent), 0644); err != nil {
		t.Fatalf("failed to write tmplPath: %v", err)
	}

	yamlContent := `
templates:
  inline_tmpl: "Host {{ .meta.Title }}"
  file_tmpl:
    path: "templates/model.tmpl"
`
	configPath := filepath.Join(tempDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write config.yaml: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed for templates config: %v", err)
	}

	if len(cfg.Templates) != 2 {
		t.Fatalf("expected 2 templates, got %d", len(cfg.Templates))
	}

	inlineTmpl, ok := cfg.Templates["inline_tmpl"]
	if !ok || inlineTmpl.Template != "Host {{ .meta.Title }}" {
		t.Errorf("unexpected inline_tmpl: %+v", inlineTmpl)
	}

	fileTmpl, ok := cfg.Templates["file_tmpl"]
	if !ok || fileTmpl.Template != fileContent {
		t.Errorf("unexpected file_tmpl: %+v", fileTmpl)
	}

	// Test non-existent template path
	invalidYaml := `
templates:
  missing_file:
    path: "templates/does_not_exist.tmpl"
`
	invalidConfigPath := filepath.Join(tempDir, "invalid_tmpl.yaml")
	if err := os.WriteFile(invalidConfigPath, []byte(invalidYaml), 0644); err != nil {
		t.Fatalf("failed to write invalid_tmpl.yaml: %v", err)
	}
	_, err = Load(invalidConfigPath)
	if err == nil {
		t.Fatal("expected error for non-existent template file path, got nil")
	}
	if !strings.Contains(err.Error(), "failed to read path") {
		t.Errorf("expected error message to contain 'failed to read path', got: %v", err)
	}
}

func TestLoad_CustomVaultEntities(t *testing.T) {
	tempDir := t.TempDir()

	yamlContent := `
vaults:
  my_custom:
    provider: "custom_vault"
    entities:
      flat_entity:
        username: "bob"
        Password: "123"
      composed_entity:
        merge:
          - "openrouter://"
        key_format: "env"
        attributes:
          dotenv: "{{ .attributes | toEnv }}"
`
	configPath := filepath.Join(tempDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write config.yaml: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed for custom vault entities: %v", err)
	}

	vault, ok := cfg.Vaults["my_custom"]
	if !ok {
		t.Fatal("expected 'my_custom' vault")
	}

	flat, ok := vault.Entities["flat_entity"]
	if !ok {
		t.Fatal("expected 'flat_entity'")
	}
	if flat.IsComposed {
		t.Error("expected flat_entity IsComposed to be false")
	}
	if flat.Attributes["username"] != "bob" || flat.Attributes["Password"] != "123" {
		t.Errorf("unexpected flat_entity attributes: %v", flat.Attributes)
	}

	composed, ok := vault.Entities["composed_entity"]
	if !ok {
		t.Fatal("expected 'composed_entity'")
	}
	if !composed.IsComposed {
		t.Error("expected composed_entity IsComposed to be true")
	}
	if len(composed.Merge) != 1 || composed.Merge[0] != "openrouter://" {
		t.Errorf("unexpected Merge in composed entity: %v", composed.Merge)
	}
	if composed.KeyFormat != "env" {
		t.Errorf("expected KeyFormat 'env', got %q", composed.KeyFormat)
	}
	if composed.Attributes["dotenv"] != "{{ .attributes | toEnv }}" {
		t.Errorf("unexpected Attributes in composed entity: %v", composed.Attributes)
	}

	// Test unknown top-level field in composed entity
	unknownKeyYaml := `
vaults:
  my_custom:
    provider: "custom_vault"
    entities:
      bad_composed:
        merge:
          - "openrouter://"
        include_feilds:
          - "Password"
`
	unknownConfigPath := filepath.Join(tempDir, "unknown.yaml")
	if err := os.WriteFile(unknownConfigPath, []byte(unknownKeyYaml), 0644); err != nil {
		t.Fatalf("failed to write unknown.yaml: %v", err)
	}
	_, err = Load(unknownConfigPath)
	if err == nil {
		t.Fatal("expected error for unknown top-level field in composed entity, got nil")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("expected error to mention unknown field, got: %v", err)
	}
}

func TestLoad_CustomVaultEntityValidation(t *testing.T) {
	tempDir := t.TempDir()

	yamlContent := `
vaults:
  validated_vault:
    provider: "custom_vault"
    entities:
      valid_entity:
        merge:
          - "openrouter://"
        key_format: "snake"
        include_fields:
          - "env:*"
        exclude_fields:
          - "secret:*"
        mapping:
          - match: "Password"
            key: "API_KEY"
`
	configPath := filepath.Join(tempDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write config.yaml: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed for entity validation: %v", err)
	}

	entity := cfg.Vaults["validated_vault"].Entities["valid_entity"]
	if len(entity.Mapping) != 1 || entity.Mapping[0].CompiledRegex == nil {
		t.Error("expected entity mapping regex to be precompiled")
	}

	// Test invalid entity key_format
	invalidFmtYaml := `
vaults:
  v:
    provider: "custom_vault"
    entities:
      e:
        merge: ["a://"]
        key_format: "invalid_format"
`
	invPath := filepath.Join(tempDir, "inv_fmt.yaml")
	_ = os.WriteFile(invPath, []byte(invalidFmtYaml), 0644)
	_, err = Load(invPath)
	if err == nil {
		t.Fatal("expected error for invalid entity key_format, got nil")
	}

	// Test invalid entity include_fields glob
	invalidGlobYaml := `
vaults:
  v:
    provider: "custom_vault"
    entities:
      e:
        merge: ["a://"]
        include_fields: ["bad[glob"]
`
	invGlobPath := filepath.Join(tempDir, "inv_glob.yaml")
	_ = os.WriteFile(invGlobPath, []byte(invalidGlobYaml), 0644)
	_, err = Load(invGlobPath)
	if err == nil {
		t.Fatal("expected error for invalid entity glob pattern, got nil")
	}
}

func TestLoad_Mapping(t *testing.T) {
	tempDir := t.TempDir()
	yamlContent := `
vaults:
  work:
    provider: "keepass"
    vault_path: "~/secrets.kdbx"
    mapping:
      - match: "env:(.*)"
        key: "\\1"
      - match: "secret_(.*)"
        key: "$1"
`
	configPath := filepath.Join(tempDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write temp config file: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	v, ok := cfg.Vaults["work"]
	if !ok {
		t.Fatal("expected 'work' vault in config")
	}

	if len(v.Mapping) != 2 {
		t.Fatalf("expected 2 mapping rules, got %d", len(v.Mapping))
	}

	if v.Mapping[0].Match != "env:(.*)" || v.Mapping[0].Key != "\\1" {
		t.Errorf("unexpected mapping[0]: %+v", v.Mapping[0])
	}
	if v.Mapping[0].CompiledRegex == nil {
		t.Error("expected CompiledRegex to be populated for mapping[0]")
	}

	if v.Mapping[1].Match != "secret_(.*)" || v.Mapping[1].Key != "$1" {
		t.Errorf("unexpected mapping[1]: %+v", v.Mapping[1])
	}
	if v.Mapping[1].CompiledRegex == nil {
		t.Error("expected CompiledRegex to be populated for mapping[1]")
	}
}

func TestLoad_InvalidMappingRegex(t *testing.T) {
	tempDir := t.TempDir()
	yamlContent := `
vaults:
  work:
    provider: "keepass"
    vault_path: "~/secrets.kdbx"
    mapping:
      - match: "[invalid(regex"
        key: "\\1"
`
	configPath := filepath.Join(tempDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write temp config file: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected error for invalid mapping regex, got nil")
	}
	if !strings.Contains(err.Error(), "invalid mapping match regex") {
		t.Errorf("expected error message to contain 'invalid mapping match regex', got: %v", err)
	}
}

func TestLoad_ResolveReferences(t *testing.T) {
	tempDir := t.TempDir()
	yamlContent := `
vaults:
  with_refs:
    provider: "keepass"
    vault_path: "~/secrets.kdbx"
    resolve_references: true
  without_refs:
    provider: "keepass"
    vault_path: "~/secrets.kdbx"
    resolve_references: false
`
	configPath := filepath.Join(tempDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write temp config file: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	v1, ok := cfg.Vaults["with_refs"]
	if !ok {
		t.Fatal("expected 'with_refs' vault in config")
	}
	if !v1.ResolveReferences {
		t.Error("expected resolve_references to be true for 'with_refs'")
	}

	v2, ok := cfg.Vaults["without_refs"]
	if !ok {
		t.Fatal("expected 'without_refs' vault in config")
	}
	if v2.ResolveReferences {
		t.Error("expected resolve_references to be false for 'without_refs'")
	}
}

func TestLoad_FieldFiltering(t *testing.T) {
	tempDir := t.TempDir()
	yamlContent := `
vaults:
  work:
    provider: "keepass"
    vault_path: "~/secrets.kdbx"
    include_fields:
      - "env:*"
      - "UserName"
    exclude_fields:
      - "*.notes"
      - "secret:*"
`
	configPath := filepath.Join(tempDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write temp config file: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	v, ok := cfg.Vaults["work"]
	if !ok {
		t.Fatal("expected 'work' vault in config")
	}

	if len(v.IncludeFields) != 2 || v.IncludeFields[0] != "env:*" || v.IncludeFields[1] != "UserName" {
		t.Errorf("unexpected IncludeFields: %v", v.IncludeFields)
	}

	if len(v.ExcludeFields) != 2 || v.ExcludeFields[0] != "*.notes" || v.ExcludeFields[1] != "secret:*" {
		t.Errorf("unexpected ExcludeFields: %v", v.ExcludeFields)
	}
}

func TestLoad_ExamplesConfigYaml(t *testing.T) {
	examplesPath := filepath.Join("..", "..", "examples", "config.yaml")
	cfg, err := Load(examplesPath)
	if err != nil {
		t.Fatalf("failed to load examples/config.yaml: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config for examples/config.yaml")
	}
}

func TestLoad_InvalidGlobPattern(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name    string
		content string
	}{
		{
			name: "invalid include_fields pattern",
			content: `
vaults:
  work:
    provider: "keepass"
    vault_path: "~/secrets.kdbx"
    include_fields:
      - "secret:["
`,
		},
		{
			name: "invalid exclude_fields pattern",
			content: `
vaults:
  work:
    provider: "keepass"
    vault_path: "~/secrets.kdbx"
    exclude_fields:
      - "bad[pattern"
`,
		},
		{
			name: "invalid key_format value",
			content: `
vaults:
  work:
    provider: "keepass"
    vault_path: "~/secrets.kdbx"
    key_format: "camel"
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(tempDir, tt.name+".yaml")
			if err := os.WriteFile(configPath, []byte(tt.content), 0644); err != nil {
				t.Fatalf("failed to write config file: %v", err)
			}

			_, err := Load(configPath)
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tt.name)
			}
			if !strings.Contains(err.Error(), "invalid") {
				t.Errorf("expected error message to contain 'invalid', got: %v", err)
			}
		})
	}

	// Test valid key_format values
	validFormats := []string{"", "none", "env", "snake", "kebab"}
	for _, fmtVal := range validFormats {
		t.Run("valid_key_format_"+fmtVal, func(t *testing.T) {
			content := fmt.Sprintf(`
vaults:
  work:
    provider: "keepass"
    vault_path: "~/secrets.kdbx"
    key_format: %q
`, fmtVal)
			configPath := filepath.Join(tempDir, "valid_"+fmtVal+".yaml")
			if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
				t.Fatalf("failed to write config file: %v", err)
			}
			cfg, err := Load(configPath)
			if err != nil {
				t.Fatalf("unexpected error for valid key_format %q: %v", fmtVal, err)
			}
			if cfg.Vaults["work"].KeyFormat != fmtVal {
				t.Errorf("expected KeyFormat %q, got %q", fmtVal, cfg.Vaults["work"].KeyFormat)
			}
		})
	}
}
