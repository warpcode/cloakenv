#!/usr/bin/env bash
set -euo pipefail

if (($# == 0)); then
  echo "Usage: sarif-to-comment.sh <report.sarif> [report.sarif ...]" >&2
  exit 2
fi

jq -s -e 'all(.[]; .version == "2.1.0" and (.runs | type == "array"))' "$@" >/dev/null
finding_count=$(jq -s '[.[] | .runs[] | .results[]?] | length' "$@")
if ((finding_count == 0)); then
  echo "No SARIF findings; skipping review."
  exit 0
fi

repository=${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}
pull_number=${PR_NUMBER:?PR_NUMBER is required}
head_sha=${PR_HEAD_SHA:?PR_HEAD_SHA is required}

pull_request=$(gh api "repos/$repository/pulls/$pull_number")
if [[ $(jq -r '.state' <<<"$pull_request") != open ||
      $(jq -r '.head.sha' <<<"$pull_request") != "$head_sha" ||
      $(jq -r '.head.repo.full_name // ""' <<<"$pull_request") != "$repository" ]]; then
  echo "Skipping stale, closed, or non-local pull request."
  exit 0
fi

payload=$(mktemp)
trap 'rm -f "$payload"' EXIT

jq -s \
  --arg sha "$head_sha" \
  --arg repository "$repository" '
  [
    .[] | .runs[] | .results[]? |
    {
      rule: (.ruleId // "SARIF finding"),
      message: ((.message.text // "No diagnostic message") | gsub("[\r\n]+"; " ")),
      path: .locations[0].physicalLocation.artifactLocation.uri,
      line: .locations[0].physicalLocation.region.startLine
    }
  ] | unique_by([.rule, .message, .path, .line]) as $findings |
  {
    commit_id: $sha,
    event: "REQUEST_CHANGES",
    body: (
      "Automated analysis found \($findings | length) issue(s):\n\n" +
      ([$findings[] |
        if .path != null and .line != null then
          "- [`\(.path):\(.line)`](https://github.com/\($repository)/blob/\($sha)/\(.path)#L\(.line)) **\(.rule)**: \(.message)"
        else
          "- **\(.rule)**: \(.message)"
        end
      ] | join("\n"))
    )
  }
' "$@" >"$payload"

gh api "repos/$repository/pulls/$pull_number/reviews" \
  --method POST \
  --input "$payload"
