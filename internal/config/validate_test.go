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
}
