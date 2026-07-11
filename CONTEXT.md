# Muesli — Context & Shared Language

This file is the shared language for working on Muesli: domain vocabulary,
architecture map, and conventions. Read it before making a change. It encodes
_working_ knowledge, not a code reference — keep it current as the code evolves.

For the full documentation index, see [`docs/index.md`](docs/index.md).

## What Muesli is, and its one invariant

Muesli is a self-hosted, privacy-first meeting-notes app (a Granola
alternative): record a meeting, get a transcript and a clean summary, stored and
viewable, with nothing leaving infrastructure you control. The Go server drives a
pipeline (transcribe → summarize → embed) that calls out to **plugins you host**
— by default a self-hosted Whisper transcriber and a self-hosted Ollama LLM.

**The one invariant:** meeting content never has to leave infrastructure the user
controls. Transcription and inference run on self-hosted plugins by default.
Every feature decision is checked against this.

- **"Local"** = the user's device (the Electron desktop client; mic/system-audio
  capture; the user's token in OS-encrypted storage).
- **"Self-hosted"** = the operator's server (the Go server, Postgres+pgvector,
  the transcriber/agent plugins).

Do not conflate them: capture is local; transcription and inference are
self-hosted. The privacy guarantee is that the self-hosted side runs on
infrastructure the user/operator controls — not a third-party SaaS.

## Domain glossary

One precise line per term, in Muesli's own words. Backing code in parentheses.

- **Note** — the central owner-scoped record of one meeting: title, status, audio
  key, timestamps; its editable body lives in a separate `note_bodies` row.
  Status is one of `recording`/`uploaded`/`transcribing`/`summarizing`/`ready`/
  `failed`. (`model.Note`, `internal/store/notes.go`, table `notes`/`note_bodies`)
- **Transcript** — one transcription run for a note: the transcriber plugin/model
  used plus an ordered list of time-coded segments (`start_ms`, `end_ms`, text,
  source, speaker). One transcript per note (unique constraint). (`model.Transcript`,
  `model.Segment`, `internal/store/transcripts.go`)
- **Summary (enhanced summary)** — an agent-produced panel for a note, rendered
  from a template: a list of sections (heading + markdown), each optionally
  carrying `refs` (positional indices back into the transcript segments that
  grounded it). Has its own status (`pending`/`ready`/`failed`). (`model.Summary`,
  `model.SummarySection`, `internal/store/summaries.go`)
- **Recipe / Template** — a reusable summary recipe: a named, ordered list of
  sections, each a heading + an instruction the agent follows. Built-ins are
  seeded on boot with `owner_id` NULL and surfaced as a derived `built_in` flag
  (`(owner_id IS NULL) AS built_in`); users can create their own. (`model.Template`,
  `model.TemplateSection`, `internal/store/templates.go`, `SeedBuiltInTemplates`)
- **Tag** — a free-text label attached to a note (many-to-many). The tag list API
  returns each name with a live-note count. (`model.Tag`, `model.TagCount`,
  `internal/store/tags.go`)
- **Smart list** — a saved, rule-based note view: a name plus an opaque JSON
  boolean tree (`and`/`or` groups over field conditions like tag/title/status/
  created). The rule's _shape_ is validated on write; it is _evaluated
  client-side_. (`model.SmartList`, `store.ValidateRule`, `internal/store/smart_lists.go`)
- **Folder** — an owner-scoped, manually-curated named container for notes,
  optionally nested under a parent (max depth 5) and orderable by position.
  Notes belong to folders via `note_folders`. (`model.Folder`,
  `internal/store/folders.go`, tables `folders`/`note_folders`)
- **Recycle bin (soft delete)** — deletes set a `deleted_at` timestamp rather than
  removing the row; trashed items are hidden from normal queries, listable via
  `…/trash`, restorable, and eventually purged. Applies to notes, folders, and
  smart lists. (columns `deleted_at`; `worker.RunTrashPurger`)
- **Plugin** — a registered external HTTP/JSON service of one kind:
  `transcriber` (POST `/transcribe`) or `agent` (POST `/generate`). The server
  calls it with a per-plugin bearer token; its config (which may hold secrets) is
  stored encrypted and decrypted only at call time. (`model.Plugin`,
  `internal/plugin/client.go`, `internal/store/plugins.go`) Embeddings are _not_
  plugins — they use a separate mechanism (see _Embedding / semantic search_).
- **Job / job queue** — one unit of pipeline work (`transcribe`/`summarize`/
  `embed`) with a status (`pending`/`running`/`done`/`failed`), attempts, and a
  lease. The queue is a Postgres `jobs` table; workers claim with
  `SELECT … FOR UPDATE SKIP LOCKED` and a lease so a crashed worker's job is
  re-claimable. (`model.Job`, `internal/store/jobs.go`, `internal/worker`)
- **Embedding / semantic search** — each ready note gets a pgvector embedding
  (`note_embeddings`, a `vector` column tagged with the producing `model`).
  `GET /api/search` blends lexical title/snippet matching with cosine-similarity
  nearest-neighbour ranking. Embedding is pluggable and may be disabled (search
  degrades to lexical-only). (`internal/embed`, `internal/store/embeddings.go`,
  `internal/api/search.go`)
- **Owner** — every user-owned row carries `owner_id` (FK to `users`, cascade
  delete). It is the tenancy boundary: every store query filters by the
  authenticated user's id, so users only ever see their own data. (`owner_id`
  columns throughout `internal/db/migrations`, e.g. `0006_folders.up.sql`)

## Architecture map

Two deployables: a **Go server** (self-hosted) and an **Electron/React desktop
client** (local), plus **plugins** (self-hosted) that the server calls over HTTP.

### Go server

- **`cmd/muesli`** — the entrypoint (`main.go`). Loads config, applies DB
  migrations, connects the pool, seeds built-in templates, registers default
  plugins _if configured_, starts the worker pool + trash purger, and runs the
  API server. Also
  hosts the `muesli reembed` subcommand. (`internal/config`, `internal/db`,
  `internal/store`, `internal/worker`, `internal/api` are all wired here.)
- **`internal/api`** — HTTP layer. A `chi` router (`server.go`) maps routes to
  handlers; `internal/auth.Middleware` guards the authenticated group. Handlers
  decode requests, call the store/worker, and write JSON. One file per feature
  (`notes.go`, `folders.go`, `tags.go`, `smart_lists.go`, `templates.go`,
  `search.go`, `upload.go`, admin handlers, …).
- **`internal/worker`** — the processing pipeline. A pool of goroutines claims
  leased jobs (kinds `transcribe`/`summarize`/`embed`) from the `jobs` table and
  drives notes `uploaded → transcribing → summarizing → ready`, calling
  transcriber/agent plugins over HTTP. Also runs embedding generation and the
  trash purger. (`worker.go`, `pipeline.go`, `embed.go`, `trashpurge.go`)
- **`internal/store`** — persistence. One Go type, `*store.Store`, wrapping a
  `pgxpool.Pool`; one file per entity (`notes.go`, `folders.go`, `jobs.go`,
  `embeddings.go`, …). Owns SQL, owner-scoping, and sentinel errors
  (`ErrNotFound`, `ErrDuplicate`, `ErrInvalidParent`).
- **`internal/db`** — connection + migrations. `db.Migrate(url)` applies the
  numbered SQL files in `internal/db/migrations`; `db.Connect` returns a pool.
- **`internal/model`** — plain data types and status/kind constants shared by
  store, api, and worker (`model.go`). No DB or HTTP imports.
- **`internal/plugin`** — the server-side HTTP/JSON _client_ for the plugin
  contract: POST `/transcribe` and POST `/generate` with a bearer token
  (`client.go`). Language-agnostic — a plugin is any service speaking this JSON.
- **`internal/auth`** — bearer-token / `muesli_session`-cookie authentication
  middleware, password hashing, and token hashing (`middleware.go`).
- **`internal/crypto`** — symmetric encryption for plugin secrets at rest
  (config encrypted in the DB, decrypted only at call time).
- **`internal/embed`** — the embedder: turns text into a vector via the
  configured embeddings backend (Ollama `/api/embeddings`), not a registered
  plugin. Pluggable and may be nil (disabled).
- **`internal/storage`** — blob storage for audio. `NewLocal` serves presigned
  upload/download URLs (`_storage/*`) signed with an HMAC key.
- **`internal/config`** — `config.FromEnv()`; all `MUESLI_*` env settings.
- **`internal/adminui`** — embeds the built admin SPA (`dist/`) and serves it at
  `/admin`. Source lives in **`web/admin`** (a separate Vite app), built into
  `internal/adminui/dist` by `make build-admin`.

### Electron/React client (`src/`)

- **`src/main`** — Electron main process: window setup, the Muesli HTTP client
  (`muesliClient.ts`), audio capture/recorder, OS-encrypted token store, and the
  IPC handlers (`ipcHandlers.ts`, adapted to `ipcMain.handle` in `main.ts`).
- **`src/preload/preload.ts`** — exposes a typed `window.muesli` bridge over
  `contextBridge` + `ipcRenderer.invoke`.
- **`src/renderer`** — the React UI (`App.tsx`, `components/`, `lib/`). Reaches
  the main process only through the typed `muesli` bridge (`renderer/api.ts`).
  The sidebar/shell lives in `components/shell` (`Sidebar.tsx`, `AppLayout.tsx`).
- **`src/shared`** — code shared across processes: `ipc.ts` (channel names +
  the `MuesliBridge` interface) and `types.ts` (wire types mirroring `model`).

### Plugins (`plugins/*`)

Self-hosted Python services implementing the plugin HTTP contract:
`whisper-transcriber` (`/transcribe`), `ollama-agent` (`/generate`), and
`conformance` (a contract test suite). Each is its own module/venv. **Out of
scope for the root Go module's tests and the first CI.**

### Data store

Postgres with the **pgvector** extension (`pgvector/pgvector:pg16`). Relational
tables for notes/folders/tags/jobs/etc.; a `vector` column on `note_embeddings`
for semantic search.

### Flow diagrams

Request path (read/write through the API):

```
client (renderer) → IPC bridge → main: muesliClient → HTTP
  → internal/api (chi route + auth middleware)
  → internal/store (owner-scoped SQL)
  → Postgres + pgvector
```

Capture-to-summary pipeline (async, plugin-backed):

```
local capture (mic/system audio) → presigned upload (internal/storage)
  → note status: uploaded; enqueue job (jobs table)
  → internal/worker claims job (FOR UPDATE SKIP LOCKED)
     ├─ transcribe → POST /transcribe → transcriber plugin
     ├─ summarize  → POST /generate   → agent plugin
     └─ embed      → internal/embed   → embeddings endpoint → note_embeddings
  → note status: ready
```

## Conventions & patterns

### Migrations

- Location: `internal/db/migrations`. Numbered, zero-padded, paired:
  `NNNN_name.up.sql` and `NNNN_name.down.sql` (e.g. `0014_embeddings_pluggable.up.sql`).
- New migration = the next number after the current highest (currently `0014`),
  with both an `up` and a `down`. Keep `down` genuinely reversible/safe (see
  `0014`'s `down`, which deletes incompatible rows before narrowing a column).
- They are applied automatically: `db.Migrate(url)` runs on server boot and in
  tests (via testutil). Never hand-run migrations against the test DB.

### The layering for a new feature (store → api → client-types → IPC)

A feature is added in the same order every time. Tracing **folders** (the
canonical example):

1. **Migration** — `internal/db/migrations/0006_folders.up.sql` (+ `.down.sql`):
   the `folders` / `note_folders` tables, owner-scoped with indexes.
2. **Model** — a type in `internal/model/model.go` (`model.Folder`).
3. **Store** — `internal/store/folders.go`: `ListFolders`/`CreateFolder`/… ,
   every query filtered by `owner_id`; sentinel errors for the API to map.
4. **API handler** — `internal/api/folders.go`: decode the request, call the
   store, map sentinel errors to status codes, `writeJSON`. Register routes in
   `internal/api/server.go` inside the authenticated `router.Group`.
5. **Client wire types** — `src/shared/types.ts` (mirror `model.Folder`).
6. **IPC contract** — add channel names + methods to `src/shared/ipc.ts`
   (`IPC.listFolders`, the `MuesliBridge` method signatures).
7. **IPC plumbing** — implement in `src/main/ipcHandlers.ts` (calling the HTTP
   client), register in `src/main/main.ts` (`ipcMain.handle`), and expose in
   `src/preload/preload.ts` (the `ipcRenderer.invoke` wrapper).
8. **Renderer** — call via the typed `muesli` bridge (`src/renderer/api.ts`) from
   components (e.g. `components/shell/AppLayout.tsx` → `Sidebar.tsx`).

### Owner-scoping (non-negotiable)

Every store query against user data filters by `owner_id` (passed in as the
authenticated user id), e.g.
`SELECT … FROM folders WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL`.
The API gets the id from `userIDFromContext(r.Context())`, set by
`internal/auth.Middleware`. A query without owner-scoping is a tenancy bug.

### Errors & status codes

- Store returns **sentinel errors** (`store.ErrNotFound`, `store.ErrDuplicate`,
  `store.ErrInvalidParent`); handlers map them with `errors.Is`.
- API writes JSON via `writeJSON`/`writeError` (`internal/api/json.go`); errors
  are `{"error": "..."}`. Conventional mapping: bad body → `400`, unauthenticated
  → `401`, not found → `404`, duplicate/unique-violation → `409`, unexpected →
  `500`. Creates return `201`.
- Soft delete sets `deleted_at`; normal reads exclude trashed rows; restore and
  `…/trash` listing are explicit endpoints.

### Tests

- **Go:** `go test ./... -p 4`. Each test gets its own isolated PostgreSQL schema,
  so packages CAN run in parallel safely (the Makefile already uses `-p 4`).
  DB tests read `TEST_DATABASE_URL`; if it's unset they **skip** (not fail).
  `internal/testutil/db.go` (`testutil.NewPool(t)`) connects to that URL, runs
  `db.Migrate` (so tests self-migrate — never pre-migrate by hand), truncates all
  data tables, and returns a ready pool. So a test file just calls
  `testutil.NewPool(t)` and gets a clean, migrated schema.
- **Client:** `npm run test` (`vitest run`) and `npm run typecheck`
  (`tsc --noEmit`). Tests sit next to their source (`*.test.ts`/`*.test.tsx`).
- **Plugins:** Python, with their own venvs/pytest under `plugins/*` — separate
  from the Go module tests.

### Naming

- Go: package = directory; one file per entity/feature (`folders.go` +
  `folders_test.go`). API handlers are `handleXxx`; store methods are verbs
  (`ListFolders`, `CreateFolder`).
- TS: IPC channels are `muesli:camelCase`; bridge methods match. Env config is
  `MUESLI_*` (`internal/config`).

## Where does X go?

Concrete, in-order file recipes. They are the folders trace in _Conventions &
patterns → The layering for a new feature_ sliced by task. Add a test alongside
each code change (`*_test.go`, `*.test.ts`).

### Add an API endpoint

1. `internal/store/<entity>.go` — the query method(s), owner-scoped, returning
   sentinel errors.
2. `internal/api/<entity>.go` — a `handleXxx` that decodes, calls the store, maps
   errors to status codes, and `writeJSON`s.
3. `internal/api/server.go` — register the route inside the authenticated
   `router.Group` (or the public top-level routes if it must be unauthenticated).
4. `internal/api/<entity>_test.go` — a handler test (uses `testutil.NewPool`).

### Add a DB migration

1. `internal/db/migrations/NNNN_name.up.sql` — next number after the highest
   existing (`ls internal/db/migrations`); owner-scoped tables with `owner_id` FK
   - indexes; add `deleted_at` if it should be soft-deletable.
2. `internal/db/migrations/NNNN_name.down.sql` — the safe reverse.
   No code runs it — boot and tests apply migrations automatically.

### Add a client IPC method

1. `src/shared/ipc.ts` — add the `IPC.xxx` channel name and the `MuesliBridge`
   method signature.
2. `src/main/ipcHandlers.ts` — implement it (call the HTTP client / `muesliClient`).
3. `src/main/main.ts` — register `ipcMain.handle(IPC.xxx, …)`.
4. `src/preload/preload.ts` — add the `ipcRenderer.invoke(IPC.xxx, …)` wrapper.
5. `src/shared/types.ts` — add/extend any wire types it carries.
6. Call it from the renderer via the typed `muesli` bridge (`src/renderer/api.ts`).

### Add a sidebar section

1. Ensure the data is reachable via an IPC method (see above), e.g.
   `muesli.listFolders()`.
2. `src/renderer/components/shell/AppLayout.tsx` — fetch into state and pass as a
   `Sidebar` prop; extend `ActiveView` if it introduces a new selectable view
   type.
3. `src/renderer/components/shell/Sidebar.tsx` — add the prop to the component
   signature and render the section.
4. Add `*.test.tsx` for the new UI behavior.

### Add a plugin (transcriber or agent)

A plugin is any HTTP service speaking the contract — it need not live in this
repo, but the reference ones do under `plugins/*` (Python/FastAPI):

1. Implement the contract: a `transcriber` answers POST `/transcribe`; an `agent`
   answers POST `/generate` (see request/response shapes in
   `internal/plugin/client.go`). Authenticate the server's bearer token (see
   `plugins/whisper-transcriber/whisper_app/auth.py`).
2. Validate it against the contract suite in `plugins/conformance`.
3. Register it: either via the admin UI / `POST /api/admin/plugins`, or as a
   default by setting `MUESLI_DEFAULT_TRANSCRIBER_URL`/`_TOKEN` (or the agent
   equivalents), which `cmd/muesli/main.go` auto-registers on boot.
4. No Go server code change is required to add a plugin — the server is the
   _client_ of the contract (`internal/plugin`).

## Build, test, run

Go 1.25.6. The root `Makefile` targets:

- `make run` — `go run ./cmd/muesli` (runs the server; needs config env, incl.
  `MUESLI_MASTER_KEY` and a `DATABASE_URL`).
- `make build` — runs `build-admin` then `go build -o bin/muesli ./cmd/muesli`
  (admin SPA built first so the fresh assets are embedded).
- `make build-admin` — `cd web/admin && npm ci && npm run build` (admin SPA into
  `internal/adminui/dist`).
- `make test` — `go test ./... -p 4` (packages run in parallel safely — each test gets its own schema; see Tests above).
- `make test-db` — starts a throwaway pgvector Postgres on **`:5433`** and prints
  the `export TEST_DATABASE_URL=…` line to copy.
- `make test-db-stop` — stops that container.
- `make tidy` — `go mod tidy`.
- `make up` — `docker compose up` (bring up the full self-hosted stack: postgres, ollama, whisper, agent, server).
- `make dev` — `npm run dev` (run the Electron desktop client in dev/watch mode).
- `make lint` — `go vet ./...` (Go static analysis; add golangci-lint / eslint here when config is added).
- `make smoke` — `go build ./... && npm run typecheck` (quick sanity check: compile Go server and typecheck TS client).

### Local test loop

```bash
make test-db                 # starts pgvector on :5433, prints the export line
export TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5433/muesli_test?sslmode=disable
make test                    # go test ./... -p 4  (DB tests self-migrate)
make test-db-stop            # tear down when done
```

Client checks (from repo root):

```bash
npm ci
npm run typecheck            # tsc --noEmit
npm run test                 # vitest run
npm run dev                  # electron-vite dev (run the desktop client)
```

Note: When CI is added (M1), it will run pgvector on `localhost:5432`; the local
`make test-db` mapping of `:5433` is only a convenience to avoid clashing with a
local Postgres on `:5432`.

## Accessibility

The renderer has a screen-reader announcement baseline:

- **`useAnnouncer` hook** (`src/renderer/hooks/useAnnouncer.ts`) — returns `{ announce, announceAssertive }`. `announce` targets the polite `aria-live` region (status/save confirmations); `announceAssertive` targets the assertive region (errors, recording state changes). Messages auto-clear after 5 s.
- **`AnnouncerProvider` / `AriaAnnouncer`** (`src/renderer/components/AriaAnnouncer.tsx`) — context provider and the pair of visually-hidden live regions placed at the app root in `App.tsx`. Any component inside the provider calls `useAnnouncer()` to post messages.
- **`eslint-plugin-jsx-a11y`** runs via `npm run lint` (CI) on all `src/renderer/**` JSX. See `docs/ACCESSIBILITY.md` for conventions and the rationale for each relaxed rule.
