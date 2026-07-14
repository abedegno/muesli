# Troubleshooting

Common issues and how to resolve them when running Muesli locally.

---

## 1. Slow first boot

When you run `docker compose up` for the first time the `ollama-pull` service
downloads the default LLM (`llama3.2:3b`, approximately 2 GB). This is
expected — there is nothing wrong with your setup.

Whisper also fetches its model on the **very first transcription** (see
[section 4](#4-whisper-model-download-on-first-transcription) below for
details).

**Expect several minutes** before the full stack is ready and accepting
requests. Subsequent starts are much faster because both models are cached in
Docker volumes.

---

## 2. CPU-inference slowness

The default configuration runs entirely on the CPU. This is intentional for a
local-first / dev setup — no GPU is required to get started.

Practical expectations:

- **Short meetings (< 10 min):** transcription and summarization typically
  finish within a minute or two.
- **Longer meetings:** may take several minutes to transcribe and summarize.
  Let the pipeline run; it will complete.

If you need faster inference, GPU acceleration is available. Each plugin has its
own Dockerfile that can be adapted for CUDA or Metal. See the `Dockerfile` and
compose overrides inside the `plugins/` directory (e.g.
`plugins/whisper-transcriber/`, `plugins/parakeet-transcriber/`, and
`plugins/ollama-agent/`) for the relevant build args and base images.

---

## 3. Port conflicts on 8080

### What it looks like

- Docker reports `address already in use` when starting the `server` container.
- The admin UI at `http://localhost:8080/admin` is unreachable even after the
  stack reports it is running.

### Find what is using the port

```bash
# macOS / Linux (lsof)
lsof -i :8080

# Linux (ss)
ss -lptn 'sport = :8080'
```

### Remap the port

1. Create (or edit) a `.env` file in the repo root:

   ```bash
   cp .env.example .env   # if you haven't already
   ```

2. Set a different address in `.env`:

   ```dotenv
   MUESLI_ADDR=:9090
   ```

3. Update the port mapping in `docker-compose.yml` (or a
   `docker-compose.override.yml`) to match:

   ```yaml
   services:
     server:
       ports:
         - '9090:9090'
   ```

4. Re-run `docker compose up`. The admin UI will now be at
   `http://localhost:9090/admin`.

---

## 4. Whisper model download on first transcription

The `whisper-transcriber` plugin downloads its model (several hundred MB) on
the **first transcription call** — not at container start.

When this is happening:

- The note will remain in **`transcribing`** status until the download
  completes.
- The `whisper` container logs will show download progress.

This is normal. Once the model is cached in the Docker volume, subsequent
transcriptions start immediately.

---

## 5. "Stack not ready yet" vs truly broken

It can be hard to tell whether the stack is still initialising or has actually
failed. Use the table below as a guide.

| Symptom                                         | Likely cause              | Action                                                             |
| ----------------------------------------------- | ------------------------- | ------------------------------------------------------------------ |
| Admin UI unreachable, containers starting       | Still loading             | Wait; check `docker compose ps` every 30 s                         |
| `ollama-pull` container still running           | LLM download in progress  | Wait (can take several minutes on a slow connection)               |
| Note stuck in `transcribing` for < 5 min        | Whisper model downloading | Wait; watch `docker compose logs -f whisper`                       |
| A container shows `Exit` or `Restarting` state  | Actual failure            | Check `docker compose logs <service>` for errors                   |
| Admin UI returns an error after several minutes | Actual failure            | Check `docker compose logs server` for a Go panic or startup error |

**Quick check:**

```bash
docker compose ps
```

All services should eventually reach a running/healthy state. If any are stuck
in `Exit` or keep `Restarting`, read the logs (see section 6).

---

## 6. How to read the logs

### Stream all services

```bash
docker compose logs -f
```

### Stream a single service

```bash
docker compose logs -f server               # core server
docker compose logs -f ollama               # Ollama LLM runtime
docker compose logs -f ollama-pull          # model download (first boot)
docker compose logs -f whisper              # Whisper transcription plugin
docker compose logs -f agent                # LLM summarisation plugin
```

### What to look for

| Pattern                                            | Meaning                                                                    |
| -------------------------------------------------- | -------------------------------------------------------------------------- |
| `panic:` (Go)                                      | Fatal server error — copy the full stack trace when filing an issue        |
| `Traceback (most recent call last):` (Python)      | Fatal plugin error                                                         |
| `registered plugin` / `registered default plugins` | Server successfully registered its built-in plugins at startup — good sign |
| `listening on`                                     | Server is up and accepting connections                                     |
| `pulling …`                                        | Ollama is downloading the LLM model                                        |

The server logs a line for each plugin it registers at startup (e.g.
`registered default plugins transcriber=… agent=…`). If you do not see that
line, the server may have crashed earlier — scroll up in the logs to find the
error.

---

## 7. OAuth connect fails for Google or Microsoft Calendar

### What it looks like

- The calendar connect flow opens, then stops with a redirect error from the
  provider.
- The provider reports a redirect URI mismatch or an invalid redirect URL.
- The Connect action is unavailable because the server reports the provider as
  unconfigured.

### Why it happens

Muesli only enables the Google/Microsoft OAuth flow when all three values are
set for that provider:

| Provider  | Required env vars                                                                                                 |
| --------- | ----------------------------------------------------------------------------------------------------------------- |
| Google    | `MUESLI_GOOGLE_OAUTH_CLIENT_ID`, `MUESLI_GOOGLE_OAUTH_CLIENT_SECRET`, `MUESLI_GOOGLE_OAUTH_REDIRECT_URL`          |
| Microsoft | `MUESLI_MICROSOFT_OAUTH_CLIENT_ID`, `MUESLI_MICROSOFT_OAUTH_CLIENT_SECRET`, `MUESLI_MICROSOFT_OAUTH_REDIRECT_URL` |

The server checks those fields in `googleOAuthConfigured()` and
`microsoftOAuthConfigured()`. If any one is missing, the OAuth start/callback
handlers treat the provider as not configured.

The redirect URL must match exactly in both places:

- the value in Muesli's env/config
- the redirect URI registered with Google or Microsoft

For the current routes, the callbacks are:

- `.../api/calendar/oauth/google/callback`
- `.../api/calendar/oauth/microsoft/callback`

### How to fix it

1. Open the admin config view and verify the OAuth rows are present and marked
   as coming from the environment:

   - `GET /api/admin/config`
   - Admin UI: `Config`

2. Make sure the client ID, client secret, and redirect URL are all set for the
   provider you are using.

3. Update the provider registration so its redirect URI matches the exact URL
   configured in Muesli.

4. Restart the server after changing env vars, then retry the connect flow.

If you want a quick status check, the provider-specific status endpoints return
`{"configured":true}` only when the full config is present:

- `GET /api/calendar/oauth/google/status`
- `GET /api/calendar/oauth/microsoft/status`

---

## 8. CalDAV or ICS feed is not syncing

### What it looks like

- The calendar source stays in `error`.
- Sync logs mention `fetch ics`, `fetch caldav`, `query caldav calendar`, or an
  upstream `401`/`403`.
- No events appear after creating the source or running a manual refresh.

### Why it happens

The feed fetchers use the URL exactly as stored:

- `FetchICS` does a plain `GET` to the feed URL.
- `FetchCalDAV` connects to the provided base URL with basic auth, then tries
  CalDAV discovery and calendar queries.

That means the usual causes are:

- the feed URL is wrong
- the calendar credentials are wrong
- the upstream server returns a non-200 response

There is no separate calendar-specific localhost/dev exception in
`internal/calendar/caldav.go` or `internal/calendar/ics.go`; the code uses the
URL you configured.

### How to fix it

1. Re-check the source URL in the calendar source record.
2. For CalDAV, confirm both the username and password are correct.
3. Verify the upstream server actually serves the feed from that URL and
   returns a successful response to the Muesli server.
4. Run a manual refresh after correcting the URL or credentials:

   ```bash
   POST /api/calendar/refresh
   ```

---

## `muesli doctor`

When startup fails or a deployment looks half-configured, `muesli doctor` is
the fastest way to separate a bad config from a real runtime problem. It does
not start the server or worker pool.

The command prints one line per check:

```text
[PASS] database: DATABASE_URL reachable; pgvector extension present
[WARN] embeddings: not configured (disabled)
[PASS] audio dir: writable: ./data/audio
Summary: 6 PASS, 2 WARN, 0 FAIL
```

The checks cover:

- database reachability and the `vector` extension
- default plugin URLs for transcriber, streaming transcriber, and agent
- embeddings reachability when configured
- master key and storage signing key presence, plus dev-default secret checks
- writability of the audio and backup directories

Exit codes:

- `0` when every check is `PASS` or `WARN`
- `1` when any check is `FAIL`

Use the summary counts to see whether the problem is a missing config value,
an unreachable dependency, or just a disabled feature such as embeddings or a
backup directory.

If the upstream server is returning `401 Unauthorized` or `403 Forbidden`, fix
the credentials first. If it is returning another HTTP error, fix the URL or the
upstream service before retrying.

---

## 9. Calendar source status `auth_error`

### What it looks like

- A calendar source in `GET /api/calendar/sources` shows `status: "auth_error"`.
- The source stops updating until you reconnect it.

### What it means

`auth_error` is one of the allowed calendar source statuses in the database
schema alongside `ok` and `error`:

- `ok`
- `auth_error`
- `error`

The sync worker sets `auth_error` when it sees an authentication problem while
refreshing a source. That includes expired or revoked OAuth refresh tokens, plus
other auth-shaped failures such as `401`, `403`, `unauthorized`, `forbidden`,
`invalid_grant`, `invalid_client`, and `unauthorized_client`.

### How to fix it

Reconnect or re-authenticate the calendar source with the provider's OAuth
flow. In practice that means running the Google or Microsoft connect flow
again so Muesli stores a fresh refresh token.

At the API level, the reconnect path is the same OAuth start/callback flow used
when the source was first connected:

- Google: `/api/calendar/oauth/google/start`
- Microsoft: `/api/calendar/oauth/microsoft/start`

After the new token is stored, the newly connected source should sync normally
again. If you no longer need the stale `auth_error` source, delete it and add
it again after re-authentication.
