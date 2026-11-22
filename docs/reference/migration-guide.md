# Migration Guide (Upgrading to ADR-0003)

`cloakenv` [ADR-0003](../adr/0003-declarative-envs-templates-and-zero-leak-cli.md) decoupled pure secret retrieval (`vaults:`) from declarative transformation pipelines (`envs:` and `templates:`) and locked down the CLI to prevent plaintext secret leaks.

`cloakenv doctor` automatically detects legacy `vaults:` keys and prints actionable migration hints.

---

## 1. Migrating Legacy `vaults:` Keys

| Legacy `vaults:` Key | Replacement in `envs:` / `templates:` |
|---|---|
| `include_fields` | `rules: [{ type: pass, include: [...] }]` |
| `exclude_fields` | `rules: [{ type: pass, exclude: [...] }]` |
| `key_format` | `rules: [{ type: pass, key_format: env \| snake \| kebab \| none }]` |
| `mapping` | `rules: [{ type: match, match: ..., key: ..., value: ... }]` |
| `attributes` | `rules: [{ type: set, values: { ... } }]` |
| `resolve_values` | `rules:` with Go `text/template` expressions |
| `searchable` | `query: '{{ ... }}'` inside `envs:` or `templates:` |
| `source_vaults` | Multiple source items listed directly under `envs:<name>[]` or `templates:<name>.vaults[]` |
| `entities`, `single_entity`, `entity_name`, `entities_root_key` | Removed (`json`/`yaml`/`custom_vault` plaintext vaults removed); use supported secret backends + `envs:`/`templates:` |

### Before (Legacy)
```yaml
vaults:
  work:
    provider: keepass
    vault_path: ~/secrets/work.kdbx
    include_fields: ["UserName", "Password"]
    key_format: env
    attributes:
      DATABASE_URL: "postgres://{{ .UserName }}:{{ .Password }}@localhost/app"
```

### After (Current)
```yaml
vaults:
  work:
    provider: keepass
    vault_path: ~/secrets/work.kdbx

envs:
  app_env:
    - vault: work
      rules:
        - type: pass
          include: ["UserName", "Password"]
          key_format: env
        - type: set
          values:
            DATABASE_URL: "postgres://{{ .UserName }}:{{ .Password }}@localhost/app"
```

---

## 2. Migrating Queries (`expr-lang` -> Go `text/template`)

| Legacy `expr` Query | Current Go `text/template` `query:` |
|---|---|
| `'title == "OpenAI"'` | `'{{ eq .title "OpenAI" }}'` |
| `'"prod" in tags'` | `'{{ hasTag "prod" .tags }}'` |
| `'group.name == "Backend" && "api" in tags'` | `'{{ and (eq .group.name "Backend") (hasTag "api" .tags) }}'` |

---

## 3. Migrating Legacy Template Functions & `${...}` Syntax

- `${scheme://entry:attr}` interpolation and `get`, `tryGet`, `entry`, `search`, `include`, `includeFile`, `exec` template functions are permanently removed.
- Instead, fetch entries via `templates.<name>.vaults[]` (with `query:` and `rules:`) and iterate `.entries` inside Go `text/template`:

```yaml
templates:
  app_config:
    driver: template
    vaults:
      - vault: work
        query: '{{ hasTag "service" .tags }}'
        rules:
          - type: pass
            include: ["title", "URL"]
            key_format: none
    template: |
      {{- range .entries }}
      {{ .title }}={{ .URL }}
      {{- end }}
```

---

## 4. Migrating Retired CLI Commands & `run` Flags

| Retired Command / Flag | Current Replacement |
|---|---|
| `cloakenv get` / `show` / `search` | `cloakenv inspect [env \| template \| run]` (always `<REDACTED>`) |
| `cloakenv set` / `delete` / `sync` | Removed (`vaults:` are strictly read-only) |
| `cloakenv config check` | `cloakenv doctor [--strict]` |
| `cloakenv run -e KEY=uri` / `-m` / `-t` / `-i` / `-f` | Declare `envs:`, `templates:`, and `autoload:` in `config.yaml`, then run `cloakenv run -- <cmd>` |
