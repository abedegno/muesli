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
case "$TARGET" in
  darwin-arm64) ;;
  *)
    echo "assemble: unsupported target '$TARGET' (only darwin-arm64 wired)" >&2
    exit 2
    ;;
esac

# Postgres: zonky's relocatable UNIVERSAL binaries (arm64+x86_64), pinned.
ZONKY_PG_VERSION="17.5.0"
ZONKY_ARTIFACT="embedded-postgres-binaries-darwin-arm64v8"
ZONKY_JAR_SHA="e9d3398e10c2ec926395498b03e75ad1a24eeaed82895e756a7e173b202cf6de"

# pgvector: pinned artifact from abedegno/embedded-postgres-vector.
PGVECTOR_TAG="pgvector-0.8.0-pg17-1"
PGVECTOR_ASSET="pgvector-darwin-arm64.tar.gz"
PGVECTOR_SHA="7ba554ea5a1a13bd1d57845f7bfe704428207e3990413d7a1bff52367b46331f"

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

# 2) Postgres (zonky) + pgvector (pinned), pre-injected into the bundle.
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT

echo "assemble: fetching zonky Postgres $ZONKY_PG_VERSION"
ZURL="https://repo1.maven.org/maven2/io/zonky/test/postgres/$ZONKY_ARTIFACT/$ZONKY_PG_VERSION/$ZONKY_ARTIFACT-$ZONKY_PG_VERSION.jar"
curl -fsSL "$ZURL" -o "$TMP/zonky.jar"
verify "$TMP/zonky.jar" "$ZONKY_JAR_SHA"
unzip -o -q "$TMP/zonky.jar" -d "$TMP/zonky-jar"
ZTXZ="$(find "$TMP/zonky-jar" -name '*.txz' -o -name '*.tar.xz' | head -1)"
tar xJf "$ZTXZ" -C "$RES/pg"
[ -x "$RES/pg/bin/postgres" ] || { echo "assemble: zonky extract missing bin/postgres" >&2; exit 1; }

echo "assemble: fetching pgvector $PGVECTOR_TAG"
PVURL="https://github.com/abedegno/embedded-postgres-vector/releases/download/$PGVECTOR_TAG/$PGVECTOR_ASSET"
curl -fsSL "$PVURL" -o "$TMP/pgvector.tgz"
verify "$TMP/pgvector.tgz" "$PGVECTOR_SHA"
mkdir -p "$TMP/pgv"; tar xzf "$TMP/pgvector.tgz" -C "$TMP/pgv"

# Pre-inject pgvector into zonky's STANDARD paths so CREATE EXTENSION finds it,
# and stage a share/extension/vector.control so the Go server's runtime pgvector
# check passes and InstallPgvector is skipped (the signed bundle stays read-only).
PKGLIB="$RES/pg/lib/postgresql"; EXTDIR="$RES/pg/share/postgresql/extension"
mkdir -p "$PKGLIB" "$EXTDIR" "$RES/pg/share/extension"
cp "$TMP/pgv/vector.dylib" "$PKGLIB/"
cp "$TMP/pgv/vector.control" "$TMP/pgv"/vector--*.sql "$EXTDIR/"
cp "$TMP/pgv/vector.control" "$RES/pg/share/extension/"

# Flat fallback copy (MUESLI_EMBEDDED_PGVECTOR_DIR), matching the prior layout.
cp "$TMP/pgv/vector.dylib" "$TMP/pgv/vector.control" "$TMP/pgv"/vector--*.sql "$RES/pgvector/"
echo "assemble: pg/ ($(du -sh "$RES/pg" | awk '{print $1}')) with pgvector injected + pgvector/ fallback"

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
