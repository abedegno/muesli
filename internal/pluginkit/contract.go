package pluginkit

import (
	"encoding/json"

	"github.com/abedegno/muesli/internal/model"
)

// Info is the common /info envelope.
type Info struct {
	Name         string          `json:"name"`
	Version      string          `json:"version"`
	PluginAPI    int             `json:"plugin_api"`
	Kind         string          `json:"kind"`
	ConfigSchema json.RawMessage `json:"config_schema,omitempty"`
}

// TranscribeRequest is the POST /transcribe body.
type TranscribeRequest struct {
	AudioURL     string          `json:"audio_url"`
	LanguageHint string          `json:"language_hint,omitempty"`
	Options      json.RawMessage `json:"options,omitempty"`
	Config       json.RawMessage `json:"config"`
}

// TranscribeResult is the engine-side transcribe result returned by
// Transcriber implementations.
type TranscribeResult struct {
	Segments   []model.Segment `json:"segments"`
	Language   string          `json:"language"`
	Model      string          `json:"model"`
	DurationMS int             `json:"duration_ms"`
}

// TemplatePayload mirrors the template shape sent to the agent.
type TemplatePayload struct {
	Sections []model.TemplateSection `json:"sections"`
}

// GenerateSourceKind identifies the shape of an optional GenerateRequest.Source.
type GenerateSourceKind string

// GenerateSourceCalendarEvent is the only source kind this slice (issue #763,
// pre-meeting briefs) defines. An engine or the pluginkit HTTP boundary must
// treat any other non-empty kind as a terminal bad request.
const GenerateSourceCalendarEvent GenerateSourceKind = "calendar_event"

// GenerateSource is an optional typed context attached to a /generate
// request, alongside (never instead of) Transcript/NotesMarkdown. It lets the
// same contract serve both transcript-driven after-summary generation
// (Source omitted, wire-compatible with every pre-existing caller) and
// calendar-driven pre-meeting-brief generation (Source present, Transcript
// empty, NotesMarkdown ""). Exactly one typed field below is populated,
// matching Kind.
type GenerateSource struct {
	Kind          GenerateSourceKind   `json:"kind"`
	CalendarEvent *CalendarEventSource `json:"calendar_event,omitempty"`
}

// CalendarEventSource is the normalized calendar-event generation source: only
// stored preparation fields (see the accepted spec's "Input and freshness"
// section) -- never credentials, provider identifiers, or source ids.
type CalendarEventSource struct {
	Title           string                  `json:"title"`
	StartsAt        string                  `json:"starts_at"`
	EndsAt          string                  `json:"ends_at"`
	Description     string                  `json:"description,omitempty"`
	Location        string                  `json:"location,omitempty"`
	ConferencingURL string                  `json:"conferencing_url,omitempty"`
	Attendees       []CalendarEventAttendee `json:"attendees,omitempty"`
}

// CalendarEventAttendee is one attendee within a CalendarEventSource.
type CalendarEventAttendee struct {
	Name     string `json:"name,omitempty"`
	Email    string `json:"email,omitempty"`
	Response string `json:"response,omitempty"`
}

// GenerateRequest is the POST /generate body.
//
// SystemPrompt, Model, and Temperature are optional per-template agent
// overrides (see model.Template). They are absent/zero when the resolved
// template has no override set, preserving prior behaviour (the agent falls
// back to its own default system prompt / plugin Config values).
//
// Source is optional and nil for every pre-existing caller (after-summary,
// chat), which keeps them wire-compatible. See GenerateSource.
type GenerateRequest struct {
	Transcript    []model.Segment `json:"transcript"`
	NotesMarkdown string          `json:"notes_markdown"`
	Template      TemplatePayload `json:"template"`
	Options       json.RawMessage `json:"options,omitempty"`
	Config        json.RawMessage `json:"config"`
	SystemPrompt  string          `json:"system_prompt,omitempty"`
	Model         string          `json:"model,omitempty"`
	Temperature   *float64        `json:"temperature,omitempty"`
	Source        *GenerateSource `json:"source,omitempty"`
}

// SummaryPayload is the produced summary in a /generate reply.
type SummaryPayload struct {
	Sections []model.SummarySection `json:"sections"`
}

// GenerateResponse is the POST /generate reply.
type GenerateResponse struct {
	Summary SummaryPayload `json:"summary"`
	Model   string         `json:"model"`
}
