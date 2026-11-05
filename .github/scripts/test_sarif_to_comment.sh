#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
script="$script_dir/sarif-to-comment.sh"
temp_dir=$(mktemp -d)
trap 'rm -rf "$temp_dir"' EXIT

mkdir -p "$temp_dir/bin"

cat >"$temp_dir/bin/gh" <<'GH'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$GH_CALLS"

if [[ "$*" == "api repos/example/repo/pulls/42" ]]; then
  printf '{"state":"open","head":{"sha":"0123456789abcdef0123456789abcdef01234567","repo":{"full_name":"example/repo"}}}\n'
elif [[ "$*" == "api repos/example/repo/pulls/42/reviews" ]]; then
  if [[ -f "$GH_EXISTING_REVIEWS" ]]; then
    cat "$GH_EXISTING_REVIEWS"
  else
    printf '[]\n'
  fi
elif [[ "$*" == *"repos/example/repo/pulls/42/files"* ]]; then
  cat "$GH_FILES"
elif [[ "$*" == *"pulls/42/reviews"* ]]; then
  if [[ "${GH_MOCK_ERROR:-}" == "self-authored" ]]; then
    while (($#)); do
      if [[ "$1" == "--input" ]]; then
        ev=$(jq -r '.event' "$2")
        if [[ "$ev" == "REQUEST_CHANGES" ]]; then
          echo "Can not request changes on your own pull request" >&2
          exit 1
        else
          cp "$2" "$GH_PAYLOAD"
          exit 0
        fi
      fi
      shift
    done
  elif [[ "${GH_MOCK_ERROR:-}" == "invalid-diff-line" ]]; then
    while (($#)); do
      if [[ "$1" == "--input" ]]; then
        has_comments=$(jq '.comments? | length' "$2")
        if ((has_comments > 0)); then
          echo "Pull request review thread line must be part of the diff" >&2
          exit 1
        else
          cp "$2" "$GH_PAYLOAD"
          exit 0
        fi
      fi
      shift
    done
  else
    while (($#)); do
      if [[ "$1" == "--input" ]]; then
        cp "$2" "$GH_PAYLOAD"
        exit 0
      fi
      shift
    done
    exit 2
  fi
fi
GH
chmod +x "$temp_dir/bin/gh"

export PATH="$temp_dir/bin:$PATH"
export GITHUB_REPOSITORY="example/repo"
export PR_NUMBER="42"
export PR_HEAD_SHA="0123456789abcdef0123456789abcdef01234567"
export GH_CALLS="$temp_dir/calls"
export GH_PAYLOAD="$temp_dir/payload.json"
export GH_EXISTING_REVIEWS="$temp_dir/reviews.json"
export GH_FILES="$temp_dir/files.json"

cat >"$GH_FILES" <<'FILES'
[
  {
    "filename": "internal/first.go",
    "patch": "@@ -1,5 +1,8 @@\n package internal\n+func Added() {\n+  var a int\n+  _ = a\n+}"
  }
]
FILES

echo "--- Test 1: Clean SARIF report on clean CI skips review ---"
touch "$GH_CALLS"
cat >"$temp_dir/clean.sarif" <<'SARIF'
{"version":"2.1.0","runs":[{"results":[]}]}
SARIF
bash "$script" "$temp_dir/clean.sarif"
if grep -q 'reviews' "$GH_CALLS"; then
  echo "FAIL: Clean report posted a review!" >&2
  exit 1
fi
echo "PASS: Clean report skipped."

echo "--- Test 2: Clean SARIF report on failing CI emits error and fails ---"
set +e
GOLANGCI_STATUS="failure" bash "$script" "$temp_dir/clean.sarif" 2>"$temp_dir/err.log"
clean_fail_code=$?
set -e
if [[ $clean_fail_code -eq 0 ]] || ! grep -q "CI checks failed" "$temp_dir/err.log"; then
  echo "FAIL: Clean report with failing CI did not fail! (code=$clean_fail_code)" >&2
  exit 1
fi
echo "PASS: Clean report with failing CI failed as expected."

echo "--- Test 3: Validation of parameters ---"
set +e
PR_NUMBER="not-a-number" bash "$script" "$temp_dir/clean.sarif" 2>/dev/null
p_code=$?
set -e
if [[ $p_code -ne 2 ]]; then
  echo "FAIL: Invalid PR_NUMBER should exit 2, got $p_code" >&2
  exit 1
fi
echo "PASS: Validation works."

echo "--- Test 4: Path normalization, diff mapping, deduplication & sanitization ---"
> "$GH_CALLS"
rm -f "$GH_PAYLOAD"
cat >"$temp_dir/findings1.sarif" <<'SARIF'
{
  "version": "2.1.0",
  "runs": [{
    "results": [
      {
        "ruleId": "errcheck",
        "message": {"text": "hello @alice\r\nunchecked error"},
        "locations": [{"physicalLocation":{"artifactLocation":{"uri":"internal\\first.go"},"region":{"startLine":2}}}]
      },
      {
        "ruleId": "errcheck",
        "message": {"text": "hello @alice unchecked error"},
        "locations": [{"physicalLocation":{"artifactLocation":{"uri":"./internal/first.go"},"region":{"startLine":2}}}]
      },
      {
        "ruleId": "govet",
        "message": {"text": "out of diff finding"},
        "locations": [{"physicalLocation":{"artifactLocation":{"uri":"internal/second.go"},"region":{"startLine":99}}}]
      }
    ]
  }]
}
SARIF

bash "$script" "$temp_dir/findings1.sarif"

# Check payload
jq -e '
  .commit_id == "0123456789abcdef0123456789abcdef01234567" and
  .event == "REQUEST_CHANGES" and
  (.comments | length == 1) and
  .comments[0].path == "internal/first.go" and
  .comments[0].line == 2 and
  .comments[0].side == "RIGHT" and
  (.comments[0].body | contains("@<!-- -->alice")) and
  (.body | contains("out of diff finding")) and
  (.body | contains("internal/second.go:99"))
' "$GH_PAYLOAD" >/dev/null
echo "PASS: Diff mapping and sanitization verified."

echo "--- Test 5: Existing review skips duplicate ---"
> "$GH_CALLS"
rm -f "$GH_PAYLOAD"
cat >"$GH_EXISTING_REVIEWS" <<'REVIEWS'
[
  {
    "commit_id": "0123456789abcdef0123456789abcdef01234567",
    "state": "CHANGES_REQUESTED",
    "body": "Existing review"
  }
]
REVIEWS

bash "$script" "$temp_dir/findings1.sarif"
if [[ -f "$GH_PAYLOAD" ]]; then
  echo "FAIL: Duplicate review was submitted!" >&2
  exit 1
fi
echo "PASS: Duplicate review skipped."
rm -f "$GH_EXISTING_REVIEWS"

echo "--- Test 6: Self-authored PR falls back to COMMENT event ---"
> "$GH_CALLS"
rm -f "$GH_PAYLOAD"
GH_MOCK_ERROR="self-authored" bash "$script" "$temp_dir/findings1.sarif"
jq -e '.event == "COMMENT"' "$GH_PAYLOAD" >/dev/null
echo "PASS: Self-authored PR fallback to COMMENT works."

echo "--- Test 7: Diff mismatch falls back to summary-only review ---"
> "$GH_CALLS"
rm -f "$GH_PAYLOAD"
GH_MOCK_ERROR="invalid-diff-line" bash "$script" "$temp_dir/findings1.sarif"
jq -e '
  .event == "REQUEST_CHANGES" and
  (.comments == null or .comments == []) and
  (.body | contains("errcheck"))
' "$GH_PAYLOAD" >/dev/null
echo "PASS: Diff mismatch fallback works."

echo "ALL TESTS PASSED!"
