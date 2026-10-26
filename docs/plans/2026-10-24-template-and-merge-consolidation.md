# `2026-10-24-template-and-merge-consolidation` — Unified Go `text/template` Engine, Per-Entry Vault Attributes, and Composed `custom_vault` Entities

> **Status**: Active
> **Created**: 2026-10-24 · **Parent**: none
> **Charter**: Replace `${scheme://...}` string interpolation with a unified Go `text/template` (`{{ ... }}`) engine, enable per-entry computed/templated `attributes:` across all vault types, support entry-metadata interpolation in `mapping.key`, and expand `custom_vault` entities into composable single-entry K/V stores that can merge, filter, map, and format entries from any vault or `search` provider for config generation (`get`/`show`) and environment injection (`run`).
> **Sources**: [ADR-0001](../adr/0001-unify-templating-on-go-text-template-and-composed-custom-vaults.md) · [Epic #261](https://github.com/warpcode/cloakenv/issues/261) · Base Branch: `epic/2026-10-24-template-and-merge-consolidation`

## Goals

### `G-001`: Single Unified `{{ ... }}` Templating & Interpolation Engine [Accepted]
- **Statement**: Replace `${scheme://...}` across `cloakenv` with Go stdlib `text/template` (`{{ ... }}`) under `resolve_values: true`, supporting secret URI lookups (`get`, `entry`, `search`), current-entry context (`.meta`, `.attributes`, `.entries`), serializers (`toEnv`/`toKv`, `toJson`, `toYaml`), string/collection helpers, and named/file template inclusions (`include`, `includeFile`).
- **Success Metric**: 100% of template and URI interpolation tests pass using `{{ ... }}` with zero external dependencies and `missingkey=error` enforcement.
- **Rationale**: Eliminates dual-syntax cognitive overhead and custom parser maintenance while unlocking full pipeline and structural templating.

### `G-002`: Per-Entry Computed & Templated Attributes on All Vaults [Accepted]
- **Statement**: Allow any vault (`keepass`, `yaml`, `json`, `search`, `custom_vault`) to define `attributes: map[string]any` evaluated per entry after `include_fields` -> `exclude_fields` -> `mapping` -> `key_format`, plus `{{ .meta.* }}` interpolation in `mapping.key`.
- **Success Metric**: Any vault entry can synthesize template-rendered attributes (e.g., `block: '{{ include "model_block" . }}'`) and disambiguate mapped keys by entry metadata (`key: "{{ .meta.Title | env }}_$1"`).
- **Rationale**: Enables per-entry template mapping on normal and `search` vaults without mutating backend stores.

### `G-003`: Composed `custom_vault` Entities for Multi-Entry Consolidation [Accepted]
- **Statement**: Expand `custom_vault` entities so each named entity (`config://jules`) can merge single-entry URIs and multi-entry vault/search URIs (`openrouter://`), apply per-source filtering, metadata-aware mapping, and key formatting, and evaluate post-merge `attributes:` templates.
- **Success Metric**: `cloakenv get config://jules:KEY`, `cloakenv show config://jules`, and `cloakenv run -m config://jules` resolve consolidated K/V pairs and rendered config files from multi-entry `search` vaults in deterministic (`Vault`, `Path`) order.
- **Rationale**: Preserves the invariant that every vault is a list of entries (`search` stays a pure query view) while making each `custom_vault` entity a first-class consolidated K/V store.

## Non-Goals (Explicitly Out of Scope)

### `NG-001`: CLI Template Flags [Accepted]
- **Statement**: Do not add new CLI template flags (such as `--render` or `-T`) to `show`, `search`, or `run` in this initiative.
- **Rationale**: Templating is managed cleanly through `config.yaml` (`templates:`, vault/entity `attributes:`) and retrieved via existing `get`, `show`, `search`, and `run` commands.

### `NG-002`: Legacy `${scheme://...}` Dual-Syntax Support [Accepted]
- **Statement**: Do not maintain backward-compatible `${scheme://...}` interpolation alongside `{{ ... }}`.
- **Rationale**: Maintaining two interpolation syntaxes complicates escaping and template evaluation; bare `scheme://...` URIs remain supported in CLI flags (`get`, `show`, `-e`, `-m`), `merge:`, and `autoload.env`.

### `NG-003`: Merging Inside `SearchProvider` [Accepted]
- **Statement**: Do not change `SearchProvider` (`provider: search`) to merge multiple search results into a single entry on `GetEntry`/`GetSecret`.
- **Rationale**: A vault must always represent a list of entries; merging into a single K/V entry is the responsibility of composed `custom_vault` entities (and `-m` merge lists).

## Assumptions

### `ASM-001`: Standard Library `text/template` Sufficiency [Accepted]
- **Statement**: Go's `text/template` with `.Option("missingkey=error")` and a custom `FuncMap` satisfies all pipeline, iteration, and inclusion requirements without third-party template libraries (such as Sprig).
- **Validation**: Validated in `TSK-002` unit tests covering all required `FuncMap` functions and pipelines · **If false**: Implement any missing helper directly in `internal/engine/template.go` using Go stdlib.

---

## Requirements

### `REQ-001`: Top-Level `templates` Configuration & Loader [Accepted] — P0
- **Depends On**: `G-001`, `ADR-0001`
- **Statement**: WHEN `config.yaml` defines a top-level `templates:` map, the `config` package SHALL parse each template entry as either an inline template string or a structured object (`path` or `template`), resolving relative file paths against the directory of `config.yaml` and expanding `~/`.
- **Acceptance Criteria**:
  - [ ] Given a top-level `templates:` entry with an inline string `ssh_host: "Host {{ .meta.Title }}"`, when `config.Load` parses the configuration, then `cfg.Templates["ssh_host"]` is registered and available to `include "ssh_host" .`.
  - [ ] Given a top-level `templates:` entry with `path: "~/templates/model.tmpl"`, when loaded, then the file content is read and compiled as a Go `text/template`.
  - [ ] Given a `templates:` entry referencing a non-existent file path or invalid Go template syntax, when loaded or evaluated, then a descriptive error naming the template key is returned.
- **Rationale**: Centralizes reusable config templates in `config.yaml` or external files.

### `REQ-002`: Unified `{{ ... }}` Template Engine & `FuncMap` [Accepted] — P0
- **Depends On**: `G-001`, `REQ-001`, `ADR-0001`
- **Statement**: WHEN `resolve_values: true` is enabled on a vault or `custom_vault` entity, the `engine` package SHALL evaluate string values containing `{{ ... }}` using Go's `text/template` with `missingkey=error`, structured context `.` (`.meta`, `.attributes`, `.entries`), and the built-in `FuncMap` (`get`, `entry`, `search`, `toEnv`, `toKv`, `toJson`, `toYaml`, `upper`, `lower`, `env`, `snake`, `kebab`, `keys`, `values`, `join`, `include`, `includeFile`), while resolving bare `scheme://...` strings in explicit URI contexts.
- **Acceptance Criteria**:
  - [ ] Given an attribute containing `{{ get "work://entry:Password" | upper }}`, when resolved with `resolve_values: true`, then the secret is fetched and piped through `upper`.
  - [ ] Given an attribute referencing a non-existent key `{{ .attributes.DoesNotExist }}`, when rendered, then evaluation fails with a `missingkey=error` error instead of outputting `<no value>`.
  - [ ] Given `{{ .attributes | toEnv }}` or `{{ .attributes | toKv }}`, when rendered on an entry, then only `.attributes` are serialized in deterministic key order using dotenv formatting (`Title`, `Tags`, `UUID`, and `Group` metadata are never included in `.attributes`).
  - [ ] Given recursion exceeding depth 5 across `get`, `entry`, `search`, `include`, or `includeFile`, when evaluated, then resolution aborts with a max recursion depth error.
- **Rationale**: Provides a single, type-safe templating and secret lookup engine.

### `REQ-003`: Child Command Argument Pass-Through Safety [Accepted] — P0
- **Depends On**: `G-001`, `REQ-002`
- **Statement**: WHEN `cloakenv run` resolves child command arguments (`resolveCmdArgs`), the `EnvBuilder` SHALL only resolve arguments that are bare known `scheme://...` URIs or `{{ ... }}` templates that invoke a `cloakenv` lookup/inclusion function (`get`, `entry`, `search`, `include`, `includeFile`), passing all other `{{ ... }}` strings through untouched.
- **Acceptance Criteria**:
  - [ ] Given `cloakenv run -- docker inspect --format '{{.State.Status}}' container`, when `BuildEnvForCommand` resolves command arguments, then `'{{.State.Status}}'` is preserved verbatim without error.
  - [ ] Given `cloakenv run -- curl -H 'Authorization: Bearer {{ get "work://api:token" }}'`, when `BuildEnvForCommand` resolves command arguments, then the `{{ get ... }}` expression is resolved to the secret value.
- **Rationale**: Prevents breaking CLI tools (`docker`, `kubectl`, `helm`) that accept Go templates as CLI arguments.

### `REQ-004`: Metadata Interpolation in `mapping.key` [Accepted] — P0
- **Depends On**: `G-002`, `REQ-002`
- **Statement**: WHEN a `MappingRule` in `mapping:` specifies `{{ ... }}` expressions in `key:` (such as `key: "{{ .meta.Title | env }}_$1"`), `MappingProvider` SHALL expand regex capture groups (`$1`, `\1`) against the matched attribute key and render `{{ ... }}` against the entry's `.meta` context (`Title`, `Tags`, `UUID`, `Group`, `Path`, `Vault`, `Provider`).
- **Acceptance Criteria**:
  - [ ] Given an entry with `Title: "OpenRouter"` and attribute `Password: "secret"` and mapping rule `match: "^Password$"`, `key: "{{ .meta.Title | env }}_API_KEY"`, when mapped, then the resulting attribute key is `"OPENROUTER_API_KEY"`.
  - [ ] Given `GetSecret(ctx, "entry:OPENROUTER_API_KEY")` on a `MappingProvider` with metadata-interpolated keys, when queried, then the mapped attribute is authorized and resolved accurately.
- **Rationale**: Allows multiple entries with identical attribute names (`Password`) to be disambiguated by entry metadata before or during merging.

### `REQ-005`: Per-Entry `attributes:` Decorator Across All Vaults [Accepted] — P0
- **Depends On**: `G-002`, `REQ-002`, `REQ-004`
- **Statement**: WHEN any vault (`keepass`, `yaml`, `json`, `search`, `custom_vault`) configures `attributes: map[string]any`, `ProviderManager` SHALL wrap the vault with an attributes decorator executed AFTER `FilteringProvider`, `MappingProvider`, and `KeyFormatProvider` so each entry's `.attributes` map is populated with the filtered, mapped, and formatted attributes before evaluating and overlaying the configured `attributes:`.
- **Acceptance Criteria**:
  - [ ] Given a `search` or `keepass` vault with `include_fields: ["Password"]`, `key_format: "env"`, `resolve_values: true`, and `attributes: { block: '{{ include "model_block" . }}' }`, when `GetEntry`, `GetSecret`, or `Search` is called, then `.attributes` inside `model_block` contains `"PASSWORD"` and the returned entry includes `"block"` without `include_fields` stripping `"block"`.
  - [ ] Given `cloakenv get <vault>://<entry>:block`, when called on a vault with computed `attributes:`, then the rendered `block` string is returned.
- **Rationale**: Enables per-entry template rendering on any vault while guaranteeing templates see the clean post-filter/post-map `.attributes`.

### `REQ-006`: Composed `custom_vault` Entities with Multi-Source Merging [Accepted] — P0
- **Depends On**: `G-003`, `REQ-002`, `REQ-004`, `REQ-005`
- **Statement**: WHEN an entity under `custom_vault.entities.<name>` specifies composition keys (`merge`, `attributes`, `mapping`, `include_fields`, `exclude_fields`, `key_format`, `resolve_values`), `CustomVaultProvider` and `config` SHALL validate that no unknown top-level keys exist on that entity (while treating entities without composition keys as legacy flat attribute maps), expand all `merge:` URIs (including multi-entry `<vault>://` URIs via `Search`), apply the entity's `include_fields` -> `exclude_fields` -> `mapping` -> `key_format` to each source entry, fold them deterministically into `.attributes` (and populate `.entries`), and evaluate the entity's `attributes:` on the merged result.
- **Acceptance Criteria**:
  - [ ] Given a `custom_vault` entity `jules` with `merge: ["openrouter://", "work://ai/jules"]`, `mapping: [{ match: "^Password$", key: "{{ .meta.Title | env }}_API_KEY" }]`, `key_format: "env"`, and `attributes: { dotenv: "{{ .attributes | toEnv }}" }`, when `GetEntry("jules")` or `GetSecret("jules:dotenv")` or `run -m config://jules` is called, then all entries from `openrouter://` and `work://ai/jules` are transformed per source entry, merged in deterministic order, and rendered into `dotenv`.
  - [ ] Given a `custom_vault` entity with `merge: ["models://"]` and an `attributes:` template using `{{ range .entries }}[{{ .meta.Title }}]={{ .attributes.Password }}{{ end }}`, when evaluated, then `.entries` contains the ordered slice of transformed source entries.
  - [ ] Given a `custom_vault` entity that includes `merge:` alongside a misspelled top-level key `include_feilds:`, when `config.Load` runs, then config validation fails with an unknown field error.
  - [ ] Given a legacy `custom_vault` entity `custom1: { username: "bob", Password: "123", tags: ["test"] }` with no composition keys, when loaded and queried, then `username` and `Password` resolve as attributes with 100% backward compatibility.
- **Rationale**: Delivers single-entry K/V consolidation and post-merge templating without overloading `SearchProvider`.

### `REQ-007`: Multi-Entry Vault URI Expansion in CLI `-m` and `autoload.merge` [Accepted] — P1
- **Depends On**: `G-003`, `REQ-006`
- **Statement**: WHEN a vault URI with an empty entry path (`<vault>://`, such as `openrouter://`) is passed to `cloakenv run -m`, `cloakenv show -m`, `autoload.merge`, or `autoload.vaults`, the merge resolver SHALL expand all entries returned by `<vault>.Search(ctx, "")` in deterministic (`Vault`, `Path`) order if `GetEntry("")` indicates multiple entries.
- **Acceptance Criteria**:
  - [ ] Given a `search` vault `openrouter` matching 3 entries with disambiguated keys, when `cloakenv show -m openrouter://` or `cloakenv run -m openrouter:// -- env` is executed, then all 3 entries are merged in deterministic order.
- **Rationale**: Keeps `merge:` behavior consistent across `custom_vault` entities, `autoload`, `show -m`, and `run -m`.

---

## NFR Checklist

| Area | Covered by / N/A reason |
|---|---|
| Security & authz | Field filters (`include_fields`/`exclude_fields`) run before `attributes:` template evaluation; excluded fields are never exposed in `.attributes` (`REQ-005`, `REQ-006`). Never log resolved secret values. |
| Privacy / secrets | Zero persistence to disk; all template rendering occurs strictly in memory (`REQ-002`). |
| Performance & scale | Templates are compiled once or cached; concurrent merge resolution remains bounded by `maxConcurrency = 16` (`REQ-006`, `REQ-007`). |
| Reliability & failure handling | `missingkey=error` catches missing template keys; recursion depth capped at 5 across all template/URI calls (`REQ-002`). |
| Observability | Descriptive error wrapping naming vault, entity, attribute key, and template name on parse/render failure (`REQ-001`, `REQ-002`). |
| Migration, compatibility & rollback | Legacy flat `custom_vault` entities remain 100% compatible (`REQ-006`); `${scheme://...}` examples and tests migrated to `{{ get "scheme://..." }}` (`TSK-006`). |
| Accessibility | N/A (CLI / backend secret orchestrator with no GUI). |
| Cross-platform (Linux/macOS) | Path handling in `templates:` and `includeFile` uses `filepath` and cross-platform home expansion; tests pass on Linux, macOS, and Windows (`REQ-001`). |
| Docs & communication | Update `README.md`, `examples/config.yaml`, and `AGENTS.md` to reflect `{{ ... }}` templating and composed `custom_vault` entities (`TSK-006`). |
| Testing strategy | Race-enabled unit and integration tests (`go test -v -race ./...`) and benchmarks (`make test-all`). |

## Risks

### `RSK-001`: Infinite Recursion via Circular `include` or `get`/`entry` Calls in Templates [Accepted]
- **Affects**: `REQ-002`, `REQ-006` · **Likelihood**: Med · **Impact**: High
- **Mitigation**: Propagate `depth` through context/evaluator across `get`, `entry`, `search`, `include`, `includeFile`, and `custom_vault` entity `merge:`, aborting at `depth > 5` (`TSK-002`, `TSK-005`) · **Owner**: warpcode

### `RSK-002`: Filter Bypass via `.meta` or Unfiltered Source Entries in Templates [Accepted]
- **Affects**: `REQ-004`, `REQ-005`, `REQ-006` · **Likelihood**: Low · **Impact**: High
- **Mitigation**: Evaluate `attributes:` strictly after `FilteringProvider`, `MappingProvider`, and `KeyFormatProvider` so `.attributes` and `.entries` only ever contain authorized, post-filter attributes (`TSK-004`, `TSK-005`) · **Owner**: warpcode

## Open Questions

### `Q-001`: Should `{{ ... }}` replace `${scheme://...}` completely or coexist? [Answered]
- **Blocking**: No · **Blocks**: `REQ-002` · **Answer**: Answered 2026-10-24 — Completely replace `${...}` with `{{ ... }}` (`text/template`) under `resolve_values: true`, keeping bare `scheme://...` URIs for CLI arguments/flags, `merge:`, and `autoload.env`.

### `Q-002`: Should multi-entry merging live in `search` vaults or `custom_vault` entities? [Answered]
- **Blocking**: No · **Blocks**: `REQ-006` · **Answer**: Answered 2026-10-24 — Keep `search` vaults strictly as list-of-entries query views, and expand `custom_vault` entities to compose, filter, map, and merge multiple entries into named single K/V entries (`config://jules`).

---

## Milestones

### `M-001`: Config Schema & Unified `text/template` Engine [Accepted]
- **Depends On**: `REQ-001`, `REQ-002`, `REQ-003`
- **Deliverables**: Top-level `templates:` config parsing, composed `custom_vault` entity config validation, `text/template` evaluator with `FuncMap` and `missingkey=error`, and removal of `${...}` (`utils.ExpandString`).
- **Exit Criteria**: Config parsing and `Resolver` unit tests pass with `-race`.

### `M-002`: Metadata-Aware Mapping, Vault `attributes:`, & Composed `custom_vault` Entities [Accepted]
- **Depends On**: `REQ-004`, `REQ-005`, `REQ-006`, `REQ-007`
- **Deliverables**: `{{ .meta.* }}` support in `mapping.key`, universal vault `attributes:` decorator, composed `custom_vault` entity resolution (`merge`, `include_fields`, `exclude_fields`, `mapping`, `key_format`, `attributes`, `.entries`), and updated docs/examples.
- **Exit Criteria**: `make test-all` and `golangci-lint run` pass with full coverage of per-entry and merged template workflows.

---

## Tasks

> **Stacked PR Base Branch**: All task pull requests (`TSK-001`..`TSK-006`) MUST target `epic/2026-10-24-template-and-merge-consolidation` as their base branch (never `main`), followed by a final review PR from `epic/2026-10-24-template-and-merge-consolidation` → `main`.

### `TSK-001`: Extend `internal/config` for `templates:` and Composed `custom_vault` Entities [Done] — 2.0 pt
- **Depends On**: `REQ-001`, `REQ-006`, `ADR-0001` · **Milestone**: `M-001`
- **Target Files**: `internal/config/config.go`, `internal/config/config_test.go`
- **Evidence**: [PR #270](https://github.com/warpcode/cloakenv/pull/270) · Commit `107f543` · [Issue #262](https://github.com/warpcode/cloakenv/issues/262) (Parent: [Epic #261](https://github.com/warpcode/cloakenv/issues/261))
- **Description**:
  - Add `Templates map[string]TemplateConfig` (supporting both inline string YAML unmarshaling and `{ path, template }` struct unmarshaling) to `Config`.
  - Add structured `CustomEntityConfig` representation / validation for `VaultConfig.Entities` supporting strict mode when any composition key (`merge`, `attributes`, `mapping`, `include_fields`, `exclude_fields`, `key_format`, `resolve_values`) is present (rejecting unknown top-level keys) and flat mode when no composition keys are present.
  - Compile any regexes in entity-level `Mapping` rules during config validation.

### `TSK-002`: Implement `text/template` Evaluator & `FuncMap` Replacing `${...}` [Done] — 3.0 pt
- **Depends On**: `REQ-001`, `REQ-002`, `REQ-003`, `TSK-001` · **Milestone**: `M-001`
- **Target Files**: `internal/engine/template.go`, `internal/engine/resolver.go`, `internal/engine/env_builder.go`, `internal/utils/expand.go`, `internal/engine/resolver_test.go`
- **Evidence**: [PR #272](https://github.com/warpcode/cloakenv/pull/272) · Commit `d2864e7` · [Issue #263](https://github.com/warpcode/cloakenv/issues/263) (Parent: [Epic #261](https://github.com/warpcode/cloakenv/issues/261))
- **Description**:
  - Create `internal/engine/template.go` providing `TemplateContext` (`.meta`, `.attributes`, `.entries`) and `FuncMap` (`get`, `entry`, `search`, `toEnv`, `toKv`, `toJson`, `toYaml`, `upper`, `lower`, `env`, `snake`, `kebab`, `keys`, `values`, `join`, `include`, `includeFile`) with `.Option("missingkey=error")` and recursion depth tracking (`maxDepth = 5`).
  - Update `Resolver` to evaluate `{{ ... }}` templates when `resolve_values: true` (and resolve bare `scheme://...` strings in `ResolveWithKey`), removing `${...}` expansion.
  - Update `EnvBuilder.resolveCmdArgs` so child CLI arguments only evaluate bare known `scheme://...` URIs or `{{ ... }}` expressions invoking `get`, `entry`, `search`, `include`, or `includeFile`.

### `TSK-003`: Add `{{ .meta.* }}` Interpolation to `MappingProvider` [Accepted] — 2.0 pt
- **Depends On**: `REQ-004`, `TSK-002` · **Milestone**: `M-002`
- **Target Files**: `internal/provider/mapping.go`, `internal/provider/mapping_test.go`, `internal/provider/search.go`
- **Evidence**: [Issue #264](https://github.com/warpcode/cloakenv/issues/264) (Parent: [Epic #261](https://github.com/warpcode/cloakenv/issues/261))
- **Description**:
  - Extend `ApplyMappingAndFilteringToAttributes` / `MappingProvider` so `rule.Key` containing `{{ ... }}` is rendered with the entry's `.meta` context (`Title`, `Tags`, `UUID`, `Group`, `Path`, `Vault`, `Provider`) and case helpers (`env`, `snake`, `kebab`, `upper`, `lower`) in addition to `$1` / `\1` regex capture group expansion.
  - Ensure `MappingProvider.GetSecret`, `GetEntry`, and `Search` consistently authorize and resolve metadata-interpolated mapped keys.

### `TSK-004`: Implement Universal Per-Entry `attributes:` Decorator on Vaults [Accepted] — 2.0 pt
- **Depends On**: `REQ-005`, `TSK-002`, `TSK-003` · **Milestone**: `M-002`
- **Target Files**: `internal/provider/attributes.go`, `internal/provider/attributes_test.go`, `internal/engine/provider_manager.go`, `internal/engine/resolver.go`
- **Evidence**: [Issue #265](https://github.com/warpcode/cloakenv/issues/265) (Parent: [Epic #261](https://github.com/warpcode/cloakenv/issues/261))
- **Description**:
  - Create `AttributesProvider` decorator in `internal/provider/attributes.go` (preserving `SearchableProvider` and `ValueResolvableProvider` optional interfaces) that wraps any vault configured with `attributes:` after `FilteringProvider`, `MappingProvider`, and `KeyFormatProvider`.
  - Evaluate configured `attributes:` against each entry's post-filter/map/format `.meta` and `.attributes` context so computed/templated keys are available on `GetEntry`, `GetSecret`, and `Search`.

### `TSK-005`: Implement Composed `custom_vault` Entities & Multi-Entry Merge Expansion [Accepted] — 3.0 pt
- **Depends On**: `REQ-006`, `REQ-007`, `TSK-003`, `TSK-004` · **Milestone**: `M-002`
- **Target Files**: `internal/provider/custom_vault.go`, `internal/provider/custom_vault_test.go`, `internal/engine/provider_manager.go`, `internal/engine/env_builder.go`, `internal/cmd/show.go`
- **Evidence**: [Issue #266](https://github.com/warpcode/cloakenv/issues/266) (Parent: [Epic #261](https://github.com/warpcode/cloakenv/issues/261))
- **Description**:
  - Expand `CustomVaultProvider` to resolve composed entities: expand `merge:` sources (including multi-entry `<vault>://` URIs via `Search`), apply per-entity `include_fields` -> `exclude_fields` -> `mapping` -> `key_format` to each source entry, fold into `.attributes` and `.entries` in deterministic order, and evaluate entity `attributes:` templates.
  - Update `EnvBuilder.resolveMergeSources` and `cmd/show.go` (`resolveMergedEntries`) to expand multi-entry `<vault>://` URIs when merging.

### `TSK-006`: Update Tests, `examples/config.yaml`, `README.md`, and `AGENTS.md` [Accepted] — 1.5 pt
- **Depends On**: `TSK-002`, `TSK-003`, `TSK-004`, `TSK-005` · **Milestone**: `M-002`
- **Target Files**: `examples/config.yaml`, `README.md`, `AGENTS.md`, `internal/engine/engine_test.go`, `internal/provider/custom_vault_test.go`
- **Evidence**: [Issue #267](https://github.com/warpcode/cloakenv/issues/267) (Parent: [Epic #261](https://github.com/warpcode/cloakenv/issues/261))
- **Description**:
  - Migrate existing `${scheme://...}` test fixtures and examples to `{{ get "scheme://..." }}`.
  - Document `templates:`, `{{ ... }}` syntax and `FuncMap`, per-vault `attributes:`, and composed `custom_vault` entities in `README.md`, `examples/config.yaml`, and `AGENTS.md`.
  - Run `make test-all` and `golangci-lint run` to verify zero regressions.

## Revision Log

| Date | Action | IDs | Evidence | Rationale |
|---|---|---|---|---|
| 2026-10-24 | Created plan | `G-001`..`G-003`, `NG-001`..`NG-003`, `REQ-001`..`REQ-007`, `TSK-001`..`TSK-006`, `ADR-0001` | — | Socratic design review completed for unified `text/template` engine, per-entry vault attributes, and composed `custom_vault` entities |
