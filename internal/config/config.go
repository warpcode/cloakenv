// Package config handles parsing of the global cloakenv configuration
// file at ~/.config/cloakenv/config.yaml.
package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
	ConfigPath string                         `yaml:"-"`
	Cache      CacheConfig                    `yaml:"cache"`
	Keyring    KeyringConfig                  `yaml:"keyring"`
	Audit      AuditConfig                    `yaml:"audit,omitempty"`
	Envs       map[string][]VaultSourceConfig `yaml:"envs,omitempty"`
	Templates  map[string]TemplateConfig      `yaml:"templates,omitempty"`
	Vaults     map[string]VaultConfig         `yaml:"vaults"`
	Autoload   []AutoloadRule                 `yaml:"autoload"`
}

// AutoloadEnvItem represents an entry in `autoload[].envs[]`, which may be either
// a named env string reference (e.g. "litellm_env") or an inline key-value map.
type AutoloadEnvItem struct {
	Name   string
	Inline map[string]string
}

// NamedEnvItem constructs an AutoloadEnvItem referencing a named top-level env.
func NamedEnvItem(name string) AutoloadEnvItem {
	return AutoloadEnvItem{Name: name}
}

// InlineEnvItem constructs an AutoloadEnvItem holding an inline key-value map.
func InlineEnvItem(m map[string]string) AutoloadEnvItem {
	return AutoloadEnvItem{Inline: m}
}

// UnmarshalYAML unmarshals an AutoloadEnvItem from either a YAML string scalar or a string map.
func (a *AutoloadEnvItem) UnmarshalYAML(value *goyaml.Node) error {
	for value.Kind == goyaml.AliasNode && value.Alias != nil {
		value = value.Alias
	}
	if value.Kind == goyaml.ScalarNode {
		if value.Tag != "!!str" && value.Tag != "" {
			return fmt.Errorf("autoload env item must be an env name string or key-value map")
		}
		a.Name = value.Value
		a.Inline = nil
		return nil
	}
	if value.Kind == goyaml.MappingNode {
		var m map[string]string
		if err := value.Decode(&m); err != nil {
			return err
		}
		a.Inline = m
		a.Name = ""
		return nil
	}
	return errors.New("autoload env item must be an env name string or key-value map")
}

// MarshalYAML marshals an AutoloadEnvItem back to YAML as a string or map.
func (a AutoloadEnvItem) MarshalYAML() (any, error) {
	if a.Name != "" {
		return a.Name, nil
	}
	return a.Inline, nil
}

// MarshalJSON marshals an AutoloadEnvItem to JSON as a string or map.
func (a AutoloadEnvItem) MarshalJSON() ([]byte, error) {
	if a.Name != "" {
		return json.Marshal(a.Name)
	}
	if a.Inline != nil {
		return json.Marshal(a.Inline)
	}
	return json.Marshal("")
}

// UnmarshalJSON unmarshals an AutoloadEnvItem from JSON string or map.
func (a *AutoloadEnvItem) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("autoload env item cannot be null")
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		a.Name = s
		a.Inline = nil
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err == nil {
		a.Inline = m
		a.Name = ""
		return nil
	}
	return errors.New("autoload env item must be a string or string map")
}

// AutoloadTemplateConfig attaches a named template generator to an autoload rule.
type AutoloadTemplateConfig struct {
	Template string `yaml:"template" json:"template"`
	Env      string `yaml:"env,omitempty" json:"env,omitempty"`
	EnvPath  string `yaml:"env_path,omitempty" json:"env_path,omitempty"`
	Path     string `yaml:"path,omitempty" json:"path,omitempty"`
	Persist  bool   `yaml:"persist,omitempty" json:"persist,omitempty"`
}

// AutoloadRule defines rules for matching commands executed via `cloakenv run -- <command>`,
// transforming command arguments, injecting `envs:`, and attaching rendered `templates:`.
type AutoloadRule struct {
	// Match is a required regex pattern, glob, or command string to match against incoming CLI command arguments.
	Match string `yaml:"match"`
	// Command is an optional target command replacement template with \1 or $1 regex capture group expansions.
	Command string `yaml:"command,omitempty"`
	// Envs is an ordered list of named envs references and/or inline key-value overrides.
	Envs []AutoloadEnvItem `yaml:"envs,omitempty"`
	// Templates is a list of template artifact deliveries attached to matching commands.
	Templates []AutoloadTemplateConfig `yaml:"templates,omitempty"`
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

// VaultConfig defines a named secret vault backend and its pure retrieval options.
type VaultConfig struct {
	// Provider identifies the provider backend type ("keepass", "pass", "vault", "env", "keyring").
	Provider string `yaml:"provider"`

	// VaultPath is the filesystem path to the backend's data store
	// (e.g., the .kdbx file for keepass or store directory for pass).
	VaultPath string `yaml:"vault_path"`

	// KeyFile specifies the path to a KeePass key file (.key / .keyx / raw key).
	KeyFile string `yaml:"key_file"`

	// KeyFileEnv specifies an environment variable name containing the path to a KeePass key file.
	KeyFileEnv string `yaml:"key_file_env"`

	// ResolveReferences enables KeePass entry placeholder and cross-entry reference resolution.
	// Defaults to false.
	ResolveReferences bool `yaml:"resolve_references"`

	// Cache configures transparent caching for this vault.
	Cache VaultCacheConfig `yaml:"cache,omitempty"`

	// Query specifies an optional provider-native query/scope filter for remote vaults.
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

	// Validate vault settings
	for vaultName, vault := range cfg.Vaults {
		if vault.Provider == "vault" {
			if vault.EngineVersion != 0 && vault.EngineVersion != 1 && vault.EngineVersion != 2 {
				return nil, fmt.Errorf("vault %q: invalid engine_version %d (must be 1 or 2)", vaultName, vault.EngineVersion)
			}
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
