# Embedded Postgres Bundle

muesli uses Postgres + pgvector in two separate contexts:

**Linux hosted / CI integration tests** use the Docker image
`pgvector/pgvector:pg16`. This path does not use the desktop bundle.

**macOS desktop app** ships a preassembled, relocatable bundle built from two
pinned artifacts:

- zonky embedded Postgres `17.5.0`
- jar SHA256:
  `e9d3398e10c2ec926395498b03e75ad1a24eeaed82895e756a7e173b202cf6de`
- artifact:
  `embedded-postgres-binaries-darwin-arm64v8`

The bundle is consumed by `github.com/fergusstrange/embedded-postgres` through
`BinariesPath`, so the app does not build PostgreSQL from source at release
time.

pgvector is supplied as a separate pinned artifact and injected into the
desktop bundle:

- release tag:
  `pgvector-0.8.0-pg17-1`
- asset:
  `pgvector-darwin-arm64.tar.gz`
- asset SHA256:
  `7ba554ea5a1a13bd1d57845f7bfe704428207e3990413d7a1bff52367b46331f`

The desktop assembly script stages pgvector into:

- `pg/lib/postgresql/vector.dylib`
- `pg/share/postgresql/extension/vector.control`

and also places `pg/share/extension/vector.control` as the runtime-skip marker
so the embedded server skips `InstallPgvector` when the bundle is already
present.

## Bump Process

Update the zonky Postgres pin and SHA together, and update the pgvector release
tag and SHA together. If the Postgres major version changes, publish or select a
matching `pgvector-<version>-pg<major>-<rev>` release first, then update both
pins in lockstep.
