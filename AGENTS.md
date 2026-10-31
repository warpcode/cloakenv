# Agent Instructions for cloakenv

> [!IMPORTANT]
> These instructions apply to all AI agents working within `cloakenv`. They complement, and never override, global rules in `~/.agents/AGENTS.md`.

`cloakenv` is a pluggable secret orchestrator and dynamic runtime environment injector written in Go.
It resolves typed secret URIs from multiple backends into temporary execution memory with zero plaintext disk persistence.

## Package Contracts

| Package | Responsibility | Invariant / Must NOT |
|---|---|---|
| `internal/cmd` | Top-level CLI subcommand dispatch and flag parsing | No secret resolution or direct backend provider instantiation |
| `internal/config` | Parse and validate YAML configuration | No secret resolution or I/O beyond file reads |
| `internal/engine` | Orchestrate URI resolution, caching, and env injection | No direct imports of backend-specific libraries |
| `internal/provider` | Implement `provider.SecretProvider` per backend | No shared mutable state between providers |
| `internal/runner` | Process execution wrapping and environment injection | No secret leakage into persistent state or child processes |
| `internal/utils` | Shared string, slice, and formatting utilities | No business logic, config loading, or URI resolution |
| `internal/yaml` | Centralized YAML encode/decode and error wrapping | No business logic or URI resolution |

## Verification Gate

All code modifications must pass the project verification command before review or merge:
```bash
go build ./... && go vet ./... && gofmt -l . && go test -race ./...
```
Equivalently, run `make verify` to execute this pipeline. Run `make agents-check` to verify documentation synchronization, package layout contracts, and referenced skills existence.

## 🔒 Security Constraints

1. **Never log secret values**:
   Debug output, test logs, and error messages must never contain resolved secret values.
2. **Zero plaintext disk persistence**:
   Secrets exist strictly in execution memory; encrypted cache (`cache://`) key lives in OS keyring.
3. **Test credentials for testing only**:
   Fixture credentials (`testdata/testDB.kdbx`) must never appear in production examples.
4. **No hardcoded credentials**:
   Never commit secrets, API keys, or passwords anywhere in source code or documentation.
5. **Keyring isolation**:
   Keyring operations go through `internal/provider/os_keyring.go`; tests must use `keyring.MockInit()` and temporary `HOME`/`XDG_CACHE_HOME`.

## 🤖 AI Guardrails

- **Surgical edits only**:
  Change only what is necessary to fulfill the request. Never perform unrequested refactors.
- **Preserve existing behavior**:
  Never delete existing error messages, logging, comments, or essential logic.
- **Dependency lock**:
  Never add external dependencies to `go.mod` without explicit user approval.
- **Workspace hygiene**:
  Never write temporary, review, or scratch files to the workspace root; use `/tmp/` or conversation scratch dirs.

## 🧬 Core Invariants

- **Entry & Tag Immutability**:
  `Entry.Tags` must never be mutated or lowercased at parse time; case-insensitive matching occurs strictly dynamically in `Search`.
- **Provider Interface & Wrappers**:
  Backends implement `provider.SecretProvider`. Wrappers (`FilteringProvider`, `MappingProvider`, etc.) must preserve optional interfaces (`SearchableProvider`, `ValueResolvableProvider`) by interface presence, not return value.
- **Wrapper Ordering**:
  Vault decorators wrap in strict order: `FilteringProvider` -> `MappingProvider` -> `KeyFormatProvider` -> `AttributesProvider`. Wrappers compose; they must never replace filtering or bypass to unfiltered underlying `GetSecret`.
- **Filter Precedence & Exemptions**:
  `include_fields` takes precedence over `exclude_fields`. Mapped fields always show regardless of include/exclude filters.
- **Filter Authorization**:
  `FilteringProvider.GetSecret` and `FilterEntryWithPath` evaluate canonical colon-qualified candidates (`entry:attr`, `title:attr`) and dot paths; single-entity and root-keyed vaults must not fall through to unredacted retrieval.
- **Unified Templating**:
  Templates evaluate Go `text/template` with `.Option("missingkey=error")` and recursion depth <= 5; legacy `${...}` interpolation is permanently removed (`docs/adr/0001-unify-templating-on-go-text-template-and-composed-custom-vaults.md`).
- **Child Command Pass-Through**:
  `resolveCmdArgs` evaluates only bare URIs or cloakenv template functions (`get`, `entry`, `search`, `include`, `includeFile`), preserving foreign format flags (e.g. Docker format).

## 🧭 Skills & Documentation Pointers

| Task / Domain | Target Resource | Purpose |
|---|---|---|
| Implement a secret backend | `.github/skills/cloakenv-add-provider` | Provider SOP, checklist, and `provider_checklist.sh` |
| Review PRs & Jules runs | `.github/skills/cloakenv-pr-review` | PR audit SOP, review traps, and `pr_review_bundle.sh` |
| Session health & stuck runners | `google-jules-triage` | Nudge-first protocol, stalled VM triage, and recovery |
| Author pull requests | `github-pr` | Conventional commits, PR creation, and lifecycle |
| Git workflow & commits | `CONTRIBUTING.md` | Branching, rebasing, and Conventional Commit format |
| Test fixtures & credentials | `testdata/README.md` | Test database schema, entries, and master passwords |
| Architecture decisions | `docs/adr/`, `docs/plans/` | ADR-0001 (templating/merge), RFCs, and design specs |
| Build targets & user docs | `README.md`, `Makefile` | CLI documentation, configuration reference, and targets |

## 🛠️ Automated Maintenance

Rather than ritualistic end-of-task review checklists, repository integrity is enforced automatically:
- Run `make agents-check` to verify that Makefile targets, internal package contracts, and referenced skills remain synchronized.
- Run `make test-all` for full local verification (formatting, vetting, unit tests with race detection, and benchmarks).
