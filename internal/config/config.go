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

	"github.com/warpcode/cloakenv/internal/utils"
)

var userHomeDir = os.UserHomeDir

// RuleConfig defines a single ordered entry transformation rule inside an env or template vault source.
type RuleConfig struct {
	// Type specifies the rule operation: "pass", "match", or "set".
	Type string `yaml:"type"`

	// Fields for type: "pass"
	Include   []string `yaml:"include,omitempty"`
	Exclude   []string `yaml:"exclude,omitempty"`
	KeyFormat string   `yaml:"key_format,omitempty"` // "env", "snake", "kebab", "none"

	// Fields for type: "match"
	Match      string `yaml:"match,omitempty"`
	ValueMatch string `yaml:"value_match,omitempty"`
	Key        string `yaml:"key,omitempty"`
	Value      string `yaml:"value,omitempty"`

	// Fields for type: "set"
	Values map[string]string `yaml:"values,omitempty"`

	// Precompiled regex patterns populated by Compile.
	CompiledMatch      *regexp.Regexp `yaml:"-"`
	CompiledValueMatch *regexp.Regexp `yaml:"-"`
}

// Compile precompiles regex patterns configured on a match RuleConfig.
func (r *RuleConfig) Compile() error {
	if r == nil {
		return nil
	}
	if r.Match != "" && r.CompiledMatch == nil {
		re, err := regexp.Compile(r.Match)
		if err != nil {
			return err
		}
		r.CompiledMatch = re
	}
	if r.ValueMatch != "" && r.CompiledValueMatch == nil {
		re, err := regexp.Compile(r.ValueMatch)
		if err != nil {
			return err
		}
		r.CompiledValueMatch = re
	}
	return nil
}

// VaultSourceConfig binds a vault reference, optional query predicate, and ordered transformation rules.
type VaultSourceConfig struct {
	Vault string       `yaml:"vault"`
	Query string       `yaml:"query,omitempty"`
	Rules []RuleConfig `yaml:"rules,omitempty"`
}

// TemplateConfig defines a top-level template/artifact generator entry.
// It supports drivers "template" (default when template/path is set), "json", "yaml", and "dotenv",
// as well as inline scalar template strings for shorthand declarations.
type TemplateConfig struct {
	Driver   string              `yaml:"driver,omitempty"`
	Path     string              `yaml:"path,omitempty"`
	Template string              `yaml:"template,omitempty"`
	FromEnv  string              `yaml:"from_env,omitempty"`
	Vaults   []VaultSourceConfig `yaml:"vaults,omitempty"`
}

// EffectiveDriver returns the resolved template driver name, defaulting to "template" when omitted.
func (t TemplateConfig) EffectiveDriver() string {
	if t.Driver != "" {
		return t.Driver
	}
	return "template"
}

// UnmarshalYAML implements custom unmarshaling for TemplateConfig to support scalar strings and structs.
func (t *TemplateConfig) UnmarshalYAML(value *goyaml.Node) error {
	for value.Kind == goyaml.AliasNode && value.Alias != nil {
		value = value.Alias
	}
	if value.Kind == goyaml.ScalarNode {
		if value.Tag == "!!null" {
			return nil
		}
		t.Driver = "template"
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
	ConfigPath        string                         `yaml:"-"`
	Cache             CacheConfig                    `yaml:"cache"`
	Keyring           KeyringConfig                  `yaml:"keyring"`
	Audit             AuditConfig                    `yaml:"audit,omitempty"`
	Envs              map[string][]VaultSourceConfig `yaml:"envs,omitempty"`
	Templates         map[string]TemplateConfig      `yaml:"templates,omitempty"`
	Vaults            map[string]VaultConfig         `yaml:"vaults"`
	Autoload          []AutoloadRule                 `yaml:"autoload"`
	KeyFormatOverride string                         `yaml:"-"`
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

// AuditConfig configures structured JSON-lines auditing for secret reads.
type AuditConfig struct {
	Enabled bool   `yaml:"enabled"`
	LogPath string `yaml:"log_path"`
	Format  string `yaml:"format"`
}

// DefaultAuditLogPath returns the default path for the audit log.
func DefaultAuditLogPath() (string, error) {
	home, err := userHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "cloakenv", "audit.jsonl"), nil
}

// VaultConfig defines a named secret vault backend and its configuration options.
type VaultConfig struct {
	// Provider identifies the provider backend type (e.g., "keepass", "pass", "env", "search", "vault").
	Provider string `yaml:"provider"`

	// VaultPath is the filesystem path to the backend's data store
	// (e.g., the .kdbx file for keepass providers).
	VaultPath string `yaml:"vault_path"`

	// KeyFile specifies the path to a KeePass key file (.key / .keyx / raw key).
	KeyFile string `yaml:"key_file"`

	// KeyFileEnv specifies an environment variable name containing the path to a KeePass key file.
	KeyFileEnv string `yaml:"key_file_env"`

	// EntitiesRootKey defines an optional root key prefix stripped during entry path resolution.
	EntitiesRootKey string `yaml:"entities_root_key"`

	// SingleEntity defines whether this vault holds a single entity/collection of attributes.
	SingleEntity *bool `yaml:"single_entity"`

	// EntityName represents the title of the single entity in search results.
	EntityName string `yaml:"entity_name"`

	// Searchable flags whether this vault is included in search results. Defaults to true.
	Searchable *bool `yaml:"searchable"`

	// Tags are labels applied to the single entity (if SingleEntity is true).
	Tags []string `yaml:"tags"`

	// Attributes holds vault-level computed attribute templates applied to entries returned by this vault.
	Attributes map[string]any `yaml:"attributes"`

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

	// Sources specifies a list of queries or entry/vault URIs to group and merge for search provider vaults.
	Sources []string `yaml:"sources,omitempty"`

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
	return load(path, true)
}

func load(path string, resolveTemplates bool) (*Config, error) {
	cfg := &Config{
		Envs:   make(map[string][]VaultSourceConfig),
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

	if err := utils.UnmarshalYAML(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}

	if cfg.Envs == nil {
		cfg.Envs = make(map[string][]VaultSourceConfig)
	}
	if cfg.Vaults == nil {
		cfg.Vaults = make(map[string]VaultConfig)
	}
	if cfg.Audit.Format == "" {
		cfg.Audit.Format = "jsonl"
	}
	switch cfg.Audit.Format {
	case "jsonl", "json":
		// valid audit format
	default:
		return nil, fmt.Errorf("invalid audit format %q (must be one of: json, jsonl)", cfg.Audit.Format)
	}
	if cfg.Audit.Enabled && cfg.Audit.LogPath == "" {
		logPath, err := DefaultAuditLogPath()
		if err != nil {
			return nil, err
		}
		cfg.Audit.LogPath = logPath
	}
	if cfg.Audit.LogPath != "" {
		logPath := expandHome(cfg.Audit.LogPath)
		if !filepath.IsAbs(logPath) && cfg.ConfigPath != "" {
			baseDir := filepath.Dir(cfg.ConfigPath)
			logPath = filepath.Join(baseDir, logPath)
		}
		cfg.Audit.LogPath = filepath.Clean(logPath)
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

	// Process templates: expand path and read file for template-driver entries
	if resolveTemplates {
		for tmplName, tmpl := range cfg.Templates {
			if tmpl.EffectiveDriver() == "template" {
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
			}
			cfg.Templates[tmplName] = tmpl
		}
	}

	validProviders := map[string]bool{
		"keepass": true,
		"env":     true,
		"pass":    true,
		"search":  true,
		"vault":   true,
		"keyring": true,
	}

	// Validate providers, include_fields, exclude_fields, and mapping rules
	for vaultName, vault := range cfg.Vaults {
		if vault.Provider == "" {
			return nil, fmt.Errorf("vault %q: missing required field 'provider'", vaultName)
		}
		if !validProviders[vault.Provider] {
			return nil, fmt.Errorf("vault %q: unsupported provider %q (must be one of: keepass, env, pass, search, vault, keyring)", vaultName, vault.Provider)
		}
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

		cfg.Vaults[vaultName] = vault
	}

	cfg.CompileAutoloadRules()
	cfg.CompileRules()

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

// CompileRules precompiles valid regex patterns for all rules in envs and templates.
func (c *Config) CompileRules() {
	if c == nil {
		return
	}
	for envName, sources := range c.Envs {
		for i := range sources {
			for j := range sources[i].Rules {
				_ = sources[i].Rules[j].Compile()
			}
		}
		c.Envs[envName] = sources
	}
	for tmplName, tmpl := range c.Templates {
		for i := range tmpl.Vaults {
			for j := range tmpl.Vaults[i].Rules {
				_ = tmpl.Vaults[i].Rules[j].Compile()
			}
		}
		c.Templates[tmplName] = tmpl
	}
}
