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

	// Reject legacy vault output/transformation keys (and unknown template keys in strict mode) via raw YAML AST inspection
	var rawDoc goyaml.Node
	if err := goyaml.Unmarshal(data, &rawDoc); err == nil && len(rawDoc.Content) > 0 {
		checkLegacyVaultKeys(rawDoc.Content[0], res)
		if strict {
			checkStrictTemplateKeys(rawDoc.Content[0], res)
		}
	}

	// Load configuration without aborting on missing/empty template files so all validation errors aggregate
	cfg, err := load(configPath, false)
	if err != nil {
		res.Errors = append(res.Errors, err.Error())
		return res, nil
	}

	baseDir := filepath.Dir(configPath)

	// 2. Validate Vaults
	validProviders := map[string]bool{
		"keepass": true,
		"pass":    true,
		"vault":   true,
		"env":     true,
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
			res.Errors = append(res.Errors, fmt.Sprintf("vault %q: unknown provider %q (must be one of: keepass, pass, vault, env, keyring)", vaultName, vault.Provider))
		} else if (vaultName == "env" && vault.Provider != "env") || (vaultName == "keyring" && vault.Provider != "keyring") {
			res.Errors = append(res.Errors, fmt.Sprintf("vault %q: built-in vault name %q must use provider %q, got %q", vaultName, vaultName, vaultName, vault.Provider))
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
			if tmpl.Path != "" && tmpl.Template != "" {
				res.Errors = append(res.Errors, fmt.Sprintf("template %q: cannot specify both 'template' and 'path'", tmplName))
			}
			if tmpl.Template != "" {
				t := texttemplate.New(tmplName).Funcs(StandardTemplateFuncs())
				if _, err := t.Parse(tmpl.Template); err != nil {
					res.Errors = append(res.Errors, fmt.Sprintf("template %q: template syntax error: %v", tmplName, err))
				}
			}
			if tmpl.Path != "" {
				resolvedPath := expandHome(tmpl.Path)
				if !filepath.IsAbs(resolvedPath) {
					resolvedPath = filepath.Join(baseDir, resolvedPath)
				}
				resolvedPath = filepath.Clean(resolvedPath)
				data, err := os.ReadFile(resolvedPath)
				if err != nil {
					if errors.Is(err, os.ErrNotExist) {
						res.Errors = append(res.Errors, fmt.Sprintf("template %q: file does not exist: %s", tmplName, tmpl.Path))
					} else {
						res.Errors = append(res.Errors, fmt.Sprintf("template %q: cannot access path %s: %v", tmplName, tmpl.Path, err))
					}
				} else if strings.TrimSpace(string(data)) == "" {
					res.Errors = append(res.Errors, fmt.Sprintf("template %q: template file %q is empty", tmplName, tmpl.Path))
				} else {
					t := texttemplate.New(tmplName).Funcs(StandardTemplateFuncs())
					if _, err := t.Parse(string(data)); err != nil {
						res.Errors = append(res.Errors, fmt.Sprintf("template %q: template syntax error: %v", tmplName, err))
					}
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
		ruleLabel := fmt.Sprintf("autoload rule #%d", i+1)
		if strings.TrimSpace(rule.Match) == "" {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: missing 'match' pattern", ruleLabel))
		} else if _, err := regexp.Compile(rule.Match); err != nil && !isWildcardAutoloadPattern(rule.Match) {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: invalid regex pattern %q: %v", ruleLabel, rule.Match, err))
		}

		for j, item := range rule.Envs {
			if item.Name != "" {
				if _, exists := cfg.Envs[item.Name]; !exists {
					res.Errors = append(res.Errors, fmt.Sprintf("%s env #%d: references unknown env %q", ruleLabel, j+1, item.Name))
				}
			} else if len(item.Inline) > 0 {
				inlineKeys := make([]string, 0, len(item.Inline))
				for k := range item.Inline {
					inlineKeys = append(inlineKeys, k)
				}
				slices.Sort(inlineKeys)
				for _, k := range inlineKeys {
					if strings.TrimSpace(k) == "" {
						res.Errors = append(res.Errors, fmt.Sprintf("%s env #%d: inline env key cannot be empty or whitespace", ruleLabel, j+1))
						continue
					}
					if strings.ContainsRune(k, '=') {
						res.Errors = append(res.Errors, fmt.Sprintf("%s env #%d: inline env key %q cannot contain '='", ruleLabel, j+1, k))
						continue
					}
					v := item.Inline[k]
					if strings.Contains(v, "{{") {
						if err := validateTemplateSyntax(fmt.Sprintf("%s env #%d key %q", ruleLabel, j+1, k), v); err != nil {
							res.Errors = append(res.Errors, err.Error())
						}
					}
				}
			} else {
				res.Errors = append(res.Errors, fmt.Sprintf("%s env #%d: env item cannot be empty", ruleLabel, j+1))
			}
		}

		seenEnvVars := make(map[string]bool)
		seenLiteralPaths := make(map[string]bool)
		for j, tmplAtt := range rule.Templates {
			attLabel := fmt.Sprintf("%s template #%d", ruleLabel, j+1)
			if strings.TrimSpace(tmplAtt.Template) == "" {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: missing required field 'template'", attLabel))
			} else if tmplAtt.Template != strings.TrimSpace(tmplAtt.Template) {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: template name %q must not have leading or trailing whitespace", attLabel, tmplAtt.Template))
			} else if _, exists := cfg.Templates[tmplAtt.Template]; !exists {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: references unknown template %q", attLabel, tmplAtt.Template))
			}
			if tmplAtt.Env != "" {
				if strings.TrimSpace(tmplAtt.Env) == "" || strings.ContainsAny(tmplAtt.Env, " \t\r\n=") {
					res.Errors = append(res.Errors, fmt.Sprintf("%s: invalid 'env' variable name %q (must not contain whitespace or '=')", attLabel, tmplAtt.Env))
				} else if seenEnvVars[tmplAtt.Env] {
					res.Errors = append(res.Errors, fmt.Sprintf("%s: duplicate environment variable target %q in autoload rule", attLabel, tmplAtt.Env))
				} else {
					seenEnvVars[tmplAtt.Env] = true
				}
			}
			if tmplAtt.EnvPath != "" {
				if strings.TrimSpace(tmplAtt.EnvPath) == "" || strings.ContainsAny(tmplAtt.EnvPath, " \t\r\n=") {
					res.Errors = append(res.Errors, fmt.Sprintf("%s: invalid 'env_path' variable name %q (must not contain whitespace or '=')", attLabel, tmplAtt.EnvPath))
				} else if seenEnvVars[tmplAtt.EnvPath] {
					res.Errors = append(res.Errors, fmt.Sprintf("%s: duplicate environment variable target %q in autoload rule", attLabel, tmplAtt.EnvPath))
				} else {
					seenEnvVars[tmplAtt.EnvPath] = true
				}
			}
			if tmplAtt.Path != "" {
				if strings.TrimSpace(tmplAtt.Path) == "" || tmplAtt.Path != strings.TrimSpace(tmplAtt.Path) {
					res.Errors = append(res.Errors, fmt.Sprintf("%s: invalid 'path' %q (must not be empty or have leading/trailing whitespace)", attLabel, tmplAtt.Path))
				} else if strings.Contains(tmplAtt.Path, "{{") {
					if err := validateTemplateSyntax(fmt.Sprintf("%s path", attLabel), tmplAtt.Path); err != nil {
						res.Errors = append(res.Errors, err.Error())
					}
				} else {
					normPath := filepath.Clean(expandHome(tmplAtt.Path))
					if seenLiteralPaths[normPath] {
						res.Errors = append(res.Errors, fmt.Sprintf("%s: duplicate literal path target %q in autoload rule", attLabel, tmplAtt.Path))
					} else {
						seenLiteralPaths[normPath] = true
					}
				}
			}
			hasTarget := strings.TrimSpace(tmplAtt.Env) != "" || strings.TrimSpace(tmplAtt.EnvPath) != "" || strings.TrimSpace(tmplAtt.Path) != ""
			if !hasTarget {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: must set at least one of 'env', 'env_path', or 'path'", attLabel))
			}
			if tmplAtt.Env != "" && (tmplAtt.EnvPath != "" || tmplAtt.Path != "") {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: cannot combine 'env' with 'env_path' or 'path'", attLabel))
			}
			if tmplAtt.Persist && tmplAtt.Env != "" {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: 'persist: true' cannot be combined with 'env' (no file is written)", attLabel))
			}
		}
	}

	return res, nil
}

var validTemplateKeys = map[string]bool{
	"driver":   true,
	"path":     true,
	"template": true,
	"from_env": true,
	"vaults":   true,
}

var validVaultSourceKeys = map[string]bool{
	"vault": true,
	"query": true,
	"rules": true,
}

var validRuleKeys = map[string]bool{
	"type":        true,
	"include":     true,
	"exclude":     true,
	"key_format":  true,
	"match":       true,
	"value_match": true,
	"key":         true,
	"value":       true,
	"values":      true,
}

var validVaultCacheKeys = map[string]bool{
	"enabled": true,
	"ttl":     true,
	"mode":    true,
}

func derefYAMLNode(n *goyaml.Node) *goyaml.Node {
	for n != nil && n.Kind == goyaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	return n
}

func checkStrictVaultSourcesKeys(owner string, seqNode *goyaml.Node, res *ValidationResult) {
	seqNode = derefYAMLNode(seqNode)
	if seqNode == nil || seqNode.Kind != goyaml.SequenceNode {
		return
	}
	for sIdx, item := range seqNode.Content {
		srcMap := derefYAMLNode(item)
		if srcMap == nil || srcMap.Kind != goyaml.MappingNode {
			continue
		}
		srcLabel := fmt.Sprintf("%s source #%d", owner, sIdx+1)
		for k := 0; k+1 < len(srcMap.Content); k += 2 {
			fieldKey := srcMap.Content[k].Value
			if !validVaultSourceKeys[fieldKey] {
				res.Errors = append(res.Errors, fmt.Sprintf("schema validation error: %s: unknown field %q", srcLabel, fieldKey))
				continue
			}
			if fieldKey == "rules" {
				rulesSeq := derefYAMLNode(srcMap.Content[k+1])
				if rulesSeq == nil || rulesSeq.Kind != goyaml.SequenceNode {
					continue
				}
				for rIdx, rItem := range rulesSeq.Content {
					rMap := derefYAMLNode(rItem)
					if rMap == nil || rMap.Kind != goyaml.MappingNode {
						continue
					}
					for rk := 0; rk+1 < len(rMap.Content); rk += 2 {
						rField := rMap.Content[rk].Value
						if !validRuleKeys[rField] {
							res.Errors = append(res.Errors, fmt.Sprintf("schema validation error: %s rule #%d: unknown field %q", srcLabel, rIdx+1, rField))
						}
					}
				}
			}
		}
	}
}

func checkStrictTemplateKeys(doc *goyaml.Node, res *ValidationResult) {
	doc = derefYAMLNode(doc)
	if doc == nil || doc.Kind != goyaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(doc.Content); i += 2 {
		switch doc.Content[i].Value {
		case "templates":
			tmplsNode := derefYAMLNode(doc.Content[i+1])
			if tmplsNode == nil || tmplsNode.Kind != goyaml.MappingNode {
				continue
			}
			for j := 0; j+1 < len(tmplsNode.Content); j += 2 {
				tmplName := tmplsNode.Content[j].Value
				tmplMap := derefYAMLNode(tmplsNode.Content[j+1])
				if tmplMap == nil || tmplMap.Kind != goyaml.MappingNode {
					continue
				}
				for k := 0; k+1 < len(tmplMap.Content); k += 2 {
					fieldKey := tmplMap.Content[k].Value
					if !validTemplateKeys[fieldKey] {
						res.Errors = append(res.Errors, fmt.Sprintf("schema validation error: template %q: unknown field %q", tmplName, fieldKey))
						continue
					}
					if fieldKey == "vaults" {
						checkStrictVaultSourcesKeys(fmt.Sprintf("template %q", tmplName), tmplMap.Content[k+1], res)
					}
				}
			}
		case "vaults":
			vaultsNode := derefYAMLNode(doc.Content[i+1])
			if vaultsNode == nil || vaultsNode.Kind != goyaml.MappingNode {
				continue
			}
			for j := 0; j+1 < len(vaultsNode.Content); j += 2 {
				vaultName := vaultsNode.Content[j].Value
				vaultMap := derefYAMLNode(vaultsNode.Content[j+1])
				if vaultMap == nil || vaultMap.Kind != goyaml.MappingNode {
					continue
				}
				for k := 0; k+1 < len(vaultMap.Content); k += 2 {
					if vaultMap.Content[k].Value != "cache" {
						continue
					}
					cacheMap := derefYAMLNode(vaultMap.Content[k+1])
					if cacheMap == nil || cacheMap.Kind != goyaml.MappingNode {
						continue
					}
					for ck := 0; ck+1 < len(cacheMap.Content); ck += 2 {
						cacheKey := cacheMap.Content[ck].Value
						if !validVaultCacheKeys[cacheKey] {
							res.Errors = append(res.Errors, fmt.Sprintf("schema validation error: vault %q cache: unknown field %q", vaultName, cacheKey))
						}
					}
				}
			}
		}
	}
}

func checkLegacyVaultKeys(doc *goyaml.Node, res *ValidationResult) {
	doc = derefYAMLNode(doc)
	if doc == nil || doc.Kind != goyaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(doc.Content); i += 2 {
		if doc.Content[i].Value != "vaults" {
			continue
		}
		vaultsNode := derefYAMLNode(doc.Content[i+1])
		if vaultsNode == nil || vaultsNode.Kind != goyaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(vaultsNode.Content); j += 2 {
			vaultName := vaultsNode.Content[j].Value
			vaultMap := derefYAMLNode(vaultsNode.Content[j+1])
			if vaultMap == nil || vaultMap.Kind != goyaml.MappingNode {
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

		vaultCfg, declaredVault := cfg.Vaults[src.Vault]
		isFlatVault := (!declaredVault && (src.Vault == "env" || src.Vault == "keyring")) ||
			(declaredVault && (vaultCfg.Provider == "env" || vaultCfg.Provider == "keyring"))
		if isFlatVault && len(src.Rules) == 0 {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s (%s): implicit wildcard pass without explicit 'include' globs may expose the full process environment or keyring", srcLabel, src.Vault))
		}

		for rIdx, rule := range src.Rules {
			ruleLabel := fmt.Sprintf("%s rule #%d", srcLabel, rIdx+1)
			switch rule.Type {
			case "pass":
				if rule.Match != "" || rule.ValueMatch != "" || rule.Key != "" || rule.Value != "" {
					res.Errors = append(res.Errors, fmt.Sprintf("%s: pass rule cannot specify match/value_match/key/value fields", ruleLabel))
				}
				if len(rule.Values) > 0 {
					res.Errors = append(res.Errors, fmt.Sprintf("%s: pass rule cannot specify 'values' field", ruleLabel))
				}
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
				if isFlatVault && len(rule.Include) == 0 {
					res.Warnings = append(res.Warnings, fmt.Sprintf("%s (%s): pass rule without explicit 'include' globs may expose the full process environment or keyring", ruleLabel, src.Vault))
				}
			case "match":
				if len(rule.Include) > 0 || len(rule.Exclude) > 0 || rule.KeyFormat != "" {
					res.Errors = append(res.Errors, fmt.Sprintf("%s: match rule cannot specify include/exclude/key_format fields", ruleLabel))
				}
				if len(rule.Values) > 0 {
					res.Errors = append(res.Errors, fmt.Sprintf("%s: match rule cannot specify 'values' field", ruleLabel))
				}
				if rule.Match == "" && rule.ValueMatch == "" {
					res.Errors = append(res.Errors, fmt.Sprintf("%s: match rule requires 'match' or 'value_match'", ruleLabel))
				}
				var keyCapCount, valCapCount int
				if rule.Match != "" {
					if re, err := regexp.Compile(rule.Match); err != nil {
						res.Errors = append(res.Errors, fmt.Sprintf("%s: invalid match regex %q: %v", ruleLabel, rule.Match, err))
						keyCapCount = -1
					} else {
						keyCapCount = re.NumSubexp() + 1
					}
				}
				if rule.ValueMatch != "" {
					if re, err := regexp.Compile(rule.ValueMatch); err != nil {
						res.Errors = append(res.Errors, fmt.Sprintf("%s: invalid value_match regex %q: %v", ruleLabel, rule.ValueMatch, err))
						valCapCount = -1
					} else {
						valCapCount = re.NumSubexp() + 1
					}
				}
				keyPrimaryCapCount := keyCapCount
				if keyPrimaryCapCount == 0 {
					keyPrimaryCapCount = valCapCount
				}
				valPrimaryCapCount := valCapCount
				if valPrimaryCapCount == 0 {
					valPrimaryCapCount = keyCapCount
				}
				if rule.Key == "" {
					res.Errors = append(res.Errors, fmt.Sprintf("%s: match rule missing 'key'", ruleLabel))
				} else if strings.Contains(rule.Key, "{{") {
					safeKey := RewriteCapturePlaceholdersForTemplate(rule.Key, keyPrimaryCapCount)
					if err := validateTemplateSyntax(fmt.Sprintf("%s key", ruleLabel), safeKey); err != nil {
						res.Errors = append(res.Errors, err.Error())
					}
				}
				if strings.Contains(rule.Value, "{{") {
					safeVal := RewriteCapturePlaceholdersForTemplate(rule.Value, valPrimaryCapCount)
					if err := validateTemplateSyntax(fmt.Sprintf("%s value", ruleLabel), safeVal); err != nil {
						res.Errors = append(res.Errors, err.Error())
					}
				}
			case "set":
				if len(rule.Include) > 0 || len(rule.Exclude) > 0 || rule.KeyFormat != "" {
					res.Errors = append(res.Errors, fmt.Sprintf("%s: set rule cannot specify include/exclude/key_format fields", ruleLabel))
				}
				if rule.Match != "" || rule.ValueMatch != "" || rule.Key != "" || rule.Value != "" {
					res.Errors = append(res.Errors, fmt.Sprintf("%s: set rule cannot specify match/value_match/key/value fields", ruleLabel))
				}
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
