#!/usr/bin/env bash
set -euo pipefail

report=${LINT_REPORT:?LINT_REPORT is required}
head_sha_file=${PR_HEAD_SHA_FILE:?PR_HEAD_SHA_FILE is required}
repository=${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}
pull_number=${PR_NUMBER:?PR_NUMBER is required}
head_sha=$(<"$head_sha_file")

pull_request=$(gh api "repos/$repository/pulls/$pull_number")
if [[ $(jq -r '.state' <<<"$pull_request") != open ||
      $(jq -r '.head.sha' <<<"$pull_request") != "$head_sha" ||
      $(jq -r '.head.repo.full_name // ""' <<<"$pull_request") != "$repository" ]]; then
  echo "Skipping stale, closed, or non-local pull request."
  exit 0
fi

jq -e '.Issues | type == "array"' "$report" >/dev/null
issue_count=$(jq '.Issues | length' "$report")
if ((issue_count == 0)); then
  echo "No lint findings; skipping review."
  exit 0
fi

files=$(mktemp)
payload=$(mktemp)
trap 'rm -f "$files" "$payload"' EXIT

gh api --paginate --slurp "repos/$repository/pulls/$pull_number/files" >"$files"

jq -n \
  --arg sha "$head_sha" \
  --slurpfile report "$report" \
  --slurpfile pages "$files" '
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

  ($pages[0] | add) as $files |
  (reduce $files[] as $file
    ({}; .[$file.filename] = ($file | changed_lines))) as $changed |
  [ $report[0].Issues[] |
    {
      path: (.Pos.Filename | sub("^\\./"; "")),
      line: .Pos.Line,
      body: (
        "**golangci-lint (\(.FromLinter // "unknown"))**: \(.Text)"
        + (if (.Pos.Column // 0) > 0 then " (column \(.Pos.Column))" else "" end)
      )
    }
  ] as $findings |
  [ $findings[] |
    select(.line as $line | ($changed[.path] // [] | index($line)) != null)
  ] | group_by([.path, .line]) |
  map({
    path: .[0].path,
    line: .[0].line,
    side: "RIGHT",
    body: (map(.body) | join("\n"))
  }) as $inline |
  [ $findings[] |
    select(.line as $line | ($changed[.path] // [] | index($line)) == null) |
    "- `\(.path):\(.line)` \(.body)"
  ] as $not_inline |
  ($inline[:100]) as $comments |
  ($not_inline + [
    $inline[100:][] | "- `\(.path):\(.line)` \(.body)"
  ]) as $summary |
  {
    commit_id: $sha,
    event: "REQUEST_CHANGES",
    body: (
      "golangci-lint reported \($findings | length) finding(s). "
      + "The lint check remains failing independently of this review."
      + (if ($summary | length) > 0 then
          "\n\nFindings not attached inline:\n" + ($summary | join("\n"))
        else "" end)
    ),
    comments: $comments
  }
' >"$payload"

gh api "repos/$repository/pulls/$pull_number/reviews" --method POST --input "$payload"
echo "Published REQUEST_CHANGES review for pull request #$pull_number."
