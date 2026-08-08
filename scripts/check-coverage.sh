#!/usr/bin/env bash
set -euo pipefail

coverage_file="${1:-coverage.out}"
floor_file="${2:-coverage-floors/server.txt}"

if [[ ! -f "$coverage_file" ]]; then
  echo "coverage profile not found: $coverage_file" >&2
  echo "produce it with: go test ./... -p 4 -parallel 2 -coverprofile=$coverage_file" >&2
  exit 2
fi

if [[ ! -f "$floor_file" ]]; then
  echo "coverage floor not found: $floor_file" >&2
  exit 2
fi

total="$(go tool cover -func="$coverage_file" | awk '/^total:/ { gsub(/%/, "", $3); print $3 }')"
floor="$(tr -d '[:space:]' < "$floor_file")"

if [[ ! "$total" =~ ^[0-9]+([.][0-9]+)?$ ]] || [[ ! "$floor" =~ ^[0-9]+([.][0-9]+)?$ ]]; then
  echo "invalid coverage value (current=$total, floor=$floor)" >&2
  exit 2
fi

printf 'Server coverage: %s%% (floor: %s%%)\n' "$total" "$floor"
awk -v total="$total" -v floor="$floor" 'BEGIN { exit(total + 0 < floor + 0) }' || {
  echo "Server coverage is below the committed floor." >&2
  exit 1
}
