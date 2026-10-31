#!/usr/bin/env bash
# provider_checklist.sh - Non-mutating verification checklist for cloakenv secret providers.
# Emits structured JSON summary and exits 0 if all checks pass, 1 if any fail.

set -euo pipefail

usage() {
    cat <<'EOF'
Usage: provider_checklist.sh <provider_name>

Performs 8 mechanical checks for a cloakenv secret provider:
  1. Source file exists (internal/provider/<name>.go)
  2. Test file exists (internal/provider/<name>_test.go)
  3. Implements 6 core provider.SecretProvider interface methods
  4. Registered in engine (internal/engine/provider_manager.go)
  5. Documented in README.md
  6. Configured in examples/config.yaml
  7. No uncommitted modifications to go.mod
  8. Cross-platform safety (no ungated OS-specific imports)

Outputs structured JSON to stdout.
EOF
}

if [[ $# -eq 0 ]] || [[ "$1" == "-h" ]] || [[ "$1" == "--help" ]]; then
    usage
    exit 0
fi

PROVIDER="$1"

# Resolve repo root directory
REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$REPO_ROOT"

PASSED_COUNT=0
TOTAL_COUNT=8

# 1. Source file check
SOURCE_PATH="internal/provider/${PROVIDER}.go"
if [[ -f "$SOURCE_PATH" ]]; then
    SOURCE_PASSED="true"
    SOURCE_DETAIL="Source file exists"
    PASSED_COUNT=$((PASSED_COUNT + 1))
else
    SOURCE_PASSED="false"
    SOURCE_DETAIL="Missing source file: ${SOURCE_PATH}"
fi

# 2. Test file check
TEST_PATH="internal/provider/${PROVIDER}_test.go"
if [[ -f "$TEST_PATH" ]]; then
    TEST_PASSED="true"
    TEST_DETAIL="Test file exists"
    PASSED_COUNT=$((PASSED_COUNT + 1))
else
    TEST_PASSED="false"
    TEST_DETAIL="Missing test file: ${TEST_PATH}"
fi

# 3. Interface compliance check
# Checks for the 6 core methods of provider.SecretProvider
MISSING_METHODS=()
if [[ "$SOURCE_PASSED" == "true" ]]; then
    # Look for method definitions on receiver types, e.g. func (p *...) MethodName
    for method in "Scheme()" "Initialize(" "GetSecret(" "SetSecret(" "DeleteSecret(" "Validate("; do
        method_name="${method%%(*}"
        if ! grep -qE "func\s+\([^)]+\)\s+${method_name}\s*\(" "$SOURCE_PATH"; then
            MISSING_METHODS+=("\"${method_name}\"")
        fi
    done
else
    MISSING_METHODS=("\"Scheme\"" "\"Initialize\"" "\"GetSecret\"" "\"SetSecret\"" "\"DeleteSecret\"" "\"Validate\"")
fi

if [[ ${#MISSING_METHODS[@]} -eq 0 ]]; then
    IFACE_PASSED="true"
    IFACE_DETAIL="Implements all 6 provider.SecretProvider methods"
    MISSING_JSON="[]"
    PASSED_COUNT=$((PASSED_COUNT + 1))
else
    IFACE_PASSED="false"
    IFACE_DETAIL="Missing required methods"
    MISSING_JOINED=$(IFS=,; echo "${MISSING_METHODS[*]}")
    MISSING_JSON="[${MISSING_JOINED}]"
fi

# 4. Engine registration check
ENGINE_FILE="internal/engine/provider_manager.go"
ORCH_FILE="internal/engine/orchestrator.go"
ENGINE_PASSED="false"
ENGINE_DETAIL="Not registered in engine provider switch or builtins"

if [[ -f "$ENGINE_FILE" ]] && grep -qE "case\s+[\"']${PROVIDER}[\"']:" "$ENGINE_FILE"; then
    ENGINE_PASSED="true"
    ENGINE_DETAIL="Registered in initVaultProvider"
    PASSED_COUNT=$((PASSED_COUNT + 1))
elif [[ -f "$ORCH_FILE" ]] && grep -qE "[\"']${PROVIDER}[\"']\s*:\s*" "$ORCH_FILE"; then
    ENGINE_PASSED="true"
    ENGINE_DETAIL="Registered as built-in provider in orchestrator"
    PASSED_COUNT=$((PASSED_COUNT + 1))
fi

# 5. README documentation check
README_FILE="README.md"
if [[ -f "$README_FILE" ]] && grep -qiE "(provider:\s*[\"']?${PROVIDER}[\"']?|#.*${PROVIDER})" "$README_FILE"; then
    README_PASSED="true"
    README_DETAIL="Provider documented in README.md"
    PASSED_COUNT=$((PASSED_COUNT + 1))
else
    README_PASSED="false"
    README_DETAIL="Provider not found in README.md"
fi

# 6. Example config check
CONFIG_FILE="examples/config.yaml"
if [[ -f "$CONFIG_FILE" ]] && grep -qiE "provider:\s*[\"']?${PROVIDER}[\"']?" "$CONFIG_FILE"; then
    CONFIG_PASSED="true"
    CONFIG_DETAIL="Provider example configured in examples/config.yaml"
    PASSED_COUNT=$((PASSED_COUNT + 1))
else
    CONFIG_PASSED="false"
    CONFIG_DETAIL="Provider example not found in examples/config.yaml"
fi

# 7. Clean dependencies check
# Checks if go.mod has unstaged or staged modifications
if git diff --quiet HEAD -- go.mod 2>/dev/null; then
    GOMOD_PASSED="true"
    GOMOD_DETAIL="No uncommitted dependency modifications in go.mod"
    PASSED_COUNT=$((PASSED_COUNT + 1))
else
    GOMOD_PASSED="false"
    GOMOD_DETAIL="Uncommitted changes detected in go.mod"
fi

# 8. Cross-platform safety check
# Scans for OS-specific imports without build tags
XP_PASSED="true"
XP_DETAIL="No ungated OS-specific imports found"

if [[ "$SOURCE_PASSED" == "true" ]]; then
    # Check if the file contains OS-specific imports
    if grep -qE '"(syscall|golang\.org/x/sys/windows|golang\.org/x/sys/unix)"' "$SOURCE_PATH"; then
        # If it has OS imports, verify it has a build tag
        if ! head -n 5 "$SOURCE_PATH" | grep -qE '^//go:build'; then
            XP_PASSED="false"
            XP_DETAIL="OS-specific package imported without //go:build constraint tag"
        fi
    fi
fi

if [[ "$XP_PASSED" == "true" ]]; then
    PASSED_COUNT=$((PASSED_COUNT + 1))
fi

# Output JSON summary
ALL_PASSED="false"
EXIT_CODE=1
if [[ $PASSED_COUNT -eq $TOTAL_COUNT ]]; then
    ALL_PASSED="true"
    EXIT_CODE=0
fi

cat <<EOF
{
  "provider": "${PROVIDER}",
  "all_passed": ${ALL_PASSED},
  "passed_count": ${PASSED_COUNT},
  "total_count": ${TOTAL_COUNT},
  "checks": {
    "source_file": {
      "passed": ${SOURCE_PASSED},
      "path": "${SOURCE_PATH}",
      "detail": "${SOURCE_DETAIL}"
    },
    "test_file": {
      "passed": ${TEST_PASSED},
      "path": "${TEST_PATH}",
      "detail": "${TEST_DETAIL}"
    },
    "interface_compliance": {
      "passed": ${IFACE_PASSED},
      "missing_methods": ${MISSING_JSON},
      "detail": "${IFACE_DETAIL}"
    },
    "engine_registration": {
      "passed": ${ENGINE_PASSED},
      "path": "${ENGINE_FILE}",
      "detail": "${ENGINE_DETAIL}"
    },
    "readme_documented": {
      "passed": ${README_PASSED},
      "path": "${README_FILE}",
      "detail": "${README_DETAIL}"
    },
    "example_config": {
      "passed": ${CONFIG_PASSED},
      "path": "${CONFIG_FILE}",
      "detail": "${CONFIG_DETAIL}"
    },
    "go_mod_clean": {
      "passed": ${GOMOD_PASSED},
      "detail": "${GOMOD_DETAIL}"
    },
    "cross_platform_safe": {
      "passed": ${XP_PASSED},
      "detail": "${XP_DETAIL}"
    }
  },
  "summary": "${PASSED_COUNT} of ${TOTAL_COUNT} checklist items passed"
}
EOF

exit "$EXIT_CODE"
