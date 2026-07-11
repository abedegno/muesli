# Muesli Backup & Restore Guide

This guide explains how to back up and restore a self-hosted Muesli instance.
Two deployment models are covered: **Docker Compose** (the primary/recommended
model) and **bare-metal** (running binaries directly on the host).

---

## What to Back Up

Muesli has two stateful pieces that must both be backed up to achieve a
complete, restorable snapshot:

| Piece                                       | Docker Compose location                                                                        | Bare-metal location                                                                                              |
| ------------------------------------------- | ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| **Postgres database** (schema + all data)   | Volume `pgdata` → `/var/lib/postgresql/data` inside the `postgres` container; DB name `muesli` | Managed by your system Postgres; DB name `muesli`                                                                |
| **Audio blob store** (uploaded audio files) | Volume `audio` → `/data/audio` inside the `server` container                                   | Directory pointed to by `$MUESLI_STORAGE_DIR` (default: `./data/audio` relative to the server working directory) |

> **Why both?** Audio keys (file paths/UUIDs) are stored in the database.
> If the DB and the blob store are out of sync — DB references a key that has
> no file, or vice-versa — the restore is incomplete or broken.

---

## Consistent-Snapshot Strategies

Because the two pieces live in separate storage systems you must take care to
keep them consistent. Choose one of the two safe strategies below:

### Strategy A — Stop the server first (simplest, zero risk)

```
docker compose stop server   # or: kill the bare-metal server process
# … perform both backups …
docker compose start server
```

With the server stopped there are no in-flight writes, so any backup of the
DB _and_ the blobs taken during that window is guaranteed consistent.

### Strategy B — Live backup (low risk, not zero)

1. Take `pg_dump` first. `pg_dump` takes a transaction-consistent snapshot
   internally, so it captures a consistent view of the DB even while Muesli
   is running.
2. Immediately after `pg_dump` completes, copy the blob directory.

**Caveat:** Audio blobs are write-once — they are never mutated after upload —
so the only gap is the brief window between the DB snapshot and the blob copy.
In that window a _new_ upload could be committed to the DB but its blob not
yet present in the blob copy. This window is typically seconds, but it is
not zero. If you can tolerate a brief service pause, Strategy A is always
safer.

---

## Backup — Docker Compose

### 1. Postgres database

```bash
docker compose exec -T postgres pg_dump -U postgres muesli \
  | gzip > muesli-$(date +%Y%m%d%H%M%S).sql.gz
```

### 2. Audio blob store

The audio volume name is prefixed by the Docker Compose **project name** (by
default the name of the directory containing `compose.yaml`). Before running
the command below, verify the exact volume name:

```bash
docker volume ls | grep audio
```

Then replace `muesli_audio` if your prefix differs:

```bash
docker run --rm \
  -v muesli_audio:/data:ro \
  -v "$(pwd)":/backup \
  alpine \
  tar czf /backup/audio-$(date +%Y%m%d%H%M%S).tar.gz -C /data .
```

This mounts the named volume read-only (`:ro`) inside an ephemeral Alpine
container and writes the archive to your current host directory.

---

## Backup — Bare-Metal

### 1. Postgres database

```bash
pg_dump -U postgres muesli \
  | gzip > muesli-$(date +%Y%m%d%H%M%S).sql.gz
```

Adjust `-U postgres` and any connection flags (`-h`, `-p`) to match your local
Postgres setup.

### 2. Audio blob store

```bash
rsync -a --delete "$MUESLI_STORAGE_DIR/" /path/to/backup/audio/
```

Or, to produce a portable archive:

```bash
tar czf audio-$(date +%Y%m%d%H%M%S).tar.gz -C "$MUESLI_STORAGE_DIR" .
```

---

## Restore Procedure

**Always restore in this order: database first, then blobs, then start the
server.** Starting the server before the blobs are in place can cause
in-progress requests to see partial data.

> **Before you begin:** identify the exact backup files you want to restore
> and assign them to variables. Using shell globs when multiple backup files
> exist can silently target the wrong artifact.

### Docker Compose

```bash
# Set these to the exact files you want to restore
DB_DUMP=muesli-20240101120000.sql.gz        # <-- your DB dump filename
AUDIO_ARCHIVE=audio-20240101120005.tar.gz   # <-- matching audio archive

# 1. Ensure the server is stopped
docker compose stop server

# 2. Restore the database
zcat "$DB_DUMP" \
  | docker compose exec -T postgres psql -U postgres muesli

# 3. Restore the audio blobs
docker run --rm \
  -v muesli_audio:/data \
  -v "$(pwd)":/backup \
  alpine \
  tar xzf /backup/"$AUDIO_ARCHIVE" -C /data

# 4. Start the server
docker compose start server
```

### Bare-Metal

```bash
# Set these to the exact files you want to restore
DB_DUMP=muesli-20240101120000.sql.gz        # <-- your DB dump filename
AUDIO_ARCHIVE=audio-20240101120005.tar.gz   # <-- matching audio archive

# 1. Stop the Muesli server process

# 2. Restore the database
zcat "$DB_DUMP" | psql -U postgres muesli

# 3. Restore the audio blobs
rsync -a /path/to/backup/audio/ "$MUESLI_STORAGE_DIR/"

# 4. Start the server
./bin/muesli   # or: systemctl start muesli
```

### Post-Restore Smoke Check

After the server is running, verify the process started successfully:

```bash
curl -f http://localhost:<PORT>/healthz
```

A `200 OK` response confirms the server process is up. It does **not**
verify database connectivity — `/healthz` is a static liveness handler only.
To confirm the DB is reachable, attempt a login in the UI, or run a quick
connectivity check directly:

```bash
# Docker Compose
docker compose exec -T postgres psql -U postgres muesli -c '\l'

# Bare-metal
psql -U postgres muesli -c '\l'
```

Browse to the UI and spot-check that a recent transcript is visible and its
audio plays correctly.

---

## Scheduling Automated Backups

### cron

Add a daily job with `crontab -e`:

```cron
# Muesli backup — daily at 02:00, keep 30 days of DB dumps
0 2 * * * docker compose -f /opt/muesli/compose.yaml exec -T postgres \
    pg_dump -U postgres muesli | gzip \
    > /var/backups/muesli/muesli-$(date +\%Y\%m\%d\%H\%M\%S).sql.gz
5 2 * * * docker run --rm -v muesli_audio:/data:ro -v /var/backups/muesli:/backup \
    alpine tar czf /backup/audio-$(date +\%Y\%m\%d\%H\%M\%S).tar.gz -C /data .
# Prune backups older than 30 days
10 2 * * * find /var/backups/muesli -mtime +30 -delete
```

### Borgbackup / Restic (recommended for production)

For deduplication, encryption, and remote storage consider
[Restic](https://restic.net/) or [Borgbackup](https://www.borgbackup.org/).
Both tools can be pointed at the directories/archives produced above.

Example with Restic:

```bash
# Initialise a repository (once)
restic -r s3:s3.amazonaws.com/my-bucket/muesli init

# Back up (run via cron or a systemd timer)
restic -r s3:s3.amazonaws.com/my-bucket/muesli backup \
  /var/backups/muesli/

# Prune old snapshots (keep 7 daily, 4 weekly, 12 monthly)
restic -r s3:s3.amazonaws.com/my-bucket/muesli forget \
  --keep-daily 7 --keep-weekly 4 --keep-monthly 12 --prune
```

See the [Restic documentation](https://restic.readthedocs.io/) or
[Borgbackup documentation](https://borgbackup.readthedocs.io/) for full setup
instructions including encryption key management.

---

## In-App Backup Feature (BAK01)

In addition to the manual procedures above, Muesli has an optional in-app,
admin-driven **backup** feature for the Postgres database. It exists to make
"click a button, get a dump" possible for self-hosters who don't want to wire
up `pg_dump`/cron by hand. **There is no restore endpoint** — restore is,
and remains, the manual `pg_restore`/`psql` procedure documented above in
this file. Dumps produced by this feature are plain `pg_dump --format=custom`
files, so they restore with the standard `pg_restore` tool exactly like any
other custom-format dump.

> This feature only covers the **Postgres database**. It does not back up the
> audio blob store — continue to back up `$MUESLI_STORAGE_DIR` (or the
> `audio` volume) separately, as described above.

### Configuration

| Env var                           | Required | Default | Meaning                                                                                                                                                                                |
| --------------------------------- | -------- | ------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `MUESLI_BACKUP_DIR`               | No       | (unset) | Directory backups are written to. Empty/unset **disables the entire feature** (admin API returns 400, no scheduler).                                                                   |
| `MUESLI_BACKUP_SCHEDULE_INTERVAL` | No       | (unset) | A `time.ParseDuration` string (e.g. `24h`, `12h30m`). Empty/unset disables the automatic scheduled backup — manual "run backup now" still works as long as `MUESLI_BACKUP_DIR` is set. |
| `MUESLI_BACKUP_RETENTION_COUNT`   | No       | `7`     | How many backups to keep. Applied as a prune step after **every** backup — manual or scheduled — so the directory never grows unbounded.                                               |

The server binary requires the `pg_dump` binary to be present on its `$PATH`
(it shells out to it directly with `--format=custom`, passing the
`DATABASE_URL` connection string as-is).

### Admin API

All routes below require the same bearer-token admin auth as every other
`/api/*` route (there is no separate "admin" auth layer):

- `POST /api/admin/backup` — runs `pg_dump` now, prunes to the configured
  retention count, and returns `201` with the new backup's metadata
  (`filename`, `size_bytes`, `created_at`). Returns `400` if
  `MUESLI_BACKUP_DIR` isn't set.
- `GET /api/admin/backups` — lists current backups, newest-first, with the
  same metadata shape. Returns an empty list (not an error) when the backup
  dir has no backups yet; `400` if the feature isn't configured.
- `GET /api/admin/backups/{filename}` — downloads one backup file as an
  attachment. `{filename}` is validated strictly against the
  `muesli-<UTC timestamp>.dump` naming convention before it ever touches the
  filesystem (rejects path traversal); `404` if it doesn't exist.

The admin UI's **Backups** tab is a thin client over these three endpoints:
a table of existing backups, a "Run backup now" button (`POST
/api/admin/backup`, then refreshes the list), and a per-row "Download"
action (`GET /api/admin/backups/{filename}`).

### Restoring a backup produced by this feature

Because these are ordinary `pg_dump --format=custom` files, restoring one is
identical to the manual bare-metal/Docker Compose restore procedures
documented earlier in this file, using `pg_restore` instead of
`psql`/`zcat` (custom format is not plain SQL):

```bash
# Bare-metal example; adjust -U/-h/-p to match your Postgres setup.
# 1. Stop the Muesli server process.
# 2. Restore (drop/recreate the target DB first if you want a clean restore):
pg_restore -U postgres -d muesli --clean --if-exists muesli-20240101120000.dump
# 3. Start the server again.
```

For Docker Compose, copy the dump into the `postgres` container (or pipe it
in) and run the equivalent `docker compose exec -T postgres pg_restore ...`.
Always restore the audio blob store too if you're doing a full disaster
recovery — see the Restore Procedure section above.
