// Server-defined note status progression (Plan 1a/1b). Order matters for UI hints.
export const NOTE_STATUSES = [
  'recording',
  'uploaded',
  'transcribing',
  'summarizing',
  'ready',
  'failed',
] as const

export type NoteStatus = (typeof NOTE_STATUSES)[number]

export interface Note {
  id: string
  owner_id?: string
  title: string
  status: NoteStatus
  pinned?: boolean
  started_at?: string | null
  ended_at?: string | null
  created_at: string
  updated_at: string
  /** Short plain-text body preview; present only in list responses. */
  snippet?: string
  /** Tag names on the note; always an array on list/full responses. */
  tags?: string[]
  /** Ids of folders the note is in; always an array on list/full responses. */
  folder_ids?: string[]
  /** ISO timestamp when the note was trashed; present only in trash responses. */
  deleted_at?: string | null
  /** True when the note's transcript is incomplete due to a mid-stream chunk failure. */
  partial_transcript: boolean
  /** Id of the calendar event this note is linked to, if any (CALLNK01/02). */
  event_id?: string
}

export interface NoteLink {
  id: string
  owner_id: string
  from_note_id: string
  to_note_id: string
  created_at: string
}

export interface NoteLinksResponse {
  outgoing: NoteLink[]
  backlinks: NoteLink[]
}

export interface UploadGrant {
  url: string
  method: 'PUT'
  key: string
  expires_at: string
}

export interface RetranscribeNoteRequest {
  model?: string
  language?: string
}

export interface RetranscribeNoteResponse {
  status: string
}

export interface AudioUrlGrant {
  url: string
  expires_at: string
}

export interface Word {
  text: string
  start_ms: number
  end_ms: number
}

export interface TranscriptSegment {
  // Present on segments returned by the diarization review endpoints (DZ04b);
  // absent on segments embedded in `FullNote.transcript` today. Mirrors
  // `model.Segment.ID` (`json:"id,omitempty"`).
  id?: string
  start_ms: number
  end_ms: number
  text: string
  // v1 records a single mixed mic+system track, so the transcriber reports a
  // single source value (e.g. 'mixed'). True per-source attribution ('mic'/'system')
  // requires recording two separate tracks — deferred to the backlog.
  source: string
  speaker?: string | null
  words?: Word[]
  // Diarization confidence in [0,1], or null when unknown/not diarized. Mirrors
  // `model.Segment.Confidence` (DZ04a/DZ04b).
  confidence?: number | null
}

export interface SummarySection {
  heading: string
  content_markdown: string
  /** 0-based transcript segment indices this section cites; emitted by the server. */
  refs?: number[]
}

interface Summary {
  // Present on real server responses; optional here so lightweight test
  // fixtures don't need to fabricate ids for every summary.
  id?: string
  template_id?: string
  template_name: string
  sections: SummarySection[]
  status: string // 'ready' | 'failed' | …
  // True when the server's truncation heuristic flagged this summary as
  // possibly cut short by a context-window overflow. Optional/absent on older
  // server responses that predate the field — always guard with `?? false`.
  truncated?: boolean
}

export interface FullNote {
  note: Note
  body_markdown: string
  // The server returns `transcript: null` until a transcript exists (e.g. while
  // the note is still recording/uploaded/transcribing). Always guard access.
  transcript: { segments: TranscriptSegment[] } | null
  summaries: Summary[]
}

export type ActionItemStatus = 'open' | 'done'

// Mirrors the server's model.ActionItem (internal/model/model.go).
export interface ActionItem {
  id: string
  note_id: string
  owner_id: string
  text: string
  owner_person_id: string | null
  status: ActionItemStatus
  due_hint: string
  created_at: string
}

// Mirrors the server's model.Decision (internal/model/model.go).
export interface Decision {
  id: string
  note_id: string
  owner_id: string
  text: string
  created_at: string
}

// Connection/credential config persisted on disk (token stored encrypted; see tokenStore).
export interface ServerConfig {
  serverUrl: string
  token: string
}

export interface GoogleOAuthStatus {
  configured: boolean
}

export interface MicrosoftOAuthStatus {
  configured: boolean
}

export type RuleField = 'tag' | 'title' | 'status' | 'created' | 'folder'
export type RuleOperator = 'is' | 'isNot' | 'contains' | 'equals' | 'withinLastDays'
export interface RuleCondition { field: RuleField; operator: RuleOperator; value: string | number }
export interface RuleGroup { op: 'and' | 'or'; children: RuleNode[] }
export type RuleNode = RuleGroup | RuleCondition

export interface SmartList {
  id: string
  name: string
  rule: RuleGroup
  created_at: string
  deleted_at?: string | null
}

export interface Folder {
  id: string
  name: string
  parent_id?: string | null
  created_at: string
  deleted_at?: string | null
}

export interface TemplateSection {
  heading: string
  instruction: string
}
export interface Template {
  id: string
  name: string
  sections: TemplateSection[]
  built_in: boolean
}

// A node is a group iff it has an `op` and a `children` array; otherwise it's a
// condition. Checking both is defensive against malformed/deserialized payloads.
export function isRuleGroup(n: RuleNode): n is RuleGroup {
  return (n as RuleGroup).op !== undefined && Array.isArray((n as RuleGroup).children)
}

export function isReady(note: Pick<Note, 'status'>): boolean {
  return note.status === 'ready'
}

export function isTerminal(status: NoteStatus): boolean {
  return status === 'ready' || status === 'failed'
}

export interface PluginStatus {
  status: 'idle' | 'downloading' | 'ready' | 'unknown'
  percent?: number
  model?: string
}

// A client-facing rename of a raw diarization label (e.g. 'SPEAKER_00' -> 'Alice').
// `speaker_label` is always the ORIGINAL/raw label used as the PUT/DELETE key on
// the server — never a previously-assigned alias name (see DZ03a).
export interface SpeakerAlias {
  note_id: string
  speaker_label: string
  alias_name: string
}

// Mirrors the server's api.SearchMatch (SRC01a/GET /api/search). Results are
// returned in ranked-note order and may contain multiple entries per note
// (e.g. a title hit AND a transcript hit) — entries sharing a note_id are
// consecutive, never interleaved.
export interface SearchMatch {
  note_id: string
  match_type: 'title' | 'transcript' | 'summary'
  /** Set only for match_type === 'transcript'; the segment to jump to. */
  segment_id?: string
  /** Set only for match_type === 'transcript'. */
  start_ms?: number
  /** Context snippet around the match; set for transcript/summary, absent for title. */
  snippet?: string
}

// Diarization review lifecycle (DZ04b/DZ04d). Mirrors the server's
// model.ReviewState* constants.
export type DiarizationReviewState = 'pending' | 'in_review' | 'completed'

// Mirrors `model.DiarizationReview`, the payload returned by
// GET/POST /api/notes/{id}/transcript/review. `turns` are pre-sorted by the
// server ascending by confidence (nulls last) then start_ms — lowest
// confidence first, so the client renders them in the given order as-is.
export interface DiarizationReview {
  note_id: string
  review_state: string
  turns: TranscriptSegment[]
}

// --- Chat (CHT01-CHT05) -----------------------------------------------------
// Mirrors the server's model.Conversation (internal/model/model.go). `note_id`
// is null/absent for a global, cross-note conversation.
export interface Conversation {
  id: string
  owner_id?: string
  note_id?: string | null
  title: string
  model_override?: string | null
  created_at: string
  updated_at: string
}

// Mirrors the server's model.Message. `role` is 'user' | 'assistant' (kept as
// `string` so an unrecognised future role never fails to render).
export interface Message {
  id: string
  conversation_id: string
  role: string
  content: string
  model: string
  tokens_used?: number | null
  created_at: string
}

// Mirrors the server's chat.Source citation (internal/chat/citations.go).
// `n` is the 1-indexed marker rendered inline in the assistant's reply
// (e.g. "[1]"); `timestamp` is milliseconds into the cited note's audio.
export interface ChatSource {
  n: number
  note_id: string
  segment_index: number
  timestamp: number
  snippet: string
}

// --- Calendar (CALUI01) -----------------------------------------------------
// Mirrors the server's model.Attendee (internal/model/calendar.go).
export interface Attendee {
  email: string
  name: string
  response: string
}

// Mirrors the server's model.CalendarEvent (internal/model/calendar.go).
export interface CalendarEvent {
  id: string
  title: string
  starts_at: string
  ends_at: string
  description: string
  location: string
  conferencing_url: string
  attendees: Attendee[]
  source_id: string
}

// Mirrors the server's model.Person (internal/model/model.go).
export interface Person {
  id: string
  primary_email: string
  display_name: string
  company_id?: string
  first_seen_at: string
  updated_at: string
}

// Mirrors the server's model.Company (internal/model/model.go).
export interface Company {
  id: string
  owner_id: string
  domain: string
  name: string
  created_at: string
  updated_at: string
}

export interface PersonWithCompany extends Person {
  company?: Company
}

export interface CompanyWithCount extends Company {
  people_count: number
}

export interface CompanyWithPeople extends Company {
  people: Person[]
}
