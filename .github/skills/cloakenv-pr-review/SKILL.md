---
name: cloakenv-pr-review
description: Review SOP and automated bundle check layered on github-pr-review for cloakenv pull requests. Use when reviewing, auditing, or evaluating pull requests for cloakenv.
---

# cloakenv Pull Request Review SOP

Review Standard Operating Procedure and mechanical checks layered on `github-pr-review` specifically tailored for the `cloakenv` repository.

---

## 1. Overview & Core Invariants

`cloakenv` handles live credential orchestration and dynamic runtime environment injection. A security regression or filter bypass can expose sensitive production secrets.

This review procedure layers on top of `github-pr-review` to enforce repo-specific review discipline, institutional memory of past regressions, and automated discovery checks.

### Core Review Invariants

1. **Silent Approval Invariant**: When approving a pull request via `gh pr review --approve`, approvals MUST have an empty body (`--body ""`) or no body flag. Never comment "LGTM" or write approval boilerplate.
2. **Never Proactively Refactor**: Strictly reject unrequested refactors (such as autoload cache changes) under the minimal change principle.
3. **Inspect the True Base**: If a PR targets a `tidy/` or `fix/` branch instead of `main`, GitHub Actions CI did NOT run, and the PR diff shown on GitHub only covers changes relative to the branch base. Always review the full diff against `origin/main`.
4. **Never Assert an Unverified Fact**: Every code claim must quote the exact predicate condition deciding it.

---

## 2. 5-Phase Review SOP

### Phase 1: Automated Discovery & Bundle Extraction

Run the automated PR review bundle script before reading individual files:

```bash
.github/skills/cloakenv-pr-review/scripts/pr_review_bundle.sh <pr_number>
```

The script extracts a full audit bundle to `/tmp/cloakenv_pr_<n>_<timestamp>/` and outputs structured JSON covering:
- PR base branch and commit SHAs.
- Full diff against `origin/main` (`main.diff`) and diff against base (`base.diff`).
- Workflow integrity checks (verifies `ci.yml` and `semgrep.yml`).
- Smuggled refactor scans (detects `regexCache.*sync\.Map` or deleted autoload rules).
- Switch drift checks (verifies read-only provider cases in `initVaultProvider`).
- Sibling PR duplicate detection (detects matching blob hashes from concurrent bot sessions).

### Phase 2: Base Branch & CI Verification

1. Inspect the `base_branch` field in the bundle JSON:
   - If `base_branch` is `main`: CI workflows (`ci.yml` and `semgrep.yml`) executed on the PR head.
   - If `base_branch` is NOT `main` (e.g. `tidy/*`): **CI workflows (`ci.yml` and `semgrep.yml`) did NOT run**!
2. When reviewing stacked or branch-based PRs:
   - Review `main.diff` in the payload directory rather than relying solely on the GitHub web UI diff.
   - Verify that all checks pass against `origin/main` before considering approval.

### Phase 3: Deep Pipeline & Security Invariant Audit

Review all changes modifying `internal/provider/`, `internal/engine/`, `internal/runner/`, or `internal/cmd/`:

1. **Pure Read-Only Vaults**: Vault providers (`keepass`, `pass`, `vault`, `env`, `keyring`) must return untouched flat `Entry` maps with zero filtering, key mapping, or template transforms.
2. **RuleEngine Semantics**: Verify `pass`, `match`, and `set` rules in `internal/engine/rules.go` preserve reserved metadata keys (`title`, `path`, `uuid`, `tags`, `group`), enforce last-rule-wins ordering, and never leak values in traces or errors.
3. **Zero-Leak Inspection & Masking**: Verify `cloakenv inspect` unconditionally emits `<REDACTED>` for every secret value, audit logs never emit resolved payloads, and `runner` streams child output through `MaskingWriter` and zeros temp template files (`0700`/`0600`, `O_NOFOLLOW`) on exit.
4. **Keyring Internal Isolation**: Confirm the built-in `keyring` vault excludes internal keys (`cache_token`, `cache-master-key`, `cache/*`, `provider/*`).
5. **Verbatim Child Command Pass-Through**: Verify `BuildEnvForCommand` passes child command arguments through verbatim without secret or template expansion.
6. **Pure Go Templating**: Templates use Go `text/template` with `.Option("missingkey=error")` and pure helpers only—no URI/exec/include lookups.

### Phase 4: Review Discipline & Fact Verification

Follow the 7 rules of review discipline documented in `@references/review-discipline.md`:

1. **Read the predicate, not the function name**: Quote the exact boolean condition when asserting behavior.
2. **Count with a command**: Count occurrences using shell tools; never estimate by eye.
3. **Never comment on output before reading it**: Do not claim an output is empty without verifying.
4. **A counterparty's "fixed" is not verification**: Diff or mutate before agreeing that an issue is resolved.
5. **Generalise only after trying to break the generalisation**: Verify boundary conditions and counterexamples.
6. **Verify the self-correction too**: Retracted claims require the same evidence standard as original findings.
7. **Mutation-verified beats argued**: When evaluating test efficacy, run tests against mutated logic across the whole package.

### Phase 5: Review Action & Decision

1. **Approval**:
   - Confirm all CI checks pass.
   - Confirm 1 approving review on GitHub satisfies the `Protect main` ruleset.
   - Execute silent approval:
     ```bash
     gh pr review <pr_number> --approve --body ""
     ```
2. **Request Changes**:
   - Provide concrete, reproducible evidence.
   - Quote exact file paths and predicate line numbers.
   - Propose minimal, targeted fixes without unnecessary refactoring.

---

## 3. Reference Files

- [Filter Traps & Invariants](references/filter-traps.md) — Historical context on provider security traps and template safety.
- [Review Discipline](references/review-discipline.md) — The 7 core rules of review discipline, ruleset and merge gates, and silent approval invariant.
