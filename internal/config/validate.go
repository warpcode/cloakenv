package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"strings"
	texttemplate "text/template"

	goyaml "gopkg.in/yaml.v3"

	"github.com/warpcode/cloakenv/internal/yaml"
)

// ValidationResult holds issues found during configuration validation.
type ValidationResult struct {
	Errors   []string
	Warnings []string
}

// IsValid returns true if there are no validation errors.
func (vr *ValidationResult) IsValid() bool {
	return len(vr.Errors) == 0
}

// Validate checks a configuration file for syntax, schema conformity, file existence, regex compilation, and template syntax.
func Validate(configPath string, strict bool) (*ValidationResult, error) {
	res := &ValidationResult{}

	data, err := os.ReadFile(configPath) //nolint:gosec // user-configured config path
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			res.Errors = append(res.Errors, fmt.Sprintf("configuration file does not exist: %s", configPath))
			return res, nil
		}
		return nil, fmt.Errorf("failed to read configuration file: %w", err)
	}

	// 1. YAML Syntax Check & Strict Schema Check
	if strict {
		decoder := goyaml.NewDecoder(bytes.NewReader(data))
		decoder.KnownFields(true)
		var testCfg Config
		if err := decoder.Decode(&testCfg); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("schema validation error: %v", err))
		}
	} else {
		var testCfg Config
		if err := yaml.Unmarshal(data, &testCfg); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("yaml syntax error: %v", err))
			return res, nil
		}
	}

	// Load configuration using the standard Load method to resolve paths
	cfg, err := Load(configPath)
	if err != nil {
		res.Errors = append(res.Errors, err.Error())
		return res, nil
	}

	baseDir := filepath.Dir(configPath)

	// 2. Validate Vaults
	validProviders := map[string]bool{
		"keepass":      true,
		"yaml":         true,
		"json":         true,
		"custom_vault": true,
		"search":       true,
	}

	for vaultName, vault := range cfg.Vaults {
		if vault.Provider == "" {
			res.Errors = append(res.Errors, fmt.Sprintf("vault %q: missing required field 'provider'", vaultName))
		} else if !validProviders[vault.Provider] {
			res.Errors = append(res.Errors, fmt.Sprintf("vault %q: unknown provider %q (must be one of: keepass, yaml, json, custom_vault, search)", vaultName, vault.Provider))
		}

		// File path existence check for file-backed providers
		if vault.Provider == "keepass" || vault.Provider == "yaml" || vault.Provider == "json" {
			if vault.VaultPath == "" {
				res.Errors = append(res.Errors, fmt.Sprintf("vault %q: provider %q requires 'vault_path'", vaultName, vault.Provider))
			} else {
				if _, err := os.Stat(vault.VaultPath); err != nil {
					if errors.Is(err, os.ErrNotExist) {
						res.Errors = append(res.Errors, fmt.Sprintf("vault %q: vault_path does not exist: %s", vaultName, vault.VaultPath))
					} else {
						res.Errors = append(res.Errors, fmt.Sprintf("vault %q: cannot access vault_path %s: %v", vaultName, vault.VaultPath, err))
					}
				}
			}
		}

		// Search provider validation
		if vault.Provider == "search" {
			if len(vault.SourceVaults) == 0 {
				res.Warnings = append(res.Warnings, fmt.Sprintf("vault %q: search provider has no 'source_vaults' configured", vaultName))
			}
		}

		// KeyFormat validation
		switch vault.KeyFormat {
		case "", "none", "env", "snake", "kebab":
		default:
			res.Errors = append(res.Errors, fmt.Sprintf("vault %q: invalid key_format %q (must be one of: none, env, snake, kebab)", vaultName, vault.KeyFormat))
		}

		// Cache configuration validation
		if vault.Cache.Enabled {
			switch vault.Cache.Mode {
			case "", "keyring", "encrypted_disk":
			default:
				res.Errors = append(res.Errors, fmt.Sprintf("vault %q: invalid cache mode %q (must be one of: keyring, encrypted_disk)", vaultName, vault.Cache.Mode))
			}
		}

		// Glob patterns
		for _, pattern := range vault.IncludeFields {
			if _, err := pathpkg.Match(pattern, ""); err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("vault %q: invalid include_fields pattern %q: %v", vaultName, pattern, err))
			}
		}
		for _, pattern := range vault.ExcludeFields {
			if _, err := pathpkg.Match(pattern, ""); err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("vault %q: invalid exclude_fields pattern %q: %v", vaultName, pattern, err))
			}
		}

		// Mapping rule regex validation
		for i, rule := range vault.Mapping {
			if rule.Match == "" {
				res.Errors = append(res.Errors, fmt.Sprintf("vault %q: mapping rule #%d missing 'match' regex", vaultName, i+1))
			} else if _, err := regexp.Compile(rule.Match); err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("vault %q: invalid mapping regex %q: %v", vaultName, rule.Match, err))
			}
		}

		// Validate custom_vault entities
		for entityName, entity := range vault.Entities {
			for _, pattern := range entity.IncludeFields {
				if _, err := pathpkg.Match(pattern, ""); err != nil {
					res.Errors = append(res.Errors, fmt.Sprintf("vault %q entity %q: invalid include_fields pattern %q: %v", vaultName, entityName, pattern, err))
				}
			}
			for _, pattern := range entity.ExcludeFields {
				if _, err := pathpkg.Match(pattern, ""); err != nil {
					res.Errors = append(res.Errors, fmt.Sprintf("vault %q entity %q: invalid exclude_fields pattern %q: %v", vaultName, entityName, pattern, err))
				}
			}
			for i, rule := range entity.Mapping {
				if rule.Match == "" {
					res.Errors = append(res.Errors, fmt.Sprintf("vault %q entity %q: mapping rule #%d missing 'match' regex", vaultName, entityName, i+1))
				} else if _, err := regexp.Compile(rule.Match); err != nil {
					res.Errors = append(res.Errors, fmt.Sprintf("vault %q entity %q: invalid mapping regex %q: %v", vaultName, entityName, rule.Match, err))
				}
			}

			// Validate entity templates in attributes
			for attrKey, attrVal := range entity.Attributes {
				if strVal, ok := attrVal.(string); ok && strings.Contains(strVal, "{{") {
					tmpl := texttemplate.New(attrKey).Funcs(StandardTemplateFuncs())
					if _, err := tmpl.Parse(strVal); err != nil {
						res.Errors = append(res.Errors, fmt.Sprintf("vault %q entity %q attribute %q: template syntax error: %v", vaultName, entityName, attrKey, err))
					}
				}
			}
		}

		// Validate vault-level attributes templates
		for attrKey, attrVal := range vault.Attributes {
			if strVal, ok := attrVal.(string); ok && strings.Contains(strVal, "{{") {
				tmpl := texttemplate.New(attrKey).Funcs(StandardTemplateFuncs())
				if _, err := tmpl.Parse(strVal); err != nil {
					res.Errors = append(res.Errors, fmt.Sprintf("vault %q attribute %q: template syntax error: %v", vaultName, attrKey, err))
				}
			}
		}
	}

	// 3. Validate Templates
	for tmplName, tmpl := range cfg.Templates {
		if tmpl.Path != "" {
			resolvedPath := tmpl.Path
			if !filepath.IsAbs(resolvedPath) {
				resolvedPath = filepath.Join(baseDir, resolvedPath)
			}
			if _, err := os.Stat(resolvedPath); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					res.Errors = append(res.Errors, fmt.Sprintf("template %q: file does not exist: %s", tmplName, tmpl.Path))
				} else {
					res.Errors = append(res.Errors, fmt.Sprintf("template %q: cannot access path %s: %v", tmplName, tmpl.Path, err))
				}
			}
		}
		if tmpl.Template != "" {
			t := texttemplate.New(tmplName).Funcs(StandardTemplateFuncs())
			if _, err := t.Parse(tmpl.Template); err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("template %q: template syntax error: %v", tmplName, err))
			}
		}
	}

	// 4. Validate Autoload Rules
	for i, rule := range cfg.Autoload {
		if rule.Match == "" {
			res.Errors = append(res.Errors, fmt.Sprintf("autoload rule #%d: missing 'match' pattern", i+1))
		} else {
			if _, err := regexp.Compile(rule.Match); err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("autoload rule #%d: invalid regex pattern %q: %v", i+1, rule.Match, err))
			}
		}
	}

	return res, nil
}
