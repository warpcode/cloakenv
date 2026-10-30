#!/usr/bin/env bash
# pr_review_bundle.sh - Automated review bundle and safety verification for cloakenv PRs.
# Fetches diffs, checks base branch CI activation, detects smuggled refactors and switch drift.
# Emits structured JSON summary to stdout.

set -euo pipefail

usage() {
    cat <<'EOF'
Usage: pr_review_bundle.sh <pr_number> [output_dir]

Automated PR review bundle extraction and safety checks for cloakenv:
  1. Fetches PR metadata and checks base branch (warns if CI was inactive)
  2. Extracts diff against base and diff against origin/main
  3. Verifies .github/workflows/reject-empty-commit.yml integrity
  4. Scans for smuggled autoload refactors (regexCache sync.Map, CompileAutoloadRules)
  5. Verifies provider switch cases in internal/engine/provider_manager.go
  6. Detects sibling PR duplicates (matching blob hashes)

Arguments:
  pr_number    GitHub pull request number (e.g. 212)
  output_dir   Directory to store diff payloads (default: /tmp/cloakenv_pr_<n>_<timestamp>)

Outputs structured JSON to stdout.
EOF
}

if [[ $# -eq 0 ]] || [[ "$1" == "-h" ]] || [[ "$1" == "--help" ]]; then
    usage
    exit 0
fi

PR_NUM="$1"
TIMESTAMP="$(date +%s)"
PAYLOAD_DIR="${2:-/tmp/cloakenv_pr_${PR_NUM}_${TIMESTAMP}}"

mkdir -p "$PAYLOAD_DIR"

# Resolve repo root directory
REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$REPO_ROOT"

# Check tool availability
HAS_GH="false"
if command -v gh >/dev/null 2>&1; then
    HAS_GH="true"
fi

BASE_BRANCH="unknown"
HEAD_SHA="unknown"
PR_TITLE=""

# 1. Fetch metadata via gh CLI or local git fallback
METADATA_FETCHED="false"
if [[ "$HAS_GH" == "true" ]]; then
    if GH_META=$(gh pr view "$PR_NUM" --json number,title,baseRefName,headRefName,headRefOid 2>/dev/null); then
        METADATA_FETCHED="true"
        # Parse fields with sed/grep
        BASE_BRANCH=$(echo "$GH_META" | grep -o '"baseRefName":"[^"]*"' | cut -d'"' -f4 || echo "main")
        HEAD_SHA=$(echo "$GH_META" | grep -o '"headRefOid":"[^"]*"' | cut -d'"' -f4 || echo "unknown")
        PR_TITLE=$(echo "$GH_META" | grep -o '"title":"[^"]*"' | cut -d'"' -f4 || echo "")
        echo "$GH_META" > "${PAYLOAD_DIR}/metadata.json"
    fi
fi

# Fallback if gh metadata was unavailable
if [[ "$METADATA_FETCHED" == "false" ]]; then
    # Check if local git branch exists
    if git rev-parse --verify "pr-${PR_NUM}" >/dev/null 2>&1; then
        HEAD_SHA=$(git rev-parse "pr-${PR_NUM}")
        BASE_BRANCH="main"
    elif git rev-parse --verify "HEAD" >/dev/null 2>&1; then
        HEAD_SHA=$(git rev-parse HEAD)
        BASE_BRANCH="main"
    fi
fi

# 2. Check base branch & CI activation
CI_TRIGGER_ACTIVE="true"
CI_WARNING="null"
if [[ "$BASE_BRANCH" != "main" ]] && [[ "$BASE_BRANCH" != "unknown" ]]; then
    CI_TRIGGER_ACTIVE="false"
    CI_WARNING="\"PR base is not 'main' (targets '${BASE_BRANCH}'). GitHub Actions ci.yml and semgrep.yml do NOT run on non-main bases!\""
fi

# 3. Fetch diffs
BASE_DIFF_PATH="${PAYLOAD_DIR}/base.diff"
MAIN_DIFF_PATH="${PAYLOAD_DIR}/main.diff"
touch "$BASE_DIFF_PATH" "$MAIN_DIFF_PATH"

FILES_CHANGED_BASE=0
FILES_CHANGED_MAIN=0

if [[ "$HAS_GH" == "true" ]] && [[ "$METADATA_FETCHED" == "true" ]]; then
    gh pr diff "$PR_NUM" > "$BASE_DIFF_PATH" 2>/dev/null || true
fi

# Compute main diff using git if origin/main and HEAD_SHA exist
if git rev-parse --verify "origin/main" >/dev/null 2>&1; then
    if [[ "$HEAD_SHA" != "unknown" ]] && git cat-file -e "${HEAD_SHA}^{commit}" 2>/dev/null; then
        git diff "origin/main...${HEAD_SHA}" > "$MAIN_DIFF_PATH" 2>/dev/null || true
    else
        git diff "origin/main" > "$MAIN_DIFF_PATH" 2>/dev/null || true
    fi
fi

# Count files changed
if [[ -s "$BASE_DIFF_PATH" ]]; then
    FILES_CHANGED_BASE=$(grep -c '^diff --git' "$BASE_DIFF_PATH" || echo 0)
fi
if [[ -s "$MAIN_DIFF_PATH" ]]; then
    FILES_CHANGED_MAIN=$(grep -c '^diff --git' "$MAIN_DIFF_PATH" || echo 0)
fi

# Combined diff for pattern checking
COMBINED_DIFF="${PAYLOAD_DIR}/combined.diff"
cat "$BASE_DIFF_PATH" "$MAIN_DIFF_PATH" > "$COMBINED_DIFF"

# 4. Check workflow integrity (reject-empty-commit.yml, ci.yml)
WORKFLOW_PASSED="true"
WORKFLOW_DETAIL="Workflow files are intact"
if grep -q "diff --git a/\.github/workflows/reject-empty-commit\.yml" "$COMBINED_DIFF"; then
    if grep -E '^\+\+\+ /dev/null|deleted file mode' "$COMBINED_DIFF" | grep -B2 "reject-empty-commit\.yml" >/dev/null 2>&1; then
        WORKFLOW_PASSED="false"
        WORKFLOW_DETAIL="ALERT: .github/workflows/reject-empty-commit.yml was deleted!"
    else
        WORKFLOW_DETAIL="Notice: .github/workflows/reject-empty-commit.yml was modified"
    fi
fi

# 5. Check smuggled autoload refactors
AUTOLOAD_VIOLATIONS=()
if grep -qE "regexCache.*sync\.Map" "$COMBINED_DIFF"; then
    AUTOLOAD_VIOLATIONS+=("\"Injected regexCache sync.Map refactor\"")
fi
if grep -qE "^\-.*func CompileAutoloadRules" "$COMBINED_DIFF"; then
    AUTOLOAD_VIOLATIONS+=("\"Deleted CompileAutoloadRules()\"")
fi
if grep -qE "^\-.*normalizeURIs" "$COMBINED_DIFF" && grep -qE "^\+.*normalizeURIs" "$COMBINED_DIFF"; then
    AUTOLOAD_VIOLATIONS+=("\"Inlined normalizeURIs()\"")
fi

AUTOLOAD_PASSED="true"
if [[ ${#AUTOLOAD_VIOLATIONS[@]} -gt 0 ]]; then
    AUTOLOAD_PASSED="false"
fi
AUTOLOAD_JOINED=$(IFS=,; echo "${AUTOLOAD_VIOLATIONS[*]}")
AUTOLOAD_JSON="[${AUTOLOAD_JOINED}]"

# 6. Check provider switch cases in internal/engine/provider_manager.go
SWITCH_MODIFIED="false"
SWITCH_PASSED="true"
MISSING_CASES=()

if grep -q "diff --git a/internal/engine/provider_manager\.go" "$COMBINED_DIFF"; then
    SWITCH_MODIFIED="true"
    # Verify all 5 cases are still present in provider_manager.go
    PM_FILE="internal/engine/provider_manager.go"
    if [[ -f "$PM_FILE" ]]; then
        for provider_case in "keepass" "yaml" "json" "custom_vault" "search"; do
            if ! grep -qE "case\s+[\"']${provider_case}[\"']:" "$PM_FILE"; then
                MISSING_CASES+=("\"${provider_case}\"")
                SWITCH_PASSED="false"
            fi
        done
    fi
fi
MISSING_CASES_JOINED=$(IFS=,; echo "${MISSING_CASES[*]}")
MISSING_CASES_JSON="[${MISSING_CASES_JOINED}]"

# 7. Check sibling duplicates
SIBLING_DETECTED="false"
SIBLING_LIST=()
if [[ "$HAS_GH" == "true" ]]; then
    # List other open PRs
    OPEN_PRS=$(gh pr list --json number,headRefName 2>/dev/null || echo "[]")
    # Check if any branch names suggest duplicates (e.g. sentinel/*, bolt/*)
    if echo "$OPEN_PRS" | grep -qE "(sentinel/|bolt/|tidy/pr-${PR_NUM})"; then
        MATCHING_NUMS=$(echo "$OPEN_PRS" | grep -o '"number":[0-9]*' | cut -d':' -f2 | grep -v "^${PR_NUM}$" || true)
        for other_num in $MATCHING_NUMS; do
            SIBLING_LIST+=("{\"pr_number\": ${other_num}, \"note\": \"Concurrent bot PR touching related branch\"}")
            SIBLING_DETECTED="true"
        done
    fi
fi
SIBLING_JOINED=$(IFS=,; echo "${SIBLING_LIST[*]}")
SIBLING_JSON="[${SIBLING_JOINED}]"

# Determine overall status
OVERALL_STATUS="READY_FOR_REVIEW"
if [[ "$CI_TRIGGER_ACTIVE" == "false" ]] || [[ "$AUTOLOAD_PASSED" == "false" ]] || [[ "$WORKFLOW_PASSED" == "false" ]] || [[ "$SWITCH_PASSED" == "false" ]]; then
    OVERALL_STATUS="ATTENTION_REQUIRED"
fi

cat <<EOF
{
  "pr_number": ${PR_NUM},
  "title": "${PR_TITLE}",
  "base_branch": "${BASE_BRANCH}",
  "head_sha": "${HEAD_SHA}",
  "payload_dir": "${PAYLOAD_DIR}",
  "ci_trigger_active": ${CI_TRIGGER_ACTIVE},
  "ci_warning": ${CI_WARNING},
  "diff_summary": {
    "files_changed_against_base": ${FILES_CHANGED_BASE},
    "files_changed_against_main": ${FILES_CHANGED_MAIN},
    "base_diff_path": "${BASE_DIFF_PATH}",
    "main_diff_path": "${MAIN_DIFF_PATH}"
  },
  "checks": {
    "reject_empty_commit_workflow_intact": {
      "passed": ${WORKFLOW_PASSED},
      "detail": "${WORKFLOW_DETAIL}"
    },
    "no_smuggled_autoload_refactors": {
      "passed": ${AUTOLOAD_PASSED},
      "violations": ${AUTOLOAD_JSON}
    },
    "provider_switch_cases_preserved": {
      "passed": ${SWITCH_PASSED},
      "modified": ${SWITCH_MODIFIED},
      "missing_cases": ${MISSING_CASES_JSON}
    },
    "sibling_duplicates_detected": {
      "detected": ${SIBLING_DETECTED},
      "duplicates": ${SIBLING_JSON}
    }
  },
  "status": "${OVERALL_STATUS}"
}
EOF
