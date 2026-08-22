package model

import (
	"encoding/json"
	"time"
)

// Note status values: draft→recording→uploaded→transcribing→summarizing→ready/failed.
const (
	NoteDraft        = "draft"
	NoteRecording    = "recording"
	NoteUploaded     = "uploaded"
	NoteTranscribing = "transcribing"
	NoteSummarizing  = "summarizing"
	NoteReady        = "ready"
	NoteFailed       = "failed"
)

const (
	ActionItemOpen = "open"
	ActionItemDone = "done"
)

const (
	DigestCadenceOff    = "off"
	DigestCadenceDaily  = "daily"
	DigestCadenceWeekly = "weekly"
)

type Tag struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// TagCount is a tag name paired with the number of live notes carrying it.
// Returned by the server-side tag list (GET /api/tags) so the client need not
// derive counts from the full note set.
type TagCount struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

type Note struct {
	ID                  string     `json:"id"`
	OwnerID             string     `json:"owner_id"`
	Title               string     `json:"title"`
	Status              string     `json:"status"`
	Pinned              bool       `json:"pinned"`
	StartedAt           *time.Time `json:"started_at,omitempty"`
	EndedAt             *time.Time `json:"ended_at,omitempty"`
	AudioObjectKey      string     `json:"-"`
	AudioHash           *string    `json:"audio_hash,omitempty"`
	NormalizedAudioHash *string    `json:"normalized_audio_hash,omitempty"`
	RetentionState      string     `json:"-"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	DeletedAt           *time.Time `json:"deleted_at,omitempty"`
	// Snippet is a short plain-text preview of the note body, populated only by
	// list queries (empty on single-note fetches). Not stored — derived on read.
	Snippet string `json:"snippet,omitempty"`
	// Tags are the note's tag names. Always serialized as an array (never null);
	// populated on list + full responses, empty elsewhere. Not a stored column.
	Tags []string `json:"tags"`
	// FolderIDs are the ids of folders the note is in. Always an array (never null).
	FolderIDs []string `json:"folder_ids"`
	// PartialTranscript is true when the note's transcript is incomplete because a
	// mid-stream chunk failed during transcription. False after a successful retry.
	// Deliberately NOT omitempty so the JSON always serializes false explicitly.
	PartialTranscript bool `json:"partial_transcript"`
	// EventID is the id of the calendar event this note is linked to, if any.
	EventID *string `json:"event_id,omitempty"`
}

// NoteLink is an explicit directed link between two notes.
type NoteLink struct {
	ID         string    `json:"id"`
	OwnerID    string    `json:"owner_id"`
	FromNoteID string    `json:"from_note_id"`
	ToNoteID   string    `json:"to_note_id"`
	CreatedAt  time.Time `json:"created_at"`
}

// Share is a revocable read-only token for a note.
type Share struct {
	ID        string     `json:"id"`
	Token     string     `json:"token"`
	NoteID    string     `json:"note_id"`
	OwnerID   string     `json:"owner_id"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

// ActionItem is a structured action item extracted from a note.
type ActionItem struct {
	ID            string    `json:"id"`
	NoteID        string    `json:"note_id"`
	OwnerID       string    `json:"owner_id"`
	Text          string    `json:"text"`
	OwnerPersonID *string   `json:"owner_person_id"`
	Status        string    `json:"status"`
	DueHint       string    `json:"due_hint"`
	CreatedAt     time.Time `json:"created_at"`
}

// DigestConfig stores the per-owner digest cadence and delivery checkpoint.
type DigestConfig struct {
	OwnerID    string     `json:"owner_id"`
	Cadence    string     `json:"cadence"`
	LastSentAt *time.Time `json:"last_sent_at,omitempty"`
	UpdatedAt  time.Time  `json:"updated_at,omitempty"`
}

// Decision is a structured decision extracted from a note.
type Decision struct {
	ID        string    `json:"id"`
	NoteID    string    `json:"note_id"`
	OwnerID   string    `json:"owner_id"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// Plugin kinds.
const (
	PluginTranscriber          = "transcriber"
	PluginStreamingTranscriber = "streaming-transcriber"
	PluginAgent                = "agent"
)

// Note statuses used by the pipeline. NoteReady/NoteTranscribing/NoteSummarizing
// come from the foundation (Plan 1a). The terminal NoteFailed (value "failed")
// is also defined by the foundation above and reused here for the
// transcribe-failure path — not redeclared.

// Job types.
const (
	JobTranscribe = "transcribe"
	JobSummarize  = "summarize"
	JobEmbed      = "embed"
)

// Job statuses.
const (
	JobPending   = "pending"
	JobRunning   = "running"
	JobDone      = "done"
	JobFailed    = "failed"
	JobCancelled = "cancelled"
)

// Summary statuses.
const (
	SummaryPending = "pending"
	SummaryReady   = "ready"
	SummaryFailed  = "failed"
)

// Plugin is a registered transcriber or agent service. Config holds JSON that
// may include secrets; it is stored encrypted (see internal/crypto) and only
// decrypted at call time, so this struct's Config is the *plaintext* form used
// in memory and over the admin API — never written to the DB in the clear.
type Plugin struct {
	ID           string          `json:"id"`
	Kind         string          `json:"kind"`
	Name         string          `json:"name"`
	EndpointURL  string          `json:"endpoint_url"`
	Token        string          `json:"-"` // server→plugin bearer token (secret)
	Config       json.RawMessage `json:"config"`
	ConfigSchema json.RawMessage `json:"config_schema"`
	Enabled      bool            `json:"enabled"`
	IsDefault    bool            `json:"is_default"`
}

// Job is one unit of pipeline work.
type Job struct {
	ID        string `json:"id"`
	NoteID    string `json:"note_id"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	Attempts  int    `json:"attempts"`
	LastError string `json:"last_error,omitempty"`
	// Priority orders pending/reclaimable jobs within ClaimJob's dequeue: higher
	// values are claimed first, ties broken FIFO by created_at. Defaults to 0;
	// bumped by BumpNoteJobPriority ("process next") for pending jobs only.
	Priority int `json:"priority"`
	// StartedAt/FinishedAt track only the most recent attempt: ClaimJob resets
	// StartedAt=now() and FinishedAt=nil on every (re)claim, so a retried job's
	// timeline reflects its latest run, not history across attempts.
	StartedAt  *time.Time      `json:"started_at,omitempty"`
	FinishedAt *time.Time      `json:"finished_at,omitempty"`
	Payload    json.RawMessage `json:"-"`
}

// Word is a single word with its timing within a segment.
type Word struct {
	Text    string `json:"text"`
	StartMS int    `json:"start_ms"`
	EndMS   int    `json:"end_ms"`
}

// Segment is a time-coded transcript chunk.
type Segment struct {
	ID         string   `json:"id,omitempty"`
	StartMS    int      `json:"start_ms"`
	EndMS      int      `json:"end_ms"`
	Text       string   `json:"text"`
	Source     string   `json:"source"`
	Speaker    string   `json:"speaker,omitempty"`
	Words      []Word   `json:"words,omitempty"`
	Confidence *float64 `json:"confidence,omitempty"`
	// Boundary is why this segment ended: "" natural, "forced" when a full
	// window forced a commit. No consumer joins segments across a forced
	// boundary — a forced commit can bisect a word.
	Boundary string `json:"boundary,omitempty"`
	// Provisional marks a segment written by a live stream, which batch
	// transcription replaces wholesale.
	Provisional bool `json:"provisional"`
}

// Transcript is one transcription run plus its segments.
type Transcript struct {
	ID                string    `json:"id"`
	NoteID            string    `json:"note_id"`
	TranscriberPlugin string    `json:"transcriber_plugin"`
	Model             string    `json:"model"`
	Segments          []Segment `json:"segments"`
	ReviewState       string    `json:"review_state"`
	// StreamID is set when a live stream authored this transcript, nil for a
	// batch-authored one. Live writes assert it, so a superseded stream cannot
	// write into the transcript that replaced it.
	StreamID *string `json:"stream_id,omitempty"`
	// Sealed marks a transcript no live stream may write to.
	Sealed bool `json:"sealed"`
	// Generation increments on every replacement. Mutators carry the value they
	// were rendered from and fail on mismatch. 0 means "no transcript".
	Generation int `json:"generation"`
	// Gaps are intervals of audio that never reached a transcriber, ordered by
	// StartSample. Nil DroppedSamples means open-ended.
	Gaps []TranscriptGap `json:"gaps,omitempty"`
}

// TranscriptGap is an interval of audio that never reached a transcriber.
// DroppedSamples is nil for an open-ended interval — one whose end is unknown
// because the participant that would have closed it died.
type TranscriptGap struct {
	ID             string `json:"id,omitempty"`
	TranscriptID   string `json:"transcript_id,omitempty"`
	StreamID       string `json:"stream_id"`
	StartSample    int64  `json:"start_sample"`
	DroppedSamples *int64 `json:"dropped_samples,omitempty"`
	Origin         string `json:"origin"`
}

// SummarySection is one rendered section of a summary.
type SummarySection struct {
	Heading         string `json:"heading"`
	ContentMarkdown string `json:"content_markdown"`
	// Refs are positional indices into the transcript segment array sent to the
	// agent (0-based), grounding a generated line back to its evidence. Integer
	// indices (not segment IDs) so an LLM can emit them reliably.
	Refs []int `json:"refs,omitempty"`
}

// Summary is one agent-produced panel for a note.
type Summary struct {
	ID           string           `json:"id"`
	NoteID       string           `json:"note_id"`
	TemplateID   string           `json:"template_id"`
	TemplateName string           `json:"template_name"`
	AgentPlugin  string           `json:"agent_plugin"`
	Model        string           `json:"model"`
	Status       string           `json:"status"`
	Sections     []SummarySection `json:"sections"`
	// Truncated flags a summary that looks 'ready' but may have been cut short by
	// the model's context window overflowing on a long transcript — distinct from
	// Status, which only tracks pending/ready/failed. See internal/worker's
	// DetectTruncation heuristic for how this is computed.
	Truncated bool `json:"truncated"`
}

// Folder is an owner-scoped named container for notes.
type Folder struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	ParentID  *string    `json:"parent_id"`
	CreatedAt time.Time  `json:"created_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
	// NoteCount is the total number of live (non-deleted) notes in this folder
	// and all of its descendants, computed server-side by ListFolders.
	NoteCount int `json:"note_count"`
}

// SmartList is a saved rule-based note view. Rule is an opaque JSON boolean tree
// (validated for shape on write; evaluated client-side).
type SmartList struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Rule      json.RawMessage `json:"rule"`
	CreatedAt time.Time       `json:"created_at"`
	DeletedAt *time.Time      `json:"deleted_at,omitempty"`
}

// TemplateSection is one section recipe of a summary template.
type TemplateSection struct {
	Heading     string `json:"heading"`
	Instruction string `json:"instruction"`
}

// Template is a summary recipe.
//
// SystemPrompt, Model, and Temperature are optional per-template agent
// overrides threaded through the /generate contract (pluginkit.GenerateRequest
// / plugin.GenerateRequest) to the agent plugin. An empty SystemPrompt/Model or
// a nil Temperature means "unset" — the agent falls back to its own default
// system prompt / plugin Config values.
type Template struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Phase        string            `json:"phase"`
	Sections     []TemplateSection `json:"sections"`
	BuiltIn      bool              `json:"built_in"`
	AutoRun      bool              `json:"auto_run"`
	SystemPrompt string            `json:"system_prompt,omitempty"`
	Model        string            `json:"model,omitempty"`
	Temperature  *float64          `json:"temperature,omitempty"`
}

// SpeakerAlias maps a raw speaker label from a transcript segment to a
// human-readable alias name, scoped to a note and owner. Substitution happens
// at read time only — the stored transcript_segments rows are never modified.
type SpeakerAlias struct {
	NoteID       string  `json:"note_id"`
	PersonID     *string `json:"person_id,omitempty"`
	SpeakerLabel string  `json:"speaker_label"`
	AliasName    string  `json:"alias_name"`
}

// Webhook delivery statuses.
const (
	DeliveryPending   = "pending"
	DeliveryInFlight  = "in_flight"
	DeliveryDelivered = "delivered"
	DeliveryFailed    = "failed"

	// Aliases kept for backward compatibility with existing store tests.
	DeliveryStatusPending   = DeliveryPending
	DeliveryStatusInFlight  = DeliveryInFlight
	DeliveryStatusDelivered = DeliveryDelivered
	DeliveryStatusFailed    = DeliveryFailed
)

// Webhook is a user-configured outbound HTTP endpoint subscription.
type Webhook struct {
	ID        string    `json:"id"`
	OwnerID   string    `json:"owner_id"`
	URL       string    `json:"url"`
	Secret    string    `json:"-"` // never serialised to clients
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// WebhookDelivery records one outbound HTTP delivery attempt for a webhook.
type WebhookDelivery struct {
	ID            string     `json:"id"`
	WebhookID     string     `json:"webhook_id"`
	Payload       []byte     `json:"payload"`
	Status        string     `json:"status"`
	Attempts      int        `json:"attempts"`
	MaxAttempts   int        `json:"max_attempts"`
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// ReviewState constants for diarization review lifecycle.
const (
	ReviewStatePending   = "pending"
	ReviewStateInReview  = "in_review"
	ReviewStateCompleted = "completed"
)

// Deterministic speaker labels produced by channel-based multitrack attribution
// (the mic channel is the local user, the system channel is the other side).
// These are ground truth, so they do not trigger the diarization review gate.
const (
	SpeakerYou  = "You"
	SpeakerThem = "Them"
)

// DiarizationReview is the payload returned by the diarization review
// endpoints. Turns are sorted ascending by confidence (NULLs last) then by
// start_ms, so the lowest-confidence segments appear first for human review.
type DiarizationReview struct {
	NoteID      string    `json:"note_id"`
	ReviewState string    `json:"review_state"`
	Turns       []Segment `json:"turns"`
}

// Conversation is an owner-scoped chat thread, optionally attached to a note
// (NoteID nil means the conversation is global / cross-note in scope).
type Conversation struct {
	ID            string    `json:"id"`
	OwnerID       string    `json:"owner_id"`
	NoteID        *string   `json:"note_id,omitempty"`
	Title         string    `json:"title"`
	ModelOverride *string   `json:"model_override,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Message is one turn within a Conversation.
type Message struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	Role           string    `json:"role"`
	Content        string    `json:"content"`
	Model          string    `json:"model"`
	TokensUsed     *int      `json:"tokens_used,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}
