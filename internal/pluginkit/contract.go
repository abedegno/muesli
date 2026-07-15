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

// GenerateRequest is the POST /generate body.
//
// SystemPrompt, Model, and Temperature are optional per-template agent
// overrides (see model.Template). They are absent/zero when the resolved
// template has no override set, preserving prior behaviour (the agent falls
// back to its own default system prompt / plugin Config values).
type GenerateRequest struct {
	Transcript    []model.Segment `json:"transcript"`
	NotesMarkdown string          `json:"notes_markdown"`
	Template      TemplatePayload `json:"template"`
	Options       json.RawMessage `json:"options,omitempty"`
	Config        json.RawMessage `json:"config"`
	SystemPrompt  string          `json:"system_prompt,omitempty"`
	Model         string          `json:"model,omitempty"`
	Temperature   *float64        `json:"temperature,omitempty"`
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
