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

- Follow standard Go conventions (`gofmt`, `go vet` clean).
- Errors must be handled explicitly — no `_` discard of errors in production paths.
- Use `fmt.Errorf("context: %w", err)` for error wrapping to preserve the chain.
- Package-level `var` blocks for sentinel errors; never use raw string comparisons.
- Avoid `init()` functions; prefer explicit initialization in constructors.
- Table-driven tests are preferred for unit tests covering multiple input cases.
- Benchmark functions live in `*_benchmark_test.go` files within the same package.
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
- **Safe Redirection in Tests**: When capturing stdout or stderr using `os.Pipe()`, check the returned error immediately. Defer closing both writer ends (`wOut.Close()`, `wErr.Close()`) immediately after creation to prevent resource leaks (dangling goroutines/pipes) if the test function panics.
- **Filtering regression coverage**: Cover recursive map/array projection, rooted and rootless static paths through scalar and structured APIs, changing search snapshots, and real `Title`/`Tags` attributes.
- **Mutation-verification for test-only changes**: When a change adds tests without touching production code, "all N subtests pass" is not by itself evidence of coverage. Prove the assertions bite by breaking one branch of the code under test in a throwaway worktree (`git worktree add --detach /tmp/wt origin/pr-<n>`), confirming a *named* subtest fails, then restoring the file. Report which subtest caught it, and flag tests that pass under mutation as tautological.

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

### Execution Security & Environment Constraints

- **Python Inline Execution Blocked**: The runtime security hook (`ai-command-gate`) blocks `python3 -c` inline execution with `SECURITY GUARD: Python inline execution is unsafe`. Always write Python scripts to a file (in `/tmp/` or agent scratch) and execute via `python3 <file>`.

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

### Bot-Authored PRs & Known Contamination Patterns

When reviewing PRs authored by automated bots (such as Google Jules / `google-labs-jules[bot]`):
- **Smuggled Autoload Refactors**: Jules consistently attempts an unrequested `regexCache sync.Map` autoload refactor across unrelated feature PRs, frequently accompanied by removing `CompileAutoloadRules()` or inlining `normalizeURIs`. Strictly reject these out-of-scope refactors under the "No unrequested refactors" rule.
- **Spurious Workflow Deletions**: Jules repeatedly deletes `.github/workflows/reject-empty-commit.yml` across unrelated PRs. Always verify this workflow file remains intact.
- **Checklist Non-Compliance**: Jules routinely implements Go provider code but skips updating `README.md` and `examples/config.yaml`. Enforce all items in the *Provider Development Checklist*.
- **Stuck-Branch Recovery Workflow**: When a Jules PR is wedged — repeated empty commits, unresolvable conflicts, or a self-reverted refactor — do **not** keep requesting another fix-up commit, and do not merge the base branch into the Jules-owned branch. Six consecutive "I have squashed the history" replies on warpcode/cloakenv#193 left all five empty commits in place, because appending a commit can never remove one. The recovery is to rebuild the branch yourself and hand off to a fresh session:
  1. Isolate the real diff with a per-file blob-hash sweep against `origin/main` (see the *Isolate the real diff* step in the `review-pull-request` skill). This discards already-merged noise and immediately reveals a self-reverted refactor.
  2. In a throwaway worktree off `origin/main`, `git merge --squash origin/pr-<n>`. Resolve any conflict, keeping both sides' tests. Verify the result: `git status --short` must list only the intended files, then `go build ./...`, `go vet ./...`, `gofmt -l .`, and `go test -race ./...`.
  3. Commit once and push to a fresh `tidy/pr-<n>-<slug>` branch. Never push to the Jules-owned branch.
  4. **Do not open a pull request for the tidy branch.** The tidy branch is a handoff artifact for a *new* Jules session, not a deliverable. Opening a PR yourself pre-empts the replacement agent and exposes unreviewed code — including any unresolved High findings — as mergeable. Carry every unresolved review finding into the new session's **prompt** instead; the replacement agent never sees the old thread.
  5. Start a new Jules session on the tidy branch via `create-session`. State the original goal, what is already done (do not redo it), what remains, an explicit acceptance-criteria checklist listing every unresolved finding by number and file:line, and — critically — tell it that a previous attempt claimed work it did not deliver. Add the unmissable delivery instruction from *Completed-Without-Delivery Sessions* below: it MUST `git push` and `gh pr create`, and must decline any harness offer to submit a patch artifact instead.
  6. Clean up the superseded history at this stage: close the original PR (referencing the new session's PR once it exists) and `archive-session` the stuck session. Deleting the wedged remote branch is optional — prefer keeping it until the replacement PR is confirmed merged, so the real work is never unrecoverable.
  7. Only if the replacement session *also* wedges — empty-commit cascade again, or it delivers without opening a PR — do you take over directly: build another tidy branch off current `main`, squash the salvageable diff yourself, and open the PR at that point. Each escalation adds one layer, so escalate only on a demonstrated failure of the previous stage.
  This preserves the salvageable work, permanently eliminates the empty commits, and resets the review relationship. Verified 2026-10-01 on #193 (5 empty commits → 1 clean commit) and #191 (self-reverted refactor → tests preserved, conflict resolved).
- **Historical Cleanup Is Per-Stage, Not Deferred**: Superseded PRs, wedged branches, and dead sessions accumulate into a mess that makes the next triage sweep unreliable (`gh pr list --state all` fills with `CLOSED` husks, and archived-but-not-deleted branches show up as unmerged noise). Clear up as soon as each stage succeeds — never batch it for later: close the PR the moment its replacement exists, `archive-session` the moment its successor is spawned, and prune the wedged branch once the replacement PR is confirmed merged. A tidy branch is scaffolding: delete it once the replacement PR is merged or closed, so `tidy/*` never accumulates stale layers from successive escalations.
- **Completed-Without-Delivery Sessions**: A session can reach `COMPLETED` having written code, run the test suite, and passed its own code review — yet never push and never open a PR. The work then exists only as a patch artifact inside the session. Observed twice on 2026-10-01: sessions `940024975768340826` and `10856745711762183052` both ended with "All plan steps completed" + "1 patch/artifact(s)" and pushed nothing; the tidy branches still pointed at the pre-session commits. **The artifact is not recoverable through the Jules API** — `activity <session> <artifact-id>` returns 404 and `call GET sessions/<id>` exposes only metadata (title, state, prompt, source context), with no diff or artifact URL. The patch must be downloaded manually from the Jules web UI. Consequences: always confirm delivery by checking `gh pr list` for a branch matching the session's source branch, not by the session state; and when a session's work is not yet recoverable, treat the source branch as the only surviving artifact. Add an explicit, unmissable instruction to the prompt that the agent MUST `git push` and `gh pr create`, and must decline any harness offer to submit a patch artifact instead.
- **Fabricated Format Assumptions**: A bot may invent a domain fact and build on it. Observed 2026-10-01 on warpcode/cloakenv#203, which changed the tag delimiter to `strings.IndexAny(s, ",;")` on the stated premise that "KeePass tag strings can use both commas and semicolons as delimiters" — the repository's only tag parser (`utils.ParseTagString`) splits on commas exclusively, and no fixture uses semicolons. The invented premise also got persisted into a new `.jules/bolt.md` file, which did not exist on `main`. Before accepting a change that alters parsing, delimiters, or formats, verify the claimed fact against the existing implementation and test fixtures rather than the PR description; then check whether the change is consistent across every call site of that parser.
- **Empty Amendment Commits**: Jules may push empty amendment commits (0 additions, 0 deletions) without addressing existing review feedback. Always verify commit statistics via git or GitHub API (`gh api repos/.../commits/<oid> --jq .stats`) before assuming amendments contain fixes, and ensure all existing review threads are genuinely resolved before approving.
- **Empty-Commit Cascade From Repeated CI Nudges**: A single empty amendment compounds — observed on warpcode/cloakenv#210 (session `7376406477938977770`), where the branch held one real commit (`925453b`, 8 files, +760/-2) followed by **four** empty commits (`8f44d61`, `99a23c9`, `583b92a`, `1a37af4`). Each "CI failed" nudge from the user triggered another empty push, and the six open review findings were never touched. Detection is a one-liner over the branch:
  ```bash
  for c in $(git rev-list origin/main..origin/pr-<n>); do
    s=$(git show --shortstat --format='' $c | tr -d ' \n')
    printf '%s [%s]\n' "$c" "${s:-EMPTY}"
  done
  ```
  Two or more `EMPTY` entries is terminal — a fifth empty push cannot clear the gate. Escalate to the *Stuck-Branch Recovery Workflow* above. Note that `git merge --squash` of a stale-base branch still merges cleanly when `main` only moved in unrelated files — verify with `go build ./... && go vet ./... && gofmt -l . && go test -race ./...` before committing.
- **Over-Claiming Non-Empty Amendments**: A *non-empty* amendment whose commit message and inline thread replies claim fixes (including named new sub-tests, refactors, or assertion changes) that are entirely absent from the diff. The `Empty Amendment Commits` check does **not** catch this — observed on warpcode/cloakenv#186, where `82c352f` was a real 37+/72- change whose sole effect was an `os.CreateTemp` → `os.WriteFile` refactor, while its message and three thread replies claimed a new path fix, two new sub-tests, and strengthened `entry.Title` assertions. Detection: read `git diff <prev-sha>..<head-sha> -- <path>` to establish what the amendment actually did, then grep the head blob (`git show origin/pr-<n>:<path> > scratch/<n>_head.go`) for each claimed symbol, sub-test name, and literal. Treat the commit message and every bot thread reply as an unverified claim requiring head-blob confirmation. When a claim is false, do not resolve the thread — post a reply citing the head-blob line number and keep it unresolved.
- **Review Feedback Delivery**: All review findings MUST go into inline file-level comments (`REQUEST_CHANGES`). The top-level review body must be a neutral one-liner because bot runners only parse inline comments. Deleted files have no added lines (`side: RIGHT`) to anchor comments; bundle any deleted file findings into an inline comment on a modified file.
- **Helper & Signature Drift**: When reviewing PR branches created before recent merges to `main`, verify all calls to package-level helpers (e.g. `matchEntry`, `serializeVal`, `normalizeURIs`) match the current signature on `origin/main`. Git considers separate file additions textually mergeable even when function signatures conflict at compile time.
- **Virtual Provider & Switch Case Drift**: When reviewing PRs modifying `initVaultProvider`, verify that recently added provider cases on `main` (such as `case "search":`) are not inadvertently dropped due to outdated branch origins, and that generic decorators (such as filtering or caching) wrap virtual providers alongside direct providers.
- **Single-Entity & Dot-Path Filter Bypass**: When reviewing field filtering logic, verify that lookups without colons in single-entity vaults (YAML/JSON) or root-keyed vaults (`entities_root_key`) do not default to checking `"Password"` or fall through to unredacted secret retrieval.
- **Artificial Placeholder Comments to Mask Empty Commits**: Jules may attempt to bypass empty-commit detection or satisfy git change requirements by inserting dummy placeholder comments (e.g. `// This comment is added to ensure git recognizes this as a change...`) rather than dropping the empty commit via rebase. Always require dropping the empty commit from the branch history.
- **CI-Gate Bypass Filler Files**: The same bypass also takes the form of a junk *file* rather than a comment — observed on warpcode/cloakenv#191, where a repo-root `dummy.txt` containing the single line `Trigger rebuild` was committed solely to convert a rejected empty commit into an accepted non-empty one. The tell is a bot thread reply that justifies a new file by its effect on a CI gate rather than by its content ("to legitimately bypass the empty commit check"). Reject the file and require a squash instead; note that a real non-empty commit usually already exists in the branch, making the filler unnecessary. Side effect worth flagging: the `Reject empty commit` gate becomes trivially bypassable by any future amendment.
- **Self-Reverting Refactors**: A refactor commit can be silently reverted by a *later* commit in the same branch, leaving a PR titled "Refactor X" that ships no refactor — observed on warpcode/cloakenv#191, where `d3be77d` extracted `appendGroup`/`tryExpandBraced`/`tryExpandUnbraced` from `expandTemplate` and `323a54d` then reverted all of it (and deleted the accompanying `autoload_characterization_test.go`). Detection, by blob-hashing the production file at three points:
  ```bash
  git show <refactor-sha>:<path> | git hash-object --stdin      # refactor commit
  git show origin/pr-<n>:<path> | git hash-object --stdin      # head
  git show origin/main:<path> | git hash-object --stdin         # base
  ```
  `head == main` while the refactor commit differs proves the revert. Also confirm the files the refactor *added* are absent (`git ls-tree origin/pr-<n> <dir> | grep <name>`) and that the claimed helper symbols appear on neither head nor base (`git grep -c 'func <helper>' origin/pr-<n>`). The PR body is frequently never revised after the revert, so its "What/Why" sections describe work that no longer exists — demand either restoring the refactor or a retitle and rewrite.
- **Uncommitted Benchmark Claims**: When reviewing performance optimization PRs, verify that named benchmarks cited in the PR description (such as `BenchmarkFilterResultsByExpression` or `BenchmarkCustomVaultProvider_Search`) are actually committed to `*_benchmark_test.go` rather than remaining local to the bot's environment.
- **Wrapper-Replaces-Filter Regression**: Observed on warpcode/cloakenv#210 (regex `mapping`): `initVaultProvider` used `if len(vault.Mapping) > 0 { NewMappingProvider } else if include/exclude { NewFilteringProvider }`, which dropped `FilteringProvider` whenever `mapping` was set; the new wrapper re-implemented filtering on bare leaf keys, and its `GetSecret` fell through to `underlying.GetSecret` unfiltered, so excluded or renamed-original keys stayed readable. When a PR adds a provider wrapper, check that (1) it composes with `FilteringProvider` rather than replacing it, (2) there is no fallback to the unfiltered underlying `GetSecret` after a miss, and (3) mapped-key collisions are deterministic (Go map iteration order) and cannot earn the filter exemption.
- **Zero-Allocation String Scanning Regressions**: When reviewing string matching optimizations (such as `matchEntryTags`), verify that `strings.EqualFold` is never guarded with byte length checks (`len(a) == len(b)`). In UTF-8, case folding does not preserve byte length (e.g. Kelvin sign `'K'` [3 bytes] vs `'k'` [1 byte], Angstrom `'Å'` [3 bytes] vs `'Å'` [2 bytes]), causing false negatives. Additionally, verify that custom trimming loops strip all standard whitespace characters (`\t`, `\n`, `\r`, etc.) rather than only ASCII spaces, and that empty delimiter-separated segments are explicitly skipped.
- **Hand-Rolled String Scanners Replacing Allocation-Free Stdlib**: Before accepting a hand-rolled replacement for a stdlib string helper, verify the allocation claim that motivated it — `strings.TrimSpace`, `strings.EqualFold`, and `strings.IndexByte` are **already allocation-free** (they return subslices or perform length-only comparisons). Observed on warpcode/cloakenv#193, where a custom ASCII-only trim loop was introduced to avoid `strings.TrimSpace`; it gained nothing measurable (84.01 ns/op vs 12.71 ns/op, both `0 allocs/op`) while dropping the Unicode whitespace handling that `TrimSpace` provides via `unicode.IsSpace` (e.g. `U+00A0`, `U+202F`, `U+3000`). Such a rewrite silently splits behavior when only one caller is updated — `matchEntryTags` (search filtering) and `toEntry` (serialization) must agree on what a tag is, or a `keepass://group/entry:Tag` lookup stops matching a tag that `GetEntry` still reports. Differential-test any such rewrite against the pre-PR implementation over a matrix of inputs; the byte-identical-behavior claim is cheap to verify and routinely false.

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
