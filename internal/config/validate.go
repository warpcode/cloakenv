package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	texttemplate "text/template"

	goyaml "gopkg.in/yaml.v3"

	"github.com/warpcode/cloakenv/internal/utils"
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

var legacyVaultKeys = map[string]string{
	"include_fields":    "use 'rules:' with 'type: pass' and 'include:' inside 'envs:' or 'templates:'",
	"exclude_fields":    "use 'rules:' with 'type: pass' and 'exclude:' inside 'envs:' or 'templates:'",
	"mapping":           "use 'rules:' with 'type: match' inside 'envs:' or 'templates:'",
	"key_format":        "use 'rules:' with 'type: pass' and 'key_format:' inside 'envs:' or 'templates:'",
	"attributes":        "use 'rules:' with 'type: set' inside 'envs:' or 'templates:'",
	"entities":          "use 'vaults:' with supported providers and 'envs:'/'templates:' rules",
	"single_entity":     "use flat entry retrieval in 'envs:' or 'templates:'",
	"entity_name":       "use 'envs:' or 'templates:' with 'query:'/'rules:'",
	"entities_root_key": "plaintext json/yaml vaults are removed; use supported secret backends",
	"searchable":        "use 'query:' inside 'envs:' or 'templates:' vault sources",
	"resolve_values":    "use 'rules:' with templates inside 'envs:' or 'templates:'",
	"source_vaults":     "list multiple sources directly under 'envs:' or 'templates:'",
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
		if err := decoder.Decode(&testCfg); err != nil && !errors.Is(err, io.EOF) {
			res.Errors = append(res.Errors, fmt.Sprintf("schema validation error: %v", err))
		}
	} else {
		var testCfg Config
		if err := utils.UnmarshalYAML(data, &testCfg); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("yaml syntax error: %v", err))
			return res, nil
		}
	}

	// Reject legacy vault output/transformation keys via raw YAML AST inspection
	var rawDoc goyaml.Node
	if err := goyaml.Unmarshal(data, &rawDoc); err == nil && len(rawDoc.Content) > 0 {
		checkLegacyVaultKeys(rawDoc.Content[0], res)
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
		"keepass": true,
		"env":     true,
		"pass":    true,
		"search":  true,
		"vault":   true,
		"keyring": true,
	}

	vaultNames := make([]string, 0, len(cfg.Vaults))
	for vaultName := range cfg.Vaults {
		vaultNames = append(vaultNames, vaultName)
	}
	slices.Sort(vaultNames)

	for _, vaultName := range vaultNames {
		vault := cfg.Vaults[vaultName]
		if vault.Provider == "" {
			res.Errors = append(res.Errors, fmt.Sprintf("vault %q: missing required field 'provider'", vaultName))
		} else if !validProviders[vault.Provider] {
			res.Errors = append(res.Errors, fmt.Sprintf("vault %q: unsupported provider %q (must be one of: keepass, env, pass, search, vault, keyring)", vaultName, vault.Provider))
		}

		// File path existence check for file-backed providers
		if vault.Provider == "keepass" {
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
			if len(vault.SourceVaults) == 0 && len(vault.Sources) == 0 {
				res.Warnings = append(res.Warnings, fmt.Sprintf("vault %q: search provider has no 'source_vaults' or 'sources' configured", vaultName))
			}
		}

		// KeyFormat validation
		switch vault.KeyFormat {
		case "", "none", "env", "snake", "kebab":
		default:
			res.Errors = append(res.Errors, fmt.Sprintf("vault %q: invalid key_format %q (must be one of: none, env, snake, kebab)", vaultName, vault.KeyFormat))
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

		// Cache configuration validation
		if vault.Cache.Enabled {
			switch vault.Cache.Mode {
			case "", "keyring", "encrypted_disk":
			default:
				res.Errors = append(res.Errors, fmt.Sprintf("vault %q: invalid cache mode %q (must be one of: keyring, encrypted_disk)", vaultName, vault.Cache.Mode))
			}
		}
	}

	// 3. Validate Envs
	envNames := make([]string, 0, len(cfg.Envs))
	for envName := range cfg.Envs {
		envNames = append(envNames, envName)
	}
	slices.Sort(envNames)

	for _, envName := range envNames {
		sources := cfg.Envs[envName]
		if len(sources) == 0 {
			res.Warnings = append(res.Warnings, fmt.Sprintf("env %q: no vault sources configured", envName))
			continue
		}
		validateVaultSources(fmt.Sprintf("env %q", envName), sources, cfg, res)
	}

	// 4. Validate Templates
	tmplNames := make([]string, 0, len(cfg.Templates))
	for tmplName := range cfg.Templates {
		tmplNames = append(tmplNames, tmplName)
	}
	slices.Sort(tmplNames)

	for _, tmplName := range tmplNames {
		tmpl := cfg.Templates[tmplName]
		driver := tmpl.EffectiveDriver()
		switch driver {
		case "template":
			if tmpl.FromEnv != "" {
				res.Errors = append(res.Errors, fmt.Sprintf("template %q: 'from_env' is only valid when driver is `dotenv`", tmplName))
			}
			if tmpl.Path == "" && tmpl.Template == "" {
				res.Errors = append(res.Errors, fmt.Sprintf("template %q: driver `template` requires 'template' or 'path'", tmplName))
			}
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
		case "json", "yaml":
			if tmpl.Template != "" || tmpl.Path != "" {
				res.Errors = append(res.Errors, fmt.Sprintf("template %q: 'template' and 'path' cannot be used with driver %q", tmplName, driver))
			}
			if tmpl.FromEnv != "" {
				res.Errors = append(res.Errors, fmt.Sprintf("template %q: 'from_env' is only valid when driver is `dotenv`", tmplName))
			}
			if len(tmpl.Vaults) == 0 {
				res.Warnings = append(res.Warnings, fmt.Sprintf("template %q: no vault sources configured", tmplName))
			}
		case "dotenv":
			if tmpl.Template != "" || tmpl.Path != "" {
				res.Errors = append(res.Errors, fmt.Sprintf("template %q: 'template' and 'path' cannot be used with driver `dotenv`", tmplName))
			}
			if tmpl.FromEnv != "" {
				if _, exists := cfg.Envs[tmpl.FromEnv]; !exists {
					res.Errors = append(res.Errors, fmt.Sprintf("template %q: from_env references unknown env %q", tmplName, tmpl.FromEnv))
				}
			} else if len(tmpl.Vaults) == 0 {
				res.Warnings = append(res.Warnings, fmt.Sprintf("template %q: no vaults or from_env configured", tmplName))
			}
		default:
			res.Errors = append(res.Errors, fmt.Sprintf("template %q: invalid driver %q (must be one of: template, json, yaml, dotenv)", tmplName, driver))
		}

		validateVaultSources(fmt.Sprintf("template %q", tmplName), tmpl.Vaults, cfg, res)
	}

	// 5. Validate Autoload Rules
	for i, rule := range cfg.Autoload {
		if strings.TrimSpace(rule.Match) == "" {
			res.Errors = append(res.Errors, fmt.Sprintf("autoload rule #%d: missing 'match' pattern", i+1))
		} else if _, err := regexp.Compile(rule.Match); err != nil && !isWildcardAutoloadPattern(rule.Match) {
			res.Errors = append(res.Errors, fmt.Sprintf("autoload rule #%d: invalid regex pattern %q: %v", i+1, rule.Match, err))
		}
	}

	return res, nil
}

func checkLegacyVaultKeys(doc *goyaml.Node, res *ValidationResult) {
	if doc == nil || doc.Kind != goyaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(doc.Content); i += 2 {
		if doc.Content[i].Value != "vaults" {
			continue
		}
		vaultsNode := doc.Content[i+1]
		if vaultsNode.Kind != goyaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(vaultsNode.Content); j += 2 {
			vaultName := vaultsNode.Content[j].Value
			vaultMap := vaultsNode.Content[j+1]
			if vaultMap.Kind != goyaml.MappingNode {
				continue
			}
			for k := 0; k+1 < len(vaultMap.Content); k += 2 {
				fieldKey := vaultMap.Content[k].Value
				if hint, isLegacy := legacyVaultKeys[fieldKey]; isLegacy {
					res.Errors = append(res.Errors, fmt.Sprintf("vault %q: legacy key %q is no longer supported (%s)", vaultName, fieldKey, hint))
				}
			}
		}
	}
}

func validateVaultSources(owner string, sources []VaultSourceConfig, cfg *Config, res *ValidationResult) {
	for idx, src := range sources {
		srcLabel := fmt.Sprintf("%s source #%d", owner, idx+1)
		if src.Vault == "" {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: missing required field 'vault'", srcLabel))
		} else {
			_, declared := cfg.Vaults[src.Vault]
			isImplicit := src.Vault == "env" || src.Vault == "keyring"
			if !declared && !isImplicit {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: references unknown vault %q", srcLabel, src.Vault))
			}
		}

		if src.Query != "" {
			if err := validateTemplateSyntax(fmt.Sprintf("%s query", srcLabel), src.Query); err != nil {
				res.Errors = append(res.Errors, err.Error())
			}
		}

		isFlatImplicit := src.Vault == "env" || src.Vault == "keyring"
		if isFlatImplicit && len(src.Rules) == 0 {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s (%s): implicit wildcard pass without explicit 'include' globs may expose the full process environment or keyring", srcLabel, src.Vault))
		}

		for rIdx, rule := range src.Rules {
			ruleLabel := fmt.Sprintf("%s rule #%d", srcLabel, rIdx+1)
			switch rule.Type {
			case "pass":
				for _, pattern := range rule.Include {
					if _, err := pathpkg.Match(pattern, ""); err != nil {
						res.Errors = append(res.Errors, fmt.Sprintf("%s: invalid include glob pattern %q: %v", ruleLabel, pattern, err))
					}
				}
				for _, pattern := range rule.Exclude {
					if _, err := pathpkg.Match(pattern, ""); err != nil {
						res.Errors = append(res.Errors, fmt.Sprintf("%s: invalid exclude glob pattern %q: %v", ruleLabel, pattern, err))
					}
				}
				switch rule.KeyFormat {
				case "", "none", "env", "snake", "kebab":
				default:
					res.Errors = append(res.Errors, fmt.Sprintf("%s: invalid key_format %q (must be one of: none, env, snake, kebab)", ruleLabel, rule.KeyFormat))
				}
				if isFlatImplicit && len(rule.Include) == 0 {
					res.Warnings = append(res.Warnings, fmt.Sprintf("%s (%s): pass rule without explicit 'include' globs may expose the full process environment or keyring", ruleLabel, src.Vault))
				}
			case "match":
				if rule.Match == "" && rule.ValueMatch == "" {
					res.Errors = append(res.Errors, fmt.Sprintf("%s: match rule requires 'match' or 'value_match'", ruleLabel))
				}
				if rule.Match != "" {
					if _, err := regexp.Compile(rule.Match); err != nil {
						res.Errors = append(res.Errors, fmt.Sprintf("%s: invalid match regex %q: %v", ruleLabel, rule.Match, err))
					}
				}
				if rule.ValueMatch != "" {
					if _, err := regexp.Compile(rule.ValueMatch); err != nil {
						res.Errors = append(res.Errors, fmt.Sprintf("%s: invalid value_match regex %q: %v", ruleLabel, rule.ValueMatch, err))
					}
				}
				if rule.Key == "" {
					res.Errors = append(res.Errors, fmt.Sprintf("%s: match rule missing 'key'", ruleLabel))
				} else if strings.Contains(rule.Key, "{{") {
					if err := validateTemplateSyntax(fmt.Sprintf("%s key", ruleLabel), rule.Key); err != nil {
						res.Errors = append(res.Errors, err.Error())
					}
				}
				if strings.Contains(rule.Value, "{{") {
					if err := validateTemplateSyntax(fmt.Sprintf("%s value", ruleLabel), rule.Value); err != nil {
						res.Errors = append(res.Errors, err.Error())
					}
				}
			case "set":
				if len(rule.Values) == 0 {
					res.Errors = append(res.Errors, fmt.Sprintf("%s: set rule requires non-empty 'values'", ruleLabel))
				}
				valKeys := make([]string, 0, len(rule.Values))
				for k := range rule.Values {
					valKeys = append(valKeys, k)
				}
				slices.Sort(valKeys)
				for _, k := range valKeys {
					v := rule.Values[k]
					if strings.Contains(k, "{{") {
						if err := validateTemplateSyntax(fmt.Sprintf("%s set key %q", ruleLabel, k), k); err != nil {
							res.Errors = append(res.Errors, err.Error())
						}
					}
					if strings.Contains(v, "{{") {
						if err := validateTemplateSyntax(fmt.Sprintf("%s set value for %q", ruleLabel, k), v); err != nil {
							res.Errors = append(res.Errors, err.Error())
						}
					}
				}
			default:
				res.Errors = append(res.Errors, fmt.Sprintf("%s: unknown rule type %q (must be one of: pass, match, set)", ruleLabel, rule.Type))
			}
		}
	}
}

func validateTemplateSyntax(label, tmplStr string) error {
	t := texttemplate.New(label).Funcs(StandardTemplateFuncs())
	if _, err := t.Parse(tmplStr); err != nil {
		return fmt.Errorf("%s: template syntax error: %w", label, err)
	}
	return nil
}

// isWildcardAutoloadPattern reports whether pattern is a plain '*' wildcard glob
// (e.g., "*.sh") supported by engine autoload fallback matching without regex syntax.
func isWildcardAutoloadPattern(pattern string) bool {
	return strings.Contains(pattern, "*") && !strings.ContainsAny(pattern, "()[]{}+?^$|\\")
}
