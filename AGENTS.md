# Agent Instructions for cloakenv

> [!IMPORTANT]
> These instructions apply to all AI agents working within the `cloakenv` repository.
> They complement, and never override, the global rules in `~/.agents/AGENTS.md`.

---

## 🏗️ Project Overview

`cloakenv` is a **pluggable secret orchestrator and dynamic runtime environment injector** written in Go. It wraps application binaries, resolves secret URIs from multiple configurable backends (KeePass, OS keyring, YAML, JSON, stored search, environment, encrypted cache), and injects secrets strictly into temporary execution memory — never persisting them to disk unencrypted.

### Key Design Principles

- **Zero-persistence**: Secrets are resolved at runtime and never written to plaintext files.
- **URI-addressed secrets**: Every secret is referenced by a typed URI (e.g., `keepass://group/entry:attr`, `keyring://service/account`).
- **Pluggable providers**: Adding a new backend means implementing the `provider.SecretProvider` interface only.
- **Cross-platform**: CI runs on Linux, macOS, and Windows. All code must compile and pass tests on all three.

---

## 📁 Project Structure

```
main.go                  # CLI entrypoint — config & router logic only
internal/
  cmd/                   # Top-level subcommand implementations
  config/                # YAML config parser (no business logic)
  engine/                # Orchestrator core — resolves URIs, injects env
  provider/              # Built-in & custom secret vaults
  runner/                # Process execution wrapping logic
  utils/                 # Shared formatting and flag utilities
  yaml/                  # Centralized YAML parsing, serialization, and error wrapping
examples/                # Example databases and config.yaml
testdata/                # Test fixtures (testDB.kdbx, YAML/JSON samples)
Makefile                 # Build, test, fmt, vet, install targets
```

### Internal Package Contracts

| Package | Responsibility | Must NOT |
|---|---|---|
| `internal/config` | Parse and validate `config.yaml` | Resolve secrets or perform I/O beyond file reads |
| `internal/engine` | Orchestrate URI resolution, caching, env injection | Directly import provider-specific libraries |
| `internal/provider` | Implement `provider.Provider` interface per backend | Share mutable state between providers |
| `internal/yaml` | Centralize YAML encoding/decoding and error context wrapping | Perform business logic or URI resolution |

---

## 🛠️ Development Workflow

### Build & Run

```bash
make build            # Compiles to bin/cloakenv
make run              # go run .
make install          # Installs using go install (defaults to $GOBIN or $GOPATH/bin)
make uninstall        # Removes the installed binary using go clean -i
```

### Testing

```bash
make test             # go test -v -race ./...
make bench            # Run benchmarks (go test -bench=. ./internal/engine/...)
make test-all         # Run formatting, vetting, unit tests, and benchmarks (runs fmt, vet, test, and bench targets)
```

> [!IMPORTANT]
> Always run tests with the **race detector** (`-race`) before committing. CI enforces this.

### Linting & Formatting

```bash
make fmt              # go fmt ./...
make vet              # go vet ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.1 run  # Full lint (mirrors CI)
```

> [!WARNING]
> **All CI checks must pass before any merge.** CI runs lint, and tests on
> `ubuntu-latest`, `macos-latest`, and `windows-latest`.
>
> Lint configuration lives in `.golangci.yml` (golangci-lint v2 format). The
> pinned golangci-lint version must be built with the same Go major version as
> `go.mod` — older binaries fail with "export data version" typecheck errors.

---

## 🧬 Coding Conventions

### Go Style

- Generic Go style, error handling, table-driven tests, and UTF-8 string scanning conventions are governed by `~/.agents/rules/go.instructions.md`.
- Benchmark functions live in `*_benchmark_test.go` files within the same package.
- **Tag Parser Consistency**: `matchEntryTags` (search filtering) and `toEntry` (serialization) must agree on what a tag is; differential-test rewrites against pre-change behavior.
- **Entry & Tag Immutability**: `Entry.Tags` must NEVER be mutated (e.g. lowercased) at parse time. Lowercasing tags at parse time destroys original casing and breaks data fidelity for `GetEntry()` callers. Case-insensitive tag matching must always be performed dynamically within the `Search` loop only (matching the pattern in `custom_vault.go` and `keepass.go`). When handling provider configs with `cfg.Tags` (such as in `internal/provider/static.go`), beware of regression paths that bypass lowercasing logic when `len(tags) == 0`.

### Interfaces & Extensibility

- The `provider.SecretProvider` interface is the **core extension point**. Adding a new backend = new file in `internal/provider/`, implementing the interface. Do not modify the interface signature without a plan review.
- URI scheme registration happens in the engine; new providers must be registered there explicitly.
- **Provider wrapper contracts**: Wrappers must preserve optional capabilities such as `ValueResolvableProvider`; the engine gates URI expansion by interface presence, not by the method return value. Do not unconditionally add optional interfaces to wrappers.
- **Provider-specific URI parsing**: YAML/JSON locations use dot paths, KeePass/custom vaults use `entity:attribute`, and search providers may resolve attribute names case-insensitively. Field filters must authorize the effective canonical key and full static path before delegating.
- **Filtered static resolution**: Root-keyed and nested YAML/JSON paths must be resolved and filtered before any fallback to the underlying provider; preserve the provider's native serializer.
- **Filtering projection invariants**: Apply field filters recursively to structured maps and arrays, using canonical root and leaf paths consistently across `GetEntry`, `Search`, and `GetSecret`. For virtual search, apply the policy before source-value resolution and stored-query evaluation, and resolve the canonical key and returned value from the same search snapshot. Preserve actual attribute-map entries before metadata sentinels. Nested virtual-search policies must compose before source resolution: intersect active include allowlists and union exclusions rather than replacing an inherited policy. Authorize the provider's authoritative result identity rather than a caller alias, include numeric array indices in canonical paths, and use effective inferred roots before dynamic resolution; isolation regressions must enable `ResolveValues` and verify excluded URIs are never dereferenced.

### File Naming

- Source files: `snake_case.go`
- Test files: `<source_file>_test.go`
- Benchmark files: `<source_file>_benchmark_test.go`

### Dependencies

- All dependencies are managed via `go.mod` / `go.sum`.
- Do not add new dependencies without explicit user approval. Prefer stdlib where possible.
- Cross-platform dependencies only — any OS-specific code must be gated with build tags.

---

## 🔒 Security Constraints

> [!CAUTION]
> This project handles live credentials. These rules are non-negotiable.

1. **Never log secret values.** Debug output, test output, and error messages must never contain resolved secret values.
2. **Never write plaintext secrets to disk.** The encrypted cache (`cache://`) is the only on-disk secret store, and its encryption key lives in the OS keyring.
3. **Testdata credentials are for testing only.** `testdata/testDB.kdbx` uses `password123` — this must never appear in production config examples.
4. **No hardcoded credentials** anywhere in source, comments, or examples. Use placeholder strings like `<your-password>` in documentation.
5. **Cross-platform keyring operations** must go through `internal/provider/os_keyring.go` via `go-keyring`. Do not bypass the abstraction layer.

---

## 🔀 Git Workflow

- **Default branch**: `main`
- **Strategy**: Rebase on pull (`git pull --rebase`).
- **Merge strategy**: Squash-and-merge for PRs.
- **Branch cleanup**: Delete remote branches immediately after merge.
- **CI gate**: All GitHub Actions (lint + cross-platform tests) must be green before any merge.

### Commit Messages

Use [Conventional Commits](https://www.conventionalcommits.org/) format:

```
feat(provider): add JSON provider with entities_root_key support
fix(engine): handle missing URI scheme gracefully
test(engine): add benchmark for concurrent URI resolution
docs: update README with JSON provider usage
```

---

## 🧪 Testing Standards

### Unit Tests

- Every new exported function or method in `internal/` must have a corresponding `_test.go` entry.
- Use `testify` only if already present in `go.mod`; otherwise use stdlib `testing` and `errors` packages.
- Mock or stub external I/O (keyring, filesystem) in unit tests. Integration tests requiring real keyring access must be skipped in CI via `t.Skip()` or build tags.
- **Keyring/Cache Testing Isolation**: Tests that execute cache operations (such as `ClearCache()`) or interact with keyring providers must invoke `keyring.MockInit()` and set environment variables (`HOME`, `XDG_CACHE_HOME`, `LocalAppData`) to a temporary directory (`t.TempDir()`) during test setup to prevent local cache erasure and test leakage.
- **Filtering regression coverage**: Cover recursive map/array projection, rooted and rootless static paths through scalar and structured APIs, changing search snapshots, and real `Title`/`Tags` attributes.

### Integration Tests

The `testdata/testDB.kdbx` database provides a stable fixture for KeePass integration tests:
- **Master Password**: `password123`
- **Entry path**: `website/Test Website`
- **Attributes**: `Password` (`testPassword123!`), `UserName` (`user@email.com`), attachment `hello.txt`

### Benchmarks

- Benchmark functions are named `BenchmarkXxx` and live in `*_benchmark_test.go`.
- Run with `go test -bench=. -benchmem ./internal/engine/...` to capture allocations.
- Do not mix benchmark and unit test logic in the same file.

---

## 🤖 AI Agent Guardrails

### Scope

- Only modify files within the `cloakenv` workspace.
- Do not modify files outside this workspace (e.g., `~/.agents/AGENTS.md`) unless the user explicitly requests a global memory update.
- **Workspace Hygiene**: Subagents and audit tasks must NEVER write files to the workspace root (`/home/jase/src/cloakenv/`) during non-invasive audits or reviews. All temporary files, review payloads, and scratch scripts must be written to `/tmp/` or the agent scratch directory (`scratch/`).

### Code Changes

- **Surgical edits only**: Change only what is necessary to fulfill the request.
- **Preserve error messages and logging**: Never silently remove existing error handling or log statements.
- **No unrequested refactors**: Do not restructure, rename, or reorganize code beyond the stated scope.
- **No new dependencies**: Do not add `go get` calls or modify `go.mod` without explicit user approval.
- **Verify compilation**: After any Go change, confirm the build still passes (`make build` or `go build ./...`).

### Testing Gate

> [!IMPORTANT]
> After any code change, always verify:
> 1. `make build` succeeds.
> 2. `go test -race ./...` passes.
> 3. `go vet ./...` is clean.

### AGENTS.md Review

> [!IMPORTANT]
> At the end of **every task**, re-read this file and verify it still accurately reflects
> the codebase. If any section is stale (e.g., a new provider was added, a Makefile target
> changed, a dependency was approved), update it before closing the task.

Questions to check before finishing:
- Does the **Project Structure** map still match the directory layout?
- Are all **Makefile targets** listed accurately?
- Do the **Testing Standards** reflect the current test fixtures and benchmark conventions?
- Does the **Provider Development Checklist** cover all required steps for a new backend?
- Are any **dependencies** in `go.mod` not yet documented in the Coding Conventions?

### Security Hygiene

- Never print, log, or output resolved secret values during any agent task.
- Do not create test fixtures that contain real credentials.
- If a task would require exposing a real secret, stop and ask the user how to proceed.

### Pull Request Workflow

Formal PRs must be created using the `github-pull-requests` skill. Required PR fields:

| Field | Requirement |
|---|---|
| Title | Conventional Commits format (`feat:`, `fix:`, `docs:`, etc.) |
| Body | What changed, why, and how to test it |
| CI | All checks green before requesting review |
| Branch | Delete immediately after merge |

### cloakenv-specific review traps

Generic Jules/bot-PR handling: see the `google-jules-triage` and `review-pull-request` skills. When recovering wedged branches via `tidy/` branches, the cloakenv verification command before committing is:
`go build ./... && go vet ./... && gofmt -l . && go test -race ./...`.

- **Smuggled Autoload Refactors**: Jules consistently attempts an unrequested `regexCache sync.Map` autoload refactor across unrelated feature PRs, frequently accompanied by removing `CompileAutoloadRules()` or inlining `normalizeURIs`. Strictly reject these out-of-scope refactors under the "No unrequested refactors" rule.
- **Spurious Workflow Deletions**: Jules repeatedly deletes `.github/workflows/reject-empty-commit.yml` across unrelated PRs. Always verify this workflow file remains intact.
- **Checklist Non-Compliance**: Jules routinely implements Go provider code but skips updating `README.md` and `examples/config.yaml`. Enforce all items in the *Provider Development Checklist*.
- **Virtual Provider & Switch Case Drift**: When reviewing PRs modifying `initVaultProvider`, verify that recently added provider cases on `main` (such as `case "search":`) are not inadvertently dropped due to outdated branch origins, and that generic decorators (such as filtering or caching) wrap virtual providers alongside direct providers.
- **Single-Entity & Dot-Path Filter Bypass**: When reviewing field filtering logic, verify that lookups without colons in single-entity vaults (YAML/JSON) or root-keyed vaults (`entities_root_key`) do not default to checking `"Password"` or fall through to unredacted secret retrieval.
- **Wrapper-Replaces-Filter Regression**: Observed on warpcode/cloakenv#210 (regex `mapping`): `initVaultProvider` used `if len(vault.Mapping) > 0 { NewMappingProvider } else if include/exclude { NewFilteringProvider }`, which dropped `FilteringProvider` whenever `mapping` was set; the new wrapper re-implemented filtering on bare leaf keys, and its `GetSecret` fell through to `underlying.GetSecret` unfiltered, so excluded or renamed-original keys stayed readable. When a PR adds a provider wrapper, check that (1) it composes with `FilteringProvider` rather than replacing it, (2) there is no fallback to the unfiltered underlying `GetSecret` after a miss, and (3) mapped-key collisions are deterministic (Go map iteration order) and cannot earn the filter exemption.
- **Colon-Qualified Filter Candidates**: `FilteringProvider.GetSecret` builds colon-qualified candidates — `entryPath+":"+effectiveAttr` (`filter.go:674`), `entryTitle+":"+effectiveAttr` (`:690`), `entityLoc+":"+effectiveAttr` (`:740`) — while the generic `FilterEntryWithPath`/`projectRecursive` builds dot paths only. `MappingProvider` authorizes through the generic filter (`mapping.go:152`), so the colon form of `exclude_fields` (documented at `README.md:587`, e.g. `website/Test Website:*`, `website/*:Password`) is silently unenforced the moment any `mapping:` block exists — even one whose rules match nothing. Observed on warpcode/cloakenv#212 and reproduced end-to-end against `testdata/testDB.kdbx`. Any new wrapper must either emit the same colon candidates or keep `FilteringProvider.GetSecret` in the chain and map only the requested attribute name.
- **Fixing "No Unfiltered Fallback" by Deleting the Fallback Over-Corrects**: The #212 follow-up to the trap above removed `return m.underlying.GetSecret(ctx, location)` from `MappingProvider.GetSecret`, which fixed the bypass but broke two documented resolution paths: (a) bare `provider://entity` URIs lost the per-scheme default attribute (`effectiveAttr := "Password"` for `keepass` at `filter.go:649` and `custom_vault` at `:713`), and (b) multi-entity YAML/JSON `GetSecret` cannot resolve at all, because `getRawEntryWithPath` reaches the entity-keyed `GetEntryWithPath` while `staticProvider.GetSecret` resolves dot paths against `rawContent`. When removing a passthrough, enumerate what it was resolving and reproduce each case explicitly.
- **Stacked Tidy-Branch PRs**: Verify the base branch's PR state before reviewing. `internal/provider/mapping.go` exists only on `tidy/pr-210-regex-key-mapping`, not on `main`; PRs #210/#211 were byte-identical duplicate submissions (both closed unmerged) and #212 was stacked on the survivor, so its reported 3 files / +438/-97 was really 8 files / +1100/-1 against `main`. Always run `git diff origin/main origin/pr-<n>` in addition to `gh pr diff`. Note that #212 was later merged by retargeting its base to `main`, and `internal/provider/mapping.go` is now on `main` — so this trap's specifics are historical, but the `git diff origin/main origin/pr-<n>` check is not.
- **CI Does Not Run For Non-`main`-Based PRs**: `.github/workflows/ci.yml` and `semgrep.yml` trigger on `pull_request: branches: [main]` only. While a PR's base is a `tidy/` branch, **no CI, lint, test, CodeQL or Semgrep job runs at all** — only `reject-empty-commit.yml`. Observed on #212: seven runs on the branch head, every one of them `Reject empty commits`, and nothing triggered in the window between retargeting and merging. So a stacked PR's "CI is green" is not evidence of anything. Read the workflow triggers before claiming a check ran.
- **Unverifiable `code_quality` Ruleset Rule**: The `Protect main` ruleset requires a check named `Code Quality`, which this repository never produces — `ci.yml`'s lint job is named `Lint Code`, and `git grep 'Code Quality' .github/` returns nothing. Every merge therefore needs `gh pr merge --admin`, and `mergeable: MERGEABLE/BLOCKED` with all check-runs `success` is the expected state, not a real gate failure. Do not present it as a signal that checks are missing; fix it by renaming the job or dropping the rule.

### Review discipline: never assert an unverified fact

The most expensive defect class in this repository's review history is not a missed code bug — it is a **review comment that states a code or test fact the reviewer never checked**, which then has to be walked back mid-thread and re-verified by both parties. Observed repeatedly across #208/#209/#212/#213:

| Claim made | Reality |
|---|---|
| "`isValidGroupNameOrNum` rejects `_`, so use `(?P<group_1>…)` to pin it" | The condition is `!isAlphaNum(c) && c != '_'` — underscores are **accepted**. The proposed test would have discriminated nothing. |
| "8 of the 15 new cases restate #206's" | Compared field by field: **1** exact duplicate, 3 behavioural, and 2 of the "duplicates" were the *only* coverage of bare `$name` refs and `\$` escaping. Folding as recommended would have deleted coverage. |
| "CI only runs the empty-commit job" (stated as a property of bot branches) | True only for PRs whose base is not `main`; `ci.yml` triggers on `pull_request: branches: [main]`. |
| "CodeQL was green on that PR, so it could have been cited" | CodeQL never evaluated #212 at all — a *correction* that was itself unverified, costing a second round trip. |
| "the merged tree is IDENTICAL to the approved head" | Compared `main` against the whole PR branch, which showed 22 files because `main` was 8 commits ahead. The correct comparison is per-file. |

Rules that follow, to be applied before writing any review text:

1. **Read the predicate, not the function name.** Any claim about what a guard accepts or rejects must quote the condition that decides it.
2. **Count with a command.** Tallies, duplicate detection and diffs come from `grep`/`diff`/a script — never from comparing names by eye, and never asserted before the command has run.
3. **Never comment on output before reading it.** Do not write "identical above" or "empty above" adjacent to a diff you have not yet looked at.
4. **A counterparty's "fixed" is not verification.** Diff it, or mutate it, before agreeing — bot replies claim work that is absent more often than not.
5. **Generalise only after trying to break the generalisation.** One counterexample check before an observation becomes a rule about the repo.
6. **Verify the self-correction too.** When retracting a claim, the replacement claim needs the same evidence as the original.
7. **Mutation-verified beats argued.** Where a cheap mutation settles whether a guard is covered, run it instead of reasoning about it — and always run it against the whole package, never a `-run` subset.

---

## 📦 Provider Development Checklist

When adding a new secret provider, verify each item:

- [ ] Implements `provider.SecretProvider` interface in `internal/provider/<name>.go`
- [ ] Has a corresponding test file `internal/provider/<name>_test.go`
- [ ] Registered in the engine's vault provider map with its URI scheme
- [ ] Documented in `README.md` with configuration snippet and usage examples
- [ ] Added to `examples/config.yaml` with commented-out example block
- [ ] Cross-platform — no OS-specific syscalls without build tags
- [ ] No new `go.mod` dependency without user approval
