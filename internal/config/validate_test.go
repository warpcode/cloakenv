package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidate(t *testing.T) {
	tempDir := t.TempDir()

	dummyDb := filepath.Join(tempDir, "test.kdbx")
	if err := os.WriteFile(dummyDb, []byte("fake kdbx"), 0600); err != nil {
		t.Fatalf("failed to write dummy db: %v", err)
	}

	dummyTmpl := filepath.Join(tempDir, "test.tmpl")
	if err := os.WriteFile(dummyTmpl, []byte("{{ .attributes | toEnv }}"), 0600); err != nil {
		t.Fatalf("failed to write dummy tmpl: %v", err)
	}

	validYAML := `
vaults:
  work:
    provider: keepass
    vault_path: ` + filepath.ToSlash(dummyDb) + `
    key_format: env
    mapping:
      - match: "^Password$"
        key: "SECRET_PASSWORD"
templates:
  app:
    path: ` + filepath.ToSlash(dummyTmpl) + `
autoload:
  - match: "^aws"
    vaults: ["work"]
`
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
	})

	t.Run("missing vault file fails validation", func(t *testing.T) {
		missingVaultYAML := `
vaults:
  missing:
    provider: keepass
    vault_path: /path/does/not/exist.kdbx
`
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

	t.Run("invalid regex fails validation", func(t *testing.T) {
		badRegexYAML := `
autoload:
  - match: "[unclosed regex"
`
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
		badTmplYAML := `
templates:
  bad:
    template: "{{ .foo | unknownFunc }}"
`
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
		unknownFieldYAML := `
vaults:
  work:
    provider: keepass
    vault_path: ` + filepath.ToSlash(dummyDb) + `
    unexpected_random_key: 123
`
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

	t.Run("invalid vault KeyFormat fails validation", func(t *testing.T) {
		invalidKeyFormatYAML := `
vaults:
  work:
    provider: keepass
    vault_path: ` + filepath.ToSlash(dummyDb) + `
    key_format: camelCase
`
		cfgPath := filepath.Join(tempDir, "invalid_keyformat.yaml")
		_ = os.WriteFile(cfgPath, []byte(invalidKeyFormatYAML), 0600)

		res, err := Validate(cfgPath, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsValid() {
			t.Fatal("expected validation failure for invalid key_format")
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

	t.Run("invalid mapping rule template fails validation", func(t *testing.T) {
		invalidMappingTemplateYAML := `
vaults:
  work:
    provider: keepass
    vault_path: ` + filepath.ToSlash(dummyDb) + `
    mapping:
      - match: "^Password$"
        key: "{{ .foo | unknownFunc }}"
`
		cfgPath := filepath.Join(tempDir, "invalid_mapping_tmpl.yaml")
		_ = os.WriteFile(cfgPath, []byte(invalidMappingTemplateYAML), 0600)

		res, err := Validate(cfgPath, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsValid() {
			t.Fatal("expected validation failure for invalid mapping template syntax")
		}
	})

	t.Run("builtin pass and env providers pass validation", func(t *testing.T) {
		builtinProvidersYAML := `vaults:
  pass_store:
    provider: pass
    vault_path: ~/.password-store
  proc_env:
    provider: env`
		cfgPath := filepath.Join(tempDir, "builtin_providers.yaml")
		if err := os.WriteFile(cfgPath, []byte(builtinProvidersYAML), 0600); err != nil {
			t.Fatalf("failed to write builtin providers config: %v", err)
		}
		res, err := Validate(cfgPath, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsValid() {
			t.Fatalf("expected env and pass providers to be valid, got errors: %v", res.Errors)
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
