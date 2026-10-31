# Contributing to cloakenv

Thank you for contributing to `cloakenv`! We welcome bug reports, feature requests, documentation improvements, and code contributions.

---

## 🛠️ Development Setup & Workflow

### Prerequisites
- Go 1.22+ (or version matching `go.mod`)
- Git
- `make`

### Building and Testing

```bash
make build            # Compiles binary to bin/cloakenv
make run              # Runs cloakenv via go run .
make test             # Runs tests with race detector (go test -v -race ./...)
make bench            # Runs engine performance benchmarks
make fmt              # Formats Go code (go fmt ./...)
make vet              # Runs go vet analysis
make verify           # Complete pipeline: build, vet, format check, race tests
```

### Linting
CI runs full static analysis using `golangci-lint`:
```bash
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.1 run
```

---

## 🔀 Git Workflow & Branching

- **Default Branch**: `main`. All pull requests must be based on and target `origin/main`.
- **Pull Strategy**: Rebase on pull (`git pull --rebase`). Avoid merge commits in feature branches.
- **Merge Strategy**: Pull requests are squash-merged into `main` to preserve a clean, linear history.
- **Branch Cleanup**: Delete feature branches immediately after merging.

---

## 📝 Commit Conventions

All commit messages must follow the [Conventional Commits](https://www.conventionalcommits.org/) specification:

```
<type>(<scope>): <short description>
```

### Common Types:
- `feat`: A new user-facing feature or provider backend.
- `fix`: A bug fix.
- `docs`: Documentation updates or additions.
- `test`: Adding or refactoring unit/integration tests or benchmarks.
- `refactor`: Code changes that neither fix a bug nor add a feature.
- `chore`: Maintenance tasks, dependency bumps, or tooling adjustments.

### Examples:
```
feat(provider): add JSON provider with entities_root_key support
fix(engine): handle missing URI scheme gracefully
test(engine): add benchmark for concurrent URI resolution
docs: update README with JSON provider usage
```

---

## 🚀 Pull Request Checklist

Before opening or requesting review on a Pull Request:
1. **Pass Verification**: Run `make verify` locally. All compilation, formatting, and race-detected tests must pass.
2. **Clean Scope**: Keep pull requests focused and surgical. Avoid combining unrelated refactors or formatting churn with feature additions.
3. **No Unapproved Dependencies**: Do not introduce new dependencies in `go.mod` without prior discussion and approval.
4. **Cross-Platform Compatibility**: Ensure code compiles and passes tests on Linux, macOS, and Windows. OS-specific code must be gated with Go build tags.
5. **No Secret Leakage**: Never commit live secrets or output sensitive data in logs, error strings, or tests.
6. **PR Fields**: Include a clear title (Conventional Commits), a concise summary of changes, motivation/rationale, and verification steps.
