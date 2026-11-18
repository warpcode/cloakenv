# ADR-0003: Pure Retrieval Vaults, Separated `envs` and `templates` Pipelines, and Zero-Leak CLI Boundary

| Metadata | Details |
|---|---|
| **Status** | Accepted |
| **Date** | 2026-11-17 |
| **Deciders** | warpcode |
| **Supersedes** | Portions of [ADR-0001](0001-unify-templating-on-go-text-template-and-composed-custom-vaults.md) (`custom_vault`, per-vault `attributes`) and [ADR-0002](0002-cross-vault-sync-and-secure-retrieval-caching.md) (`cloakenv sync`, `WritableProvider`) |
| **Reversibility** | One-way |

---

## 1. Context and Problem Statement

`cloakenv` previously coupled secret retrieval, filtering (`include_fields`, `exclude_fields`), regex key mapping (`mapping`), key formatting (`key_format`), and computed templates (`attributes`) directly onto `vaults:` via a 4-layer `SecretProvider` decorator chain (`FilteringProvider -> MappingProvider -> KeyFormatProvider -> AttributesProvider`), while also exposing `get`, `show`, and `search` CLI subcommands and ad-hoc `run -e/-m` URI flags.

This created four architectural problems:
1. **Vault Coupling**: A single vault could not easily project different outputs for different consumers without creating secondary `search` or `custom_vault` virtual vaults.
2. **Environment vs. Template Mismatch**: Flat `KEY=VALUE` environment variable injection (`envs`) and hierarchical/file artifact rendering (`templates` in JSON, YAML, Go `text/template`, or `.env` format) have fundamentally different output shapes and delivery semantics.
3. **External Expression Dependency**: Entry searching relied on `github.com/expr-lang/expr` and an 850-line AST walker despite Go's `text/template` engine already being present in standard library.
4. **AI / CLI Exfiltration Risk**: Because `cloakenv`'s core purpose is injecting and masking secrets so AI coding agents and terminal logs never see plaintext credentials, allowing CLI subcommands (`get`, `show`, `search`) or ad-hoc CLI flags (`run -e KEY=uri`) to print or bind arbitrary secrets defeated the threat model.

---

## 2. Decision Drivers

- **P0 (Security & Threat Model)**:
  - `config.yaml` is the strict security policy boundary: CLI commands must never print plaintext secret values to stdout or allow ad-hoc secret extraction outside configured `autoload` rules.
  - Zero plaintext persistence on disk after child process termination (`0600` ephemeral files zeroed with `0x00` and unlinked on exit or signal).
- **P1 (Architectural Simplicity)**:
  - Vaults are pure read-only retrieval operations returning untouched `[]Entry` maps.
  - Complete separation between flat environment generators (`envs:`) and artifact/file generators (`templates:`).
  - Zero external query DSL dependencies (remove `github.com/expr-lang/expr`).
- **P2 (Ergonomics & Debuggability)**:
  - Ordered, composable `rules:` (`pass`, `match`, `set`) with deterministic last-rule-wins precedence.
  - Redacted inspection (`cloakenv inspect`) where secret values are always `<REDACTED>`.

---

## 3. Decision Outcome

### 3.1 Pure Retrieval `vaults:` and Uniform `Entry` Model
- Vault configurations under `vaults:` contain only:
  - Provider type (`keepass`, `pass`, `vault`, `env`, `keyring`),
  - Native provider options (e.g. `vault_path`, `password_uri`, `resolve_references`),
  - Optional initial provider-native `query` / scope (for online or large vaults), and
  - Optional encrypted `cache` configuration.
- Built-in stateless vaults (`env`, `keyring`) are available implicitly without requiring declaration in `vaults:`.
- `json`, `yaml`, `custom_vault`, and `search` vault types are removed.
- Every vault returns `[]Entry`, where each `Entry` is a flat `map[string]any` of its raw fields (including canonical metadata fields `title`, `path`, `uuid`, `tags`, `group` alongside secret fields). Multi-field providers (`keepass`, `pass`, `vault`) emit one `Entry` per record; flat K/V providers (`env`, `keyring`) emit one `Entry` whose fields are the K/V pairs.

### 3.2 Entry Selection (`query`) Without `expr-lang/expr`
- Remove `github.com/expr-lang/expr` from `go.mod`.
- Entry selection (`query:`) exclusively filters which entries are selected from a vault using native provider search parameters and/or Go `text/template` boolean expressions (e.g. `query: '{{ hasTag "litellm" .tags }}'`).

### 3.3 Ordered Transformation `rules:` (`pass`, `match`, `set`)
Inside `envs:` and `templates:`, each vault source transforms selected entries through an ordered `rules:` slice:
- **Default**: If `rules:` is omitted, a single `type: pass` rule runs by default. If `rules:` is specified, **only** the listed rules execute in sequential order.
- **Template Context**: Rule templates (`match`, `set`) access raw entry fields directly at the top level (`{{ .title }}`, `{{ .password }}`), with `.output` exposing keys written by earlier rules on the same entry (and `.value` inside `match`).
- **Rule Types**:
  1. `type: pass`: Glob `include` -> `exclude` filtering over raw entry fields (skipping reserved metadata keys on wildcard `*` unless explicitly named) and `key_format` (`env`, `snake`, `kebab`, `none`).
  2. `type: match`: Matches raw fields by key regex (`match`) and/or value regex (`value_match`), evaluating `key` (`$1..$N` capture groups and Go templates) and optional `value` template.
  3. `type: set`: Evaluates explicit `values: map[string]string` key and value templates.
- **Precedence**: Later rules in `rules:` strictly override keys written by earlier rules on that entry, producing a resolved `map[string]string` per entry (`.entries`).

### 3.4 Separated `envs:` and `templates:` Generators
1. **`envs:` (Flat Environment Generators)**:
   - Runs `vaults -> query -> rules` (defaulting `pass` to `key_format: env`) and flattens `.entries` in deterministic order (`path`, then `title` within vaults; source order across `vaults:`) into a single `map[string]string`.
2. **`templates:` (Artifact / File Generators)**:
   - Runs `vaults -> query -> rules` to produce post-rules `.entries` (`[]map[string]string`).
   - Supports four drivers:
     - `driver: template`: Renders inline `template:` or `path:` via Go `text/template` with root context exposing strictly `.entries` (post-rules).
     - `driver: json`: Serializes post-rules `.entries` as JSON.
     - `driver: yaml`: Serializes post-rules `.entries` as YAML.
     - `driver: dotenv`: Flattens post-rules `.entries` in order (defaulting `key_format: env`, optionally seeded from `from_env: <name>`) and formats `KEY="VALUE"` lines.

### 3.5 `autoload` Execution & Ephemeral File Delivery
Each `autoload` rule matches a command executed via `cloakenv run -- <command>` and attaches:
- `envs:`: Ordered list of named `envs:` strings (e.g. `"litellm_env"`) and/or inline `{ KEY: "value_or_template" }` maps.
- `templates:`: List of template deliveries supporting four modes via `env`, `env_path`, and `path`:
  1. `env: VAR`: Assigns raw rendered template content directly to `$VAR`.
  2. `env_path: VAR` (no `path`): Writes rendered template to an ephemeral `0600` temp file (`/dev/shm` or `os.CreateTemp`) and sets `$VAR=/path/to/tmpfile`.
  3. `env_path: VAR` + `path: "<template>"`: Evaluates `path` (an inline Go template populated with resolved `envs` data), writes rendered template to `path` (`0600`), and sets `$VAR=<resolved_path>`.
  4. `path: "<template>"` (no `env_path`): Writes rendered template to `path` (`0600`) without setting an ENV var.
- All files created during `run`/`autoload` (both auto temp-files and explicit `path:` files) are zeroed (`0x00`) and unlinked on process exit or `SIGINT`/`SIGTERM` by default unless `persist: true` is explicitly set.

### 3.6 Zero-Leak CLI Surface
- Remove `get`, `show`, `search`, `set`, `delete`, and `sync` CLI subcommands, and remove ad-hoc secret flags (`-m`, `-e`, `-t`, `-i`, `-f`) from `cloakenv run`.
- Add `cloakenv inspect [vault|env|template|command]` which evaluates queries, rules, and autoload matches while strictly redacting all secret values as `<REDACTED>`.
- Retain `cloakenv run`, `cloakenv inspect`, `cloakenv doctor`, `cloakenv auth`, `cloakenv cache`, `cloakenv version`, and `cloakenv completion`.


### 3.7 Clarifications and Constraints
- **`query:` truthiness**: A Go `text/template` predicate selects an entry only when its trimmed output is exactly `true`. Any other output (including `false` or empty) skips the entry. Template execution errors (including `missingkey=error`) fail the run with an error rather than silently skipping.
- **Flat `Entry` and nested values**: `tags` is a list and `group` is a nested map on an otherwise flat `Entry`; templates may traverse them (e.g. `.group.name`).
- **Implicit `env` / `keyring` vaults**: These return a single `Entry` of all K/V pairs. A wildcard `pass` over them would re-inject the whole process environment, so sources referencing them should declare explicit `include` globs. Reserved metadata keys are guarded against wildcard clobbering as per the `RuleEngine` spec (#471).
- **`path:` safety**: Templated `path:` values must not follow symlinks, must not overwrite a pre-existing file, and parent directories are created with `0700`.
- **Zero-on-exit is best-effort**: Overwriting with `0x00` and unlinking cannot be guaranteed on `SIGKILL`/crash, nor on copy-on-write, journalled, or SSD-backed filesystems. `/dev/shm` is preferred where available, and `cloakenv doctor` SHOULD report stale ephemeral files left by an aborted run.
- **`inspect` disclosure**: `cloakenv inspect` redacts all secret values; entry titles, paths, tags and generated key names are shown and are not treated as secret. No flag may disable redaction.
- **Query naming**: A vault-level `query:` is a provider-native scope; a source-level `query:` under `envs:`/`templates:` is a template predicate over entries.

## 4. Consequences

- **Breaking change**: Removal of the `get`, `show`, `search`, `set`, `delete` and `sync` commands, the `run -m/-e/-t/-i/-f` flags, and the vault keys `include_fields`, `exclude_fields`, `mapping`, `key_format`, `attributes`, `searchable`, `resolve_values`, `entities`, `source_vaults`. Config validation MUST reject legacy keys with a message pointing to the replacement, and the release MUST include a CHANGELOG migration note.
- **Superseded**: ADR-0001's `custom_vault` and `get`/`entry`/`search` template functions, and ADR-0002's `cloakenv sync` and `WritableProvider`, are retired. Caching from ADR-0002 is retained.
- **Documentation debt**: `AGENTS.md` invariants (wrapper ordering, filter precedence/authorization, provider wrappers, child-command pass-through, `internal/yaml` contract), `README.md`, shell completions and `.github/skills/` must be updated alongside #469, #471, #473, #476 and #479.
- **Suggested implementation order**: #476 and #469 → #477 and #470 → #471, #472 and #478 → #474 → #473 and #479.
