# `2026-11-17-declarative-envs-templates-pipeline` — Pure Retrieval Vaults, Separated `envs`/`templates` Pipelines & Zero-Leak CLI

> **Status**: Active
> **Created**: 2026-11-17 · **Parent**: none
> **Charter**: Decouple secret retrieval (`vaults:`) from flat environment injection (`envs:`) and artifact/file rendering (`templates:`), eliminate `expr-lang/expr` and multi-layer provider wrappers in favor of an ordered `RuleEngine` (`pass`, `match`, `set`), support ephemeral `0600` file delivery with zero-on-exit cleanup in `autoload`, and lock down the CLI surface (`config.yaml` as the strict security policy boundary) by removing plaintext-exposing commands (`get`, `show`, `search`, `set`, `delete`, `sync`).
> **Sources**: [ADR-0003](../adr/0003-declarative-envs-templates-and-zero-leak-cli.md) · [Epic #467](https://github.com/warpcode/cloakenv/issues/467) · Branch: `epic468_refactor_secret_pipeline`

---

## 1. Goals

### `G-001`: Pure Read-Only Vaults & Uniform Flat `Entry` Model
- **Statement**: Simplify `vaults:` to pure retrieval configurations (provider type, native provider options, optional provider-native initial query/scope, and optional cache), returning untouched `[]Entry` (`map[string]any`). Built-in stateless vaults (`env`, `keyring`) require no `vaults:` declaration.
- **Success Metric**: All provider decorators (`FilteringProvider`, `MappingProvider`, `KeyFormatProvider`, `AttributesProvider`), `SearchProvider`, `ValueResolvableProvider`, and writable sync methods are removed; every provider emits uniform flat field maps.

### `G-002`: Ordered Entry Transformation Pipeline (`rules:`) Without External Query DSL
- **Statement**: Replace `github.com/expr-lang/expr` with Go `text/template` boolean expressions (plus provider-native search params) for entry selection (`query:`), and process selected entries through an ordered `rules:` list (`pass`, `match`, `set`) with strict last-rule-wins precedence.
- **Success Metric**: `github.com/expr-lang/expr` is removed from `go.mod`; `rules:` evaluates deterministically against top-level entry fields (`{{ .title }}`, `{{ .password }}`) and `.output`.

### `G-003`: Complete Separation of `envs:` and `templates:` Generators
- **Statement**: Separate flat ENV injection (`envs:`) from artifact rendering (`templates:` with `driver: template | json | yaml | dotenv`) and attach both cleanly in `autoload:` with ephemeral file delivery (`env`, `env_path`, `path`) and zero-on-exit cleanup.
- **Success Metric**: `cloakenv run` injects resolved `envs:` and delivers `templates:` safely, wiping (`0x00`) and deleting ephemeral files on normal exit and `SIGINT`/`SIGTERM`.

### `G-004`: Strict Zero-Leak CLI Policy Boundary
- **Statement**: Remove CLI subcommands and ad-hoc flags that print or extract arbitrary plaintext secrets (`get`, `show`, `search`, `set`, `delete`, `sync`, and `run -e/-m/-t/-i/-f`), leaving `config.yaml` as the single policy boundary and introducing a strictly redacted `cloakenv inspect`.
- **Success Metric**: Zero CLI commands can print plaintext secret values to stdout; `cloakenv inspect` always masks field values as `<REDACTED>`.

---

## 2. Non-Goals (Explicitly Out of Scope)

### `NG-001`: Plaintext File & Virtual Search/Composition Vaults
- **Statement**: `json`, `yaml`, `custom_vault` (removed in `#468`), and `search` vaults (`#477`) are not retained or emulated inside `vaults:`.

### `NG-002`: Cross-Vault Synchronization & Arbitrary CLI Secret Writes
- **Statement**: Unidirectional secret copying (`cloakenv sync`) and arbitrary secret mutation (`cloakenv set`, `cloakenv delete`) are retired; `vaults:` are strictly read-only retrievers (`auth` and `cache` manage keyring/cache lifecycles independently).

### `NG-003`: Plaintext Secret Inspection on CLI
- **Statement**: No `--raw` plaintext dump or unredacted output mode is permitted on `cloakenv inspect` or any other CLI command.

---

## 3. Target `config.yaml` Architecture

```yaml
vaults:
  work:
    provider: keepass
    vault_path: ~/secrets/work.kdbx
    password_uri: keyring://cloakenv/work
    resolve_references: true   # Native KeePass {REF:...} expansion
    cache:
      enabled: true
      ttl: 15m

envs:
  litellm_env:
    - vault: work
      query: '{{ hasTag "litellm" .tags }}'
      rules:
        - type: pass
          include: ["username", "env:*"]
          exclude: ["env:test_*"]
          key_format: env
        - type: match
          match: "^env:(.*)$"
          key: "$1"
        - type: set
          values:
            "{{ .title | env }}_API_KEY": "{{ .password }}"
    - vault: env
      rules:
        - type: match
          match: "^(.*_API_KEY)$"
          value_match: "^sk-.*"
          key: "$1"

templates:
  litellm_config:
    driver: template
    path: ~/.config/litellm/config.tmpl
    vaults:
      - vault: work
        query: '{{ hasTag "litellm-model" .tags }}'
        rules:
          - type: pass
            include: ["title", "api_base", "model_name"]
            key_format: none
          - type: set
            values:
              api_key: "os.environ/{{ .title | env }}_API_KEY"

  app_dotenv:
    driver: dotenv
    from_env: litellm_env

autoload:
  - match: "^litellm\\s+(.*)$"
    envs:
      - litellm_env
      - LOG_LEVEL: INFO
    templates:
      - template: litellm_config
        env_path: LITELLM_CONFIG_PATH
    command: "litellm --config $LITELLM_CONFIG_PATH \\1"
```

---

## 4. Requirements

### `REQ-001`: Pure Retrieval Vaults & Uniform Flat `Entry` [Accepted] — P0
- **Depends On**: `G-001`, `ADR-0003`
- **Statement**: WHEN any vault retrieves records, the `provider` package SHALL return `[]Entry` where each `Entry` is an untouched flat `map[string]any` (containing canonical metadata fields such as `title`, `path`, `uuid`, `tags`, `group` alongside raw secret fields), with multi-field providers emitting one `Entry` per record and flat K/V providers (`env`, `keyring`) emitting a single `Entry` holding all K/V pairs.
- **Acceptance Criteria**:
  - [ ] Given a KeePass entry with `Title: "OpenAI"`, `Password: "sk-123"`, and `Tags: ["ai"]`, when retrieved from the vault, then the raw `Entry` contains `"title"`, `"password"`, `"Password"`, and `"tags"` at the top level with no `.meta`, `.attributes`, `.orig`, or `.output` wrappers.
  - [ ] Given `vault: env` with `OPENAI_API_KEY=sk-123` and `PATH=/usr/bin`, when retrieved, then a single `Entry` map is returned containing those keys untouched.
  - [ ] Given `env` or `keyring` referenced in `envs:` or `templates:` without a declaration in `vaults:`, when `config.Load` and engine initialization run, then they resolve implicitly as built-in providers.

### `REQ-002`: Go Template `query:` Selection & Removal of `expr-lang/expr` [Accepted] — P0
- **Depends On**: `G-002`, `REQ-001`
- **Statement**: WHEN a source entry in `envs:` or `templates:` specifies a `query:` expression, the engine SHALL delegate native filters where supported and evaluate Go `text/template` boolean predicates against each candidate `Entry`, without importing `github.com/expr-lang/expr`.
- **Acceptance Criteria**:
  - [ ] Given `query: '{{ hasTag "litellm" .tags }}'` on a vault source, when evaluated, then only entries carrying tag `"litellm"` pass selection.
  - [ ] Given `go.mod`, when `grep expr-lang go.mod` runs after `#477`, then `github.com/expr-lang/expr` is absent.

### `REQ-003`: Ordered `RuleEngine` (`pass`, `match`, `set`) [Accepted] — P0
- **Depends On**: `G-002`, `REQ-001`
- **Statement**: WHEN a vault source evaluates `rules:` against a selected `Entry`, the `RuleEngine` SHALL execute each rule sequentially (`pass`, `match`, `set`), defaulting omitted `rules:` to a single `type: pass` rule and resolving key collisions strictly in favor of later rules.
- **Acceptance Criteria**:
  - [ ] Given omitted `rules:` inside `envs:`, when evaluated, then a single `type: pass` with `key_format: env` runs and skips reserved metadata keys (`title`, `path`, `uuid`, `tags`, `group`, `vault`, `provider`) during wildcard `*` expansion.
  - [ ] Given an explicit `rules:` containing only `type: match` or `type: set`, when evaluated, then raw pass-through does NOT run unless a `type: pass` rule is explicitly listed.
  - [ ] Given a `type: pass` rule followed by a `type: set` rule writing the same key `API_KEY`, when evaluated, then the `set` rule's value overwrites the `pass` value.
  - [ ] Given a `type: match` rule with `value_match: "^sk-.*"`, when evaluated against an `env` entry, then only keys whose values match `^sk-.*` are captured.

### `REQ-004`: Separated `envs:` and `templates:` Generators [Accepted] — P0
- **Depends On**: `G-003`, `REQ-003`
- **Statement**: WHEN resolving an `envs:` entry, the engine SHALL flatten post-rule `[]map[string]string` entries in deterministic order (`path`, then `title` within a vault; source list order across `vaults:`) into a single `map[string]string`; WHEN resolving a `templates:` entry, `TemplatesRenderer` SHALL render `.entries` for `driver: template | json | yaml` or flatten `.entries` for `driver: dotenv`.
- **Acceptance Criteria**:
  - [ ] Given two matched entries with colliding output key `API_KEY`, when flattened by `EnvsBuilder`, then the later entry in deterministic order wins.
  - [ ] Given `driver: json` or `driver: yaml` in `templates:`, when rendered, then output serializes the post-rules `.entries` slice (`[]map[string]string`).
  - [ ] Given `driver: dotenv` with `from_env: litellm_env`, when rendered, then it emits sorted `KEY="VALUE"` dotenv lines matching `litellm_env`.

### `REQ-005`: `autoload` Delivery Modes & Zero-on-Exit Ephemeral Files [Accepted] — P0
- **Depends On**: `G-003`, `REQ-004`
- **Statement**: WHEN an `autoload` rule matches a `cloakenv run` command, the runner SHALL resolve `envs:` (named env strings and inline maps), render attached `templates:` according to `env` / `env_path` / `path`, register all resolved secrets in the stdout/stderr masking trie, and zero (`0x00`) plus unlink all created files on exit or `SIGINT`/`SIGTERM` unless `persist: true` is set.
- **Acceptance Criteria**:
  - [ ] Given `env: RAW_CFG`, when `autoload` runs, then `$RAW_CFG` contains the rendered template content in memory with no file written.
  - [ ] Given `env_path: LITELLM_CONFIG_PATH` with no `path:`, when `autoload` runs, then an ephemeral `0600` file is created in `/dev/shm` (or `os.CreateTemp`), `$LITELLM_CONFIG_PATH` points to it, and the file is zeroed and unlinked on exit or signal.
  - [ ] Given `env_path: CFG` and `path: "/tmp/{{ .APP_ENV }}.yaml"`, when `autoload` runs, then `path` is templated against resolved `envs`, written with `0600`, `$CFG` points to the resolved path, and the file is zeroed and unlinked on exit unless `persist: true`.
  - [ ] Given `path: "./.env.local"` without `env_path:`, when `autoload` runs, then the file is written without adding an extra ENV var and cleaned up on exit unless `persist: true`.

### `REQ-006`: Zero-Leak CLI Surface & Redacted `cloakenv inspect` [Accepted] — P0
- **Depends On**: `G-004`, `ADR-0003`
- **Statement**: WHEN any CLI subcommand executes, `cloakenv` SHALL prohibit plaintext secret dumping by removing `get`, `show`, `search`, `set`, `delete`, `sync`, and ad-hoc `run` secret flags, while providing `cloakenv inspect` with all secret values unconditionally masked as `<REDACTED>`.
- **Acceptance Criteria**:
  - [ ] Given `cloakenv get`, `cloakenv show`, `cloakenv search`, `cloakenv set`, `cloakenv delete`, or `cloakenv sync`, when invoked, then the CLI reports an unknown command error and exits non-zero.
  - [ ] Given `cloakenv inspect <env|template|command>`, when invoked, then matching vault entries, rule transformations, and autoload targets are displayed with every secret value replaced by `<REDACTED>`.

---

## 5. Work Breakdown Structure (Mapped to Epic #467)

| Issue | Phase | Scope Summary | Status |
|---|---|---|---|
| **#468** | Cleanup | Remove `json`, `yaml`, and `custom_vault` plaintext providers | Done (Closed) |
| **#475** | Cleanup | Merge `cloakenv config check` into `cloakenv doctor` | Done (Closed) |
| **#476** | Cleanup | Fold `internal/yaml` helpers into `internal/config` / `internal/utils` | Open |
| **#469** | Core Model | Simplify `Entry` to flat `map[string]any` and make vaults pure read-only retrievers | Open |
| **#477** | Core Model | Remove `SearchProvider`, `ValueResolvableProvider`, and `github.com/expr-lang/expr` | Open |
| **#470** | Config | Implement pure `vaults:` schema + top-level `envs:` and `templates:` config sections | Open |
| **#471** | Pipeline | Replace wrapper chain with ordered `RuleEngine` (`pass`, `match`, `set`) | Open |
| **#472** | Pipeline | Implement `EnvsBuilder` and `TemplatesRenderer` (`template`, `json`, `yaml`, `dotenv`) | Open |
| **#478** | Autoload | Update `autoload` schema to consume `envs:` (strings + inline maps) and `templates:` | Open |
| **#474** | Runner | Implement `autoload` template delivery (`env`, `env_path`, `path`) + zero-on-exit cleanup | Open |
| **#473** | CLI | Remove `get`/`show`/`search`/`set`/`delete`/`sync` + ad-hoc `run` flags; add redacted `inspect` | Open |
| **#479** | Cleanup | Remove legacy single-secret URI resolver, `sync` engine, and `template_parser.go` | Open |

---

## 6. Verification & Fitness Functions

1. All modifications must pass `make verify` (`go build ./... && go vet ./... && gofmt -l . && go test -race ./...`) and `make agents-check`.
2. Automated CLI tests confirm no subcommand or flag prints unredacted secret values to stdout/stderr.
3. Process-signal tests confirm `SIGINT`/`SIGTERM` zero (`0x00`) and unlink all non-persistent generated files.
