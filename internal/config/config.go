// Package config handles parsing of the global cloakenv configuration
// file at ~/.config/cloakenv/config.yaml.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/warpcode/cloakenv/internal/yaml"
)

var userHomeDir = os.UserHomeDir

// Config is the top-level configuration structure.
type Config struct {
	// ConfigPath is the absolute path to the configuration file.
	ConfigPath string                 `yaml:"-"`
	Cache      CacheConfig            `yaml:"cache"`
	Keyring    KeyringConfig          `yaml:"keyring"`
	Vaults     map[string]VaultConfig `yaml:"vaults"`
	Autoload   []AutoloadRule         `yaml:"autoload"`
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
}

// CacheConfig holds cache-related configuration settings.
type CacheConfig struct {
	DefaultTTL string `yaml:"default_ttl"`
}

// KeyringConfig holds keyring-related configuration settings.
type KeyringConfig struct {
	Prefix  string `yaml:"prefix"`
	Isolate bool   `yaml:"isolate"`
}

// VaultConfig defines a named secret vault backend and its configuration options.
type VaultConfig struct {
	// Provider identifies the provider backend type (e.g., "keepass", "custom_vault").
	Provider string `yaml:"provider"`

	// VaultPath is the filesystem path to the backend's data store
	// (e.g., the .kdbx file for keepass providers).
	VaultPath string `yaml:"vault_path"`

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
	Entities map[string]map[string]any `yaml:"entities"`

	// ResolveValues enables URI resolution for attribute values within this vault.
	// When true, any attribute value that is a valid URI referencing a registered
	// scheme is resolved recursively (up to depth 5) before being returned.
	// Only whole-value replacement is supported; inline interpolation is not.
	// Defaults to false.
	ResolveValues bool `yaml:"resolve_values"`
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

	// Expand ~ in all vault paths
	for name, vault := range cfg.Vaults {
		vault.VaultPath = expandHome(vault.VaultPath)
		cfg.Vaults[name] = vault
	}

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
