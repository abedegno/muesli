# Configuration Reference

All Muesli server configuration is loaded from environment variables by
`internal/config/config.go`. Copy `.env.example` to `.env` and override
what you need; Docker Compose picks it up automatically.

## Core / Secrets

| Variable              | Type          | Default                          | Required? | Description                                                                                                                                                                  |
| --------------------- | ------------- | -------------------------------- | --------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `DATABASE_URL`        | string        | —                                | **Yes**   | PostgreSQL connection URL, e.g. `postgres://user:pass@host/db?sslmode=disable`.                                                                                              |
| `MUESLI_ADDR`         | string        | `:8080`                          | No        | TCP address the HTTP server listens on.                                                                                                                                      |
| `MUESLI_MASTER_KEY`   | base64 string | —                                | **Yes**   | 32-byte key (base64-encoded) used to encrypt plugin secrets at rest. Generate: `openssl rand -base64 32`.                                                                    |
| `MUESLI_PUBLIC_URL`   | string        | `http://localhost:8080`          | No        | Public base URL advertised to clients in presigned upload/download URLs and the desktop app. Set to your public hostname in production (e.g. `https://muesli.example.com`).  |
| `MUESLI_INTERNAL_URL` | string        | _(value of `MUESLI_PUBLIC_URL`)_ | No        | Base URL plugins use to reach the server over the internal network. Defaults to `MUESLI_PUBLIC_URL`; override in multi-host compose deployments (e.g. `http://server:8080`). |

## Storage

| Variable                     | Type   | Default        | Required? | Description                                                                                                                                                                                         |
| ---------------------------- | ------ | -------------- | --------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `MUESLI_STORAGE_DIR`         | string | `./data/audio` | No        | Directory where uploaded audio files are stored on disk.                                                                                                                                            |
| `MUESLI_STORAGE_SIGNING_KEY` | string | —              | **Yes**   | HMAC signing key for presigned upload/download URLs. Must be stable across restarts and replicas (a change invalidates in-flight URLs). At least 16 bytes; generate with `openssl rand -base64 32`. |
| `MUESLI_AUDIO_RETENTION`     | enum   | `keep`         | No        | What to do with raw audio after transcription completes. `keep` retains the file; `discard` deletes it immediately. Any other value is rejected at startup.                                         |

## Embeddings / Semantic Search

Semantic search is powered by a self-hosted Ollama embeddings endpoint. Leave
`MUESLI_EMBEDDINGS_URL` unset (or empty) to disable semantic search entirely;
search falls back to lexical-only mode.

| Variable                           | Type   | Default              | Required? | Description                                                                                                                                                          |
| ---------------------------------- | ------ | -------------------- | --------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `MUESLI_EMBEDDINGS_URL`            | string | _(empty — disabled)_ | No        | Base URL of the Ollama embeddings endpoint (e.g. `http://ollama:11434`). Empty disables semantic search.                                                             |
| `MUESLI_EMBEDDINGS_MODEL`          | string | `nomic-embed-text`   | No        | Ollama embedding model name. The vector column is unsized, so you can switch models without a migration; run `muesli reembed` after switching to re-embed all notes. |
| `MUESLI_EMBEDDINGS_DIM`            | int    | `768`                | No        | Embedding dimension. Must match the model's actual output dimension. Changing this triggers a full re-backfill. Invalid values (≤ 0) fall back to 768.               |
| `MUESLI_EMBEDDINGS_MIN_SCORE`      | float  | `0.6`                | No        | Cosine-similarity cutoff (0–1) for a semantic hit. Higher = stricter (fewer, more-relevant results). Tuned for prefixed `nomic-embed-text`.                          |
| `MUESLI_EMBEDDINGS_DOC_PREFIX`     | string | `search_document: `  | No        | Task prefix prepended to note text before embedding (asymmetric models like `nomic-embed-text` expect this). Leave at default for prefix-less models.                |
| `MUESLI_EMBEDDINGS_QUERY_PREFIX`   | string | `search_query: `     | No        | Task prefix prepended to search queries before embedding.                                                                                                            |
| `MUESLI_EMBED_BACKFILL_BATCH_SIZE` | int    | `500`                | No        | Maximum number of notes to backfill embeddings for on startup. Must be > 0; invalid values fall back to 500.                                                         |

## Calendar integration

Calendar integration is split between per-user calendar sources and server-side
OAuth app credentials. CalDAV and ICS sources are configured in the UI by each
user and do not need any Muesli server environment variables. Leave the OAuth
variables below unset to hide the Google/Microsoft Connect buttons in the UI;
Google/Microsoft calendar sync then degrades cleanly, while CalDAV/ICS keeps
working. For the provider setup steps, see
[Calendar OAuth setup](CALENDAR_OAUTH_SETUP.md).

| Variable                               | Type   | Default   | Required? | Description                                                                                                                                            |
| -------------------------------------- | ------ | --------- | --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `MUESLI_GOOGLE_OAUTH_CLIENT_ID`        | string | _(empty)_ | No        | Google OAuth client ID. Set together with the client secret and redirect URL to enable the Google Connect button and Google calendar OAuth flow.       |
| `MUESLI_GOOGLE_OAUTH_CLIENT_SECRET`    | string | _(empty)_ | No        | Google OAuth client secret. Set together with the client ID and redirect URL to enable Google calendar OAuth.                                          |
| `MUESLI_GOOGLE_OAUTH_REDIRECT_URL`     | string | _(empty)_ | No        | Google OAuth redirect URL for the `/api/calendar/oauth/google/callback` handler. Set together with the client ID and secret to enable Google calendar. |
| `MUESLI_MICROSOFT_OAUTH_CLIENT_ID`     | string | _(empty)_ | No        | Microsoft OAuth client ID. Set together with the client secret and redirect URL to enable the Microsoft Connect button and Microsoft calendar OAuth.   |
| `MUESLI_MICROSOFT_OAUTH_CLIENT_SECRET` | string | _(empty)_ | No        | Microsoft OAuth client secret. Set together with the client ID and redirect URL to enable Microsoft calendar OAuth.                                    |
| `MUESLI_MICROSOFT_OAUTH_REDIRECT_URL`  | string | _(empty)_ | No        | Microsoft OAuth redirect URL for the `/api/calendar/oauth/microsoft/callback` handler. Set together with the client ID and secret to enable Microsoft. |

## Default Plugins

Muesli calls transcriber and agent plugins over HTTP. You can register plugins
via the Admin UI, or auto-register defaults at startup by setting the variables
below. A kind is auto-registered only when **both** its URL **and** token are
set.

### Transcriber (Whisper)

| Variable                            | Type        | Default  | Required? | Description                                                                                             |
| ----------------------------------- | ----------- | -------- | --------- | ------------------------------------------------------------------------------------------------------- |
| `MUESLI_DEFAULT_TRANSCRIBER_URL`    | string      | _(none)_ | No        | HTTP base URL of the default transcriber plugin (e.g. `http://whisper:8000`).                           |
| `MUESLI_DEFAULT_TRANSCRIBER_TOKEN`  | string      | _(none)_ | No        | Bearer token the server sends to the transcriber plugin. Must match the plugin's `MUESLI_PLUGIN_TOKEN`. |
| `MUESLI_DEFAULT_TRANSCRIBER_CONFIG` | JSON string | `{}`     | No        | Plugin-specific config JSON passed at registration (e.g. `{"model":"base"}`).                           |

### Streaming transcriber

| Variable                                   | Type        | Default  | Required? | Description                                                                                               |
| ------------------------------------------ | ----------- | -------- | --------- | --------------------------------------------------------------------------------------------------------- |
| `MUESLI_DEFAULT_STREAMING_TRANSCRIBER_URL`  | string      | _(none)_ | No        | HTTP base URL of the default streaming transcriber plugin (e.g. `http://streaming-transcriber:8000`).     |
| `MUESLI_DEFAULT_STREAMING_TRANSCRIBER_TOKEN` | string      | _(none)_ | No        | Bearer token the server sends to the streaming transcriber plugin. Must match `MUESLI_PLUGIN_TOKEN`.      |
| `MUESLI_DEFAULT_STREAMING_TRANSCRIBER_CONFIG` | JSON string | `{}`     | No        | Plugin-specific config JSON passed at registration (e.g. `{"model":"tiny.en"}`).                         |

### Agent (Ollama)

| Variable                      | Type        | Default  | Required? | Description                                                                                                             |
| ----------------------------- | ----------- | -------- | --------- | ----------------------------------------------------------------------------------------------------------------------- |
| `MUESLI_DEFAULT_AGENT_URL`    | string      | _(none)_ | No        | HTTP base URL of the default agent plugin (e.g. `http://agent:8000`).                                                   |
| `MUESLI_DEFAULT_AGENT_TOKEN`  | string      | _(none)_ | No        | Bearer token the server sends to the agent plugin. Must match the plugin's `MUESLI_PLUGIN_TOKEN`.                       |
| `MUESLI_DEFAULT_AGENT_CONFIG` | JSON string | `{}`     | No        | Plugin-specific config JSON passed at registration (e.g. `{"ollama_url":"http://ollama:11434","model":"llama3.2:3b"}`). |

## Plugin-side

These variables are set on the **plugin services** (not the Muesli server).

| Variable              | Type   | Default               | Required?            | Description                                                                                                                                                                                                       |
| --------------------- | ------ | --------------------- | -------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `MUESLI_PLUGIN_TOKEN` | string | —                     | **Yes** (on plugins) | Bearer token each plugin uses to authenticate incoming requests from the Muesli server. Must match the corresponding `MUESLI_DEFAULT_*_TOKEN` on the server.                                                      |
| `WHISPER_MODEL`       | string | `base`                | No                   | Whisper model size loaded by the whisper-transcriber plugin (e.g. `base`, `small`, `medium`, `large`). Set on the **whisper** service.                                                                            |
| `OLLAMA_URL`          | string | `http://ollama:11434` | No                   | Base URL of the Ollama server the ollama-agent plugin uses for inference. Set on the **agent** service.                                                                                                           |
| `OLLAMA_MODEL`        | string | `llama3.2:3b`         | No                   | Ollama model name the ollama-agent plugin sends generation requests to. Set on the **agent** service.                                                                                                             |
| `LLM_BASE_URL`        | string | _(empty)_             | No                   | OpenAI-compatible base URL for the ollama-agent plugin. When set, the agent routes requests to `{LLM_BASE_URL}/chat/completions` instead of local Ollama. Empty = use local Ollama. Set on the **agent** service. |
| `LLM_API_KEY`         | string | _(empty)_             | No                   | API key sent as `Authorization: Bearer <key>` to the provider set by `LLM_BASE_URL`. Empty = no auth header. Set on the **agent** service.                                                                        |

## Retention

| Variable                      | Type | Default | Required? | Description                                                                                                                                            |
| ----------------------------- | ---- | ------- | --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `MUESLI_TRASH_RETENTION_DAYS` | int  | `30`    | No        | Number of days soft-deleted notes, folders, and smart-lists are kept before permanent purge. Must be a positive integer; invalid values default to 30. |

## Docker Compose Shortcuts

These variables are convenience wrappers used by `docker-compose.yml` only.
They are **not** read directly by the Muesli server or plugins — Compose
expands them into the real variables above.

| Variable            | What it expands to                                                | Default             | Description                                                                                                     |
| ------------------- | ----------------------------------------------------------------- | ------------------- | --------------------------------------------------------------------------------------------------------------- |
| `POSTGRES_PASSWORD` | `POSTGRES_DB` password / `DATABASE_URL`                           | `postgres`          | Postgres superuser password. Compose injects it into both the postgres service and the server’s `DATABASE_URL`. |
| `WHISPER_TOKEN`     | `MUESLI_DEFAULT_TRANSCRIBER_TOKEN` + plugin `MUESLI_PLUGIN_TOKEN` | `dev-whisper-token` | Shared bearer token wired by Compose to both the server’s transcriber-token setting and the whisper plugin.     |
| `STREAMING_TOKEN`   | `MUESLI_DEFAULT_STREAMING_TRANSCRIBER_TOKEN` + plugin `MUESLI_PLUGIN_TOKEN` | `dev-streaming-token` | Shared bearer token wired by Compose to both the server’s streaming-transcriber setting and the streaming plugin. |
| `AGENT_TOKEN`       | `MUESLI_DEFAULT_AGENT_TOKEN` + plugin `MUESLI_PLUGIN_TOKEN`       | `dev-agent-token`   | Shared bearer token wired by Compose to both the server’s agent-token setting and the ollama-agent plugin.      |
