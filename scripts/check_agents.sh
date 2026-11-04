#!/usr/bin/env bash
# ==============================================================================
# scripts/check_agents.sh
#
# Automated consistency check for cloakenv repository:
# 1. Diffs Makefile targets against documentation in README.md.
# 2. Diffs internal/ directories against package contracts in AGENTS.md.
# 3. Verifies that all skill names referenced in AGENTS.md exist.
#
# Exit status: 0 if all checks pass, 1 if any discrepancy is found.
# ==============================================================================

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${REPO_ROOT}"

FAILED=0

echo "==> Running cloakenv repository integrity checks..."

# ------------------------------------------------------------------------------
# Check 1: Makefile targets vs README.md documentation
# ------------------------------------------------------------------------------
echo "--- [1/3] Checking Makefile targets documentation..."

# Extract declared targets in Makefile (excluding internal pattern rules)
MAKEFILE_TARGETS=$(grep -E '^[a-zA-Z0-9_-]+:' Makefile | cut -d: -f1 | grep -v '^\.' | sort -u)

# Expected public targets that MUST be documented in README.md
REQUIRED_TARGETS="build run test bench test-all clean fmt vet install uninstall verify agents-check"

MISSING_TARGETS=()
for target in ${REQUIRED_TARGETS}; do
    if ! echo "${MAKEFILE_TARGETS}" | grep -qx "${target}"; then
        MISSING_TARGETS+=("Makefile missing target: ${target}")
    fi
    if ! grep -qE "make ${target}([[:space:]]|$|[^a-zA-Z0-9_-])" README.md; then
        MISSING_TARGETS+=("README.md missing documentation for: make ${target}")
    fi
done

if [ ${#MISSING_TARGETS[@]} -eq 0 ]; then
    TARGET_COUNT=$(echo "${REQUIRED_TARGETS}" | wc -w)
    echo "  [PASS] All ${TARGET_COUNT} required Makefile targets exist and are documented in README.md."
else
    echo "  [FAIL] Makefile target discrepancy detected:"
    for err in "${MISSING_TARGETS[@]}"; do
        echo "    - ${err}"
    done
    FAILED=1
fi

# ------------------------------------------------------------------------------
# Check 2: internal/ packages vs AGENTS.md documentation
# ------------------------------------------------------------------------------
echo "--- [2/3] Checking internal/ subpackages vs AGENTS.md..."

ACTUAL_PACKAGES=$(find internal -mindepth 1 -maxdepth 1 -type d | sed 's|^internal/||' | sort)

MISSING_PKGS=()
for pkg in ${ACTUAL_PACKAGES}; do
    if ! grep -qE "internal/${pkg}\b" AGENTS.md; then
        MISSING_PKGS+=("AGENTS.md does not document internal/${pkg}")
    fi
done

# Check if AGENTS.md documents a phantom internal package that doesn't exist
DOCUMENTED_PKGS=$(grep -oE "internal/[a-zA-Z0-9_-]+" AGENTS.md | sed 's|^internal/||' | sort -u || true)
for doc_pkg in ${DOCUMENTED_PKGS}; do
    if [ -n "${doc_pkg}" ] && [ ! -d "internal/${doc_pkg}" ]; then
        MISSING_PKGS+=("AGENTS.md documents non-existent package internal/${doc_pkg}")
    fi
done

if [ ${#MISSING_PKGS[@]} -eq 0 ]; then
    PKG_LIST=$(echo "${ACTUAL_PACKAGES}" | tr '\n' ' ' | sed 's/[[:space:]]*$//')
    echo "  [PASS] All internal/ packages (${PKG_LIST}) match AGENTS.md."
else
    echo "  [FAIL] Package layout discrepancy detected:"
    for err in "${MISSING_PKGS[@]}"; do
        echo "    - ${err}"
    done
    FAILED=1
fi

# ------------------------------------------------------------------------------
# Check 3: Referenced skills in AGENTS.md exist
# ------------------------------------------------------------------------------
echo "--- [3/3] Checking referenced skills existence..."

# Extract skill names referenced in AGENTS.md:
# 1. Matches patterns like `skill-name` (skill|skills|SOP) or backticked skills in text
# shellcheck disable=SC2016
REFERENCED_SKILLS=$(grep -E '`[a-zA-Z0-9_-]+` (skill|skills|SOP)' AGENTS.md | grep -oE '`[a-zA-Z0-9_-]+`' | tr -d '`' | sort -u || true)

# 2. Extract skills from markdown table rows referencing skills
# shellcheck disable=SC2016
TABLE_SKILLS=$(grep -E '\|[[:space:]]*`[a-zA-Z0-9_-]+`[[:space:]]*\|' AGENTS.md | grep -oE '`[a-zA-Z0-9_-]+`' | tr -d '`' | grep -vx 'go' | sort -u || true)

# 3. Extract repo-local skills referenced as .github/skills/<name> or .agents/skills/<name>
PATH_SKILLS=$(grep -oE '\.(github|agents)/skills/[a-zA-Z0-9_-]+' AGENTS.md | sed -E 's|.*skills/||' | sort -u || true)

ALL_REFERENCED_SKILLS=$(printf "%s\n%s\n%s\n" "${REFERENCED_SKILLS}" "${TABLE_SKILLS}" "${PATH_SKILLS}" | grep -v '^$' | sort -u || true)

MISSING_SKILLS=()
SKILL_COUNT=0
for skill in ${ALL_REFERENCED_SKILLS}; do
    # Filter out obvious non-skill identifiers if any caught by regex
    if [[ "${skill}" == "cloakenv" || "${skill}" == "main" || "${skill}" == "Password" || "${skill}" == "go" ]]; then
        continue
    fi
    SKILL_COUNT=$((SKILL_COUNT + 1))
    FOUND=0

    # 1. Check repo-local skills in .github/skills/
    if [ -f ".github/skills/${skill}/SKILL.md" ]; then
        FOUND=1
    # 2. Check repo-local skills in .agents/skills/
    elif [ -f ".agents/skills/${skill}/SKILL.md" ]; then
        FOUND=1
    # 3. Check user global skills in ~/.gemini/config/skills/
    elif [ -n "${HOME:-}" ] && [ -f "${HOME}/.gemini/config/skills/${skill}/SKILL.md" ]; then
        FOUND=1
    # 4. Check user plugin skills in ~/.gemini/config/plugins/*/skills/
    elif [ -n "${HOME:-}" ] && compgen -G "${HOME}/.gemini/config/plugins/*/skills/${skill}/SKILL.md" > /dev/null; then
        FOUND=1
    fi

    if [ ${FOUND} -eq 0 ]; then
        MISSING_SKILLS+=("Skill '${skill}' referenced in AGENTS.md could not be found in repo-local or user skill paths")
    fi
done

if [ ${#MISSING_SKILLS[@]} -eq 0 ]; then
    echo "  [PASS] All ${SKILL_COUNT} referenced skills exist."
else
    echo "  [FAIL] Missing skill references detected:"
    for err in "${MISSING_SKILLS[@]}"; do
        echo "    - ${err}"
    done
    FAILED=1
fi

# ------------------------------------------------------------------------------
# Summary and Exit
# ------------------------------------------------------------------------------
if [ ${FAILED} -eq 0 ]; then
    echo "==> [SUCCESS] All repository and documentation checks passed."
    exit 0
else
    echo "==> [FAILURE] One or more integrity checks failed. See errors above."
    exit 1
fi
