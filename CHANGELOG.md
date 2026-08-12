# Changelog

All notable changes to Muesli are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Muesli is pre-1.0 and under active development; tagged desktop releases
(`desktop-vX.Y.Z`) are cut from accumulated `Unreleased` changes, and
`[Unreleased]` tracks what's merged to `main` since the last tag.

## [Unreleased]

## [0.1.17] - 2026-08-12

### Added

- **Live recording feedback.** Recordings now show the microphone input level
  and indicate when silence is detected, making it clear that audio is being
  captured. (#583)
- **Visible model startup.** The app now streams a model-loading status during
  the first recording after startup instead of appearing idle during the wait.
  (#582)

### Changed

- **Transcript search.** Search now matches words in transcript text as well as
  note titles. (#589)
- **Clearer draft notes.** Draft notes are now visually distinct from recordings
  that are currently active. (#590)

### Fixed

- **Notes recover after a force-quit.** Restarting after a crash or force-quit no
  longer leaves existing notes unreachable behind the create-account screen.
  (#585)
- **Recorded audio plays back.** The desktop app's security policy now permits
  playback of locally recorded audio. (#588, #628)
- **Missing agent configuration is explained.** AI features now show a clear
  message when the required agent plugin is missing or misconfigured instead of
  failing silently. (#591)
- **Blank-audio markers stay hidden.** The `[BLANK_AUDIO]` transcription
  placeholder is no longer displayed in transcripts. (#581)
- **Embedded processes exit with the app.** Force-quitting no longer leaves the
  embedded Postgres database or Whisper transcriber plugin running in the
  background. (#586, #624)

## [0.1.15] - 2026-08-01

Same application as `0.1.14`, which did not publish a macOS build: a bug in the
release smoke test itself failed the job after the app had been built and
notarized. No application code changed.

### Fixed

- **Release smoke test could not reopen the app under its `dist-desktop` path.**
  The close-and-reopen scenario shelled out to `open -a <path>`, which resolves
  its argument as an application _name_ first, so a relative path to an
  unregistered bundle failed while the absolute `/Volumes` path used for the dmg
  worked. The path is now resolved before launching.

## [0.1.14] - 2026-08-01

### Fixed

- **Reopening the window no longer hangs on "Starting…".** After closing the
  window with menu-bar running enabled, reopening it from the tray (or the Dock)
  left the app stuck on the startup screen forever, even though the embedded
  server was running: the startup gate only listened for a status event that had
  already fired before the new window existed. The main process now remembers the
  latest startup status and the window asks for it on open. (#499)

### Changed

- The packaged-app smoke test now covers closing and reopening the window, and
  the Linux AppImage smoke runs under a D-Bus session so it stops failing
  releases for reasons unrelated to the build. (#498, #499)

## [0.1.13] - 2026-08-01

### Added

- **Home screen.** The app now opens on a Home view that shows what's coming up
  alongside your recent notes, instead of a flat list of all notes. The separate
  "Coming up" destination is folded into it, and its empty state now links
  straight to calendar settings. (#493)
- **Opt-in menu-bar running.** A new setting keeps Muesli running in the menu bar
  when you close the window, with a tray menu for opening the app, starting a
  meeting, reaching Settings, and quitting. It is off by default and no tray icon
  appears unless you turn it on. (#495)

### Fixed

- **Auto-record now works with the window closed.** Meeting detection ran only in
  the app window, so enabling "Auto-record detected meetings" silently stopped
  working the moment you closed it. Detection now runs in the main process, and
  opens a window when a meeting needs recording. (#494)
- **Closing the window quits on macOS** unless you have opted into menu-bar
  running, rather than leaving the app resident with no visible trace. (#495)
- **First-run messaging.** Onboarding no longer tells you to open the desktop app
  you are already in or implies recordings leave your device when the built-in
  server is handling them; the Ollama banner no longer repeats the AI settings
  section it links to, and its in-app action now looks like the link it is. (#492)

## [0.1.12] - 2026-07-26

No user-facing changes. This release carries only test and release-pipeline
hardening, and behaves identically to `0.1.11`.

### Changed

- **Testing and release gates.** Added tests for the preload bridge boundary,
  main-process wiring, and the note stream relay; a shared context-bridge test
  helper matching Electron's read-only, non-configurable exposure; coverage
  reporting for client and server; dependency-gated test skips that now fail
  under CI instead of passing silently; the packaged-app smoke test on pull
  requests that touch `src/**`; and smoke tests that mount the shipped dmg and
  drive a short user journey before a release is published.

## [0.1.11] - 2026-07-25

### Fixed

- **Blank window on launch.** `desktop-v0.1.10` could ship a blank window
  because the renderer context bridge was proxied incorrectly; `308acac`
  removes that proxying so `desktop-v0.1.11` launches into the app shell
  again.

## [0.1.10] - 2026-07-25

### Added

- **Desktop onboarding and AI settings.** The desktop app gained onboarding
  and AI settings surfaces (`c67cc1b`).

### Changed

- **Embedded server appdata is unified.** The desktop app now uses a unified
  embedded-server appdata location (`e1839fc`).
- **Settings and sidebar affordances were tightened up.** Settings now uses a
  dark-styled cadence select and checkbox, and the sidebar exposes one search
  affordance (`01bb1a6`).

### Fixed

- **Startup banner overflow.** The startup banner no longer overflows the
  window (`69036e6`).
- **Invalid auth tokens recover cleanly.** The desktop client now recovers
  from invalid auth tokens instead of getting stuck (`6959222`).

## [0.1.9] - 2026-07-18

### Added

- **Calendar sync, sources, and OAuth connect.** Calendar data now has its own
  `calendar_sources` and `calendar_events` schema, with owner-scoped API routes
  for `/api/calendar/sources`, `/api/calendar/refresh`, and
  `/api/calendar/events?from=&to=`. The background sync engine and scheduler
  keep sources fresh automatically, and source creation now supports ICS and
  CalDAV with sealed credentials at rest. When `MUESLI_GOOGLE_OAUTH_CLIENT_ID`,
  `MUESLI_GOOGLE_OAUTH_CLIENT_SECRET`, `MUESLI_GOOGLE_OAUTH_REDIRECT_URL`, and
  `MUESLI_MICROSOFT_OAUTH_CLIENT_ID`, `MUESLI_MICROSOFT_OAUTH_CLIENT_SECRET`,
  `MUESLI_MICROSOFT_OAUTH_REDIRECT_URL` are configured, Settings exposes
  Connect buttons that drive the `/api/calendar/oauth/google/*` and
  `/api/calendar/oauth/microsoft/*` flows.
- **Meeting detection and calendar-linked notes.** Settings adds an
  **Auto-record detected meetings** toggle, and the client now runs a
  background detection loop that watches upcoming calendar events and prompts
  you to start recording from the shell or note screen. Notes can be linked to
  a calendar event through the note header, backed by `POST` and `DELETE
/api/notes/{id}/event`, so the same meeting can stay connected across the
  calendar and note views. The new **Coming up** calendar view uses that same
  event data to surface imminent meetings in the UI.
- **Expanded export, import, and re-transcribe controls.** The note actions menu
  now offers plain-text, DOCX, PDF, SRT, ASS, and WebVTT subtitle exports
  alongside the existing Markdown export, all through the client's native
  save/download flow. The client also adds `POST /api/notes/import` for
  importing recorded audio into a new note, and the note header's **Enhance…**
  dialog for re-transcribing a note with optional model and language overrides
  via `POST /api/notes/{id}/retranscribe`.

### Added

- **Speaker diarization.** Transcripts can now carry a **per-segment speaker label and confidence**,
  produced by an optional diarization stage in the Whisper transcriber plugin (`WHISPER_DIARIZATION_ENABLED`,
  `WHISPER_DIARIZATION_HF_TOKEN`, `WHISPER_DIARIZATION_MODEL`). The transcript view groups consecutive
  segments into **speaker turns**, and a note-level **speaker alias** lets you rename a raw label (e.g.
  `SPEAKER_00` → "Alex") for that note. A **diarization review** step (pending → in review → completed)
  lets you confirm or correct speaker labels before the note is summarized — finalization is **held**
  until review completes, so summaries are written against the reviewed transcript. Resolved speaker
  labels carry through to Markdown export/copy. (Aliases are per-note; a cross-note speaker directory
  isn't in yet.)
- **Webhook delivery, with retry and an admin status view.** Notes can now notify an external endpoint:
  a `note.completed` event (note id, title, summary, and speaker-tagged transcript segments) is queued
  for delivery when a note finishes. Deliveries are persisted and sent with exponential backoff on
  failure, guarded against SSRF (scheme, private-IP, and DNS-rebinding checks re-validated at send
  time). The admin **Webhook deliveries** view lists attempts, last error, and next retry, with a
  manual **Retry** action. A self-serve way to register your own webhook (and the fuller public
  integration API) is still to come.
- **Export all notes.** Alongside the existing per-note Markdown export, the desktop app now offers a
  bulk **Export all notes** action: pick a save location and every note is written as its own Markdown
  file inside a single `.zip`.
- **Configurable Whisper model and precision.** The Whisper transcriber plugin's model size and
  compute precision are now runtime-configurable — `WHISPER_MODEL` selects the faster-whisper size
  (tiny/base/small/medium/large-v3) and `WHISPER_COMPUTE_TYPE` selects precision (`default`, `int8`,
  `float16`, `float32`), surfaced via `/info` and honored by `/transcribe`, with an automatic fallback
  to `int8` when `float16` is requested on a CPU device. Notes also carry a `partial_transcript` flag
  that stays true when a chunk failure left the transcript incomplete, clearing once a retry succeeds.
- **Duplicate-audio warning.** Before uploading a recording, the desktop client hashes the audio
  (SHA-256) and checks `POST /api/audio/dedup-check` for existing notes with a matching hash, warning
  you — with a chance to cancel — before you accidentally re-import the same recording.

### Changed

- **Worker recovers stale jobs left by a crash.** Beyond the existing lease (`FOR UPDATE SKIP LOCKED` +
  `lease_expires_at`), the worker now resets orphaned `running` jobs back to `pending` both at startup
  and on a recurring background sweep, so a job abandoned mid-flight by a crashed worker process gets
  picked back up automatically instead of staying stuck.

### Added

- **Re-run summary (regenerate).** A note’s overflow (⋯) menu gains **Re-run summary**, and the processing banner’s **Re-run** now works for failed notes — both re-generate the note’s summaries with its current templates (built-ins + yours) and re-poll back to ready. The summarize fan-out was extracted to a shared store method so the pipeline and the re-run path can’t drift. `POST /api/notes/{id}/resummarize` (owner-scoped; 409 if there’s no transcript).
- **Export a note as Markdown.** The note overflow (⋯) menu gains **Export as Markdown…**, which opens a native save dialog and writes the whole note (summary panels + your notes + transcript) to a `.md` file.
- **Copy a note as Markdown.** The note toolbar gains a **Copy** button that copies the current view (the selected Enhanced panel, or My notes) as Markdown to the clipboard, with a transient "Copied" confirmation. The note overflow (⋯) menu now closes on outside-click / Escape.
- **Smart lists go to Trash too.** Deleting a smart list now **moves it to Trash** (reversible) — the
  context-menu action is **Move to Trash**, and the Trash screen has a **Smart lists** section with
  Restore / Delete forever, 30-day auto-purge. `DELETE /api/smart-lists/{id}` is now a soft delete;
  new `/trash`, `/restore`, `/permanent` routes.
- **Folders go to Trash too.** Deleting a folder now **moves it (and everything nested inside it) to Trash** instead of permanently destroying the subtree — the folder dialog's action is now **Move to Trash** ("recoverable for 30 days"). The notes themselves are untouched (they keep their other folders); folder membership is preserved and restored intact. The **Trash** screen gains a **Folders** section with **Restore** (brings the whole subtree back, re-filing its notes) and **Delete forever**; the 30-day auto-purge sweeps old trashed folders. A trashed folder can't be filed into, renamed, or re-parented. `DELETE /api/folders/{id}` is now a soft (subtree) delete; new `GET /api/folders/trash`, `POST /api/folders/{id}/restore`, `DELETE /api/folders/{id}/permanent`.
- **Recycle bin (Trash).** Deleting a note now **moves it to Trash** instead of erasing it — the overflow (⋯) menu’s **Move to Trash** (confirm: "recoverable for 30 days") soft-deletes it, hiding it from the feed, search, and every view while keeping its transcript, summaries, tags, folder membership, and audio. A **Trash** entry in the sidebar lists deleted notes with **Restore** and **Delete forever**; trashed items are **auto-purged after 30 days** by a background job (which also removes their audio blobs). A note trashed mid-processing is skipped by the worker. `DELETE /api/notes/{id}` is now a soft delete; new `GET /api/notes/trash`, `POST /api/notes/{id}/restore`, `DELETE /api/notes/{id}/permanent` (all owner-scoped).
- *_⌘K command palette._ Now also includes **action commands** (New meeting, Manage templates, Settings) and a **⌘N** shortcut for a new meeting. A global ⌘K (Ctrl+K) opens a quick-jump palette that searches across
  your **notes, folders, smart lists, and tags** (grouped) and either opens a note or switches
  the active sidebar view — keyboard-driven (↑/↓, Enter, Esc). Client-side over loaded data.

### Added

- **Clearer organizers + right-click menus.** The sidebar now distinguishes its three organizers at a
  glance: **Lists → "Smart lists"** with a funnel icon and a muted **first-condition subtitle** under
  each (e.g. `status is ready`) so a saved query reads as a _rule_, not a folder; folders **glow when
  you drag a note** over them (smart lists/tags stay inert — only folders are drop targets). Every
  organizer row gains a **right-click context menu**: smart lists → _Edit rule… / Delete list_;
  folders → _New subfolder… / Rename… / Move to Trash_; tags → _Save as smart list_ (creates a list
  filtered to that tag). (Muesli's first context-menu surface, on a new owned Radix primitive.)
- **Right-click a note in the feed** → _Move to Trash_, _Add to folder ▸_ (folder submenu), or _Re-run
  summary_ — without opening it.
- **Summary citations.** When a summary section cites transcript spans (`refs`), it now shows clickable
  **Sources** chips that open the transcript drawer and highlight + scroll to the cited segment.
- **Semantic search (self-hosted embeddings).** Search now finds notes by **meaning**, not just keywords.
  Notes are embedded on the self-hosted server via an Ollama model (`nomic-embed-text`, 768-dim) into a pgvector table;
  `GET /api/search` blends cosine nearest-neighbour with the existing lexical title/snippet match, and
  the search box additively surfaces conceptually-related notes the keyword filter missed. Stays on
  your self-hosted infrastructure (privacy-preserving) and **optional** — set `MUESLI_EMBEDDINGS_URL` to enable; unset, search
  stays lexical-only and nothing else changes. (Requires the `pgvector/pgvector` Postgres image.)
- **Drag folders to re-arrange them.** Sidebar folders are now draggable — drop one **onto another
  folder** to nest it, onto the **Folders header** to move it to top level, or **between two sibling
  rows to reorder** them (a `position` column persists the order; dropping into a gap under a
  different parent re-parents there). Cycle and depth violations are rejected (client guard + server).

### Changed

- **Swappable embedding models (any dimension).** The semantic-search embedding model is no longer
  pinned to 768 dims — `note_embeddings.embedding` is now an unsized `vector` tagged with its `model`,
  and search compares only same-model (= same-dimension) vectors. Point `MUESLI_EMBEDDINGS_MODEL` at
  any Ollama embedding model (e.g. `mxbai-embed-large` 1024, `bge-m3` 1024) and restart to auto
  re-embed, or run `muesli reembed` to force it. Existing nomic-768 embeddings are preserved.
- **Upload hardening.** The signed audio-upload endpoint now rejects non-`audio/*` content types (415)
  and the 1 GiB size cap is enforced on streaming uploads with no/lying `Content-Length` (closing the
  previously-untested over-cap path).
- **Sharper semantic search.** Embeddings now use `nomic-embed-text`'s task prefixes
  (`search_document:` on stored notes, `search_query:` on the query) for better relevance; the
  default similarity cutoff was re-tuned to **0.6** to match the prefixed score range. (Configurable
  via `MUESLI_EMBEDDINGS_DOC_PREFIX` / `MUESLI_EMBEDDINGS_QUERY_PREFIX` / `MUESLI_EMBEDDINGS_MIN_SCORE`;
  existing deployments should re-embed — truncate `note_embeddings` and restart — to adopt prefixes.)
- **Default summary model → `llama3.2:3b`.** Bumped the default agent model from `llama3.2:1b` to
  `3b` for materially better instruction-following — it now writes "None recorded." for empty
  sections (e.g. _Action items_ on a meeting that had none) instead of dumping transcript lines, the
  one residual summary-quality issue on the 1B model. Costs ~2 GB and is slower on CPU; override with
  the agent plugin's `model` config or `OLLAMA_MODEL`.
- **HTTPS guardrail on connect.** The desktop client now **refuses to connect to a non-loopback
  server over plain HTTP** (which would send your audio, notes, and password in the clear), showing a
  warning with an explicit "connect anyway" override. Loopback (`localhost`/`127.0.0.1`) is still
  allowed for local development, as is a global `MUESLI_ALLOW_INSECURE=1` dev flag. Prefer `https://`.
- **Electron 31 → 42.** Bumped the desktop runtime from 31.7.7 to 42.4.0, clearing the outstanding
  Electron security advisories. Smoke-verified (build + typecheck + tests green; the app launches and
  renders under 42 with the preload IPC bridge intact). The build toolchain (electron-vite 2.3.0) is
  unchanged. (Remaining `npm audit` items are dev-only — esbuild/vite via electron-vite — and ship in
  no artifact.)
- **Collapsible, resizable sidebar.** Drag the sidebar's right edge to resize it (200–420px), or
  collapse it to a thin icon rail with the header toggle or **⌘\\ (Ctrl+\\)**. The chosen width and
  collapsed state persist across launches.
- **Loading skeleton on launch.** The notes list shows a skeleton while the first load is in flight instead of briefly flashing the "No notes yet" empty state.
- **Cleaner default meeting titles.** New meetings are titled "Meeting — Jun 13, 2:10 PM" (locale-aware) instead of a raw `13/06/2026, 19:56:54` timestamp.
- **Search within a transcript.** The transcript drawer gains a search box that filters the lines to those matching your query and highlights the match — quick to find a quote in a long meeting.
- **Per-note template switcher.** The Enhanced note view no longer flattens every template's
  panel into one wall — it shows a **single selected panel** (defaulting to the richest) with a
  **✦ template picker** to switch among the note's panels (built-ins + your templates). Pure
  view-selection over the already-generated summaries — no re-run. (Regenerate-on-demand stays
  deferred.)

### Security

- **v1 hardening pass** (pre-public gate). Passwords now use the standard **argon2id
  PHC** string (`$argon2id$v=19$m=,t=,p=$…`) with params parsed on verify (legacy hashes
  still authenticate). **Login timing equalized** on the unknown-email path (dummy verify)
  to blunt user-enumeration. The signed-upload PUT now **caps body size** (413 over 1 GiB,
  via Content-Length check + `MaxBytesReader`). Malformed note ids return **404 instead of
  500** across the note/upload/tag/folder handlers. `isKeyForNote` tightened to require the
  full `notes/{id}/audio/` prefix and reject `..`. The Ollama agent maps **upstream/transport
  errors to 502** (not 500). `package.json` license aligned to **AGPL-3.0-or-later**.

### Changed

- **UI polish.** Three refinements: the note list is now a **date-grouped feed** (Today /
  Yesterday / weekday / date) with richer rows — a colored **monogram tile** (title
  initial, deterministic tint) and a right-aligned **time**; **serif display titles**
  (Newsreader) on the note title and the main-pane page heading, with body and UI text
  staying Inter; and the **transcript** moves from a co-equal segmented tab to a toggled
  **side drawer**, so the note view is **Enhanced / My notes** with the enhanced summary
  kept primary. Renderer-only; verified in light + dark via the visual loop.
- **Sidebar is navigation-only; the main window is the meeting list.** The sidebar
  previously rendered the full note list, cramming filtered meetings into the narrow
  left column and pushing Lists/Tags/Suggested below the fold. The note list now lives
  in the main pane (where the active tag/list filter applies), the sidebar holds just
  navigation (All notes · Lists · Tags · Suggested), and the main heading reflects the
  active view (the list name, `#tag`, or "All notes") with a dedicated filtered
  empty-state — closer to the Granola layout.

### Added

- **v2 — nested folders.** Folders can now contain **sub-folders** (a tree). A folder has an
  optional **parent** (set in the folder dialog's "Parent folder" picker), the sidebar Folders
  section renders an **expandable tree** (chevron expand/collapse, indented), and the server
  guards against **cycles** (a folder can't be parented under itself or a descendant) and
  **depth > 5**. Counts and filtering stay **direct** (a folder shows its own notes; recursive
  aggregation is a follow-up). `0007_folder_parent` migration; `parent_id` on the folders API.
- **v2 — editable summary templates.** Users can now **author, edit, and delete their own
  summary templates** — named groups of sections (`heading` + `instruction`) the agent fills
  in — alongside the read-only built-ins. New notes are summarized against the built-ins **and**
  the note owner's templates, so an authored template shapes future notes' Enhanced view.
  `GET/POST/PUT/DELETE /api/templates` (owner-scoped; built-ins read-only; duplicate name →
  409), the summarize pipeline fans out over built-ins + the owner's templates, and a
  **Templates manager** (Settings → Manage templates) lists built-ins and your templates with
  a section editor (add/remove/reorder sections). The Granola-style **per-note template
  switcher / regenerate-on-demand** is deferred (tracked).
- **v2 — folders.** Manual, named, owner-scoped collections — the third note-organization
  slice (after tags and smart lists). A note can be in **any number of folders**
  (many-to-many, no single-home trap). `0006_folders` schema (`folders` + a `note_folders`
  join; folder names unique per owner, case-insensitive) + `GET/POST/PUT/DELETE /api/folders`
  (duplicate name → 409) and `POST/DELETE /api/notes/{id}/folders` membership; every note
  response carries `folder_ids`. In the client: a sidebar **Folders** section with a live
  count and single-select filtering, a **FolderBar** on the note (add/remove, plus
  create-new), **two filing paths** (the FolderBar picker and **dragging a note from the
  feed onto a sidebar folder**), and a create/rename/delete dialog (deleting the active
  folder resets the view). Folders evaluate client-side over the loaded notes. Verified
  end-to-end against the Docker stack, light + dark.
- **v2 — smart lists.** Saved, rule-based, auto-updating note views (the second slice of
  the note-organization sub-project, after tags). A smart list is a named **boolean rule**
  (AND/OR, nestable) over a note's **tag / title / status / created-date**. `0005_smart_lists`
  schema (owner-scoped, rule stored as JSONB) + `GET/POST/PUT/DELETE /api/smart-lists` with
  server-side rule-shape validation (field/operator/value-type, known status, positive
  `withinLastDays`, depth ≤ 8). In the client: a **Lists** section in the sidebar with a live
  count and single-select filtering that composes with text search, and a **rule editor**
  dialog (create / edit / delete, nestable groups). Rules evaluate client-side over the loaded
  notes (the matcher powers both the filtered list and each count). Plus **auto-detected
  recurring meetings** — a **Suggested** section clusters notes by normalized title (≥3
  sharing a stem, trailing date/number tokens stripped) and offers one-click smart lists,
  which is how Muesli links related/recurring meetings without calendar data (calendar/
  attendee grouping is v3). Verified end-to-end against the Docker stack.
- **v2 — note tags.** Manual, note-level, many-to-many tags (the first slice of the
  note-organization sub-project). `0004_tags` schema (owner-scoped, case-insensitive
  unique, orphan cleanup); `POST /api/notes/{id}/tags` + `DELETE …/tags?name=` (the
  client is tag-id-free — remove is by name); every note response carries `tags[]`.
  In the client: a **Tags** section in the sidebar with single-select filtering that
  composes with text search, and a **TagBar** on the note for adding/removing tags
  with autocomplete. Verified end-to-end against the Docker stack.
- **v2 — desktop client UI redesign.** A clean, Granola-grade rebuild
  of the Electron client (first v2 sub-project; see `ROADMAP.md`), verified
  end-to-end against the Docker stack.
  - **Design system** — Tailwind CSS v4 with semantic CSS-variable tokens (teal
    brand on slate ink), light + dark following the OS with a manual toggle, Inter +
    JetBrains Mono (self-hosted via Fontsource, no CDN), and owned shadcn-style
    primitives on Radix (Button, Input, Badge, Skeleton, SegmentedControl, Toast).
  - **Sidebar app shell** — react-router (memory router): persistent sidebar (New
    meeting · search · All notes) + content pane, with a slot reserved for folders/lists.
  - **Live note-taking** — a TipTap rich-text editor autosaves your notes (debounced)
    while recording; on stop the audio uploads to the already-created note and the
    pipeline runs (capture → upload → transcribe → summarize → ready) with a polling
    status banner.
  - **Note view** — enhanced-summary-primary with a segmented control switching
    Enhanced / My notes / Transcript; editable note title; empty/loading/error states.
  - **Server** — editable note title (`PATCH /api/notes/{id}`) and a body snippet in
    the note-list response.
  - **IPC** — granular `createNote`/`updateBody`/`updateTitle`/`uploadAudio` bridge
    (replaces the bundled `finishMeeting`) for the up-front-create live-notes flow.
  - **Dev tooling** — `npm run dev:debug` + a Chrome DevTools Protocol visual check for screenshot-based UI verification (see `SMOKE.md`).
- **v1 — end-to-end thin slice.** Self-hosted server, language-agnostic HTTP/JSON
  plugin contract, default self-hosted plugins, an Electron desktop client, and an
  embedded admin UI. A recording flows: capture → upload → transcribe →
  summarize → viewable note.
  - **Go server** — chi router, pgx/Postgres, embedded migrations, argon2id auth
    with bearer/session tokens, owner-scoped notes API, presigned local-filesystem
    uploads (HMAC-signed URLs), AES-256-GCM-sealed plugin secrets, a Postgres-table
    job queue leased by a worker pool (transcribe → summarize), and seeded summary
    templates.
  - **Plugin contract** — `GET /info`, `GET /health`, `POST /transcribe`,
    `POST /generate`, with bearer auth and `X-Muesli-Plugin-API: 1`. Summary `refs`
    are 0-based positional transcript indices.
  - **Default plugins** (Python/FastAPI) — Whisper transcriber and Ollama agent
    (self-hosted by default, BYO-cloud opt-in), plus a conformance suite that validates
    any plugin against the contract.
  - **Desktop client** (Electron/TypeScript) — mixed mic+system audio capture,
    upload state machine, OS-keychain token storage, React capture/note views.
  - **Admin UI** (React/Vite) — setup, login, plugin registry with JSON-Schema-driven
    config forms, and a job monitor; built and embedded into the server binary,
    served at `/admin`.
- **One-command deploy** — multi-stage `Dockerfile` (builds the admin SPA → static
  Go binary embedding it) and a full-stack `docker-compose.yml` (Postgres, Ollama
  with one-shot model pull, Whisper, agent, server) that runs with no `.env`.
  Default plugins auto-register from environment on startup. `MUESLI_INTERNAL_URL`
  distinguishes the plugin-facing presign base from the client-facing
  `MUESLI_PUBLIC_URL`.
- **Startup banner** — an ASCII muesli-bowl "everything is up" banner with the
  admin/API/health URLs, printed once the server is fully initialized.
- **Open-source project docs** — AGPL-3.0 `LICENSE`, `CONTRIBUTING.md`,
  `SECURITY.md`, contributor-facing `docs/ARCHITECTURE.md`, GitHub issue/PR
  templates, and this changelog.

### Fixed

- **Ollama agent hallucination / scaffold echo.** With the small default model
  (`llama3.2:1b`) the agent copied the prompt's literal example as fake summary
  content and dropped sections from multi-section output. Reworked to generate one
  section per model call, supply headings from the template, enforce JSON shape via
  Ollama structured output (no copyable in-prompt example), and ground the model to
  use only the notes + transcript. (A residual: tiny models still sometimes echo the
  transcript for inapplicable sections instead of "None recorded." — tracked in `backlog.md`.)
- Empty transcript was sent to the agent as JSON `null` (causing a 422); the worker
  now always sends `[]` and the agent tolerates empty/null input.

[Unreleased]: https://github.com/abedegno/muesli/compare/desktop-v0.1.11...main
[0.1.11]: https://github.com/abedegno/muesli/compare/desktop-v0.1.10...desktop-v0.1.11
[0.1.10]: https://github.com/abedegno/muesli/compare/desktop-v0.1.9...desktop-v0.1.10
[0.1.9]: https://github.com/abedegno/muesli/compare/desktop-v0.1.8...desktop-v0.1.9
