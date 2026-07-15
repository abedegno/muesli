# Embedded Postgres Bundle

muesli needs Postgres + pgvector in two contexts, sourced differently:

**Linux (hosted / CI integration tests)** vendors the prebuilt
`boomship/postgres-vector-embedded` bundle (v0.2.2, PostgreSQL 17.5 + pgvector
0.8.0, MIT):

- Linux asset:
  `https://github.com/boomship/postgres-vector-embedded/releases/download/v0.2.2/postgres-full-linux-x64.tar.gz`
- SHA256:
  `5b62bbc684d8d8fc813b42b88613c3ed631fbbe18440a6f68be873f406337a83`

**macOS (desktop app)** builds its OWN self-contained Postgres via
`scripts/build-postgres-macos.sh` (PostgreSQL 17.5 `--without-icu` + pgvector
0.8.0). The prebuilt macOS bundles are NOT usable: they link Homebrew ICU by
absolute build-machine path and are not relocatable, so Postgres cannot start on
a user's Mac. Building `--without-icu` (Postgres falls back to libc collation)
leaves only Postgres's own dylibs as non-system deps, which the script relocates
to `@loader_path`. `.github/workflows/desktop-release.yml` runs the script and
`scripts/assemble-desktop-resources.sh` stages the result into the app.

## Bump Process

To update the bundle, change the version and download URL, fetch the new
archive, recompute the SHA256, and update this document and
`.github/workflows/ci.yml` together. Keep the checksum pinned in CI so the job
fails loudly if the asset changes unexpectedly.
