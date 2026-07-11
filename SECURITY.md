# Security Policy

Muesli handles meeting audio and notes — inherently sensitive data — so we take
security seriously. Thank you for helping keep it and its users safe.

## Reporting a vulnerability

**Please do not report security vulnerabilities through public GitHub issues,
discussions, or pull requests.**

Instead, report privately using **GitHub's private vulnerability reporting**:

1. Go to the repository's **Security** tab.
2. Click **Report a vulnerability**.
3. Describe the issue, the impact, and steps to reproduce.

If private reporting is unavailable to you, contact a maintainer directly rather
than disclosing publicly.

Please include, where possible:

- The type of issue (e.g. auth bypass, SSRF via plugin URL, signed-URL forgery,
  injection, secret exposure).
- The affected component (server, a plugin, desktop client, admin UI) and
  version/commit.
- Step-by-step reproduction and any proof-of-concept.
- The impact and how an attacker might exploit it.

We will acknowledge your report, keep you updated on remediation, and credit you
in the release notes if you'd like (and unless you prefer to stay anonymous).
Please give us a reasonable window to fix the issue before any public disclosure.

## Scope

In scope: the Muesli server, the reference plugins (`plugins/`), the desktop
client, the admin UI, and the deployment artifacts (`Dockerfile`,
`docker-compose.yml`).

The plugin trust boundary matters: the server makes outbound HTTP calls to
configured plugin URLs and passes them presigned audio URLs. Issues such as
server-side request forgery via plugin configuration, presigned-URL forgery or
path traversal, plugin-secret exposure, or auth/session weaknesses are all
in scope and of high interest.

## Operator responsibilities (not vulnerabilities)

Some things are the operator's responsibility, not bugs in Muesli:

- **The shipped Docker defaults are DEV-ONLY.** `docker-compose.yml` ships
  default secrets (master key, storage signing key, plugin tokens, Postgres
  password) so `docker compose up` just works. **Never run these in production.**
  Generate real values (`openssl rand -base64 32` for `MUESLI_MASTER_KEY` and
  `MUESLI_STORAGE_SIGNING_KEY`) before any real deployment. See
  [`.env.example`](.env.example).
- **Run behind TLS.** Muesli speaks plain HTTP; terminate TLS at a reverse proxy
  in any internet-facing deployment. The desktop client **blocks plain-HTTP
  connections to a non-loopback server** (your audio, notes, and password would be
  sent in the clear) — connect over `https://`, or override per-connection in the
  connect dialog, or globally for local development with `MUESLI_ALLOW_INSECURE=1`.
- **Choose trustworthy plugins.** Plugins receive audio and transcript content.
  The privacy guarantee depends on running plugins you trust on infrastructure
  you control (the defaults run on your self-hosted server for exactly this reason).

## Supported versions

Muesli is pre-1.0 and under active development. Security fixes are applied to
`main`; until a stable release line exists, please run a recent `main`.
