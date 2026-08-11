/** Server-produced note states in progression order; renderer uses the order for hints. */
export const NOTE_STATUSES = [
  'draft',
  'recording',
  'uploaded',
  'transcribing',
  'summarizing',
  'ready',
  'failed',
] as const

/** Current server processing state in a note snapshot consumed by the renderer. */
export type NoteStatus = (typeof NOTE_STATUSES)[number]

/** Server-produced note snapshot; optional fields vary by endpoint and processing state. */
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

/** Server-persisted directed edge between two notes, consumed as a snapshot by renderer. */
export interface NoteLink {
  id: string
  owner_id: string
  from_note_id: string
  to_note_id: string
  created_at: string
}

/** Server-ranked related-note result; `score` is relevance, with larger values closer. */
export interface RelatedNote {
  note_id: string
  score: number
}

/** Snapshot separating links created by this note from links targeting this note. */
export interface NoteLinksResponse {
  outgoing: NoteLink[]
  backlinks: NoteLink[]
}

/** Server-produced share grant; null expiry means it remains valid until revoked. */
export interface Share {
  id: string
  token: string
  note_id: string
  owner_id: string
  created_at: string
  expires_at?: string | null
  revoked_at?: string | null
}

/** Renderer-to-main share settings; omitted expiry requests a non-expiring grant. */
export interface CreateShareRequest {
  expires_at?: string
}

/** Newly created server share token and absolute URL returned through main to renderer. */
export interface CreateShareResponse {
  token: string
  url: string
}

/** Time-limited server authorization for main to upload bytes directly with HTTP PUT. */
export interface UploadGrant {
  url: string
  method: 'PUT'
  key: string
  expires_at: string
}

/** Renderer-selected transcription overrides; omissions retain server defaults. */
export interface RetranscribeNoteRequest {
  model?: string
  language?: string
}

/** Immediate server acknowledgement, not a live view of later transcription progress. */
export interface RetranscribeNoteResponse {
  status: string
}

/** Time-limited server URL used by the renderer to play a note's audio. */
export interface AudioUrlGrant {
  url: string
  expires_at: string
}

/** Server transcript token whose offsets are milliseconds from the audio start. */
export interface Word {
  text: string
  start_ms: number
  end_ms: number
}

/** Server transcript turn; offsets are milliseconds and confidence is in `[0, 1]`. */
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

/** Server-generated markdown summary section with snapshot transcript references. */
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

/** Complete server note snapshot returned through main; transcript null means not available yet. */
export interface FullNote {
  note: Note
  body_markdown: string
  // The server returns `transcript: null` until a transcript exists (e.g. while
  // the note is still recording/uploaded/transcribing). Always guard access.
  transcript: { segments: TranscriptSegment[] } | null
  summaries: Summary[]
}

/** Mutable server action-item lifecycle value rendered by the client. */
export type ActionItemStatus = 'open' | 'done'

/** Server action-item snapshot; null owner means the item is currently unassigned. */
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

/** Server-extracted decision snapshot consumed by the renderer. */
export interface Decision {
  id: string
  note_id: string
  owner_id: string
  text: string
  created_at: string
}

/** Main-owned persisted connection config exposed to renderer as a point-in-time snapshot. */
export interface ServerConfig {
  serverUrl: string
  token: string
  manualServer?: boolean
}

/** Server OAuth readiness snapshot; `false` means credentials are not configured. */
export interface GoogleOAuthStatus {
  configured: boolean
}

/** Server OAuth readiness snapshot; `false` means credentials are not configured. */
export interface MicrosoftOAuthStatus {
  configured: boolean
}

/** Server digest settings snapshot; null `last_sent_at` means no digest has been sent. */
export interface DigestConfig {
  owner_id: string
  cadence: 'off' | 'daily' | 'weekly'
  last_sent_at?: string | null
  updated_at?: string
}

/** Server plugin execution category consumed by renderer. */
export type PluginKind = 'transcriber' | 'streaming-transcriber' | 'agent'

/** Server plugin configuration snapshot; optional config is absent when not supplied. */
export interface Plugin {
  id: string
  kind: PluginKind
  name: string
  endpoint_url: string
  enabled: boolean
  is_default: boolean
  config_schema?: Record<string, unknown>
  config?: Record<string, unknown>
}

/** Point-in-time plugin probe result; error is present only for an unhealthy probe. */
export interface PluginHealth {
  healthy: boolean
  error?: string
}

/** Server-supported smart-list field identifier. */
export type RuleField = 'tag' | 'title' | 'status' | 'created' | 'folder'
/** Operator interpreted by the server for a smart-list condition. */
export type RuleOperator = 'is' | 'isNot' | 'contains' | 'equals' | 'withinLastDays'
/** Renderer-authored leaf predicate sent to main and then the server unchanged. */
export interface RuleCondition { field: RuleField; operator: RuleOperator; value: string | number }
/** Renderer-authored recursive boolean group; children may contain further groups. */
export interface RuleGroup { op: 'and' | 'or'; children: RuleNode[] }
/** Recursive wire node accepted and returned by smart-list APIs. */
export type RuleNode = RuleGroup | RuleCondition

/** Server smart-list snapshot; null deletion time is active, a timestamp is trashed. */
export interface SmartList {
  id: string
  name: string
  rule: RuleGroup
  created_at: string
  deleted_at?: string | null
}

/** Server folder snapshot; null/omitted parent is a root folder. */
export interface Folder {
  id: string
  name: string
  parent_id?: string | null
  created_at: string
  deleted_at?: string | null
}

/** Renderer-authored summary heading and model instruction sent through main. */
export interface TemplateSection {
  heading: string
  instruction: string
}

/** Server scheduling phase for a summary template. */
export type TemplatePhase = 'after' | 'pre' | 'during' | 'cross'

/** Server template snapshot consumed and edited by renderer. */
export interface Template {
  id: string
  name: string
  phase: TemplatePhase
  sections: TemplateSection[]
  built_in: boolean
  auto_run: boolean
}

/** Narrows a deserialized rule only when both group discriminators are valid. */
export function isRuleGroup(n: RuleNode): n is RuleGroup {
  return (n as RuleGroup).op !== undefined && Array.isArray((n as RuleGroup).children)
}

/** Reports whether a note snapshot has completed successfully, excluding failures. */
export function isReady(note: Pick<Note, 'status'>): boolean {
  return note.status === 'ready'
}

/** Reports whether server processing has stopped, successfully or permanently failed. */
export function isTerminal(status: NoteStatus): boolean {
  return status === 'ready' || status === 'failed'
}

/** Point-in-time default-transcriber state; percent, when present, is in `[0, 100]`. */
export interface PluginStatus {
  status: 'idle' | 'downloading' | 'ready' | 'unknown'
  percent?: number
  model?: string
}

/** Main-to-renderer startup progress event; percent is `[0, 100]`, null means unknown. */
export interface EmbeddedStartupProgress {
  status: 'progress'
  phase: string
  detail: string
  percent?: number | null
  degraded: boolean
}

/** Main-to-renderer terminal startup event; degraded means usable with reduced capability. */
export interface EmbeddedStartupReady {
  status: 'ready'
  degraded: boolean
}

/** Main-to-renderer terminal startup failure with a main-owned diagnostic log path. */
export interface EmbeddedStartupError {
  status: 'error'
  message: string
  logPath: string
}

/** Live main-to-renderer embedded-server startup event union. */
export type EmbeddedStartupStatus = EmbeddedStartupProgress | EmbeddedStartupReady | EmbeddedStartupError

/**
 * Server-persisted client-facing rename of a raw diarization label.
 * `speaker_label` remains the original label and is never a previous alias.
 */
export interface SpeakerAlias {
  note_id: string
  speaker_label: string
  alias_name: string
}

/**
 * Server search snapshot in ranked-note order; entries for one note are consecutive.
 * Transcript offsets are milliseconds from audio start and absent for other match types.
 */
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

export interface SearchResult {
  matches: SearchMatch[]
  semanticSearchAvailable: boolean
}

/** Server-owned diarization review lifecycle rendered by the client. */
export type DiarizationReviewState = 'pending' | 'in_review' | 'completed'

/**
 * Server review snapshot whose turns are sorted by confidence ascending (nulls last),
 * then by millisecond start offset; renderer must preserve that review order.
 */
export interface DiarizationReview {
  note_id: string
  review_state: string
  turns: TranscriptSegment[]
}

/** Server conversation snapshot; null/absent `note_id` denotes a global conversation. */
export interface Conversation {
  id: string
  owner_id?: string
  note_id?: string | null
  title: string
  model_override?: string | null
  created_at: string
  updated_at: string
}

/** Server chat-message snapshot; role remains open-ended for forward-compatible rendering. */
export interface Message {
  id: string
  conversation_id: string
  role: string
  content: string
  model: string
  tokens_used?: number | null
  created_at: string
}

/** Server citation snapshot; `n` is 1-based and `timestamp` is milliseconds into audio. */
export interface ChatSource {
  n: number
  note_id: string
  segment_index: number
  timestamp: number
  snippet: string
}

/** Server calendar attendee snapshot consumed by renderer. */
export interface Attendee {
  email: string
  name: string
  response: string
}

/** Server calendar-event snapshot; start/end are serialized timestamps, not live values. */
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

/** Server person snapshot; absent company means no company association is loaded. */
export interface Person {
  id: string
  primary_email: string
  display_name: string
  company_id?: string
  first_seen_at: string
  updated_at: string
}

/** Server company snapshot consumed by renderer. */
export interface Company {
  id: string
  owner_id: string
  domain: string
  name: string
  created_at: string
  updated_at: string
}

/** Person snapshot optionally enriched with the associated company object. */
export interface PersonWithCompany extends Person {
  company?: Company
}

/** Company snapshot enriched with a point-in-time number of associated people. */
export interface CompanyWithCount extends Company {
  people_count: number
}

/** Company snapshot enriched with its current server-returned people list. */
export interface CompanyWithPeople extends Company {
  people: Person[]
}

/** Server insight bucket; day is a calendar date and count is meeting count. */
export interface MeetingCountByDay {
  day: string
  count: number
}

/** Server insight bucket; hours are fractional hours for the week-start date. */
export interface MeetingHoursByWeek {
  week_start: string
  hours: number
}

/** Person snapshot enriched with the selected insight window's meeting count. */
export interface PersonWithMeetingCount extends Person {
  count: number
}

/** Company snapshot enriched with the selected insight window's meeting count. */
export interface CompanyWithMeetingCount extends Company {
  count: number
}

/** Folder snapshot enriched with the selected insight window's meeting count. */
export interface FolderWithMeetingCount extends Folder {
  count: number
}

/** Server-computed analytics snapshot for the renderer-selected date window; hours are hours. */
export interface InsightsResponse {
  meetings_per_day: MeetingCountByDay[]
  total_hours: number
  hours_per_week: MeetingHoursByWeek[]
  top_people: PersonWithMeetingCount[]
  top_companies: CompanyWithMeetingCount[]
  top_folders: FolderWithMeetingCount[]
}
