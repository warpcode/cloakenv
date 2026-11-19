# Changelog

All notable changes to `cloakenv` are documented in this file.

## [Unreleased] — Epic #467: Declarative `envs:`/`templates:` Pipeline & Zero-Leak CLI

### Breaking Changes

- **Zero-Leak CLI Lockdown**:
  - Removed secret-exfiltrating subcommands `get`, `show`, `search`, `set`, and `delete`, as well as `sync`.
  - Removed ad-hoc `cloakenv run` injection flags (`-e`, `-m`, `-t`, `-i`, `-f`).
  - Retained CLI subcommands: `run`, `inspect` (all secret values unconditionally replaced with `<REDACTED>`), `auth`, `cache`, `doctor`, `completion`, `version`, and `internal`.
- **Retired Config Keys & Providers**:
  - Removed `custom_vault`, `json`, `yaml`, and `search` providers (`keepass`, `pass`, `vault`, `env`, `keyring`, and `cache` remain).
  - Removed vault wrapper keys `include_fields`, `exclude_fields`, `mapping`, `key_format`, `attributes`, `searchable`, `single_entity`, `entity_name`, `entities_root_key`, `entities`, `resolve_values`, `source_vaults`, and `tags` from `vaults:`. Strict validation rejects these keys with migration guidance.
- **Pure Go `text/template` Helpers**:
  - Retired legacy URI/command template functions (`get`, `tryGet`, `entry`, `search`, `include`, `includeFile`, `exec`), legacy `${...}` interpolation, and Go Expr `search://` queries.
  - Replaced provider wrappers with declarative `envs:`, `templates:`, and ordered `RuleEngine` rules (`pass`, `match`, `set`).

### Migration Guide

1. **Move vault filters/mappings to `envs:` rules**:
   Replace `include_fields`, `exclude_fields`, `mapping`, and `key_format` inside `vaults.<name>` with `envs.<env_name>` entries containing `vault:`, optional Go `text/template` `query:` predicates, and ordered `rules:` (`type: pass`, `type: match`, `type: set`).
2. **Replace `cloakenv run -e/-m/-t` flags with `autoload:` rules**:
   Bind commands in `autoload:` using `envs:` (named env references or inline key-value maps) and `templates:` (ephemeral `env_path` or `path:` artifact delivery).
3. **Migrate `.env` / file templates from `{{ get "..." }}` to `templates:`**:
   Define `templates.<name>` with `driver: template | json | yaml | dotenv`, attach `vaults:` (or `from_env:`), and reference entry fields directly using pure Go `text/template` expressions (e.g., `{{ .password }}`, `{{ toEnv . }}`, `{{ json .entries }}`).
4. **Replace `cloakenv get` / `cloakenv show` / `cloakenv search` checks with `cloakenv inspect`**:
   Use `cloakenv inspect`, `cloakenv inspect env <name>`, `cloakenv inspect template <name>`, or `cloakenv inspect run -- <cmd>` to audit matched entries, generated keys, and autoload bindings safely (`<REDACTED>`).
