# Agent Instructions for cloakenv

> [!IMPORTANT]
> These instructions apply to all AI agents working within `cloakenv`. They complement, and never override, global rules in `~/.agents/AGENTS.md`.

`cloakenv` is a declarative, zero-leak secret orchestrator and dynamic runtime environment injector written in Go.
It resolves raw entries from read-only secret vaults through a declarative `RuleEngine` into named `envs` and `templates` with zero plaintext disk persistence and redacted inspection.

## Package Contracts

| Package | Responsibility | Invariant / Must NOT |
|---|---|---|
| `internal/audit` | Structured JSON-lines audit logging for secret access attempts | No plaintext secret values or resolved payloads in log output |
| `internal/cmd` | Top-level CLI subcommand dispatch (`run`, `inspect`, `auth`, `doctor`, `cache`, `completion`, `version`, `internal`) and flag parsing | No secret resolution, plaintext secret printing, or direct backend provider instantiation |
| `internal/config` | Parse and validate YAML configuration (`vaults`, `envs`, `templates`, `autoload`) | No secret resolution or I/O beyond file reads |
| `internal/engine` | Orchestrate vault loading, `RuleEngine` transforms, `EnvsBuilder`, `TemplatesRenderer`, autoload matching, and caching | No direct imports of backend-specific libraries |
| `internal/provider` | Implement read-only `provider.SecretProvider` / `provider.SearchableProvider` per backend (`keepass`, `pass`, `vault`, `env`, `keyring`, `cache`) | No shared mutable state between providers; no filtering, mapping, or template transforms |
| `internal/runner` | Process execution wrapping, environment injection, streaming Aho-Corasick output masking, and temp-file zeroing | No secret leakage into persistent state or unmasked child output |
| `internal/utils` | Shared string, slice, YAML, and formatting utilities | No business logic, config loading, or secret resolution |

## Verification Gate

All code modifications must pass the project verification command before review or merge:
```bash
go build ./... && go vet ./... && gofmt -l . && go test -race ./...
```
Equivalently, run `make verify` to execute this pipeline. Run `make agents-check` to verify documentation synchronization, package layout contracts, and referenced skills existence.

## 🔒 Security Constraints

1. **Never log secret values**:
   Debug output, test logs, audit logs, traces, `cloakenv inspect`, and error messages must never contain resolved secret values.
2. **Zero plaintext disk persistence**:
   Secrets exist strictly in execution memory (or mode-`0700`/`0600` `O_NOFOLLOW` temp files zeroed on child exit); per-vault encrypted disk cache (`cache.mode: encrypted_disk`) key lives in OS keyring.
3. **Test credentials for testing only**:
   Fixture credentials (`testdata/testDB.kdbx`) must never appear in production examples.
4. **No hardcoded credentials**:
   Never commit secrets, API keys, or passwords anywhere in source code or documentation.
5. **Keyring isolation**:
   Keyring operations go through `internal/provider/os_keyring.go`; tests must use `keyring.MockInit()` and temporary `HOME`/`XDG_CACHE_HOME`. Internal keyring keys (`cache_token`, `cache_disk_token`, `cache-master-key`, `cache/*`, `cache_v2/*`, `provider/*`) must never be exposed through the built-in `keyring` vault.

## 🤖 AI Guardrails

- **Surgical edits only**:
  Change only what is necessary to fulfill the request. Never perform unrequested refactors.
- **Preserve existing behavior**:
  Never delete existing error messages, logging, comments, or essential logic.
- **Dependency lock**:
  Never add external dependencies to `go.mod` without explicit user approval.
- **Workspace hygiene**:
  Never write temporary, review, or scratch files to the workspace root; use `/tmp/` or conversation scratch dirs.
- **Documentation synchronization**:
  Whenever any feature, provider, rule, template function, configuration key, or CLI command/flag is added, modified, or removed, audit and update the affected guides in `docs/` (see `docs/README.md`), `README.md`, `examples/config.yaml`, `AGENTS.md`, and `docs/adr/` / `docs/plans/` in the same change so documentation never drifts from code ground truth.

## 🧬 Core Invariants

- **Pure Read-Only Vaults & Flat `Entry`**:
  Vault backends implement read-only `provider.SecretProvider` (`Scheme`, `Initialize`, `GetSecret`, `Validate`) and `provider.SearchableProvider` (`Search`, `GetEntry`) returning raw flat `Entry` (`map[string]any`) maps containing canonical metadata (`title`, `path`, `uuid`, `tags`, `group`) alongside raw secret fields. `Entry.Tags` must never be mutated or lowercased at parse time; case-insensitive matching occurs strictly dynamically.
- **Built-in `env` & `keyring` Vaults**:
  `env` and `keyring` are implicitly available as read-only vaults in `envs:` and `templates:` without explicit `vaults:` blocks, while `vaults:` may only override `env` with `provider: env` or `keyring` with `provider: keyring`.
- **Declarative `RuleEngine` Pipeline**:
  All filtering, key remapping, formatting, and value transformation happen in `internal/engine/rules.go` (`pass`, `match`, `set` with last-rule-wins semantics and reserved metadata key protection) and `internal/engine/output_pipeline.go` (`EnvsBuilder`, `TemplatesRenderer`), never inside provider wrappers.
- **Unified Pure Go Templating**:
  Templates evaluate Go `text/template` with `.Option("missingkey=error")` and pure helper functions (`hasTag`, `default`, `coalesce`, `toEnv`, `toKv`, `toJSON`, `fromJSON`, `toYAML`, `fromYAML`, `upper`, `lower`, `env`, `snake`, `kebab`, `keys`, `values`, `join`, `shquote`). Legacy URI/exec/include template functions (`get`, `tryGet`, `entry`, `search`, `include`, `includeFile`, `exec`) and `${...}` interpolation are permanently removed.
- **Zero-Leak CLI & Verbatim Child Command Pass-Through**:
  The CLI exposes only `run`, `inspect` (unconditional `<REDACTED>`), `auth`, `doctor`, `cache`, `completion`, `version`, and `internal`. Child command arguments in `BuildEnvForCommand` pass through completely verbatim without template evaluation or secret expansion.

## 🧭 Skills & Documentation Pointers

| Task / Domain | Target Resource | Purpose |
|---|---|---|
| Implement a secret backend | `.github/skills/cloakenv-add-provider` | Provider SOP, checklist, and `provider_checklist.sh` |
| Review pull requests | `.github/skills/cloakenv-pr-review` | PR audit SOP, review traps, and `pr_review_bundle.sh` |
| Author pull requests | `github-pr` | Conventional commits, PR creation, and lifecycle |
| Git workflow & commits | `CONTRIBUTING.md` | Branching, rebasing, and Conventional Commit format |
| Test fixtures & credentials | `testdata/README.md` | Test database schema, entries, and master passwords |
| Architecture decisions | `docs/adr/`, `docs/plans/` | ADR-0001 to ADR-0003, RFCs, and design specs |
| User & operator guides | `docs/README.md` | Full documentation portal, provider guides, pipeline, and cookbooks |
| Build targets & user docs | `README.md`, `Makefile` | CLI overview, configuration reference, and targets |

## 🛠️ Automated Maintenance

Rather than ritualistic end-of-task review checklists, repository integrity is enforced automatically:
- Run `make agents-check` to verify that Makefile targets, internal package contracts, and referenced skills remain synchronized.
- Audit `docs/`, `README.md`, and `examples/config.yaml` alongside `make agents-check` whenever behavior, CLI flags, or configuration schemas change.
- Run `make test-all` for full local verification (formatting, vetting, unit tests with race detection, and benchmarks).
