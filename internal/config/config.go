// Package config handles parsing of the global cloakenv configuration
// file at ~/.config/cloakenv/config.yaml.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

var userHomeDir = os.UserHomeDir

// Config is the top-level configuration structure.
type Config struct {
	// ConfigPath is the absolute path to the configuration file.
	ConfigPath string                 `yaml:"-"`
	Cache      CacheConfig            `yaml:"cache"`
	Keyring    KeyringConfig          `yaml:"keyring"`
	Vaults     map[string]VaultConfig `yaml:"vaults"`
}

// CacheConfig holds cache-related configuration settings.
type CacheConfig struct {
	DefaultTTL string `yaml:"default_ttl"`
}

// KeyringConfig holds keyring-related configuration settings.
type KeyringConfig struct {
	Prefix string `yaml:"prefix"`
}

// VaultConfig defines a named secret vault backend and its configuration options.
type VaultConfig struct {
	// Provider identifies the provider backend type (e.g., "keepass", "custom_vault").
	Provider string `yaml:"provider"`

	// DatabasePath is the filesystem path to the backend's data store
	// (e.g., the .kdbx file for keepass providers).
	DatabasePath string `yaml:"database_path"`

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

	data, err := os.ReadFile(path)
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

	// Expand ~ in all vault database paths
	for name, vault := range cfg.Vaults {
		vault.DatabasePath = expandHome(vault.DatabasePath)
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
