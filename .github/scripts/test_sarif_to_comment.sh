#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
script="$script_dir/sarif-to-comment.sh"
temp_dir=$(mktemp -d)
trap 'rm -rf "$temp_dir"' EXIT

mkdir "$temp_dir/bin"
cat >"$temp_dir/bin/gh" <<'GH'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$GH_CALLS"
if [[ "$*" == "api repos/example/repo/pulls/42" ]]; then
  printf '{"state":"open","head":{"sha":"abc123","repo":{"full_name":"example/repo"}}}\n'
elif [[ "$*" == *"pulls/42/reviews"* ]]; then
  while (($#)); do
    if [[ "$1" == "--input" ]]; then
      cp "$2" "$GH_PAYLOAD"
      exit 0
    fi
    shift
  done
  exit 2
fi
GH
chmod +x "$temp_dir/bin/gh"
export PATH="$temp_dir/bin:$PATH"
export GITHUB_REPOSITORY=example/repo
export PR_NUMBER=42
export PR_HEAD_SHA=abc123
export GH_CALLS="$temp_dir/calls"
export GH_PAYLOAD="$temp_dir/payload.json"
touch "$GH_CALLS"

cat >"$temp_dir/clean.sarif" <<'SARIF'
{"version":"2.1.0","runs":[{"results":[]}]}
SARIF
bash "$script" "$temp_dir/clean.sarif"
if grep -q 'pulls/42/reviews' "$GH_CALLS"; then
  echo "A clean SARIF report must not post a review." >&2
  exit 1
fi

cat >"$temp_dir/first.sarif" <<'SARIF'
{"version":"2.1.0","runs":[{"results":[{"ruleId":"errcheck","message":{"text":"unchecked error"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"internal/first.go"},"region":{"startLine":8}}}]}]}]}
SARIF
cat >"$temp_dir/second.sarif" <<'SARIF'
{"version":"2.1.0","runs":[{"results":[{"ruleId":"govet","message":{"text":"bad conversion"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"internal/second.go"},"region":{"startLine":19}}}]}]}]}
SARIF
bash "$script" "$temp_dir/first.sarif" "$temp_dir/second.sarif"

jq -e '
  .commit_id == "abc123" and
  .event == "REQUEST_CHANGES" and
  (.body | contains("internal/first.go:8")) and
  (.body | contains("unchecked error")) and
  (.body | contains("internal/second.go:19")) and
  (.body | contains("bad conversion"))
' "$GH_PAYLOAD" >/dev/null
[[ $(grep -c 'pulls/42/reviews' "$GH_CALLS") == 1 ]]
echo "SARIF review publisher tests passed."
