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

## Prod vs dev compose

`docker-compose.yml` is the development stack: it builds `whisper`, `agent`,
`streaming-transcriber`, and `server` locally from source and includes dev-only
fallback secrets so iteration stays fast.

`docker-compose.prod.yml` uses the pinned GHCR images published by the build
workflow and documented in [`docs/PUBLISHED-IMAGES.md`](./PUBLISHED-IMAGES.md).
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
