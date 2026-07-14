# Deployment Guide

Muesli listens on plain HTTP port **8080**. For any internet-facing deployment,
TLS must be terminated at a reverse proxy in front of it. The desktop client
**blocks plain-HTTP connections to a non-loopback server** — your audio, notes,
and credentials would be sent in the clear — so a TLS-terminating proxy is
required before exposing Muesli to the public internet. See
[`SECURITY.md`](../SECURITY.md) for the full security policy.

---

## Prerequisites & quick start

Start the full stack with Docker Compose:

```bash
cp .env.example .env          # fill in real secrets — see the checklist below
docker compose up -d
```

Or run the server binary directly (Go 1.23+):

```bash
go run ./cmd/muesli
```

Either way, the Muesli HTTP server listens on `localhost:8080` (or `server:8080`
when addressed from another service inside the Compose network). Your reverse
proxy must forward HTTPS traffic to that address.

---

## `muesli doctor`

Run `muesli doctor` before starting a deployment when you want a quick
config-and-connectivity check. It does not start the server or worker pool.

It prints one line per check in this format:

```text
[PASS] database: DATABASE_URL reachable; pgvector extension present
[WARN] embeddings: not configured (disabled)
[FAIL] default agent plugin: configured but unhealthy/unreachable: plugin returned 503
Summary: 5 PASS, 2 WARN, 1 FAIL
```

Checks cover:

- `DATABASE_URL` reachability and the `vector` extension
- configured default plugin URLs (`transcriber`, `streaming transcriber`,
  `agent`) via `/info`
- embeddings reachability when `MUESLI_EMBEDDINGS_URL` is set
- master key and storage signing key presence, plus known dev-default secrets
- writability of `MUESLI_STORAGE_DIR` and `MUESLI_BACKUP_DIR`

Exit codes:

- `0` when every check is `PASS` or `WARN`
- `1` when any check is `FAIL`

`WARN` is reserved for valid-but-disabled settings such as an unset embeddings
URL or an empty backup directory. `FAIL` means the setting is missing,
unreachable, or otherwise unusable.

---

## Prod vs dev compose

`docker-compose.yml` is the development stack: it builds `whisper`, `agent`,
`streaming-transcriber`, and `server` locally from source and includes dev-only
fallback secrets so iteration stays fast.

`docker-compose.prod.yml` uses the pinned GHCR images published by the build
workflow and documented in [`docs/PUBLISHED-IMAGES.md`](./PUBLISHED-IMAGES.md).
For provenance verification, see
[`Verifying provenance`](./PUBLISHED-IMAGES.md#verifying-provenance) for the
exact `gh attestation verify` command.
It does not build anything locally, and it reads secrets and other
environment-specific values directly from `.env` with no fallback defaults.
Follow the [Production secrets checklist](#production-secrets-checklist) and
set `MUESLI_IMAGE_TAG` to a real published tag. Today that tag is the short git
SHA produced by the publish workflow.

Use the production stack like this:

```bash
docker compose -f docker-compose.prod.yml pull && \
docker compose -f docker-compose.prod.yml up -d
```

---

## Operator Makefile targets

The root `Makefile` adds convenience targets for the production compose stack.
They all default to the repo root, but you can point them at a real install
directory with `PROD_DIR=/opt/muesli`:

```bash
make prod-up PROD_DIR=/opt/muesli
```

Targets:

- `prod-up` wraps `docker compose --env-file $(PROD_DIR)/.env -f $(PROD_DIR)/docker-compose.prod.yml up -d`
- `prod-down` wraps `docker compose --env-file $(PROD_DIR)/.env -f $(PROD_DIR)/docker-compose.prod.yml down`
- `prod-logs` wraps `docker compose --env-file $(PROD_DIR)/.env -f $(PROD_DIR)/docker-compose.prod.yml logs -f`
- `prod-ps` wraps `docker compose --env-file $(PROD_DIR)/.env -f $(PROD_DIR)/docker-compose.prod.yml ps`
- `prod-backup` wraps the Postgres dump command from [docs/BACKUP.md](./BACKUP.md) using the same `pg_dump` invocation as the upgrade reminder
- `prod-upgrade` wraps `scripts/upgrade.sh --dir $(PROD_DIR)` and follows the upgrade flow in [docs/UPGRADING.md](./UPGRADING.md)

Use these targets from the repo root or from an install directory created by
[`scripts/install.sh`](../scripts/install.sh). The `PROD_DIR` override lets the
same Makefile commands point at either location without changing the command
shape.

---

## Caddy (recommended)

[Caddy](https://caddyserver.com) obtains and renews TLS certificates from Let's
Encrypt automatically — no `certbot` or manual cert management needed.

**`Caddyfile`**

```caddyfile
muesli.example.com {
    reverse_proxy localhost:8080
}
```

Replace `muesli.example.com` with your actual domain. That is the entire
configuration Caddy needs; it handles ACME challenge, certificate renewal, HTTP
→ HTTPS redirect, and HTTP/2 out of the box.

**Running Caddy**

```bash
# Install Caddy (Debian/Ubuntu)
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' \
  | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
echo "deb [signed-by=/usr/share/keyrings/caddy-stable-archive-keyring.gpg] \
  https://dl.cloudsmith.io/public/caddy/stable/deb/debian any-version main" \
  | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo apt update && sudo apt install caddy

# Start with your Caddyfile
sudo caddy run --config /path/to/Caddyfile
```

Caddy can also be run as a systemd service; see the
[official docs](https://caddyserver.com/docs/running#linux-service) for details.

---

## nginx + certbot

**`/etc/nginx/sites-available/muesli`**

```nginx
server {
    listen 80;
    server_name muesli.example.com;
    # certbot will manage the ACME challenge here
    location /.well-known/acme-challenge/ { root /var/www/certbot; }
    location / { return 301 https://$host$request_uri; }
}

server {
    listen 443 ssl;
    server_name muesli.example.com;

    ssl_certificate     /etc/letsencrypt/live/muesli.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/muesli.example.com/privkey.pem;

    location / {
        proxy_pass http://localhost:8080;

        proxy_http_version 1.1;
        proxy_set_header Connection "";

        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
    }
}
```

Replace `muesli.example.com` with your domain and update the certificate paths
to match your setup.

**Obtaining a certificate with certbot**

```bash
sudo apt install certbot python3-certbot-nginx
sudo certbot --nginx -d muesli.example.com
```

Certbot edits the nginx config to wire in the certificate paths and sets up
automatic renewal via a systemd timer or cron job. After the initial run, test
renewal with:

```bash
sudo certbot renew --dry-run
```

The `proxy_http_version 1.1` + `Connection ""` pair enables HTTP/1.1 keep-alive
between nginx and the Muesli backend, which avoids the per-request connection
overhead of HTTP/1.0 (nginx's default upstream mode).

---

## TLS + running as a service

For a copy-paste-ready reverse proxy setup, start with the tracked examples in
`infra/reverse-proxy/`:

- `infra/reverse-proxy/Caddyfile` is the documented default. Caddy handles TLS
  automatically and is the simplest option for most deployments.
- `infra/reverse-proxy/nginx.conf` is an nginx example with certbot-style TLS
  paths, HTTP -> HTTPS redirect, and ACME challenge handling.

The inline snippets above are kept as quick illustrations, but the files in
`infra/reverse-proxy/` are the versions to copy into a real deployment.

For a bare-metal single-box install, use `infra/systemd/muesli.service`:

```bash
sudo cp infra/systemd/muesli.service /etc/systemd/system/muesli.service
sudoedit /etc/systemd/system/muesli.service  # adjust WorkingDirectory first
sudo systemctl daemon-reload
sudo systemctl enable --now muesli.service
```

The unit runs `docker compose -f docker-compose.prod.yml up` in the foreground
so systemd can supervise it, and it stops the stack with `docker compose -f
docker-compose.prod.yml down` on shutdown.

---

## `MUESLI_PUBLIC_URL`

Set this environment variable to the **public HTTPS base URL** of your
deployment — for example:

```dotenv
MUESLI_PUBLIC_URL=https://muesli.example.com
```

Muesli uses this value when generating presigned upload and download URLs for
audio files. Without it, the server will embed `http://localhost:8080` in those
URLs, which are unreachable from any client that is not on the same machine.

Add the line to your `.env` file alongside the other secrets (see below). The
compose stack picks it up automatically on the next `docker compose up`.

---

## Install script (A3)

For a fresh production box, the recommended path is the one-line installer in
[`scripts/install.sh`](../scripts/install.sh) (see the README's One-line
install section). It fetches `docker-compose.prod.yml` and `.env.example` from
the git ref you choose, writes fresh `MUESLI_MASTER_KEY` and
`MUESLI_STORAGE_SIGNING_KEY` values into `.env`, and can optionally start the
stack with `--up`.

Use `MUESLI_INSTALL_REF` to pin the files you fetch and `MUESLI_IMAGE_TAG` to
pin the image tag written into `.env`. `--force` regenerates `.env` even if one
already exists, and `--dir` / `-d` choose the install directory.

The installer handles the two secrets that are most error-prone to generate by
hand, but you still need to review the remaining production values before the
first boot, especially `WHISPER_TOKEN`, `AGENT_TOKEN`, `MUESLI_PUBLIC_URL`, and
the `MUESLI_IMAGE_TAG` you want to run. Cross-check those against the manual
[Production secrets checklist](#production-secrets-checklist) below.

---

## Resource sizing

For a single-node production deployment that runs the server, Postgres, Ollama,
and Whisper on the same host, a practical starting point is **4 vCPU / 16 GiB
RAM**. If you expect concurrent meetings or want the box to stay comfortable
under load, **8 vCPU / 32 GiB RAM** is a safer baseline. The README's
recommendation of 8 GiB for CPU inference with `llama3.2:3b` is the floor, not
the production target.

Disk needs are driven by retention. Start with **10-20 GiB** for the Postgres
volume, then grow it with your note history and semantic-search data. Size the
audio blob volume from recording volume and how long you keep recordings; for a
small team that retains audio, **50-100 GiB** is a reasonable first allocation.

If you use [`docker-compose.gpu.yml`](../docker-compose.gpu.yml) or the GPU
section in the README, GPU acceleration shifts most of the CPU and RAM burden
away from Ollama and Whisper, so the sizing math changes accordingly.

---

## Backups

For backup and restore, see [`docs/BACKUP.md`](./BACKUP.md). It covers the
Postgres database and the audio blob volume, and you need both pieces for a
restorable snapshot.

---

## Upgrades

For rolling a deployment forward, see [`docs/UPGRADING.md`](./UPGRADING.md).
It covers pulling or rebuilding images, restarting the stack, and the fact that
DB migrations run automatically on server boot.

---

## Production secrets checklist

The `docker-compose.yml` ships dev-only defaults so that `docker compose up`
works with no configuration. **Every item below must be replaced before going
live.** Copy `.env.example` to `.env` and fill in real values:

```bash
cp .env.example .env
$EDITOR .env
```

- [ ] **`MUESLI_MASTER_KEY`** — encrypts plugin secrets at rest. Generate with:
  ```bash
  openssl rand -base64 32
  ```
- [ ] **`MUESLI_STORAGE_SIGNING_KEY`** — signs presigned upload/download URLs.
      Must remain stable across restarts and replicas (changing it invalidates all
      previously issued URLs). Generate with:
  ```bash
  openssl rand -base64 32
  ```
- [ ] **`WHISPER_TOKEN`** — shared bearer token between the server and the
      Whisper transcription plugin. Replace the `dev-whisper-token` default. Generate with:
  ```bash
  openssl rand -hex 32
  ```
- [ ] **`AGENT_TOKEN`** — shared bearer token between the server and the Ollama
      agent plugin. Replace the `dev-agent-token` default. Generate with:
  ```bash
  openssl rand -hex 32
  ```
- [ ] **`POSTGRES_PASSWORD`** — database password. Replace the `postgres`
      default with a strong random password. Generate with:
  ```bash
  openssl rand -hex 32
  ```
- [ ] **`MUESLI_PUBLIC_URL`** — set to your public HTTPS URL as described
      above; the default `http://localhost:8080` is never correct for production.

> **Note:** the dev defaults live in `docker-compose.yml` as `${VAR:-default}`
> fallbacks. Once you have a `.env` file with real values, those fallbacks are
> ignored. Never commit your `.env` file to version control.

See [`SECURITY.md`](../SECURITY.md) for the full security policy, including
vulnerability reporting and the operator responsibility boundary.

---

## Production smoke checklist

- [ ] Open `/admin`, create a new account, and sign in.
- [ ] Record a short meeting in the desktop client, or upload an audio file, and confirm a new note appears.
- [ ] Wait for the note transcript panel to populate.
- [ ] Wait for the summary section to populate.
- [ ] Confirm the note status badge changes to `ready`.
- [ ] Restart the stack with `docker compose -f docker-compose.prod.yml restart` or `sudo systemctl restart muesli.service`.
- [ ] Reopen the same note and confirm the transcript, summary, and audio still load after the restart.
