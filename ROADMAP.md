# Muesli Roadmap

This is a **directional** roadmap — themes and milestones, not committed dates.
It charts two arcs:

1. **Close the gap to Granola** — match the consumer/prosumer experience that
   makes a meeting-notes app delightful to use every day, while keeping Muesli's
   privacy and self-hosting advantages.
2. **Go beyond, into the enterprise** — the multi-user, security, and scale
   functionality an organization needs to deploy Muesli across its workforce.

Milestones are sequenced by dependency and value, not locked to a calendar. Each
lists a **goal**, the **headline work**, and **exit criteria** (what "done"
means). The fine-grained, always-current task list lives internally; this
document is the high-level map above it.

> **Legend:** ✅ shipped · 🔜 next · 📋 planned · 🔭 exploratory

---

## v1 — Foundation ✅ (shipped)

**Goal:** a working end-to-end thin slice, self-hosted and private by default.

- Self-hosted Go server: notes, auth, presigned upload, Postgres-backed job queue
- Language-agnostic HTTP plugin contract (transcribe / generate)
- Default **self-hosted** plugins: Whisper transcriber + Ollama agent (+ conformance suite)
- Electron desktop client: mixed mic+system capture → upload → view
- Embedded admin UI (setup, plugin registry, job monitor)
- One-command `docker compose up` quickstart

**Exit criteria (met):** record a meeting → transcript + clean summary, stored
and viewable, with nothing leaving infrastructure you control.

---

## v2 — A product you'd choose 🔜

**Theme: close the _core UX_ gap.** Make single-user Muesli something you'd pick
over Granola for daily use — on privacy _and_ feel.

**Goal:** the day-to-day capture-and-review loop is fast, clean, and pleasant.

- ✅ **Clean, Granola-grade UI** (shipped) — sidebar shell, live-notes TipTap
  editor, enhanced-summary-primary note view, light/dark design system, and the
  full capture→transcribe→summarize loop, all on a tokenized Tailwind/Radix system.
- ✅ **Speaker diarization** (shipped) — optional per-segment speaker + confidence
  (Whisper transcriber plugin: `WHISPER_DIARIZATION_ENABLED`, `WHISPER_DIARIZATION_HF_TOKEN`,
  `WHISPER_DIARIZATION_MODEL`); the transcript view groups segments into speaker turns, a
  per-note speaker alias lets you rename a raw label, and a diarization review step
  (pending → in review → completed) holds note finalization/summarization until you've
  confirmed the labels. Resolved speakers carry through to export. Deferred: a cross-note
  speaker identity directory (aliases are per-note only).
- 🔜 **Note organization** — **tags + smart lists + folders shipped**. Tags: note-level,
  many-to-many, sidebar single-select filtering. Smart lists: saved boolean rules
  (AND/OR over tag/title/status/created) with a live-count sidebar section and a
  rule editor, plus auto-detected recurring meetings (normalized-title clusters
  suggested as one-click lists). Folders: manual many-to-many collections with a
  FolderBar on the note, drag-onto-folder filing, and a live-count sidebar section.
  Next within this sub-project: folder nesting/sharing; then search/browse polish.
- ✅ **Collapsible, resizable sidebar** (shipped) — drag-resize (200–420px) and a collapse-to-rail
  toggle (⌘\\); width + collapsed state persisted in `localStorage`.
- ✅ **Drag folders to re-parent** (shipped) — drag a folder onto another to nest, or onto the
  Folders header for top level (cycle/depth-guarded). Deferred: sibling reordering (`position` column).
- ✅ **Semantic search (self-hosted embeddings)** (shipped) — pgvector `note_embeddings`, self-hosted Ollama
  `nomic-embed-text` embeddings on ready + backfill, `GET /api/search` hybrid (cosine + lexical),
  additive client integration. Config-gated; lexical-only when disabled. Dimension-aware embeddings:
  `MUESLI_EMBEDDINGS_DIM` config, dim-segregated storage, admin surfacing, task prefixes, and `muesli reembed`
  command all shipped. Model/dim are pluggable (switch via config + reembed).
- ✅ **Recycle bin (soft delete)** (shipped) — deleting a **note or folder** moves it to Trash
  (soft `deleted_at`, excluded from all normal queries; folder delete trashes the whole subtree,
  children/memberships/audio kept); a sidebar Trash view (Notes + Folders sections) restores or
  permanently deletes, and a background job auto-purges after 30 days.
- ✅ **User-editable summary templates ("recipes")** (shipped) — author/edit/delete
  your own templates (section heading + instruction); summarization fans out over
  built-ins + your templates; a Templates manager in Settings. Deferred: the
  per-note template switcher / regenerate-on-demand.
- ✅ **Product hardening** (shipped) — argon2 PHC, login timing, upload cap (including the
  content-type allowlist and the streaming over-cap path), malformed-ID 404s, Ollama→502, AGPL,
  **Electron 31→42** clearing the runtime advisories, **HTTPS connect guardrail** blocking
  plain-HTTP to remote servers. Plus: **duplicate-audio detection** (SHA-256 content hash on
  upload, a dedup-check endpoint warns you before re-importing a recording you already have),
  a bulk **export all notes** action (zips one Markdown file per note), and a **more durable
  job queue** — beyond the baseline lease, the worker now recovers `running` jobs orphaned by
  a crashed worker both at startup and on a recurring sweep.
- ✅ **Capture & review craft** (shipped, closing the meetily gaps) — **audio import**
  (transcribe an uploaded file without live capture), **transcript↔audio playback sync**
  (audio player + click-a-line-to-seek), **DOCX/PDF export**, and **search-&-replace**
  across the note. Re-transcription ("Enhance") ships **server-side** (`/api/notes/{id}/retranscribe`
  with model/language overrides); the only remainder is a client button to trigger it.
  Remaining craft gaps: a **client provider/model picker** + BYO OpenAI-compatible endpoint
  UX (admin plugin config already exists), and **pluggable transcription engines** beyond
  Whisper (e.g. Parakeet). See backlog "Competitive gaps (vs meetily)".

**Exit criteria:** a privacy-minded individual would happily switch from Granola
and not miss the core experience.

---

## v3 — Context & knowledge 🔜

**Theme: close the _intelligence_ gap.** Granola feels smart because it knows
your calendar, your people, and your past notes. Match that. (Calendar + Chat/RAG
shipped; People/attendees, live transcription, and the web app remain.)

**Goal:** Muesli understands meeting context, not just audio.

- ✅ **Calendar integration** (shipped 2026-07-10) — connect **CalDAV/ICS, Google, or
  Microsoft** calendars (read-only, server-side sync into normalized `calendar_events`),
  a "Coming up" view, manual note↔event linking (`notes.event_id`), and client-side
  meeting **detection + opt-in auto-record**. Deferred: pre-meeting-brief "Recipes" (own spec).
- 📋 **People / attendees** — model participants and contacts as a first-class entity
  (today attendees are only inline JSONB on `calendar_events`); attribute and enrich.
  (Speaker diarization already ships in v2 — resolved speakers feed this.) **Next arc.**
- 📋 **Live / streaming transcription** — real-time partial results during the
  meeting (vs. v1 batch); **promoted from exploratory** — it's table-stakes for the
  Granola/meetily feel. Needs a streaming plugin variant + a live client surface.
- ✅ **Chat / RAG over your notes** (shipped) — cross-note retrieval + agent chat with
  segment citations (`internal/chat/retrieval.go`, `NoteChatPanel`), against your own LLM.
- 📋 **End-user web app** — browse, search, and organize notes in a browser, not
  just capture on the desktop.

**Exit criteria:** Muesli connects each meeting to its context and lets you query
your own knowledge base — privately.

---

## v4 — Teams & collaboration 📋

**Theme: from single-user to shared.** The data model already carries `owner_id`,
so this is largely additive.

**Goal:** a team can use Muesli together.

- 📋 **Multi-user + sharing/permissions** — accounts, per-note ACLs, workspaces.
- 📋 **Sharing** — share a note or a link; control visibility.
- 📋 **Collaborative editing** — CRDT (Yjs-style) note bodies for real-time
  multi-user editing.
- 📋 **Richer admin console** — user management, usage metrics, retention-policy UI.

**Exit criteria:** multiple people in a workspace can capture, share, and
co-edit notes with sensible access control.

---

## v5 — Enterprise & scale 📋

**Theme: deploy across an organization.** Identity, operations, integration, and
the guarantees a business requires.

**Goal:** an org can run Muesli for its whole workforce with confidence.

- 📋 **SSO** — SAML / OIDC alongside local accounts.
- 📋 **HA/DR + horizontal scaling** — multi-replica server, managed/HA Postgres,
  autoscaled plugins.
- 📋 **Externalized job queue** — move off the Postgres-table queue to
  Redis/Kafka/etc. when scale demands.
- 📋 **Governance** — audit logging, configurable retention, role-based access,
  data-residency and encryption-at-rest options.
- 🔜 **Public / integration API + webhooks** — **first slice shipped**: a
  webhook subscription fires a `note.completed` event (note id, title, summary, speaker-tagged
  transcript segments) when a note finishes, delivered with SSRF-guarded, retried-with-backoff
  delivery and an admin **Webhook deliveries** status view (attempts, last error, next retry,
  manual retry). Remaining: self-serve webhook registration (a CRUD endpoint/UI), a documented
  public external API, and OAuth/API keys so calendars, CRMs, and automations integrate.

**Exit criteria:** an IT/security team can adopt, operate, secure, and integrate
Muesli at organizational scale.

---

## Cross-cutting / exploratory 🔭

These don't belong to a single milestone — they land when their dependencies and
demand line up:

- **Live / streaming transcription** — **promoted into v3** (real-time partial
  results; needs a streaming plugin variant). Was exploratory; moved up because it
  is table-stakes for the Granola/meetily feel.
- 🔭 **Mobile client** (iOS/Android) — viewing first, capture later.
- 🔭 **Client-side plugin execution** — run transcription on the device so raw
  audio never leaves the laptop at all (a privacy _deepening_ beyond self-hosting).
- 🔭 **gRPC / streaming plugin protocol** and **resumable/multipart upload** —
  revisit when streaming or very long recordings demand them.

---

## How this maps to Granola

| Granola capability                       | Muesli milestone                            |
| ---------------------------------------- | ------------------------------------------- |
| Clean capture + note UX                  | **v2**                                      |
| Folders / smart lists                    | **v2**                                      |
| Editable summary templates ("recipes")   | **v2**                                      |
| Speaker-aware transcripts                | **v2**                                      |
| Calendar-aware notes, pre-meeting briefs | **v3**                                      |
| People / attendees                       | **v3**                                      |
| Chat over your notes                     | **v3**                                      |
| Web access to notes                      | **v3**                                      |
| Sharing & collaboration                  | **v4**                                      |
| Org SSO / admin / scale                  | **v5** _(beyond Granola's self-host story)_ |
| Webhooks / integrations                  | **v5**                                      |

Throughout, Muesli's differentiator holds: **self-hosted-by-default transcription and
inference, self-hosted, with your meeting content never required to leave
infrastructure you control.**
