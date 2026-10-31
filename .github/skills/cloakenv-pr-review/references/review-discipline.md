# cloakenv Review Discipline & Merge Gates

This document codifies the review standards, verification rules, and GitHub ruleset invariants for the `cloakenv` repository.

---

## 1. Historical Context: The Cost of Unverified Claims

The most expensive defect class in this repository's review history is not a missed code bug — it is a **review comment that states a code or test fact the reviewer never checked**, which then has to be walked back mid-thread and re-verified by both parties.

Observed repeatedly across PRs #208, #209, #212, and #213:

| Claim Made | Reality |
|---|---|
| "`isValidGroupNameOrNum` rejects `_`, so use `(?P<group_1>…)` to pin it" | The condition is `!isAlphaNum(c) && c != '_'` — underscores are **accepted**. The proposed test would have discriminated nothing. |
| "8 of the 15 new cases restate #206's" | Compared field by field: **1** exact duplicate, 3 behavioural, and 2 of the "duplicates" were the *only* coverage of bare `$name` refs and `\$` escaping. Folding as recommended would have deleted coverage. |
| "CI only runs the empty-commit job" (stated as a property of bot branches) | True only for PRs whose base is not `main`; `ci.yml` triggers on `pull_request: branches: [main]`. |
| "CodeQL was green on that PR, so it could have been cited" | CodeQL never evaluated #212 at all — a *correction* that was itself unverified, costing a second round trip. |
| "The merged tree is IDENTICAL to the approved head" | Compared `main` against the whole PR branch, which showed 22 files because `main` was 8 commits ahead. The correct comparison is per-file. |

---

## 2. The 7 Core Rules of Review Discipline

Apply these seven rules before drafting or posting any review comments:

1. **Read the predicate, not the function name**: Any claim about what a guard accepts or rejects must quote the exact condition that decides it.
2. **Count with a command**: Tallies, duplicate detection, and diffs come from `grep -c`, `diff -u`, or automated scripts — never from scanning names by eye, and never asserted before the command has run.
3. **Never comment on output before reading it**: Do not write "identical above" or "empty above" adjacent to a diff or output you have not yet inspected.
4. **A counterparty's "fixed" is not verification**: Diff it, or mutate it, before agreeing — replies claim work that is absent more often than not.
5. **Generalise only after trying to break the generalisation**: Check at least one counterexample before an observation becomes an asserted rule about the repository.
6. **Verify the self-correction too**: When retracting or adjusting a claim, the replacement claim requires the exact same standard of evidence as the original.
7. **Mutation-verified beats argued**: Where a cheap code mutation settles whether a guard is covered, run the test suite against the mutation instead of reasoning about it — and always run against the whole package (`go test ./...`), never a `-run` subset.

---

## 3. GitHub Rulesets & Merge Gates Reference

### `Protect main` Ruleset Architecture
Verified against GitHub repository ruleset configuration (`repos/warpcode/cloakenv/rulesets/20865389`):

- **Required Approvals**: `required_approving_review_count: 1`. Exactly one approving review is required before merging into `main`.
- **Merge Strategy**: `allowed_merge_methods: ["squash"]`. All PRs merged into `main` must be squash-merged.
- **CodeQL Scanning Gate**: Gated at `errors_and_warnings` and security severity `medium_or_higher`.
- **No "Code Quality" Status Check**: There is NO status check named "Code Quality". The `code_quality` rule is a code-scanning alert gate, not a required CI workflow check.
- **Stale Reviews on Push**: `dismiss_stale_reviews_on_push: true`. Every push to a PR branch automatically dismisses prior reviews, requiring re-review of the new branch head.
- **Bypass Privileges**: `current_user_can_bypass: "always"`. The `--admin` flag on `gh pr merge` serves as an operational bypass when needed.

### Silent Approval Invariant
When submitting an approving review on GitHub:
```bash
gh pr review <pr_number> --approve --body ""
```
- The review body MUST be empty (`--body ""`) or omitted.
- Never write "LGTM", approval boilerplate, or commentary when approving.
- If feedback or notes are warranted, post them as individual review comments before approving, or submit a comment review.
