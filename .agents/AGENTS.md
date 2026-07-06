# Agent Instructions for cloakenv

> [!IMPORTANT]
> These instructions apply to all AI agents working within the `cloakenv` repository.
> They complement, and never override, the global rules in `~/.agents/AGENTS.md`.

---

## 🏗️ Project Overview

`cloakenv` is a **pluggable secret orchestrator and dynamic runtime environment injector** written in Go. It wraps application binaries, resolves secret URIs from multiple configurable backends (KeePass, OS keyring, YAML, JSON, environment, encrypted cache), and injects secrets strictly into temporary execution memory — never persisting them to disk unencrypted.

### Key Design Principles

- **Zero-persistence**: Secrets are resolved at runtime and never written to plaintext files.
- **URI-addressed secrets**: Every secret is referenced by a typed URI (e.g., `keepass://group/entry:attr`, `keyring://service/account`).
- **Pluggable providers**: Adding a new backend means implementing the `provider.Provider` interface only.
- **Cross-platform**: CI runs on Linux, macOS, and Windows. All code must compile and pass tests on all three.

---

## 📁 Project Structure

```
cmd/cloakenv/main.go     # CLI entrypoint — keep thin; wire deps only
internal/
  config/                # YAML config parser (no business logic)
  engine/                # Orchestrator core — resolves URIs, injects env
  provider/              # Built-in & remote secret providers
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

---

## 🛠️ Development Workflow

### Build & Run

```bash
make build            # Compiles to bin/cloakenv
make run              # go run ./cmd/cloakenv
make install          # Installs to /usr/local/bin (or PREFIX=$HOME/.local make install)
make uninstall        # Removes the installed binary
```

### Testing

```bash
make test             # go test -v ./...
go test -v -race ./...  # With race detector (mirrors CI)
go test -bench=. ./internal/engine/...  # Run benchmarks
```

> [!IMPORTANT]
> Always run tests with the **race detector** (`-race`) before committing. CI enforces this.

### Linting & Formatting

```bash
make fmt              # go fmt ./...
make vet              # go vet ./...
go run github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8 run  # Full lint (mirrors CI)
```

> [!WARNING]
> **All CI checks must pass before any merge.** CI runs lint, and tests on
> `ubuntu-latest`, `macos-latest`, and `windows-latest`.

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

### Interfaces & Extensibility

- The `provider.Provider` interface is the **core extension point**. Adding a new backend = new file in `internal/provider/`, implementing the interface. Do not modify the interface signature without a plan review.
- URI scheme registration happens in the engine; new providers must be registered there explicitly.

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
feat(provider): add JSON provider with entries_key support
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

- Only modify files within the `cloakenv` workspace (`/home/jase/src/cloakenv`).
- Do not modify files outside this workspace (e.g., `~/.agents/AGENTS.md`) unless the user explicitly requests a global memory update.

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

---

## 📦 Provider Development Checklist

When adding a new secret provider, verify each item:

- [ ] Implements `provider.Provider` interface in `internal/provider/<name>.go`
- [ ] Has a corresponding test file `internal/provider/<name>_test.go`
- [ ] Registered in the engine's provider map with its URI scheme
- [ ] Documented in `README.md` with configuration snippet and usage examples
- [ ] Added to `examples/config.yaml` with commented-out example block
- [ ] Cross-platform — no OS-specific syscalls without build tags
- [ ] No new `go.mod` dependency without user approval
