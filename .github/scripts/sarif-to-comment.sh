#!/usr/bin/env bash
set -euo pipefail

if (($# == 0)); then
  echo "Usage: sarif-to-comment.sh <report.sarif> [report.sarif ...]" >&2
  exit 2
fi

repository=${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}
pull_number=${PR_NUMBER:?PR_NUMBER is required}
head_sha=${PR_HEAD_SHA:?PR_HEAD_SHA is required}

# Validate environment variables before use in paths or API calls
if [[ ! "$pull_number" =~ ^[0-9]+$ ]]; then
  echo "Error: PR_NUMBER must be numeric: $pull_number" >&2
  exit 2
fi

if [[ ! "$repository" =~ ^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$ ]]; then
  echo "Error: GITHUB_REPOSITORY must be in 'owner/repo' format: $repository" >&2
  exit 2
fi

if [[ ! "$head_sha" =~ ^[0-9a-fA-F]{40}$ ]]; then
  echo "Error: PR_HEAD_SHA must be a 40-character hex commit SHA: $head_sha" >&2
  exit 2
fi

# Validate input SARIF files
jq -s -e 'all(.[]; .version == "2.1.0" and (.runs | type == "array"))' "$@" >/dev/null
finding_count=$(jq -s '[.[] | .runs[] | .results[]?] | length' "$@")

if ((finding_count == 0)); then
  if [[ "${GOLANGCI_STATUS:-success}" == "failure" || "${TEST_STATUS:-success}" == "failure" ]]; then
    echo "::error::CI checks failed (lint: ${GOLANGCI_STATUS:-unknown}, test: ${TEST_STATUS:-unknown}) but SARIF reports contain 0 findings." >&2
    exit 1
  fi
  echo "No SARIF findings; skipping review."
  exit 0
fi

# Fetch current pull request details
pull_request=$(gh api "repos/$repository/pulls/$pull_number")
pr_state=$(jq -r '.state // ""' <<<"$pull_request")
pr_head_sha=$(jq -r '.head.sha // ""' <<<"$pull_request")
pr_repo=$(jq -r '.head.repo.full_name // ""' <<<"$pull_request")

if [[ "$pr_state" != "open" || "$pr_head_sha" != "$head_sha" || "${pr_repo,,}" != "${repository,,}" ]]; then
  echo "Skipping stale, closed, or non-local pull request."
  exit 0
fi

# Check for existing bot review for the exact head SHA with pagination to prevent duplicate reviews
existing_reviews=$(gh api --paginate --slurp "repos/$repository/pulls/$pull_number/reviews")
if jq -n -e --arg sha "$head_sha" --argjson existing_reviews "$existing_reviews" '
  ($existing_reviews | flatten) as $all_revs |
  any($all_revs[];
    .commit_id == $sha and
    (
      ((.body // "") | contains("<!-- cloakenv-sarif-review -->")) or
      ((.user.login // "") | test("^github-actions\\[bot\\]$"))
    ) and
    (.state == "CHANGES_REQUESTED" or .state == "COMMENTED")
  )
' >/dev/null 2>&1; then
  echo "Review already submitted for commit $head_sha; skipping duplicate review."
  exit 0
fi

# Fetch PR files to identify changed lines on the right side of the diff
files_json=$(mktemp)
payload=$(mktemp)
trap 'rm -f "$files_json" "$payload"' EXIT

gh api --paginate --slurp "repos/$repository/pulls/$pull_number/files" >"$files_json"

jq -s \
  --arg sha "$head_sha" \
  --arg repository "$repository" \
  --slurpfile files "$files_json" '
  def sanitize_str:
    if type == "string" then
      gsub("[\r\n\t]+"; " ") |
      gsub("[\u0000-\u001f\u007f]"; "") |
      gsub("@"; "@<!-- -->")
    else "" end;

  def sanitize_rule:
    if type == "string" then
      sanitize_str | .[0:100]
    else "Finding" end;

  def sanitize_msg:
    if type == "string" then
      sanitize_str | .[0:1000]
    else "No diagnostic message" end;

  def clean_line:
    if type == "number" and . > 0 then .
    elif type == "string" and test("^[0-9]+$") and (. | tonumber) > 0 then (. | tonumber)
    else null end;

  def normalize_path:
    if type == "string" then
      gsub("\\\\"; "/") |
      sub("^file://+"; "") |
      sub("^\\./"; "")
    else null end;

  def is_repo_relative:
    if type == "string" and length > 0 then
      (test("^(/|[a-zA-Z]:|\\.\\.)") | not) and
      (contains("/../") | not) and
      (startswith("../") | not) and
      (endswith("/..") | not) and
      (. != "..")
    else false end;

  def changed_lines:
    reduce ((.patch // "") | split("\n")[]) as $line
      ({line: null, added: []};
       if ($line | startswith("@@ ")) then
         .line = (($line | capture("^@@ -[0-9]+(,[0-9]+)? \\+(?<number>[0-9]+)").number) | tonumber)
       elif .line == null then .
       elif ($line | startswith("+")) then
         .added += [.line] | .line += 1
       elif ($line | startswith("-")) then .
       elif ($line | startswith(" ")) then .line += 1
       else .
       end) | .added;

  ($files | flatten) as $pr_files |
  (reduce $pr_files[] as $f ({}; .[$f.filename] = ($f | changed_lines))) as $changed |

  [
    .[] | .runs[] | .results[]? |
    (.locations[0].physicalLocation.artifactLocation.uri | normalize_path) as $raw_path |
    (.locations[0].physicalLocation.region.startLine | clean_line) as $line |
    (if ($raw_path | is_repo_relative) then $raw_path else null end) as $path |
    {
      rule: ((.ruleId // "Finding") | sanitize_rule),
      message: ((.message.text // "No diagnostic message") | sanitize_msg),
      path: $path,
      line: $line
    }
  ] | unique_by([.rule, .message, .path, .line]) as $findings |

  [
    $findings[] |
    select(.path != null and .line != null) |
    select(.line as $l | ($changed[.path] // [] | index($l)) != null)
  ] as $diff_matches |

  ($diff_matches | group_by([.path, .line])) as $diff_groups |

  ($diff_groups[:50] | map({
    path: .[0].path,
    line: .[0].line,
    side: "RIGHT",
    body: (if length == 1 then
             "**\(.[0].rule)**: \(.[0].message)"
           else
             ([.[] | "- **\(.rule)**: \(.message)"] | join("\n"))
           end)
  })) as $comments |

  # Derive inline findings and overflow findings consistently from group slices
  ($diff_groups[:50] | flatten) as $inline_findings |
  ($diff_groups[50:] | flatten) as $overflow_findings |

  [
    $findings[] |
    select(
      .path == null or .line == null or
      (.line as $l | ($changed[.path] // [] | index($l)) == null)
    )
  ] as $not_inline |

  (
    "<!-- cloakenv-sarif-review -->\n" +
    "Automated analysis found \($findings | length) issue(s):\n\n" +
    (
      if ($inline_findings | length) > 0 then
        "### Inline Findings\n" +
        ([$inline_findings[] | "- [`\(.path):\(.line)`](https://github.com/\($repository)/blob/\($sha)/\(.path)#L\(.line)) **\(.rule)**: \(.message)"] | join("\n")) +
        "\n\n"
      else "" end
    ) +
    (
      if ($overflow_findings | length) > 0 then
        "### Additional Changed-Line Findings\n" +
        ([$overflow_findings[] | "- [`\(.path):\(.line)`](https://github.com/\($repository)/blob/\($sha)/\(.path)#L\(.line)) **\(.rule)**: \(.message)"] | join("\n")) +
        "\n\n"
      else "" end
    ) +
    (
      if ($not_inline | length) > 0 then
        "### Findings Outside Changed Lines\n" +
        ([$not_inline[] |
          if .path != null and .line != null then
            "- [`\(.path):\(.line)`](https://github.com/\($repository)/blob/\($sha)/\(.path)#L\(.line)) **\(.rule)**: \(.message)"
          else
            "- **\(.rule)**: \(.message)"
          end
        ] | join("\n"))
      else "" end
    )
  ) as $full_body |

  ($full_body | if length > 60000 then .[0:60000] + "\n\n... [Truncated remaining findings due to length limit]" else . end) as $safe_body |

  {
    commit_id: $sha,
    event: "REQUEST_CHANGES",
    body: $safe_body,
    comments: $comments
  }
' "$@" >"$payload"

# Function to post review with retries and graceful fallback
post_review() {
  local payload_file="$1"
  local attempt=1
  local max_attempts=3
  local response=""
  local exit_code=0

  while ((attempt <= max_attempts)); do
    response=$(gh api "repos/$repository/pulls/$pull_number/reviews" \
      --method POST \
      --input "$payload_file" 2>&1) && exit_code=0 || exit_code=$?

    if ((exit_code == 0)); then
      echo "Successfully published pull request review."
      return 0
    fi

    # Handle self-authored PR limitation on GitHub REST API
    if [[ "$response" == *"Can not request changes on your own pull request"* || \
          "$response" == *"disallows REQUEST_CHANGES on self-authored"* ]]; then
      echo "Self-authored pull request detected; falling back to COMMENT event." >&2
      local comment_payload
      comment_payload=$(mktemp)
      jq '.event = "COMMENT"' "$payload_file" >"$comment_payload"
      gh api "repos/$repository/pulls/$pull_number/reviews" \
        --method POST \
        --input "$comment_payload"
      local res_code=$?
      rm -f "$comment_payload"
      return "$res_code"
    fi

    # Handle diff line mismatch fallback
    if [[ "$response" == *"Pull request review thread line must be part of the diff"* || \
          ( "$response" == *"Unprocessable Entity"* && "$response" == *"comments"* ) ]]; then
      echo "Inline comments failed diff validation; falling back to summary-only review." >&2
      local fallback_payload
      fallback_payload=$(mktemp)
      jq 'del(.comments)' "$payload_file" >"$fallback_payload"
      gh api "repos/$repository/pulls/$pull_number/reviews" \
        --method POST \
        --input "$fallback_payload"
      local res_code=$?
      rm -f "$fallback_payload"
      return "$res_code"
    fi

    echo "Attempt $attempt to post review failed ($response). Retrying in $((attempt * 2))s..." >&2
    sleep $((attempt * 2))
    attempt=$((attempt + 1))
  done

  echo "Error: Failed to publish review after $max_attempts attempts: $response" >&2
  return 1
}

post_review "$payload"
