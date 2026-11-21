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

func TestValidate_ReviewRegressionCoverage(t *testing.T) {
	tempDir := t.TempDir()

	t.Run("all 12 legacy vault keys are rejected with migration hints", func(t *testing.T) {
		legacyCases := []struct {
			key  string
			yaml string
		}{
			{"include_fields", "include_fields: [\"*\"]"},
			{"exclude_fields", "exclude_fields: [\"Notes\"]"},
			{"mapping", "mapping:\n      - match: \"(.*)\"\n        to: \"$1\""},
			{"key_format", "key_format: env"},
			{"attributes", "attributes:\n      A: \"b\""},
			{"entities", "entities:\n      e1:\n        attributes:\n          A: \"b\""},
			{"single_entity", "single_entity: true"},
			{"entity_name", "entity_name: main"},
			{"entities_root_key", "entities_root_key: secrets"},
			{"searchable", "searchable: true"},
			{"resolve_values", "resolve_values: true"},
			{"source_vaults", "source_vaults: [env]"},
		}
		if len(legacyCases) != len(legacyVaultKeys) {
			t.Fatalf("test covers %d legacy keys, want %d", len(legacyCases), len(legacyVaultKeys))
		}
		for _, tc := range legacyCases {
			t.Run(tc.key, func(t *testing.T) {
				cfgYAML := "vaults:\n  v:\n    provider: env\n    " + tc.yaml + "\n"
				p := filepath.Join(tempDir, "legacy_"+tc.key+".yaml")
				if err := os.WriteFile(p, []byte(cfgYAML), 0o600); err != nil {
					t.Fatal(err)
				}
				res, err := Validate(p, false)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if res.IsValid() {
					t.Fatalf("expected legacy key %q to fail validation", tc.key)
				}
				found := false
				for _, e := range res.Errors {
					if strings.Contains(e, tc.key) && strings.Contains(e, "no longer supported") {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected migration hint for %q, got errors: %v", tc.key, res.Errors)
				}
			})
		}
	})

	t.Run("strict mode rejects unknown fields inside templates.<name>", func(t *testing.T) {
		cfgYAML := `templates:
  bad_tmpl:
    driver: dotenv
    from_envs: app_env
    vault: env
`
		p := filepath.Join(tempDir, "strict_tmpl_unknown.yaml")
		if err := os.WriteFile(p, []byte(cfgYAML), 0o600); err != nil {
			t.Fatal(err)
		}
		res, err := Validate(p, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		joined := strings.Join(res.Errors, "\n")
		if !strings.Contains(joined, `unknown field "from_envs"`) || !strings.Contains(joined, `unknown field "vault"`) {
			t.Fatalf("expected strict unknown field errors for from_envs and vault, got: %v", res.Errors)
		}
	})

	t.Run("driver template missing path/empty template aggregates with other errors", func(t *testing.T) {
		cfgYAML := `templates:
  empty_tmpl:
    driver: template
    vaults:
      - vault: env
        rules:
          - type: pass
            include: ["FOO"]
  missing_file_tmpl:
    driver: template
    path: does-not-exist.tmpl
    vaults:
      - vault: env
        rules:
          - type: pass
            include: ["FOO"]
  bad_dotenv:
    driver: dotenv
    from_env: nonexistent_env
`
		p := filepath.Join(tempDir, "aggregate_tmpl_errors.yaml")
		if err := os.WriteFile(p, []byte(cfgYAML), 0o600); err != nil {
			t.Fatal(err)
		}
		res, err := Validate(p, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		joined := strings.Join(res.Errors, "\n")
		for _, want := range []string{
			`template "empty_tmpl": driver`,
			`template "missing_file_tmpl": file does not exist`,
			`template "bad_dotenv": from_env references unknown env "nonexistent_env"`,
		} {
			if !strings.Contains(joined, want) {
				t.Errorf("expected aggregated error containing %q, got:\n%s", want, joined)
			}
		}
	})

	t.Run("driver validation error branches", func(t *testing.T) {
		cases := []struct {
			name    string
			yaml    string
			wantErr string
		}{
			{
				name:    "from_env rejected on template driver",
				yaml:    "envs:\n  e:\n    - vault: env\ntemplates:\n  t:\n    driver: template\n    template: \"ok\"\n    from_env: e\n",
				wantErr: "'from_env' is only valid when driver is `dotenv`",
			},
			{
				name:    "from_env rejected on json driver",
				yaml:    "envs:\n  e:\n    - vault: env\ntemplates:\n  t:\n    driver: json\n    from_env: e\n",
				wantErr: "'from_env' is only valid when driver is `dotenv`",
			},
			{
				name:    "from_env rejected on yaml driver",
				yaml:    "envs:\n  e:\n    - vault: env\ntemplates:\n  t:\n    driver: yaml\n    from_env: e\n",
				wantErr: "'from_env' is only valid when driver is `dotenv`",
			},
			{
				name:    "template rejected on json driver",
				yaml:    "templates:\n  t:\n    driver: json\n    template: \"hi\"\n",
				wantErr: "'template' and 'path' cannot be used with driver \"json\"",
			},
			{
				name:    "path rejected on yaml driver",
				yaml:    "templates:\n  t:\n    driver: yaml\n    path: foo.tmpl\n",
				wantErr: "'template' and 'path' cannot be used with driver \"yaml\"",
			},
			{
				name:    "template rejected on dotenv driver",
				yaml:    "templates:\n  t:\n    driver: dotenv\n    template: \"hi\"\n",
				wantErr: "'template' and 'path' cannot be used with driver `dotenv`",
			},
			{
				name:    "match rule missing match and value_match",
				yaml:    "envs:\n  e:\n    - vault: env\n      rules:\n        - type: match\n          key: FOO\n",
				wantErr: "match rule requires 'match' or 'value_match'",
			},
			{
				name:    "match rule missing key",
				yaml:    "envs:\n  e:\n    - vault: env\n      rules:\n        - type: match\n          match: \"^FOO$\"\n",
				wantErr: "match rule missing 'key'",
			},
			{
				name:    "set rule empty values",
				yaml:    "envs:\n  e:\n    - vault: env\n      rules:\n        - type: set\n",
				wantErr: "set rule requires non-empty 'values'",
			},
			{
				name:    "invalid pass exclude glob",
				yaml:    "envs:\n  e:\n    - vault: env\n      rules:\n        - type: pass\n          exclude: [\"[bad\"]\n",
				wantErr: "invalid exclude glob pattern",
			},
			{
				name:    "missing source vault",
				yaml:    "envs:\n  e:\n    - query: \"true\"\n",
				wantErr: "missing required field 'vault'",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				p := filepath.Join(tempDir, strings.ReplaceAll(tc.name, " ", "_")+".yaml")
				if err := os.WriteFile(p, []byte(tc.yaml), 0o600); err != nil {
					t.Fatal(err)
				}
				res, err := Validate(p, true)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				joined := strings.Join(res.Errors, "\n")
				if !strings.Contains(joined, tc.wantErr) {
					t.Errorf("expected error containing %q, got:\n%s", tc.wantErr, joined)
				}
			})
		}
	})

	t.Run("wildcard pass warning fires for implicit and explicit env and keyring vaults", func(t *testing.T) {
		cfgYAML := `vaults:
  proc_env:
    provider: env
  os_kr:
    provider: keyring
envs:
  e_implicit:
    - vault: env
    - vault: keyring
      rules:
        - type: pass
  e_explicit:
    - vault: proc_env
    - vault: os_kr
      rules:
        - type: pass
`
		p := filepath.Join(tempDir, "flat_warnings.yaml")
		if err := os.WriteFile(p, []byte(cfgYAML), 0o600); err != nil {
			t.Fatal(err)
		}
		res, err := Validate(p, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsValid() {
			t.Fatalf("expected valid config with warnings, got errors: %v", res.Errors)
		}
		if len(res.Warnings) != 4 {
			t.Fatalf("expected 4 wildcard pass warnings (implicit + explicit env/keyring), got %d: %v", len(res.Warnings), res.Warnings)
		}
	})
}

func TestValidatePR484ReviewFixes(t *testing.T) {
	tempDir := t.TempDir()
	emptyFile := filepath.Join(tempDir, "empty.tmpl")
	if err := os.WriteFile(emptyFile, []byte("   \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	validFile := filepath.Join(tempDir, "valid.tmpl")
	if err := os.WriteFile(validFile, []byte("{{ .entries }}"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name:    "strict unknown key in template vault source",
			yaml:    "templates:\n  t:\n    driver: json\n    vaults:\n      - vault: env\n        bogus_src: true\n",
			wantErr: `unknown field "bogus_src"`,
		},
		{
			name:    "strict unknown key in template vault rule",
			yaml:    "templates:\n  t:\n    driver: json\n    vaults:\n      - vault: env\n        rules:\n          - type: pass\n            bogus_rule: true\n",
			wantErr: `unknown field "bogus_rule"`,
		},
		{
			name:    "strict unknown key in vault cache mapping",
			yaml:    "vaults:\n  v:\n    provider: env\n    cache:\n      ttl: 5m\n      bogus_cache: true\n",
			wantErr: `unknown field "bogus_cache"`,
		},
		{
			name:    "builtin env vault name with mismatched provider",
			yaml:    "vaults:\n  env:\n    provider: keyring\n",
			wantErr: `built-in vault name "env" must use provider "env"`,
		},
		{
			name:    "builtin keyring vault name with mismatched provider",
			yaml:    "vaults:\n  keyring:\n    provider: env\n",
			wantErr: `built-in vault name "keyring" must use provider "keyring"`,
		},
		{
			name:    "template driver combining template and path",
			yaml:    "templates:\n  t:\n    driver: template\n    template: \"hello\"\n    path: \"" + filepath.ToSlash(validFile) + "\"\n",
			wantErr: "cannot specify both 'template' and 'path'",
		},
		{
			name:    "template driver with empty file at path",
			yaml:    "templates:\n  t:\n    driver: template\n    path: \"" + filepath.ToSlash(emptyFile) + "\"\n",
			wantErr: "is empty",
		},
		{
			name:    "template null scalar is not literal null",
			yaml:    "templates:\n  foo: null\n",
			wantErr: "requires 'template' or 'path'",
		},
		{
			name:    "pass rule with match field",
			yaml:    "envs:\n  e:\n    - vault: env\n      rules:\n        - type: pass\n          match: '^A'\n",
			wantErr: "pass rule cannot specify match/value_match/key/value fields",
		},
		{
			name:    "pass rule with values field",
			yaml:    "envs:\n  e:\n    - vault: env\n      rules:\n        - type: pass\n          values:\n            K: V\n",
			wantErr: "pass rule cannot specify 'values' field",
		},
		{
			name:    "match rule with include field",
			yaml:    "envs:\n  e:\n    - vault: env\n      rules:\n        - type: match\n          match: '^A'\n          key: 'B'\n          include: ['*']\n",
			wantErr: "match rule cannot specify include/exclude/key_format fields",
		},
		{
			name:    "match rule with values field",
			yaml:    "envs:\n  e:\n    - vault: env\n      rules:\n        - type: match\n          match: '^A'\n          key: 'B'\n          values:\n            K: V\n",
			wantErr: "match rule cannot specify 'values' field",
		},
		{
			name:    "set rule with key_format field",
			yaml:    "envs:\n  e:\n    - vault: env\n      rules:\n        - type: set\n          key_format: env\n          values:\n            K: V\n",
			wantErr: "set rule cannot specify include/exclude/key_format fields",
		},
		{
			name:    "set rule with match field",
			yaml:    "envs:\n  e:\n    - vault: env\n      rules:\n        - type: set\n          match: '^A'\n          values:\n            K: V\n",
			wantErr: "set rule cannot specify match/value_match/key/value fields",
		},
		{
			name:    "legacy vault keys via YAML anchor alias",
			yaml:    "vaults:\n  base: &base_vault\n    provider: env\n    include_fields:\n      - FOO\n  aliased: *base_vault\n",
			wantErr: `vault "aliased": legacy key "include_fields" is no longer supported`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(tempDir, strings.ReplaceAll(tc.name, " ", "_")+".yaml")
			if err := os.WriteFile(p, []byte(tc.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			res, err := Validate(p, true)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			joined := strings.Join(res.Errors, "\n")
			if !strings.Contains(joined, tc.wantErr) {
				t.Errorf("expected error containing %q, got:\n%s", tc.wantErr, joined)
			}
		})
	}
}

func TestValidate_MatchRuleCapturePlaceholdersInTemplates(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "secrets.kdbx")
	if err := os.WriteFile(dbPath, []byte("dummy"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("accepts $N and ${N} capture placeholders in match key and value templates", func(t *testing.T) {
		cfgYAML := `vaults:
  work:
    provider: keepass
    vault_path: ` + dbPath + `
envs:
  mapped:
    - vault: work
      rules:
        - type: match
          match: "^env:(.*)$"
          key: "{{ upper $1 }}"
          value: "prefix-{{ lower ${1} }}-{{ .value }}"
        - type: match
          match: "^svc_([a-z]+)_([a-z]+)$"
          key: "{{ upper ${1} }}_{{ upper $2 }}"
        - type: match
          value_match: "^token:(.*)$"
          key: "TOKEN_{{ upper $1 }}"
          value: "{{ lower ${1} }}"
`
		p := filepath.Join(tempDir, "valid_captures.yaml")
		if err := os.WriteFile(p, []byte(cfgYAML), 0o600); err != nil {
			t.Fatal(err)
		}
		res, err := Validate(p, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsValid() {
			t.Fatalf("expected valid config for capture placeholders in templates, got errors: %v", res.Errors)
		}
	})

	t.Run("rejects out-of-range capture placeholder and invalid template syntax", func(t *testing.T) {
		cfgYAML := `vaults:
  work:
    provider: keepass
    vault_path: ` + dbPath + `
envs:
  bad_captures:
    - vault: work
      rules:
        - type: match
          match: "^env:(.*)$"
          key: "{{ upper $2 }}"
        - type: match
          match: "^env:(.*)$"
          key: "{{ upper $1"
`
		p := filepath.Join(tempDir, "invalid_captures.yaml")
		if err := os.WriteFile(p, []byte(cfgYAML), 0o600); err != nil {
			t.Fatal(err)
		}
		res, err := Validate(p, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsValid() {
			t.Fatal("expected validation errors for out-of-range capture and unclosed template")
		}
		joined := strings.Join(res.Errors, "\n")
		if !strings.Contains(joined, `$2`) {
			t.Errorf("expected out-of-range $2 error, got:\n%s", joined)
		}
		if !strings.Contains(joined, "template syntax error") {
			t.Errorf("expected template syntax error, got:\n%s", joined)
		}
	})
}
