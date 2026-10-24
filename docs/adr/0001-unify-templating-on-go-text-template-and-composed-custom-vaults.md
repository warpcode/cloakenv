# ADR-0001: Unify Templating on Go `text/template` and Composed `custom_vault` Entities

| Metadata | Details |
|---|---|
| **Status** | Accepted |
| **Date** | 2026-10-24 |
| **Deciders** | warpcode |
| **Depends On** | `REQ-001`, `REQ-002`, `REQ-003`, `REQ-004` |
| **Reversibility** | One-way |

---

## 1. Context and Problem Statement

`cloakenv` needs to generate configuration file outputs and consolidated environment variables from vault entries in two modes: (1) mapping each entry in a vault/search to a rendered template block, and (2) merging multiple entries (including whole `search` vaults) into a single K/V store for environment injection (`run`) or consolidated template rendering (`get`/`show`). Previously, `cloakenv` used a custom `${scheme://...}` string interpolator with no pipeline, iteration, or current-entry context support, and `custom_vault` entities only supported static inline K/V maps.

## 2. Decision Drivers

- **P0 (Non-Negotiables)**: Zero new external dependencies (use Go stdlib), strict separation between multi-entry vaults (`search`) and single-entry K/V stores (`custom_vault` entities), and fail-closed template evaluation (`missingkey=error`) so secret typos never silently produce `<no value>`.
- **P1 (Differentiators)**: Single unified templating syntax (`{{ ... }}`) replacing `${scheme://...}`, per-entry computed `attributes` on all vault types, and metadata-aware `mapping.key` disambiguation (`{{ .meta.Title | env }}_$1`).
- **P2 (Tie-Breakers)**: Idiomatic Go `text/template` pipeline semantics (`|`) and backward compatibility for legacy flat `custom_vault` entities and CLI bare URIs.

---

## 3. Considered Options

1. **Option 1**: Standardize on Go stdlib `text/template` (`{{ ... }}`) replacing `${scheme://...}`, add per-entry `attributes:` across all vaults, and expand `custom_vault` entities with `merge`, `include_fields`, `exclude_fields`, `mapping`, and `key_format`.
2. **Option 2**: Extend the custom `${...}` parser with bespoke pipeline syntax (`${. | toKv}`) and nested expression parsing alongside Go `text/template`.
3. **Option 3**: Overload `SearchProvider` (`provider: search`) to merge entries directly when accessed via `GetEntry`/`GetSecret` and add CLI-only template flags.

---

## 4. Decision Outcome

**Chosen option**: **Option 1 (Unified Go `text/template` + Composed `custom_vault` Entities)**, because it eliminates dual-syntax complexity (`${...}` vs `{{ ... }}`), leverages Go's battle-tested `text/template` engine with zero external dependencies, and preserves the architectural invariant that a vault is always a collection of entries while a single entry (`config://jules`) is a K/V store.

> *In the context of `internal/engine`, `internal/provider`, and `internal/config`, facing the need for both per-entry and post-merge configuration templating without fracturing vault abstractions, we decided to replace `${scheme://...}` with Go `text/template` (`{{ ... }}`) under `resolve_values: true` and expand `custom_vault` entities into composable K/V stores to achieve unified templating and multi-entry consolidation, accepting the breaking removal of `${scheme://...}` interpolation.*

### Positive Consequences
- Single template/interpolation engine (`text/template`) handles secret URI lookups (`{{ get "vault://entry:attr" }}`), current-entry formatting (`{{ .attributes | toEnv }}`), metadata key mapping (`{{ .meta.Title | env }}_$1`), and named/file template inclusions (`include`, `includeFile`).
- Every vault remains a list of entries, while `custom_vault` entities (`config://jules`) act as first-class merged K/V entries usable directly with `cloakenv get`, `cloakenv show`, and `cloakenv run -m`.
- Evaluating `attributes:` after `include_fields` -> `exclude_fields` -> `mapping` -> `key_format` ensures templates receive the exact filtered, mapped, and formatted `.attributes` map.

### Negative Consequences / Trade-offs
- Existing configs using `${scheme://...}` must migrate to `{{ get "scheme://..." }}` (while bare `scheme://...` URIs in CLI flags, `merge:`, and `autoload.env` continue working unchanged).

---

## 5. Pros and Cons of the Options

### Option 1: Unified Go `text/template` + Composed `custom_vault` Entities
- Good, because it uses Go's standard library (`text/template`) with zero new dependencies.
- Good, because `.meta`, `.attributes`, and `.entries` cleanly separate entry metadata from K/V attributes (`toEnv`/`toKv` never leak `Title` or `Tags`).
- Good, because strict/legacy mode detection on `custom_vault` entities catches config key typos on composed entities while preserving 100% backward compatibility for flat entities.
- Bad, because `${scheme://...}` syntax in existing test fixtures and example configs must be updated to `{{ get "scheme://..." }}`.

### Option 2: Custom `${...}` Pipeline Parser
- Good, because it preserves unquoted `${scheme://...}` inside `${...}`.
- Bad, because it duplicates `text/template` lexing, pipelines, loops, and conditionals in custom parser code and conflicts with Go `regexp` `${name}` capture group syntax in `mapping.key`.

### Option 3: Overload `SearchProvider` to Merge Entries Directly
- Good, because it avoids defining a `custom_vault` entity over a `search` vault.
- Bad, because it violates the core abstraction that a vault is a list of entries and overloads `search` with single-entry merge responsibilities.

---

## 6. Fitness Function & Validation

- **Fitness Function**:
  1. Unit and integration tests in `internal/engine/` and `internal/provider/` verifying `{{ ... }}` evaluation (`missingkey=error`), `FuncMap` functions (`get`, `entry`, `search`, `toEnv`, `toKv`, `toJson`, `toYaml`, `upper`, `lower`, `env`, `snake`, `kebab`, `keys`, `values`, `join`, `include`, `includeFile`), per-vault `attributes:`, and composed `custom_vault` entities merging `search` vaults.
  2. Child command argument safety tests verifying `cloakenv run -- docker inspect --format '{{.State.Status}}'` passes through untouched.
- **Enforcement**: `make test` (`go test -v -race ./...`), `make vet`, and `golangci-lint run`.
