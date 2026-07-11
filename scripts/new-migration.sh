#!/usr/bin/env bash
# Create a new timestamped database migration pair (up + down).
# Timestamp IDs are collision-free so parallel branches never pick the same
# version. Usage: scripts/new-migration.sh <snake_case_name>
set -euo pipefail
name="${1:-}"
if [ -z "$name" ]; then
  echo "usage: $0 <snake_case_name>" >&2
  exit 2
fi
if ! printf '%s' "$name" | grep -Eq '^[a-z0-9_]+$'; then
  echo "name must be snake_case (lowercase letters, digits, underscores): $name" >&2
  exit 2
fi
ts="$(date -u +%Y%m%d%H%M%S)"
dir="internal/db/migrations"
up="$dir/${ts}_${name}.up.sql"
down="$dir/${ts}_${name}.down.sql"
printf -- '-- %s (up)\n' "$name" > "$up"
printf -- '-- %s (down)\n' "$name" > "$down"
echo "created:"
echo "  $up"
echo "  $down"
