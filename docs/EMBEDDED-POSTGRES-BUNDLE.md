# Embedded Postgres Bundle

Muesli vendors the `boomship/postgres-vector-embedded` release bundle to run
the embedded desktop backend in CI and local integration tests.

- Vendor: `boomship/postgres-vector-embedded`
- Version: `v0.2.2`
- License: MIT
- PostgreSQL: `17.5`
- pgvector: `0.8.0`
- Linux asset:
  `https://github.com/boomship/postgres-vector-embedded/releases/download/v0.2.2/postgres-full-linux-x64.tar.gz`
- SHA256:
  `5b62bbc684d8d8fc813b42b88613c3ed631fbbe18440a6f68be873f406337a83`

## Bump Process

To update the bundle, change the version and download URL, fetch the new
archive, recompute the SHA256, and update this document and
`.github/workflows/ci.yml` together. Keep the checksum pinned in CI so the job
fails loudly if the asset changes unexpectedly.
