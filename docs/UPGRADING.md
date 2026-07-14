# Upgrading Muesli

This guide covers upgrading an existing Muesli deployment to a newer version.
The short version: rebuild (or pull) the images, restart the server, and
you are done. **DB migrations run automatically on every server start** — no
`psql`, no manual migration commands.

---

## Upgrade procedure (Docker Compose)

The reference `docker-compose.yml` builds the application services from
source (`build: .` for the server; `build: ./plugins/…` for the plugins).
The upgrade flow therefore rebuilds images locally rather than pulling them
from a registry.

### 1. Pull source changes

```bash
git pull
```

### 2. Rebuild and restart

```bash
docker compose build          # rebuilds server, whisper, and agent images
docker compose up -d          # recreates containers whose image changed
```

If you are running a registry-backed deployment where images are pushed and
pulled rather than built locally, replace step 2 with:

```bash
docker compose pull server whisper agent
docker compose up -d
```

The Muesli server applies any pending DB migrations **before** it starts
accepting requests, so the database schema is always consistent with the
running binary by the time the first API call arrives.

### 3. Verify the upgrade

```bash
# All services should be in the "Up" state
docker compose ps

# The server logs "muesli listening on <addr> (worker pool started)" when ready
docker compose logs server --tail 40
```

No further steps are needed for a standard upgrade.

---

## Upgrade procedure (hosted install / compose.prod)

This path is for installs created by `scripts/install.sh`. Those installs use
`docker-compose.prod.yml` with image names under `ghcr.io/abedegno/muesli-*`
pinned literally to the release tag the compose file was fetched from.

### 1. Back up first

Before upgrading, follow the backup guide in [docs/BACKUP.md](BACKUP.md).
For the Postgres snapshot, the one-line command is:

```bash
docker compose -f docker-compose.prod.yml exec -T postgres pg_dump -U postgres muesli | gzip > muesli-$(date +%Y%m%d%H%M%S).sql.gz
```

Run that from the install directory that contains `docker-compose.prod.yml`
and `.env`.

### 2. Pull the new release's compose file

Re-run the installer pointed at the new release tag so it refetches
`docker-compose.prod.yml` (with the new tag's images pinned in) alongside a
fresh `install.sh` and `.env.example`:

```bash
MUESLI_RELEASE_TAG=v1.2.3 scripts/install.sh --dir /opt/muesli
```

`docker-compose.prod.yml`, `.env.example`, and `install.sh` are always
refetched and overwritten from the chosen release. Do **not** add `--force`
here: without it the installer leaves your existing `.env` (and its secrets)
untouched, which is what you want on an upgrade; `--force` would regenerate
`MUESLI_MASTER_KEY` and `MUESLI_STORAGE_SIGNING_KEY`, invalidating existing
encrypted data and presigned URLs.

### 3. Upgrade

The recommended one-command path is:

```bash
scripts/upgrade.sh --dir /opt/muesli
```

What it does:

- confirms you have the right install directory
- reminds you to take a backup
- prints the current `MUESLI_IMAGE_TAG` from `.env` for rollback reference if
  it happens to be set (release-based installs normally leave it unset, since
  the image tags are pinned literally in `docker-compose.prod.yml` instead)
- pulls the pinned GHCR images referenced by the installed
  `docker-compose.prod.yml`
- recreates the containers with `docker compose up -d`

The Muesli server applies migrations on boot, so the upgrade script does not
run any manual `psql` or migration command.

### 4. Roll back if needed

If the new release does not work, re-run the installer with the previous
release tag (`MUESLI_RELEASE_TAG=<previous tag> scripts/install.sh --dir
/opt/muesli`, again without `--force`) to restore the old
`docker-compose.prod.yml`, then run the same `pull` and `up -d` commands
again.

If the failed version introduced a migration that the old binary cannot read,
restore the database backup you took before the upgrade using the restore
procedure in [docs/BACKUP.md](BACKUP.md). See the existing Rollback section
below for the general rule: restore the pre-upgrade database snapshot before
starting the old version again.

---

## Upgrade procedure (binary / systemd)

1. **Stop the running server.** If you are using systemd:

   ```bash
   sudo systemctl stop muesli
   ```

2. **Replace the binary** with the new build:

   ```bash
   # Example — adjust paths to match your installation
   sudo cp muesli /usr/local/bin/muesli
   ```

3. **Start the server:**
   ```bash
   sudo systemctl start muesli
   ```
   Migrations run on startup before the server begins accepting connections.

---

## How DB migrations work

`db.Migrate()` in `internal/db/db.go` uses
[golang-migrate](https://github.com/golang-migrate/migrate) with the migration
SQL files **embedded directly in the server binary** (`//go:embed migrations/*.sql`).

Key properties:

- **Automatic** — called unconditionally at the top of `main()`, before the
  connection pool is opened and before any worker or API handler starts.
- **Idempotent** — already-applied migrations are tracked in the
  `schema_migrations` table; re-running the server on an up-to-date schema is
  a no-op.
- **Append-only** — migrations are numbered sequentially
  (`0001_init.up.sql`, `0002_name.up.sql`, …). New versions only add
  migrations; they never alter or remove existing ones.
- **Never hand-run** — do not run `psql` migration commands by hand against a
  database that the server manages; golang-migrate's version tracking will
  diverge. Let the server apply them.

---

## Manual steps for specific upgrade scenarios

Most upgrades need none of the steps below. Check the release notes for your
target version to confirm.

### Changing the embeddings model (`MUESLI_EMBEDDINGS_MODEL`)

Embeddings are stored per-model (`note_embeddings.model`). If you change
`MUESLI_EMBEDDINGS_MODEL` (or update the embedding task prefixes), existing
vectors from the old model are retained but become stale — semantic search will
return results only for notes re-embedded with the new model.

To re-embed all ready notes under the new model, run the `reembed` subcommand
**after** the new server has started:

```bash
# Docker Compose (service name: server)
docker compose exec server muesli reembed

# Binary
./muesli reembed
```

`muesli reembed` clears the current model's embeddings and enqueues a fresh
`embed` job for every `ready` note. The worker pool processes them in the
background. Progress is visible in the server logs.

### Rotating `MUESLI_MASTER_KEY`

`MUESLI_MASTER_KEY` is a base64-encoded 32-byte key that encrypts the plugin
`config` payload at rest (the JSON blob stored in the `plugins` table). Bearer
tokens are stored unencrypted in the same table and are not affected by master-key
rotation. Rotating the master key requires:

1. Update `MUESLI_MASTER_KEY` in your `.env` file (or environment).
2. Restart the server.
3. Re-register each plugin via the admin UI (`/admin` → Plugins): delete the
   existing plugin entry, then add it again with the same URL and the same bearer
   token. The server encrypts the config with the new master key on creation.
   Old encrypted config from the previous key cannot be decrypted and must be
   replaced this way. Rotating the bearer token is a separate, independent step
   that also requires reconfiguring the plugin container.

### Rotating `MUESLI_STORAGE_SIGNING_KEY`

`MUESLI_STORAGE_SIGNING_KEY` signs presigned audio upload/download URLs. Rotating
it invalidates any URLs that were issued before the restart (e.g. an in-flight
upload whose presigned URL was generated seconds ago). Already-uploaded audio
is unaffected — only the temporary URLs change.

Rotation steps: update the key in `.env`, restart. No other action is needed.

---

## Rollback

Muesli does not apply down-migrations automatically. If you need to roll back
to a previous version after an upgrade introduced a new migration:

1. **Stop the server.**
2. **Restore your database** from a pre-upgrade snapshot (e.g.
   `pg_restore` from your backup). This is the safe path — it returns the
   schema and data to a known-good state.
3. **Start the old binary.** It will see an already up-to-date schema and skip
   all migrations.

Down-migration SQL files (`*.down.sql`) exist in `internal/db/migrations/` for
reference and developer use, but they are never executed automatically.

> **Tip:** Always take a Postgres snapshot before upgrading a production
> deployment. With Docker Compose, a one-liner backup is:
>
> ```bash
> docker compose exec postgres pg_dump -U postgres muesli | gzip > muesli-backup-$(date +%Y%m%d-%H%M%S).sql.gz
> ```
