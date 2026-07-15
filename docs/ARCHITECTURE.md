# Muesli Architecture

This document orients **contributors** to how Muesli is built: the moving parts,
how a recording becomes a note, and where each responsibility lives in the tree.
It is the practical companion to the design spec,
which captures the _why_ behind these decisions. When the two disagree, the code
is the source of truth — please send a PR to fix the doc.

## The privacy thesis

Muesli is a self-hosted alternative to cloud meeting-notes apps. The defining
constraint: **meeting content never has to leave infrastructure you control.**
Transcription and summarization run as plugins you host — by default a self-hosted
Whisper transcriber and a self-hosted Ollama LLM — so audio and notes are not
shipped to a third-party SaaS. Everything below serves that constraint.

## Terminology: _local_ vs _self-hosted_

Two words that are easy to conflate; Muesli uses them precisely:

- **local** — the **end-user's device**: the machine running the Electron desktop
  app (and, later, a mobile app or a browser reaching a web client).
- **self-hosted** — components an **operator runs** on a server, NAS, or PaaS VM,
  whether on a LAN or internet-facing.

The privacy guarantee is about **self-hosted**, not _local_. Meeting audio leaves
the local device and crosses the network to your **self-hosted** server, where a
plugin you control transcribes it (a self-hosted Whisper by default) — it is never
sent to a third-party SaaS unless an operator explicitly configures one. Because a
self-hosted server can be internet-facing, terminate TLS in front of it (see
`SECURITY.md`) so audio and credentials are encrypted in transit.

## Components at a glance

```
┌────────────────────┐         ┌──────────────────────────────────────────┐
│  Electron desktop  │  HTTPS  │              Muesli server (Go)            │
│  client (capture,  │ ──────▶ │  chi router · pgx/Postgres · worker pool   │
│  upload, view)     │         │  embedded admin SPA at /admin              │
└────────────────────┘         └───────────────┬──────────────────────────┘
                                                │ HTTP/JSON plugin contract
                          ┌─────────────────────┼─────────────────────┐
                          ▼                                           ▼
                ┌───────────────────┐                     ┌───────────────────┐
                │ Transcriber plugin │                     │   Agent plugin     │
                │ (Whisper, FastAPI) │                     │ (Ollama, FastAPI)  │
                └───────────────────┘                     └───────────────────┘
```

Four independently deployable pieces, talking over HTTP:

1. **Server** (Go) — the hub. Owns identity, storage, the note/job data model,
   the processing pipeline, and the admin UI. The only stateful component
   (Postgres + a storage volume).
2. **Desktop client** (Electron/TypeScript) — captures a single mixed mic+system
   audio track, uploads it via a presigned URL, and views the resulting note.
3. **Transcriber plugin** — audio → transcript segments.
4. **Agent plugin** — transcript + sparse user notes → a structured Markdown summary.

Plugins are **language-agnostic HTTP services**. The two shipped reference
plugins are Python/FastAPI, but anything that speaks the contract qualifies.

## The pipeline (how a recording becomes a note)

```
client uploads audio ──▶ note row created, audio stored
        │
        ▼
  enqueue "transcribe" job ──▶ worker leases job ──▶ POST /transcribe to plugin
        │                                                    │
        │                                          transcript segments stored
        ▼
  enqueue "summarize" job ──▶ worker leases job ──▶ POST /generate to agent plugin
        │                                                    │
        │                                            summary sections stored
        ▼
     note state = ready ──▶ (if embeddings enabled) enqueue "embed" job ──▶
                            embed title+summary+transcript via the embeddings
                            model, store the vector for semantic search
```

- **Batch, not streaming** (v1). The user records a whole meeting, then uploads.
- The queue is a **Postgres table**, leased with `SELECT … FOR UPDATE SKIP LOCKED`.
  No Redis/Kafka dependency until scale demands it (see [ROADMAP.md](../ROADMAP.md)).
- A **worker pool** of goroutines leases jobs, calls the relevant plugin over
  HTTP, persists results, and advances the note's state. Job types: `transcribe`,
  `summarize` (one per template — built-ins + the owner's), and `embed`.
- Failures are recorded on the job and the note enters an error state rather than
  hanging; jobs are retried within bounds.
- **Background jobs** (no client request): a trash auto-purge sweep (permanently
  removes notes, folders, and smart lists trashed > 30 days, every 6h) and a startup embeddings
  backfill (embeds existing `ready` notes that lack a vector).
- **Soft delete (recycle bin):** deleting a note, a folder, or a smart list sets `deleted_at`
  rather than removing the row; normal queries exclude trashed rows, and a folder
  delete trashes its whole subtree (children + memberships preserved). Restore
  clears `deleted_at`; "delete forever" / the 30-day sweep hard-delete (cascade).

## Calendar

The v3 calendar subsystem is the server-side sync layer plus the client-side
meeting detector. It owns the `calendar_sources` / `calendar_events` tables,
normalizes upstream events into one shape, and links calendar events to notes.

- Supported sources are `ics`, `caldav`, `google`, and `microsoft`.
  - `internal/calendar/ics.go` fetches a plain ICS URL and parses VEVENTs.
  - `internal/calendar/caldav.go` does CalDAV discovery/query with basic auth.
  - `internal/calendar/google.go` uses the Calendar API with a refresh token;
    it always includes `primary` and appends selected calendars from
    `selected_calendars`.
  - `internal/calendar/microsoft.go` uses Graph `calendarView` on `me`; the
    fetcher currently targets the primary calendar only.
- `internal/worker/calendarsync.go` runs a scheduler immediately and every
  `15m`. Each `SyncSource` call opens sealed source credentials, fetches a
  `[now-24h, now+14d]` window, upserts rows, prunes missing `external_id`s, and
  records the source status.
- `internal/calendar/normalize.go` defines the shared `NormalizedEvent` shape.
  The source adapters fill that shape, and `internal/calendar/conferencing.go`
  extracts the first Zoom / Google Meet / Teams URL from native text, location,
  then description. `internal/calendar/diff.go` turns fetched events into the
  prune keep-set.
- `calendar_sources` stores the source row: `owner_id`, `kind`, `display_name`,
  sealed `credentials`, `selected_calendars` JSONB, sync `status`, and
  `last_synced_at`. `calendar_events` stores normalized events with
  `owner_id`, `source_id`, `external_id`, timings, text fields, attendees JSONB,
  and `conferencing_url`; `(source_id, external_id)` is unique and the events
  are indexed by `(owner_id, starts_at)`.
- The store layer mirrors those tables with `model.CalendarSource` and
  `model.CalendarEvent` in `internal/model/calendar.go`. `internal/store/calendar.go`
  handles JSONB encode/decode, owner-scoped source listings, time-windowed event
  reads, and source event upserts/pruning.
- Note-to-event linking is one `notes.event_id` FK. `internal/store/notes.go`
  verifies the event belongs to the same owner before setting the link, and the
  `20260709205704_note_event_id` migration adds the column with
  `ON DELETE SET NULL`.
- Client-side meeting detection lives in `src/renderer/lib/meetingDetect.ts`,
  `src/renderer/lib/meetingDetectionLoop.ts`, and
  `src/renderer/hooks/useMeetingDetectionLoop.ts`. The renderer polls every
  `45s` and on focus, fetches events from `now-2h` to `now+30m`, picks the active
  event with a conferencing URL whose start time is the latest among overlaps,
  and either prompts or auto-records. The opt-in auto-record flag is persisted
  in `src/renderer/lib/calendarPrefs.ts` under
  `muesli.calendar.autoRecordDetectedMeetings`.

## Server package map (`internal/`)

Each package has one clear responsibility. Files that change together live together.

| Package                   | Responsibility                                                                                                                                                                                                                                |
| ------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `config`                  | Load + validate configuration from environment (`MUESLI_*`). Env vars include `MUESLI_ALLOWED_ORIGINS` (comma-separated CORS origin allow-list; empty = deny all cross-origin, the secure default).                                           |
| `crypto`                  | AES-256-GCM seal/open for plugin secret config; key derived from `MUESLI_MASTER_KEY`.                                                                                                                                                         |
| `db`                      | pgx pool connection + embedded SQL migrations (golang-migrate).                                                                                                                                                                               |
| `db/migrations`           | Versioned schema (`*.up.sql` / `*.down.sql`), embedded via `go:embed`.                                                                                                                                                                        |
| `model`                   | Domain types (note, job, transcript, summary, plugin, user) and enums.                                                                                                                                                                        |
| `store`                   | Data-access layer over Postgres: notes, jobs, transcripts, summaries, plugins, users, templates, tags, smart lists, folders, note embeddings — incl. soft-delete/trash and cosine vector search. The only place SQL lives outside migrations. |
| `auth`                    | argon2id password hashing, session/bearer tokens, identity.                                                                                                                                                                                   |
| `storage`                 | `StorageProvider` abstraction; local-filesystem impl with HMAC-signed presigned upload/download URLs.                                                                                                                                         |
| `plugin`                  | Typed HTTP client for the plugin contract (`/info`, `/health`, `/transcribe`, `/generate`) with auth headers.                                                                                                                                 |
| `embed`                   | Optional text-embedding client (Ollama `/api/embeddings`) for semantic search; `New` returns nil when unconfigured (the feature is config-gated and degrades to lexical-only).                                                                |
| `worker`                  | Leased job-queue worker pool: the processor (transcribe → summarize → embed), the pool that runs it, and the trash auto-purge sweep.                                                                                                          |
| `api`                     | chi router, HTTP handlers, middleware (auth, request scoping), wiring (`NewServer`).                                                                                                                                                          |
| `adminui`                 | `go:embed` of the built admin SPA (`dist/`) + handler that serves it at `/admin` with SPA fallback.                                                                                                                                           |
| `plugintest` / `testutil` | In-process fake plugin servers and DB test harness (truncation between tests).                                                                                                                                                                |

Entry point: `cmd/muesli/main.go` wires it all together —
config → crypto → migrate → connect → store → seed templates → auto-register
default plugins → storage → worker pool → HTTP server → ready banner.

## The plugin contract

The contract is the project's most important interface — it's what makes Muesli
pluggable and keeps audio off third-party SaaS. A plugin is any HTTP service
exposing:

| Endpoint           | Auth     | Purpose                                                                                                                                                                                                       |
| ------------------ | -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /info`        | bearer   | `{name, version, plugin_api: 1, kind, config_schema}` — identity + JSON-Schema for its config.                                                                                                                |
| `GET /health`      | **none** | Liveness probe.                                                                                                                                                                                               |
| `POST /transcribe` | bearer   | `{audio_url, language_hint?, options?, config}` → `{segments:[{start_ms,end_ms,text,source,speaker?}], language, model, duration_ms}`.                                                                        |
| `POST /generate`   | bearer   | `{transcript:[seg], notes_markdown, template:{sections:[{heading,instruction}]}, options?, config, system_prompt?, model?, temperature?}` → `{summary:{sections:[{heading,content_markdown,refs?}]}, model}`. |

- **Auth:** `Authorization: Bearer <token>` plus `X-Muesli-Plugin-API: 1`.
- **`refs`** are **0-based positional indices into the transcript array** (`[]int`),
  not segment IDs — used to cite which transcript spans support a summary point.
- **`system_prompt`, `model`, `temperature`** on `/generate` are optional
  per-template agent overrides (set from the resolved `model.Template`, see
  `internal/store/templates.go`). Absent/empty/nil means "unset" — a
  conformant agent falls back to its own default system prompt / plugin
  `config` values, so plugins that predate this field can ignore it safely.
- A plugin's `config_schema` (JSON Schema) drives the admin UI's config form;
  fields marked `writeOnly` / `format:"password"` are treated as secrets and
  sealed with AES-GCM before storage.

A conformance suite (`plugins/conformance/`) validates any plugin against this
contract. **Run it against any new plugin.**

## Other source trees

| Path                                  | What it is                                                                                            |
| ------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| `cmd/muesli/`                         | Server entry point and startup banner.                                                                |
| `cmd/ollama-agent/`                   | Go plugin binary spawned in `--embedded` mode as the desktop default local agent (see below).         |
| `cmd/whisper-cpp-transcriber/`        | Go plugin binary spawned in `--embedded` mode as the desktop default transcriber (see below).         |
| `plugins/whisper-transcriber/`        | Reference transcriber (FastAPI + faster-whisper); optional swap for the embedded whisper.cpp default. |
| `plugins/parakeet-transcriber/`       | GPU-oriented transcriber (FastAPI + NeMo / Parakeet).                                                 |
| `plugins/ollama-agent/`               | Reference agent (FastAPI + Ollama; self-hosted default, BYO-cloud opt-in).                            |
| `plugins/conformance/`                | CLI + library that checks a plugin against the contract.                                              |
| `src/{main,preload,renderer,shared}/` | Electron desktop client (TypeScript/React). See "Desktop client" below.                               |
| `web/admin/`                          | Admin SPA (Vite + React + TS). Built into `internal/adminui/dist/` and embedded in the Go binary.     |

### `--embedded` mode default plugins

When the server starts with `--embedded` (the desktop path), it spawns two
bundled Go plugin binaries as subprocesses and auto-registers them as the
default plugins for their kind (`internal/embedded/agent.go`,
`internal/embedded/whisper.go`, wired in `cmd/muesli/main.go`):

- **Agent:** `cmd/ollama-agent` is spawned and registered as the default
  agent plugin when a local Ollama install is detected.
- **Transcriber:** `cmd/whisper-cpp-transcriber` is always spawned and
  registered as the default transcriber plugin - it is the desktop default,
  no detection gate needed. The Python `plugins/whisper-transcriber` plugin
  remains available as an optional swap; see [`docs/PLUGINS.md`](PLUGINS.md#7-registering-your-plugin)
  for the `MUESLI_DEFAULT_TRANSCRIBER_URL`/`MUESLI_DEFAULT_TRANSCRIBER_TOKEN`
  override mechanism.

## Desktop client (`src/`)

The Electron client is split across processes with a strict trust boundary:

- `src/main/` — privileged main process. `MuesliClient` (typed server API client),
  the upload machine, `RecordingSession`, and `TokenStore` (OS-keychain via
  `safeStorage`). All network + secret handling lives here.
- `src/preload/` — `contextBridge` exposing a typed `window.muesli` bridge; the
  renderer never touches Node or the network directly. The renderer runs sandboxed
  (`contextIsolation: true`, `nodeIntegration: false`, `sandbox: true`).
- `src/shared/` — types + the single source of truth for IPC channel names
  (`ipc.ts`: `MuesliBridge`).
- `src/renderer/` — the React UI (v2 redesign):
  - `styles/` — design tokens (CSS variables, light + `.dark`) consumed by Tailwind v4.
  - `components/ui/` — owned shadcn-style primitives on Radix.
  - `components/shell/` — `AppLayout` + `Sidebar` (the app shell). The **sidebar is
    navigation-only** (New meeting, search, All notes / Lists / Tags / Suggested); the
    **main pane** is the meeting list (`NotesListScreen`), filtered by the active view,
    with a note-detail route layered on top. `AppLayout` owns the `activeView` state and
    feeds the filtered notes + a heading down through the router outlet.
  - `components/` — screens (Connect, NotesList, Note, NewMeeting, Settings) and
    feature components (RecordControl, NoteEditor, NoteView, TagBar, RuleEditor, etc.).
    The notes list is a **date-grouped feed** (`lib/datetime` buckets by `created_at`;
    rows carry a deterministic `lib/monogram` tile + a time); note titles and page
    headings use a serif display face (`--font-serif`). `NoteView` shows **Enhanced /
    My notes** with the transcript in a toggled side drawer (enhanced summary primary).
    Note-level tags surface as a sidebar Tags section (single-select filter) and a
    per-note TagBar; the tag index is derived client-side from the loaded notes.
    **Smart lists** are saved boolean rules (AND/OR over tag/title/status/created/folder,
    stored as JSONB, owner-scoped) shown as a sidebar Lists section with a live count
    and single-select filtering; the `AppLayout` `activeView` union (all / tag / list)
    drives the filter (mutually exclusive, with text search composing on top), the main
    pane's heading reflects the active view, rules evaluate client-side, and a Suggested
    section offers one-click lists from normalized-title recurring-meeting clusters.
    **Folders** are manual many-to-many collections (`0006_folders` + a `note_folders`
    join; notes carry `folder_ids`): a sidebar Folders section (live count, single-select
    filter via the `activeView` `folder` case), a per-note FolderBar, **nesting**
    (`parent_id`, tree with collapse), drag-a-feed-row-onto-a-folder filing (`text/note-id`),
    and drag-a-folder-onto-a-folder **re-parenting** (`text/folder-id`, cycle/depth guarded).
  - **Beyond the v2 base:** a **collapsible + resizable sidebar** (`lib/sidebarPrefs`,
    persisted to `localStorage`, ⌘\\); a **⌘K command palette** (jump to notes/folders/
    lists/tags + actions); a **Trash** screen (the recycle bin — restore / delete-forever for
    soft-deleted notes, folders, _and_ smart lists); **semantic search** — the sidebar search box debounces
    a `GET /api/search` call and _additively_ surfaces meaning-matched notes alongside the
    instant lexical filter (`AppLayout`); per-note **template switcher**, **re-run summary**,
    Markdown **copy/export**, and a transcript search drawer.
  - **Connect / transport:** the Connect screen enforces an **HTTPS guardrail** — it refuses
    a plain-`http://` connection to a non-loopback server (audio + credentials would be sent
    in the clear) unless the user opts in or `MUESLI_ALLOW_INSECURE=1` is set (`shared/url.ts`).
  - `lib/` — `cn`, `debounce`, `pollNote`, `status`, `smartList` (rule matcher),
    `recurring` (title-cluster suggestions), `sidebarPrefs`, `folders` (subtree helpers),
    `datetime`, `noteMarkdown` helpers.

**Routing** is a react-router _memory_ router (no URL bar). **Capture** records a
single mixed mic+system track in the renderer, transferred to main as an
`ArrayBuffer` for the presigned upload. **UI verification** uses a Chrome DevTools Protocol visual check (`npm run dev:debug`).

## Tech choices, briefly

- **Go server:** single static binary, cheap to run, container-native, strong
  concurrency for the worker pool. Easy to self-host on a NAS or scale on cloud.
- **Postgres:** one datastore for relational data _and_ the job queue — fewer
  moving parts to self-host.
- **HTTP/JSON plugins (not gRPC, not in-process):** any language, independently
  scalable, and the privacy boundary is explicit and inspectable.
- **Electron client:** cross-platform capture of mixed mic+system audio from one
  codebase. The v2 UI is Tailwind v4 + design tokens + owned shadcn/Radix
  primitives (no heavy component library), for a clean, themeable, accessible feel
  with full visual control.
- **Embedded admin SPA:** the operator UI ships _inside_ the server binary — one
  artifact to deploy, no separate web server.

## Local development

See [`CONTRIBUTING.md`](../CONTRIBUTING.md) for the full setup. The short version:

```bash
# Whole stack, one command (Postgres + Ollama + Whisper + agent + server):
docker compose up

# Or work on the Go server directly:
colima start && make test-db   # throwaway Postgres on :5433
make test                      # Go suite (serialized: -p 1)
make build                     # build admin SPA + embed + compile server

# Desktop client:
npm install
npm test                       # Vitest (renderer/main)
npm run dev                    # launch the client
npm run dev:debug              # launch with CDP on :9333 for the visual loop
```

## Where to go next

- **What's planned / deferred:** [`ROADMAP.md`](../ROADMAP.md)
- **How to contribute:** [`CONTRIBUTING.md`](../CONTRIBUTING.md)
- **Reporting a vulnerability:** [`SECURITY.md`](../SECURITY.md)
