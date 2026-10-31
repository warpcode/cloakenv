---
name: cloakenv-pr-review
description: Review SOP and automated bundle check layered on github-pr-review for cloakenv pull requests. Use when reviewing, auditing, or evaluating pull requests or Jules automated changes for cloakenv.
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
- Workflow integrity checks (verifies `reject-empty-commit.yml`, `ci.yml`, and `semgrep.yml`).
- Smuggled refactor scans (detects `regexCache.*sync\.Map` or deleted autoload rules).
- Switch drift checks (verifies provider cases in `initVaultProvider`).
- Sibling PR duplicate detection (detects matching blob hashes from concurrent bot sessions).

### Phase 2: Base Branch & CI Verification

1. Inspect the `base_branch` field in the bundle JSON:
   - If `base_branch` is `main`: CI workflows (`ci.yml` and `semgrep.yml`) executed on the PR head.
   - If `base_branch` is NOT `main` (e.g. `tidy/*`): **CI did NOT run**! Only `reject-empty-commit.yml` ran.
2. When reviewing stacked or branch-based PRs:
   - Review `main.diff` in the payload directory rather than relying solely on the GitHub web UI diff.
   - Verify that all checks pass against `origin/main` before considering approval.

### Phase 3: Deep Filtering & Invariant Audit

Review all changes modifying `internal/provider/` or `internal/engine/` against the institutional traps documented in `@references/filter-traps.md`:

1. **Decorator Chain Order**: Verify that `initVaultProvider` applies wrappers in exact sequence:
   - Base Provider -> `FilteringProvider` -> `MappingProvider` -> `KeyFormatProvider` -> `AttributesProvider`.
2. **Wrapper Composition**: Ensure any new wrapper composes with `FilteringProvider` rather than replacing it (PR #210 regression).
3. **No Unfiltered Fallback**: Confirm there is no fallback to `underlying.GetSecret` after a miss that bypasses filtering.
4. **Default Attribute & Dot-Path Lookups**: Verify that eliminating fallback does not break bare `provider://entity` lookups or multi-entity dot-path resolution (PR #212 regression).
5. **Colon-Qualified Filter Candidates**: Ensure filter candidates include colon forms (`entryPath+":"+attr`, `entryTitle+":"+attr`, `entityLoc+":"+attr`).
6. **Include Precedence**: Ensure `include_fields` rules evaluate before and take precedence over `exclude_fields`.
7. **Virtual Search Integrity**: Ensure `SearchProvider` preserves candidate entry metadata (`Title`, `Tags`) so stored queries match.
8. **Template Evaluation Order**: Ensure `.attributes` templates evaluate only after filtering, mapping, and key formatting.

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

- [Filter Traps & Invariants](references/filter-traps.md) — Detailed breakdown of provider wrappers, fallback traps, colon candidates, and template ordering.
- [Review Discipline](references/review-discipline.md) — The 7 core rules of review discipline, ruleset and merge gates, and silent approval invariant.
