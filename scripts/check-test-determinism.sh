#!/usr/bin/env bash
# check-test-determinism.sh — gate banning time.Sleep and time.Now in Go unit tests.
#
# Usage: bash scripts/check-test-determinism.sh
#   Exit 0 if clean, exit 1 if banned calls are found (with offending lines printed).
#
# Skip list: scripts/check-test-determinism-skip.txt
#   Add a file's relative path (from repo root) to exempt it while a migration PR is pending.
#   Do not add new entries without an accompanying migration PR.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SKIP_FILE="${REPO_ROOT}/scripts/check-test-determinism-skip.txt"

# Build a list of skip files (relative paths from repo root).
skip_files=()
if [[ -f "$SKIP_FILE" ]]; then
    while IFS= read -r line; do
        # Strip comments and blank lines.
        line="${line%%#*}"
        line="${line// /}"
        [[ -z "$line" ]] && continue
        skip_files+=("$line")
    done < "$SKIP_FILE"
fi

# Find all *_test.go files, excluding any path that contains the string "e2e"
# (covers /e2e/ directory, e2e_test.go, e2e_embed_test.go, etc.)
mapfile -t test_files < <(
    find "${REPO_ROOT}" -name '*_test.go' \
        | grep -v 'e2e' \
        | sort
)

# Filter out skip-listed files.
filtered=()
for f in "${test_files[@]}"; do
    rel="${f#${REPO_ROOT}/}"
    skip=0
    for s in "${skip_files[@]}"; do
        if [[ "$rel" == "$s" ]]; then
            skip=1
            break
        fi
    done
    [[ "$skip" -eq 0 ]] && filtered+=("$f")
done

if [[ "${#filtered[@]}" -eq 0 ]]; then
    echo "OK: no non-e2e Go test files to check."
    exit 0
fi

# Grep for banned calls.
offenders="$(grep -n -E 'time\.Sleep\(|time\.Now\(' "${filtered[@]}" 2>/dev/null || true)"

if [[ -n "$offenders" ]]; then
    echo "FAIL: banned time calls found in non-e2e Go test files:"
    echo ""
    echo "$offenders"
    echo ""
    echo "Fix: use testutil.FakeClock instead of time.Now(), and avoid time.Sleep()."
    echo "To exempt a file while a migration PR is pending, add its relative path to:"
    echo "  scripts/check-test-determinism-skip.txt"
    exit 1
fi

echo "OK: no banned time calls in non-e2e Go test files."
exit 0
