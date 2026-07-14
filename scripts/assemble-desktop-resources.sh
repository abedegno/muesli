#!/usr/bin/env bash
# Stage every native dependency the muesli desktop app bundles into
# build/resources/, which electron-builder ships as extraResources (-> the
# packaged app's Contents/Resources/). The Electron supervisor resolves each
# from process.resourcesPath and passes the matching env to the embedded server
# (see src/main/resourcePaths.ts) -- so this layout is a contract with that file.
#
# Layout produced (must match resourcePaths.ts):
#   build/resources/server/{muesli,whisper-cpp-transcriber,ollama-agent}
#   build/resources/pg/                 extracted postgres-full bundle (bin/lib/share)
#   build/resources/pgvector/           vector.{dylib,control} + vector--*.sql
#   build/resources/models/whisper/ggml-tiny.en.bin
#   build/resources/bin/ffmpeg
#
# Prereqs: the Go binaries already built into build/bin/, and `npm ci` run so
# ffmpeg-static is present. Usage: scripts/assemble-desktop-resources.sh [target]
set -euo pipefail

TARGET="${1:-darwin-arm64}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RES="$ROOT/build/resources"

# --- pinned vendored assets (bump alongside docs/EMBEDDED-POSTGRES-BUNDLE.md) ---
PG_VER="v0.2.2"
case "$TARGET" in
  darwin-arm64)
    PG_ASSET="postgres-full-darwin-arm64.tar.gz"
    PG_SHA="cfb6621b3fa9fb36b558d5004ee4d0acec122c01fa84691d39b4004a69d2ceaf"
    ;;
  *)
    echo "assemble: unsupported target '$TARGET' (only darwin-arm64 wired)" >&2
    exit 2
    ;;
esac
PG_URL="https://github.com/boomship/postgres-vector-embedded/releases/download/${PG_VER}/${PG_ASSET}"

MODEL="ggml-tiny.en.bin"
MODEL_URL="https://huggingface.co/ggerganov/whisper.cpp/resolve/main/${MODEL}"
MODEL_SHA="921e4cf8686fdd993dcd081a5da5b6c365bfde1162e72b08d75ac75289920b1f"

sha_of() { shasum -a 256 "$1" | awk '{print $1}'; }
verify() {
  local got; got="$(sha_of "$1")"
  [ "$got" = "$2" ] || { echo "assemble: SHA256 mismatch for $1: got $got want $2" >&2; exit 1; }
}

echo "assemble: target=$TARGET -> $RES"
rm -rf "$RES"
mkdir -p "$RES/server" "$RES/pg" "$RES/pgvector" "$RES/models/whisper" "$RES/bin"

# 1) Go binaries (built by the workflow into build/bin/)
for b in muesli whisper-cpp-transcriber ollama-agent; do
  if [ -x "$ROOT/build/bin/$b" ]; then
    cp "$ROOT/build/bin/$b" "$RES/server/$b"; chmod +x "$RES/server/$b"
    echo "assemble: server/$b"
  else
    echo "assemble: WARN build/bin/$b missing (build it first)" >&2
  fi
done

# 2) Postgres + pgvector bundle
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
echo "assemble: fetching $PG_ASSET"
curl -fsSL "$PG_URL" -o "$TMP/pg.tgz"
verify "$TMP/pg.tgz" "$PG_SHA"
mkdir -p "$TMP/x"; tar -xzf "$TMP/pg.tgz" -C "$TMP/x"
PGBIN="$(find "$TMP/x" -type f -path '*/bin/postgres' | head -1)"
[ -n "$PGBIN" ] || { echo "assemble: postgres binary not found in bundle" >&2; exit 1; }
PGROOT="$(dirname "$(dirname "$PGBIN")")"
cp -R "$PGROOT/." "$RES/pg/"
# Split pgvector artifacts into pgvector/. The embedded code (internal/embedded)
# skips the copy when the bundle already ships vector.control, but still needs
# MUESLI_EMBEDDED_PGVECTOR_DIR pointed at a real dir to run CREATE EXTENSION.
find "$RES/pg" \( -name 'vector.dylib' -o -name 'vector.so' -o -name 'vector.control' -o -name 'vector--*.sql' \) \
  -exec cp {} "$RES/pgvector/" \; 2>/dev/null || true
echo "assemble: pg/ ($(du -sh "$RES/pg" | awk '{print $1}')) + pgvector/ ($(ls "$RES/pgvector" 2>/dev/null | wc -l | tr -d ' ') files)"

# 3) whisper model
echo "assemble: fetching $MODEL"
curl -fsSL "$MODEL_URL" -o "$RES/models/whisper/$MODEL"
verify "$RES/models/whisper/$MODEL" "$MODEL_SHA"
echo "assemble: models/whisper/$MODEL"

# 4) ffmpeg (host-platform static binary from the ffmpeg-static devDep)
FF="$(node -e 'process.stdout.write(require("ffmpeg-static")||"")' 2>/dev/null || true)"
[ -n "$FF" ] && [ -f "$FF" ] || { echo "assemble: ffmpeg-static not resolved (run npm ci first)" >&2; exit 1; }
cp "$FF" "$RES/bin/ffmpeg"; chmod +x "$RES/bin/ffmpeg"
echo "assemble: bin/ffmpeg"

echo "assemble: DONE. tree:"
find "$RES" -maxdepth 2 -type d | sort | sed 's/^/  /'
du -sh "$RES" | awk '{print "assemble: total "$1}'
