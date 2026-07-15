#!/usr/bin/env bash
# Build a SELF-CONTAINED PostgreSQL + pgvector for macOS and stage it as a
# relocatable bundle the muesli desktop app embeds.
#
# Why we build our own (vs a prebuilt bundle): common prebuilt macOS Postgres
# bundles link Homebrew ICU via absolute build-machine paths and are not
# relocatable, so they cannot start on a user's Mac. We configure --without-icu
# (Postgres falls back to libc collation, no ICU dependency at all) so the only
# non-system dylib is Postgres's own libpq, which we relocate to @loader_path.
#
# Output: a directory ($OUT) containing bin/ lib/ share/ that
# MUESLI_EMBEDDED_PG_BINARIES can point at directly. Postgres self-locates its
# share/lib relative to the binary, so the whole tree is relocatable once the
# libpq install names are fixed.
#
# Usage: scripts/build-postgres-macos.sh <output-dir>
set -euo pipefail

OUT="${1:?usage: build-postgres-macos.sh <output-dir>}"
PG_VERSION="${PG_VERSION:-17.5}"
PGVECTOR_VERSION="${PGVECTOR_VERSION:-0.8.0}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
PREFIX="$WORK/install"
JOBS="$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 4)"

echo "build-postgres: PostgreSQL $PG_VERSION + pgvector $PGVECTOR_VERSION -> $OUT"

# 1. PostgreSQL (self-contained: no ICU, no readline -> no Homebrew deps)
cd "$WORK"
curl -fsSL "https://ftp.postgresql.org/pub/source/v${PG_VERSION}/postgresql-${PG_VERSION}.tar.bz2" -o pg.tar.bz2
tar xjf pg.tar.bz2
cd "postgresql-${PG_VERSION}"
./configure --prefix="$PREFIX" --without-icu --without-readline >/dev/null
make -j"$JOBS" >/dev/null
make install >/dev/null

# 2. pgvector, built against the just-installed Postgres
cd "$WORK"
curl -fsSL "https://github.com/pgvector/pgvector/archive/refs/tags/v${PGVECTOR_VERSION}.tar.gz" -o pgvector.tgz
tar xzf pgvector.tgz
cd "pgvector-${PGVECTOR_VERSION}"
make PG_CONFIG="$PREFIX/bin/pg_config" >/dev/null
make install PG_CONFIG="$PREFIX/bin/pg_config" >/dev/null

# 3. Relocate: rewrite the absolute libpq install name to @loader_path-relative
#    so the binaries load libpq from the bundle instead of the build prefix.
# Drop build-only static archives first (not needed at runtime; they also
# confuse otool -L, which lists an archive's member object paths).
find "$PREFIX/lib" -name '*.a' -delete 2>/dev/null || true
# Rewrite EVERY absolute build-prefix dylib reference (and each dylib's own id)
# to a @loader_path-relative path so the whole tree is relocatable. All PG
# dylibs live in lib/, referenced relative to each Mach-O's own location.
while IFS= read -r f; do
  file "$f" 2>/dev/null | grep -q 'Mach-O' || continue
  rel="$(python3 -c 'import os,sys; print(os.path.relpath(sys.argv[1], sys.argv[2]))' "$PREFIX/lib" "$(dirname "$f")")"
  case "$f" in
    *.dylib) install_name_tool -id "@loader_path/$(basename "$f")" "$f" 2>/dev/null || true ;;
  esac
  # || true: a Mach-O with no build-prefix refs makes grep exit 1, which would
  # abort the script under `set -e`.
  for ref in $(otool -L "$f" 2>/dev/null | awk 'NR>1{print $1}' | grep "^$PREFIX/" || true); do
    install_name_tool -change "$ref" "@loader_path/$rel/$(basename "$ref")" "$f" 2>/dev/null || true
  done
done < <(find "$PREFIX/bin" "$PREFIX/lib" -type f ! -name '*.a')
# Guard: no shipped dylib/executable may still reference the build prefix.
STRAY="$(find "$PREFIX/bin" "$PREFIX/lib" -type f ! -name '*.a' 2>/dev/null | while read -r f; do
  file "$f" 2>/dev/null | grep -q 'Mach-O' || continue
  if otool -L "$f" 2>/dev/null | awk 'NR>1{print $1}' | grep -q "^$PREFIX"; then echo "$f"; fi
done || true)"
if [ -n "$STRAY" ]; then
  echo "build-postgres: ERROR unresolved build-prefix refs remain:" >&2
  echo "$STRAY" | while read -r f; do echo "  $f: $(otool -L "$f" | grep "$PREFIX")"; done >&2
  exit 1
fi

# 4. Stage: the embedded code checks <root>/share/extension/vector.control to
#    decide pgvector is present (and then only runs CREATE EXTENSION, no copy).
#    Postgres itself keeps + uses pgvector in its own relative paths
#    (lib/postgresql, share/postgresql/extension), so this is just for the check.
mkdir -p "$PREFIX/share/extension"
cp "$PREFIX"/share/postgresql/extension/vector.control "$PREFIX/share/extension/" 2>/dev/null || {
  echo "build-postgres: ERROR pgvector control not found after install" >&2; exit 1; }

# 5. Emit the bundle
rm -rf "$OUT"; mkdir -p "$OUT"
cp -R "$PREFIX/." "$OUT/"
echo "build-postgres: OK. initdb deps:"
otool -L "$OUT/bin/initdb" | grep -vE '/usr/lib|/System' | sed 's/^/  /'
"$OUT/bin/initdb" --version | sed 's/^/  /'
du -sh "$OUT" | awk '{print "build-postgres: bundle size "$1}'
