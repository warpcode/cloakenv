package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	tempDir := t.TempDir()
	dummyDb := filepath.Join(tempDir, "test.kdbx")
	if err := os.WriteFile(dummyDb, []byte("fake kdbx"), 0600); err != nil {
		t.Fatalf("failed to write dummy db: %v", err)
	}
	dummyTmpl := filepath.Join(tempDir, "test.tmpl")
	if err := os.WriteFile(dummyTmpl, []byte("{{ range .entries }}{{ .API_KEY }}{{ end }}"), 0600); err != nil {
		t.Fatalf("failed to write dummy tmpl: %v", err)
	}

	validYAML := `vaults:
  work:
    provider: keepass
    vault_path: ` + filepath.ToSlash(dummyDb) + `
    resolve_references: true

envs:
  litellm_env:
    - vault: work
      query: '{{ hasTag "litellm" .tags }}'
      rules:
        - type: pass
          include: ["*"]
          exclude: ["notes"]
          key_format: env
        - type: match
          match: "^(OPENAI|ANTHROPIC)_KEY$"
          key: "LLM_$1"
        - type: set
          values:
            APP_ENV: "prod"
    - vault: env
      rules:
        - type: pass
          include: ["HTTP_PROXY"]

templates:
  app:
    driver: template
    path: ` + filepath.ToSlash(dummyTmpl) + `
    vaults:
      - vault: work
  export_json:
    driver: json
    vaults:
      - vault: work
  export_yaml:
    driver: yaml
    vaults:
      - vault: work
  dotenv_cfg:
    driver: dotenv
    from_env: litellm_env

autoload:
  - match: "^aws"`
	validConfig := filepath.Join(tempDir, "valid.yaml")
	if err := os.WriteFile(validConfig, []byte(validYAML), 0600); err != nil {
		t.Fatalf("failed to write valid config: %v", err)
	}

	t.Run("valid configuration passes", func(t *testing.T) {
		res, err := Validate(validConfig, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsValid() {
			t.Fatalf("expected valid config, got errors: %v", res.Errors)
		}
		if len(res.Warnings) != 0 {
			t.Fatalf("expected zero warnings, got: %v", res.Warnings)
		}
	})

	t.Run("missing vault file fails validation", func(t *testing.T) {
		missingVaultYAML := `vaults:
  missing:
    provider: keepass
    vault_path: /path/does/not/exist.kdbx`
		cfgPath := filepath.Join(tempDir, "missing_vault.yaml")
		_ = os.WriteFile(cfgPath, []byte(missingVaultYAML), 0600)
		res, err := Validate(cfgPath, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsValid() {
			t.Fatal("expected validation failure for missing vault path, but passed")
		}
	})

	t.Run("invalid regex in autoload fails validation", func(t *testing.T) {
		badRegexYAML := `autoload:
  - match: "[unclosed regex"`
		cfgPath := filepath.Join(tempDir, "bad_regex.yaml")
		_ = os.WriteFile(cfgPath, []byte(badRegexYAML), 0600)
		res, err := Validate(cfgPath, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsValid() {
			t.Fatal("expected validation failure for bad regex, but passed")
		}
	})

	t.Run("invalid template syntax fails validation", func(t *testing.T) {
		badTmplYAML := `templates:
  bad:
    template: "{{ .foo | unknownFunc }}"`
		cfgPath := filepath.Join(tempDir, "bad_tmpl.yaml")
		_ = os.WriteFile(cfgPath, []byte(badTmplYAML), 0600)
		res, err := Validate(cfgPath, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsValid() {
			t.Fatal("expected validation failure for bad template syntax, but passed")
		}
	})

	t.Run("strict mode flags unknown fields", func(t *testing.T) {
		unknownFieldYAML := `vaults:
  work:
    provider: keepass
    vault_path: ` + filepath.ToSlash(dummyDb) + `
    unexpected_random_key: 123`
		cfgPath := filepath.Join(tempDir, "unknown_field.yaml")
		_ = os.WriteFile(cfgPath, []byte(unknownFieldYAML), 0600)
		res, err := Validate(cfgPath, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsValid() {
			t.Fatal("expected strict validation to flag unknown field, but passed")
		}
	})

	t.Run("empty config file passes in strict mode", func(t *testing.T) {
		cfgPath := filepath.Join(tempDir, "empty.yaml")
		_ = os.WriteFile(cfgPath, []byte(""), 0600)
		res, err := Validate(cfgPath, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsValid() {
			t.Fatal("expected empty config to pass in strict mode")
		}
	})

	t.Run("legacy vault output keys are rejected", func(t *testing.T) {
		for _, legacyField := range []string{
			`include_fields: ["*"]`,
			`exclude_fields: ["notes"]`,
			`key_format: env`,
			`mapping: [{match: "^Password$", key: "SECRET"}]`,
			`attributes: {foo: bar}`,
		} {
			yamlData := "vaults:\n  work:\n    provider: keepass\n    vault_path: " + filepath.ToSlash(dummyDb) + "\n    " + legacyField + "\n"
			cfgPath := filepath.Join(tempDir, "legacy_"+strings.Split(legacyField, ":")[0]+".yaml")
			if err := os.WriteFile(cfgPath, []byte(yamlData), 0600); err != nil {
				t.Fatalf("failed to write config: %v", err)
			}
			res, err := Validate(cfgPath, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.IsValid() {
				t.Fatalf("expected validation failure for legacy key %s, but passed", legacyField)
			}
			if !strings.Contains(strings.Join(res.Errors, "\n"), "legacy key") {
				t.Errorf("expected legacy key error message for %s, got: %v", legacyField, res.Errors)
			}
		}
	})

	t.Run("invalid drivers, rule types, regexes, templates, and missing references fail validation", func(t *testing.T) {
		badYAML := `vaults:
  work:
    provider: keepass
    vault_path: ` + filepath.ToSlash(dummyDb) + `

envs:
  bad_env:
    - vault: nonexistent_vault
      query: "{{ .tags | unknownFunc }}"
      rules:
        - type: unknown_rule
        - type: pass
          key_format: camelCase
          include: ["[bad-glob"]
        - type: match
          match: "[unclosed"
          value_match: "[unclosed2"
          key: "{{ .bad | missingFunc }}"
          value: "{{ .bad | missingFunc }}"
        - type: set
          values:
            "{{ .bad | missingFunc }}": "{{ .x | missingFunc }}"

templates:
  bad_driver:
    driver: xml
  bad_from_env:
    driver: dotenv
    from_env: missing_env_ref`
		cfgPath := filepath.Join(tempDir, "bad_pipeline.yaml")
		_ = os.WriteFile(cfgPath, []byte(badYAML), 0600)
		res, err := Validate(cfgPath, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsValid() {
			t.Fatal("expected validation errors, got none")
		}
		joined := strings.Join(res.Errors, "\n")
		for _, want := range []string{
			`unknown vault "nonexistent_vault"`,
			"template syntax error",
			`unknown rule type "unknown_rule"`,
			`invalid key_format "camelCase"`,
			"invalid include glob pattern",
			"invalid match regex",
			"invalid value_match regex",
			`invalid driver "xml"`,
			`unknown env "missing_env_ref"`,
		} {
			if !strings.Contains(joined, want) {
				t.Errorf("expected errors to contain %q, got:\n%s", want, joined)
			}
		}
	})

	t.Run("unsupported providers fail validation", func(t *testing.T) {
		for _, prov := range []string{"yaml", "json", "custom_vault"} {
			cfgYAML := "vaults:\n  bad:\n    provider: " + prov + "\n"
			cfgPath := filepath.Join(tempDir, "unsupported_"+prov+".yaml")
			_ = os.WriteFile(cfgPath, []byte(cfgYAML), 0600)

			res, err := Validate(cfgPath, false)
			if err != nil {
				t.Fatalf("unexpected error for %s: %v", prov, err)
			}
			if res.IsValid() {
				t.Fatalf("expected validation failure for unsupported provider %q", prov)
			}
		}
	})

	t.Run("search vault with sources passes validation without warnings", func(t *testing.T) {
		cfgYAML := `
vaults:
  work:
    provider: keepass
    vault_path: ` + filepath.ToSlash(dummyDb) + `
  grouped:
    provider: search
    sources:
      - "work://app"
      - '"prod" in tags'
`
		cfgPath := filepath.Join(tempDir, "search_sources.yaml")
		_ = os.WriteFile(cfgPath, []byte(cfgYAML), 0600)

		res, err := Validate(cfgPath, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsValid() {
			t.Fatalf("expected valid config, got errors: %v", res.Errors)
		}
		if len(res.Warnings) != 0 {
			t.Fatalf("expected 0 warnings, got %v", res.Warnings)
		}
	})

	t.Run("implicit env and keyring vaults pass without explicit declaration", func(t *testing.T) {
		implicitYAML := `envs:
  local:
    - vault: env
      rules:
        - type: pass
          include: ["API_*"]
    - vault: keyring
      rules:
        - type: pass
          include: ["TOKEN"]`
		cfgPath := filepath.Join(tempDir, "implicit_vaults.yaml")
		_ = os.WriteFile(cfgPath, []byte(implicitYAML), 0600)
		res, err := Validate(cfgPath, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsValid() {
			t.Fatalf("expected implicit env and keyring vaults to pass validation, got errors: %v", res.Errors)
		}
	})

	t.Run("builtin pass, env, and keyring providers pass validation", func(t *testing.T) {
		builtinProvidersYAML := `vaults:
  pass_store:
    provider: pass
    vault_path: ~/.password-store
  proc_env:
    provider: env
  os_kr:
    provider: keyring`
		cfgPath := filepath.Join(tempDir, "builtin_providers.yaml")
		if err := os.WriteFile(cfgPath, []byte(builtinProvidersYAML), 0600); err != nil {
			t.Fatalf("failed to write builtin providers config: %v", err)
		}
		res, err := Validate(cfgPath, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsValid() {
			t.Fatalf("expected env, keyring, and pass providers to be valid, got errors: %v", res.Errors)
		}
	})

	t.Run("autoload wildcard glob pattern passes validation while broken regex fails", func(t *testing.T) {
		validGlobYAML := `autoload:
  - match: "*.sh"
    env:
      FOO: "env://FOO"
  - match: "npm run *"
    env:
      BAR: "env://BAR"`
		cfgPath := filepath.Join(tempDir, "autoload_glob.yaml")
		if err := os.WriteFile(cfgPath, []byte(validGlobYAML), 0600); err != nil {
			t.Fatalf("failed to write autoload glob config: %v", err)
		}
		res, err := Validate(cfgPath, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsValid() {
			t.Fatalf("expected wildcard glob autoload patterns to be valid, got errors: %v", res.Errors)
		}

		invalidRegexYAML := `autoload:
  - match: "^(aws"
    env:
      FOO: "env://FOO"`
		badPath := filepath.Join(tempDir, "autoload_bad_regex.yaml")
		if err := os.WriteFile(badPath, []byte(invalidRegexYAML), 0600); err != nil {
			t.Fatalf("failed to write invalid regex config: %v", err)
		}
		badRes, err := Validate(badPath, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if badRes.IsValid() {
			t.Fatal("expected validation failure for malformed regex autoload pattern")
		}
	})
}
