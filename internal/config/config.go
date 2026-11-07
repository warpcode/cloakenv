// Package config handles parsing of the global cloakenv configuration
// file at ~/.config/cloakenv/config.yaml.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"strings"

	goyaml "gopkg.in/yaml.v3"

	"github.com/warpcode/cloakenv/internal/yaml"
)

var userHomeDir = os.UserHomeDir

// TemplateConfig defines a top-level reusable template entry.
// It can be specified as either an inline template string or a structured object with path or template fields.
type TemplateConfig struct {
	Path     string `yaml:"path,omitempty"`
	Template string `yaml:"template,omitempty"`
}

// UnmarshalYAML implements custom unmarshaling for TemplateConfig to support scalar strings and structs.
func (t *TemplateConfig) UnmarshalYAML(value *goyaml.Node) error {
	if value.Kind == goyaml.ScalarNode {
		t.Template = value.Value
		return nil
	}
	type rawTemplate TemplateConfig
	var raw rawTemplate
	if err := value.Decode(&raw); err != nil {
		return err
	}
	*t = TemplateConfig(raw)
	return nil
}

// Config is the top-level configuration structure.
type Config struct {
	// ConfigPath is the absolute path to the configuration file.
	ConfigPath        string                    `yaml:"-"`
	Cache             CacheConfig               `yaml:"cache"`
	Keyring           KeyringConfig             `yaml:"keyring"`
	Templates         map[string]TemplateConfig `yaml:"templates,omitempty"`
	Vaults            map[string]VaultConfig    `yaml:"vaults"`
	Autoload          []AutoloadRule            `yaml:"autoload"`
	KeyFormatOverride string                    `yaml:"-"`
}

// AutoloadRule defines rules for matching, masking, and autoloading secret vaults/URIs/env vars
// based on matching commands executed via `cloakenv run -- <command>`.
type AutoloadRule struct {
	// Match is a required regex pattern, glob, or command string to match against incoming CLI command arguments.
	Match string `yaml:"match"`

	// Command is an optional target command replacement template with \1 or $1 regex capture group expansions.
	Command string `yaml:"command"`

	// Vaults is a list of vault names or URIs to merge into environment variables.
	Vaults []string `yaml:"vaults"`

	// Merge is a list of entry URIs or vault URIs to merge into environment variables.
	Merge []string `yaml:"merge"`

	// Env is a map of environment variable names to secret URIs.
	Env map[string]string `yaml:"env"`

	// Whitelist is a list of key names to filter merged entries.
	Whitelist []string `yaml:"whitelist"`

	// CompiledRegex holds the precompiled regular expression for Match.
	CompiledRegex *regexp.Regexp `yaml:"-"`
}

// CompileAutoloadRules precompiles regular expressions for all autoload rules in the configuration.
func (c *Config) CompileAutoloadRules() {
	if c == nil {
		return
	}
	for i := range c.Autoload {
		c.Autoload[i].Compile()
	}
}

// Compile precompiles the regular expression pattern for the autoload rule if Match is non-empty.
func (r *AutoloadRule) Compile() {
	if r == nil {
		return
	}
	pattern := strings.TrimSpace(r.Match)
	if pattern != "" && r.CompiledRegex == nil {
		if re, err := regexp.Compile(pattern); err == nil {
			r.CompiledRegex = re
		}
	}
}

// MappingRule defines a regex-based field key renaming rule for entry attributes.
type MappingRule struct {
	// Match is a required regex pattern to match against field names.
	Match string `yaml:"match"`

	// Key is the target output field name template, supporting $1 or \1 capture group replacements.
	Key string `yaml:"key"`

	// CompiledRegex holds the precompiled regular expression for Match.
	CompiledRegex *regexp.Regexp `yaml:"-"`
}

// Compile precompiles the regular expression pattern for the mapping rule if Match is non-empty.
func (r *MappingRule) Compile() error {
	if r == nil {
		return nil
	}
	pattern := strings.TrimSpace(r.Match)
	if pattern != "" && r.CompiledRegex == nil {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return err
		}
		r.CompiledRegex = re
	}
	return nil
}

// CacheConfig holds cache-related configuration settings.
type CacheConfig struct {
	DefaultTTL string `yaml:"default_ttl"`
}

// VaultCacheConfig defines transparent caching settings for a vault.
type VaultCacheConfig struct {
	Enabled bool   `yaml:"enabled"`
	TTL     string `yaml:"ttl"`
	Mode    string `yaml:"mode"` // "keyring" (default) or "encrypted_disk"
}

// UnmarshalYAML implements custom unmarshaling for VaultCacheConfig.
// It supports boolean scalar (`cache: true`), string scalar (`cache: "15m"`), or mapping (`cache: { ttl: 15m, mode: keyring }`).
func (c *VaultCacheConfig) UnmarshalYAML(value *goyaml.Node) error {
	if value.Kind == goyaml.ScalarNode {
		if value.Tag == "!!bool" || value.Value == "true" || value.Value == "false" {
			var enabled bool
			if err := value.Decode(&enabled); err != nil {
				return err
			}
			c.Enabled = enabled
			return nil
		}
		// String scalar (e.g. cache: "15m")
		c.Enabled = true
		c.TTL = value.Value
		return nil
	}

	type rawVaultCache VaultCacheConfig
	var raw rawVaultCache
	if err := value.Decode(&raw); err != nil {
		return err
	}
	*c = VaultCacheConfig(raw)
	// If unmarshaled from a mapping, default Enabled to true unless explicitly set false
	c.Enabled = true
	for i := 0; i < len(value.Content); i += 2 {
		if value.Content[i].Value == "enabled" {
			var explicitEnabled bool
			if err := value.Content[i+1].Decode(&explicitEnabled); err == nil {
				c.Enabled = explicitEnabled
			}
			break
		}
	}
	return nil
}

// KeyringConfig holds keyring-related configuration settings.
type KeyringConfig struct {
	Prefix  string `yaml:"prefix"`
	Isolate bool   `yaml:"isolate"`
}

// CustomEntityConfig defines a composed or flat entity within a custom_vault.
type CustomEntityConfig struct {
	// IsComposed indicates whether the entity was defined using composition keys.
	IsComposed bool `yaml:"-"`

	Title         string         `yaml:"title,omitempty"`
	Tags          []string       `yaml:"tags,omitempty"`
	Merge         []string       `yaml:"merge,omitempty"`
	Attributes    map[string]any `yaml:"attributes,omitempty"`
	Mapping       []MappingRule  `yaml:"mapping,omitempty"`
	IncludeFields []string       `yaml:"include_fields,omitempty"`
	ExcludeFields []string       `yaml:"exclude_fields,omitempty"`
	KeyFormat     string         `yaml:"key_format,omitempty"`
	ResolveValues *bool          `yaml:"resolve_values,omitempty"`
}

// ToCustomEntityMap converts a map of raw attribute maps into a map of CustomEntityConfig.
func ToCustomEntityMap(m map[string]map[string]any) map[string]CustomEntityConfig {
	if m == nil {
		return nil
	}
	res := make(map[string]CustomEntityConfig, len(m))
	for k, v := range m {
		res[k] = CustomEntityConfig{Attributes: v}
	}
	return res
}

// Compile precompiles regular expressions for mapping rules in the entity configuration.
func (c *CustomEntityConfig) Compile() error {
	if c == nil {
		return nil
	}
	for i := range c.Mapping {
		if err := c.Mapping[i].Compile(); err != nil {
			return err
		}
	}
	return nil
}

// UnmarshalYAML implements custom unmarshaling for CustomEntityConfig.
// It differentiates between legacy flat attribute maps and composed entities.
func (c *CustomEntityConfig) UnmarshalYAML(value *goyaml.Node) error {
	if value.Kind != goyaml.MappingNode {
		return errors.New("custom_vault entity must be a map")
	}

	compositionKeys := map[string]bool{
		"merge":          true,
		"attributes":     true,
		"mapping":        true,
		"include_fields": true,
		"exclude_fields": true,
		"key_format":     true,
		"resolve_values": true,
	}

	allowedKeys := map[string]bool{
		"title":          true,
		"tags":           true,
		"merge":          true,
		"attributes":     true,
		"mapping":        true,
		"include_fields": true,
		"exclude_fields": true,
		"key_format":     true,
		"resolve_values": true,
	}

	var keyNames []string
	hasCompositionKey := false

	for i := 0; i < len(value.Content); i += 2 {
		keyName := value.Content[i].Value
		keyNames = append(keyNames, keyName)
		if compositionKeys[keyName] {
			hasCompositionKey = true
		}
	}

	if !hasCompositionKey {
		c.IsComposed = false
		c.Attributes = make(map[string]any)
		if err := value.Decode(&c.Attributes); err != nil {
			return err
		}
		return nil
	}

	c.IsComposed = true
	for _, keyName := range keyNames {
		if !allowedKeys[keyName] {
			return fmt.Errorf("unknown field %q in composed custom_vault entity", keyName)
		}
	}

	type rawEntity CustomEntityConfig
	var raw rawEntity
	if err := value.Decode(&raw); err != nil {
		return err
	}
	*c = CustomEntityConfig(raw)
	c.IsComposed = true
	return nil
}

// VaultConfig defines a named secret vault backend and its configuration options.
type VaultConfig struct {
	// Provider identifies the provider backend type (e.g., "keepass", "custom_vault").
	Provider string `yaml:"provider"`

	// VaultPath is the filesystem path to the backend's data store
	// (e.g., the .kdbx file for keepass providers).
	VaultPath string `yaml:"vault_path"`

	// KeyFile specifies the path to a KeePass key file (.key / .keyx / raw key).
	KeyFile string `yaml:"key_file"`

	// KeyFileEnv specifies an environment variable name containing the path to a KeePass key file.
	KeyFileEnv string `yaml:"key_file_env"`

	// EntitiesRootKey defines the dictionary key under which entries are listed in the YAML/JSON database.
	// Optional. Defaults to "entities" or "entries". Use "." to map directly to the root of the database file.
	EntitiesRootKey string `yaml:"entities_root_key"`

	// SingleEntity defines whether this vault holds a single entity/collection of attributes.
	SingleEntity *bool `yaml:"single_entity"`

	// EntityName represents the title of the single entity in search results.
	EntityName string `yaml:"entity_name"`

	// Searchable flags whether this vault is included in search results. Defaults to true.
	Searchable *bool `yaml:"searchable"`

	// Tags are labels applied to the single entity (if SingleEntity is true).
	Tags []string `yaml:"tags"`

	// Attributes holds inline key-value attributes for custom_vault (SingleEntity: true).
	Attributes map[string]any `yaml:"attributes"`

	// Entities holds inline entities for custom_vault (SingleEntity: false).
	Entities map[string]CustomEntityConfig `yaml:"entities"`

	// ResolveValues enables URI resolution for attribute values within this vault.
	// When true, any attribute value that is a valid URI referencing a registered
	// scheme is resolved recursively (up to depth 5) before being returned.
	// Template interpolation is supported using {{ get "scheme://..." }} syntax.
	// Defaults to false.
	ResolveValues bool `yaml:"resolve_values"`

	// ResolveReferences enables KeePass entry placeholder and cross-entry reference resolution.
	// Defaults to false.
	ResolveReferences bool `yaml:"resolve_references"`

	// Cache configures transparent caching for this vault.
	Cache VaultCacheConfig `yaml:"cache,omitempty"`

	// IncludeFields limits returned entry attributes to field names matching these glob patterns.
	IncludeFields []string `yaml:"include_fields,omitempty"`

	// ExcludeFields removes entry attributes with field names matching these glob patterns.
	ExcludeFields []string `yaml:"exclude_fields,omitempty"`

	// Mapping defines regex-based field key renaming rules for entry attributes in this vault.
	Mapping []MappingRule `yaml:"mapping,omitempty"`

	// KeyFormat specifies the key transformation format for attributes returned by this vault.
	// Options: "none" (default, passes original key name), "env" (UPPERCASE_WITH_UNDERSCORES),
	// "snake" (lowercase_with_underscores), "kebab" (lowercase-with-hyphens).
	KeyFormat string `yaml:"key_format,omitempty"`

	// SourceVaults specifies the list of vault names to query for search provider vaults.
	SourceVaults []string `yaml:"source_vaults"`

	// Query specifies the search expression for search provider vaults.
	Query string `yaml:"query"`

	// Address specifies the HashiCorp Vault server URL (e.g., "https://vault.example.com:8200").
	Address string `yaml:"address,omitempty"`

	// Token specifies the HashiCorp Vault access token.
	Token string `yaml:"token,omitempty"`

	// RoleID specifies the AppRole role_id for HashiCorp Vault AppRole authentication.
	RoleID string `yaml:"role_id,omitempty"`

	// SecretID specifies the AppRole secret_id for HashiCorp Vault AppRole authentication.
	SecretID string `yaml:"secret_id,omitempty"`

	// EngineVersion specifies the KV secret engine version (1 or 2). Defaults to 2.
	EngineVersion int `yaml:"engine_version,omitempty"`

	// Namespace specifies the Vault namespace (for Vault Enterprise).
	Namespace string `yaml:"namespace,omitempty"`

	// MountPath specifies the KV secrets engine mount path (e.g., "secret").
	MountPath string `yaml:"mount_path,omitempty"`
}

// DefaultConfigPath returns the default configuration file path:
// ~/.config/cloakenv/config.yaml
func DefaultConfigPath() (string, error) {
	home, err := userHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve home directory: %w", err)
	}

	return filepath.Join(home, ".config", "cloakenv", "config.yaml"), nil
}

// Load reads and parses a YAML configuration file.
// Returns an empty Config with initialized maps if the file does not exist.
func Load(path string) (*Config, error) {
	cfg := &Config{
		Vaults: make(map[string]VaultConfig),
	}

	absPath, err := filepath.Abs(path)
	if err == nil {
		cfg.ConfigPath = absPath
	} else {
		cfg.ConfigPath = path
	}

	data, err := os.ReadFile(path) //nolint:gosec // operator-configured config path
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}

	if cfg.Vaults == nil {
		cfg.Vaults = make(map[string]VaultConfig)
	}

	// Expand ~ and anchor relative paths to config directory in all vault paths
	for name, vault := range cfg.Vaults {
		if vault.VaultPath != "" {
			vaultPath := expandHome(vault.VaultPath)
			if !filepath.IsAbs(vaultPath) && cfg.ConfigPath != "" {
				baseDir := filepath.Dir(cfg.ConfigPath)
				vaultPath = filepath.Join(baseDir, vaultPath)
			}
			vault.VaultPath = filepath.Clean(vaultPath)
		}
		if vault.KeyFileEnv != "" {
			if envVal := os.Getenv(vault.KeyFileEnv); envVal != "" {
				vault.KeyFile = envVal
			}
		}
		if vault.KeyFile != "" {
			keyPath := expandHome(vault.KeyFile)
			if !filepath.IsAbs(keyPath) && cfg.ConfigPath != "" {
				baseDir := filepath.Dir(cfg.ConfigPath)
				keyPath = filepath.Join(baseDir, keyPath)
			}
			vault.KeyFile = filepath.Clean(keyPath)

			info, statErr := os.Stat(vault.KeyFile) //nolint:gosec // operator-configured key_file path
			if statErr != nil {
				return nil, fmt.Errorf("vault %q: key_file %q not found or inaccessible: %w", name, vault.KeyFile, statErr)
			}
			if info.IsDir() {
				return nil, fmt.Errorf("vault %q: key_file %q is a directory, expected regular file", name, vault.KeyFile)
			}
		}
		cfg.Vaults[name] = vault
	}

	if cfg.Templates == nil {
		cfg.Templates = make(map[string]TemplateConfig)
	}

	// Process templates: expand path, read file if path is specified, and validate
	for tmplName, tmpl := range cfg.Templates {
		if tmpl.Path != "" {
			expandedPath := expandHome(tmpl.Path)
			if !filepath.IsAbs(expandedPath) && cfg.ConfigPath != "" {
				baseDir := filepath.Dir(cfg.ConfigPath)
				expandedPath = filepath.Join(baseDir, expandedPath)
			}
			expandedPath = filepath.Clean(expandedPath)
			data, err := os.ReadFile(expandedPath)
			if err != nil {
				return nil, fmt.Errorf("template %q: failed to read path %q: %w", tmplName, tmpl.Path, err)
			}
			tmpl.Template = string(data)
			tmpl.Path = expandedPath
		}
		if tmpl.Template == "" {
			return nil, fmt.Errorf("template %q: template content is empty", tmplName)
		}
		cfg.Templates[tmplName] = tmpl
	}

	// Validate include_fields, exclude_fields, and mapping rules
	for vaultName, vault := range cfg.Vaults {
		for _, pattern := range vault.IncludeFields {
			if _, err := pathpkg.Match(pattern, ""); err != nil {
				return nil, fmt.Errorf("vault %q: invalid include_fields glob pattern %q: %w", vaultName, pattern, err)
			}
		}
		for _, pattern := range vault.ExcludeFields {
			if _, err := pathpkg.Match(pattern, ""); err != nil {
				return nil, fmt.Errorf("vault %q: invalid exclude_fields glob pattern %q: %w", vaultName, pattern, err)
			}
		}
		for i := range vault.Mapping {
			if err := vault.Mapping[i].Compile(); err != nil {
				return nil, fmt.Errorf("vault %q: invalid mapping match regex %q: %w", vaultName, vault.Mapping[i].Match, err)
			}
		}
		switch vault.KeyFormat {
		case "", "none", "env", "snake", "kebab":
			// valid key format
		default:
			return nil, fmt.Errorf("vault %q: invalid key_format %q (must be one of: none, env, snake, kebab)", vaultName, vault.KeyFormat)
		}

		if vault.Provider == "vault" {
			if vault.EngineVersion != 0 && vault.EngineVersion != 1 && vault.EngineVersion != 2 {
				return nil, fmt.Errorf("vault %q: invalid engine_version %d (must be 1 or 2)", vaultName, vault.EngineVersion)
			}
			// If AppRole is partially configured, require both role_id and secret_id
			if (vault.RoleID != "" && vault.SecretID == "") || (vault.RoleID == "" && vault.SecretID != "") {
				return nil, fmt.Errorf("vault %q: both role_id and secret_id must be provided for approle auth", vaultName)
			}
		}

		if vault.Cache.Enabled {
			switch vault.Cache.Mode {
			case "", "keyring", "encrypted_disk":
				// valid mode
			default:
				return nil, fmt.Errorf("vault %q: invalid cache mode %q (must be one of: keyring, encrypted_disk)", vaultName, vault.Cache.Mode)
			}
		}

		for entityName, entity := range vault.Entities {
			for _, pattern := range entity.IncludeFields {
				if _, err := pathpkg.Match(pattern, ""); err != nil {
					return nil, fmt.Errorf("vault %q entity %q: invalid include_fields glob pattern %q: %w", vaultName, entityName, pattern, err)
				}
			}
			for _, pattern := range entity.ExcludeFields {
				if _, err := pathpkg.Match(pattern, ""); err != nil {
					return nil, fmt.Errorf("vault %q entity %q: invalid exclude_fields glob pattern %q: %w", vaultName, entityName, pattern, err)
				}
			}
			if err := entity.Compile(); err != nil {
				return nil, fmt.Errorf("vault %q entity %q: invalid mapping match regex: %w", vaultName, entityName, err)
			}
			switch entity.KeyFormat {
			case "", "none", "env", "snake", "kebab":
				// valid key format
			default:
				return nil, fmt.Errorf("vault %q entity %q: invalid key_format %q (must be one of: none, env, snake, kebab)", vaultName, entityName, entity.KeyFormat)
			}
			vault.Entities[entityName] = entity
		}

		cfg.Vaults[vaultName] = vault
	}

	cfg.CompileAutoloadRules()

	return cfg, nil
}

// expandHome replaces a leading "~/" with the user's home directory.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}

	home, err := userHomeDir()
	if err != nil {
		return path
	}

	return filepath.Join(home, path[2:])
}

// KeyringPrefix returns the active keyring prefix. If using a custom config file,
// it computes a 10-character SHA-256 hash of its absolute path and appends it.
func (c *Config) KeyringPrefix() string {
	if c == nil {
		return "cloakenv"
	}
	prefix := c.Keyring.Prefix
	if prefix == "" {
		prefix = "cloakenv"
	}

	if !c.Keyring.Isolate || c.ConfigPath == "" {
		return prefix
	}

	defaultPath, err := DefaultConfigPath()
	if err != nil {
		return prefix
	}

	defaultAbs, err := filepath.Abs(defaultPath)
	if err != nil || c.ConfigPath == defaultAbs {
		return prefix
	}

	h := sha256.Sum256([]byte(c.ConfigPath))
	hashStr := hex.EncodeToString(h[:])[:10]
	return fmt.Sprintf("%s_%s", prefix, hashStr)
}
