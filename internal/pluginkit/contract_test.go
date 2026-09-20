package pluginkit

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/model"
)

func TestWireTypesJSON(t *testing.T) {
	info := Info{Name: "n", Version: "v", PluginAPI: 1, Kind: "transcriber", ConfigSchema: json.RawMessage(`{}`)}
	gotInfo, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal info: %v", err)
	}
	if string(gotInfo) != `{"name":"n","version":"v","plugin_api":1,"kind":"transcriber","config_schema":{}}` {
		t.Fatalf("info json = %s", gotInfo)
	}

	trReq := TranscribeRequest{
		AudioURL:     "data:audio/wav;base64,AAAA",
		LanguageHint: "en",
		Options:      json.RawMessage(`{"foo":"bar"}`),
		Config:       json.RawMessage(`{"cfg":true}`),
	}
	gotTRReq, err := json.Marshal(trReq)
	if err != nil {
		t.Fatalf("marshal transcribe request: %v", err)
	}
	if string(gotTRReq) != `{"audio_url":"data:audio/wav;base64,AAAA","language_hint":"en","options":{"foo":"bar"},"config":{"cfg":true}}` {
		t.Fatalf("transcribe request json = %s", gotTRReq)
	}

	// model.Segment.Provisional has no `omitempty` — it is deliberately always
	// on the wire (live-streamed segments vs. batch ones), so every expected
	// JSON literal below that embeds a bare Segment carries "provisional":false.
	genReq := GenerateRequest{
		Transcript:    []model.Segment{{StartMS: 0, EndMS: 1, Text: "hi", Source: "mic"}},
		NotesMarkdown: "- note",
		Template:      TemplatePayload{Sections: []model.TemplateSection{{Heading: "Overview", Instruction: "Summarise."}}},
		Options:       json.RawMessage(`{"temperature":0}`),
		Config:        json.RawMessage(`{"cfg":true}`),
	}
	gotGenReq, err := json.Marshal(genReq)
	if err != nil {
		t.Fatalf("marshal generate request: %v", err)
	}
	if string(gotGenReq) != `{"transcript":[{"start_ms":0,"end_ms":1,"text":"hi","source":"mic","provisional":false}],"notes_markdown":"- note","template":{"sections":[{"heading":"Overview","instruction":"Summarise."}]},"options":{"temperature":0},"config":{"cfg":true}}` {
		t.Fatalf("generate request json = %s", gotGenReq)
	}

	trRes := TranscribeResult{
		Segments:   []model.Segment{{StartMS: 0, EndMS: 1, Text: "hi", Source: "mic"}},
		Language:   "en",
		Model:      "m",
		DurationMS: 1,
	}
	gotTRRes, err := json.Marshal(trRes)
	if err != nil {
		t.Fatalf("marshal transcribe result: %v", err)
	}
	if string(gotTRRes) != `{"segments":[{"start_ms":0,"end_ms":1,"text":"hi","source":"mic","provisional":false}],"language":"en","model":"m","duration_ms":1}` {
		t.Fatalf("transcribe result json = %s", gotTRRes)
	}

	genRes := GenerateResponse{
		Summary: SummaryPayload{Sections: []model.SummarySection{{Heading: "Overview", ContentMarkdown: "Done."}}},
		Model:   "m",
	}
	gotGenRes, err := json.Marshal(genRes)
	if err != nil {
		t.Fatalf("marshal generate response: %v", err)
	}
	if string(gotGenRes) != `{"summary":{"sections":[{"heading":"Overview","content_markdown":"Done."}]},"model":"m"}` {
		t.Fatalf("generate response json = %s", gotGenRes)
	}
}

// TestGenerateRequestOptionalOverridesJSON covers the optional per-template
// agent override fields (SystemPrompt, Model, Temperature): absent when unset
// (preserving prior wire behaviour), present and round-tripping when set.
func TestGenerateRequestOptionalOverridesJSON(t *testing.T) {
	base := GenerateRequest{
		Transcript:    []model.Segment{{StartMS: 0, EndMS: 1, Text: "hi", Source: "mic"}},
		NotesMarkdown: "- note",
		Template:      TemplatePayload{Sections: []model.TemplateSection{{Heading: "Overview", Instruction: "Summarise."}}},
		Config:        json.RawMessage(`{}`),
	}

	unsetJSON, err := json.Marshal(base)
	if err != nil {
		t.Fatalf("marshal unset: %v", err)
	}
	for _, field := range []string{"system_prompt", "\"model\"", "temperature"} {
		if strings.Contains(string(unsetJSON), field) {
			t.Fatalf("unset override field %q must be omitted from wire JSON, got %s", field, unsetJSON)
		}
	}
	var roundTripped GenerateRequest
	if err := json.Unmarshal(unsetJSON, &roundTripped); err != nil {
		t.Fatalf("unmarshal unset: %v", err)
	}
	if roundTripped.SystemPrompt != "" || roundTripped.Model != "" || roundTripped.Temperature != nil {
		t.Fatalf("unset overrides should round-trip as zero values: %+v", roundTripped)
	}

	temp := 0.9
	withOverrides := base
	withOverrides.SystemPrompt = "You are terse."
	withOverrides.Model = "llama3.2:3b"
	withOverrides.Temperature = &temp

	setJSON, err := json.Marshal(withOverrides)
	if err != nil {
		t.Fatalf("marshal set: %v", err)
	}
	var gotSet GenerateRequest
	if err := json.Unmarshal(setJSON, &gotSet); err != nil {
		t.Fatalf("unmarshal set: %v", err)
	}
	if gotSet.SystemPrompt != "You are terse." || gotSet.Model != "llama3.2:3b" || gotSet.Temperature == nil || *gotSet.Temperature != 0.9 {
		t.Fatalf("overrides did not round-trip: %+v", gotSet)
	}
}

// TestGenerateSourceJSON proves the optional Source field is wire-compatible:
// omitted entirely when nil (every pre-existing after-summary/chat caller),
// and round-trips a calendar_event source with its typed attendee list intact.
func TestGenerateSourceJSON(t *testing.T) {
	sourceless := GenerateRequest{
		Transcript: []model.Segment{},
		Template:   TemplatePayload{Sections: []model.TemplateSection{{Heading: "Overview", Instruction: "Summarise."}}},
		Config:     json.RawMessage(`{}`),
	}
	got, err := json.Marshal(sourceless)
	if err != nil {
		t.Fatalf("marshal sourceless: %v", err)
	}
	if strings.Contains(string(got), "\"source\"") {
		t.Fatalf("omitted source must not appear on the wire: %s", got)
	}

	withSource := GenerateRequest{
		Transcript: []model.Segment{},
		Template:   TemplatePayload{Sections: []model.TemplateSection{{Heading: "Overview", Instruction: "Summarise."}}},
		Config:     json.RawMessage(`{}`),
		Source: &GenerateSource{
			Kind: GenerateSourceCalendarEvent,
			CalendarEvent: &CalendarEventSource{
				Title:           "Planning",
				StartsAt:        "2026-09-17T09:00:00Z",
				EndsAt:          "2026-09-17T09:30:00Z",
				Description:     "Sprint planning",
				Location:        "Room 2",
				ConferencingURL: "https://meet.example.com/abc",
				Attendees: []CalendarEventAttendee{
					{Name: "Jane Example", Email: "jane@example.com", Response: "accepted"},
				},
			},
		},
	}
	gotSrc, err := json.Marshal(withSource)
	if err != nil {
		t.Fatalf("marshal with source: %v", err)
	}
	var roundTripped GenerateRequest
	if err := json.Unmarshal(gotSrc, &roundTripped); err != nil {
		t.Fatalf("unmarshal with source: %v", err)
	}
	if roundTripped.Source == nil || roundTripped.Source.Kind != GenerateSourceCalendarEvent {
		t.Fatalf("source did not round trip: %+v", roundTripped.Source)
	}
	if roundTripped.Source.CalendarEvent == nil || roundTripped.Source.CalendarEvent.Title != "Planning" {
		t.Fatalf("calendar_event did not round trip: %+v", roundTripped.Source.CalendarEvent)
	}
	if len(roundTripped.Source.CalendarEvent.Attendees) != 1 || roundTripped.Source.CalendarEvent.Attendees[0].Email != "jane@example.com" {
		t.Fatalf("attendees did not round trip: %+v", roundTripped.Source.CalendarEvent.Attendees)
	}
	// Legacy fields stay empty/omitted on a pre request -- calendar data is
	// never hidden in transcript/notes_markdown.
	if len(withSource.Transcript) != 0 || withSource.NotesMarkdown != "" {
		t.Fatalf("pre request must carry empty transcript/notes_markdown, got %+v", withSource)
	}
}

func TestValidateGenerateSource(t *testing.T) {
	if err := validateGenerateSource(nil); err != nil {
		t.Fatalf("nil source should be valid: %v", err)
	}
	if err := validateGenerateSource(&GenerateSource{Kind: "unknown"}); err == nil {
		t.Fatal("expected an unknown source kind to be rejected")
	}
	if err := validateGenerateSource(&GenerateSource{Kind: GenerateSourceCalendarEvent}); err == nil {
		t.Fatal("expected a calendar_event source with a nil CalendarEvent to be rejected")
	}
	if err := validateGenerateSource(&GenerateSource{
		Kind:          GenerateSourceCalendarEvent,
		CalendarEvent: &CalendarEventSource{Title: "Planning"},
	}); err != nil {
		t.Fatalf("valid calendar_event source rejected: %v", err)
	}
}

// TestGenerateRequestDocumentsJSON round-trips a two-document ordered
// GenerateRequest (issue #765's cross-meeting analysis input) end to end
// through JSON, proving document order, segment order, and per-document
// metadata all survive.
func TestGenerateRequestDocumentsJSON(t *testing.T) {
	req := GenerateRequest{
		Documents: []Document{
			{
				NoteID:               "note-1",
				Title:                "Sprint planning",
				OccurredAt:           "2026-07-01T09:00:00Z",
				TranscriptGeneration: 2,
				Segments: []model.Segment{
					{StartMS: 0, EndMS: 1000, Text: "Let's plan the sprint.", Source: "mic", Speaker: "Alice"},
					{StartMS: 1000, EndMS: 2000, Text: "Sounds good.", Source: "mic", Speaker: "Bob"},
				},
			},
			{
				NoteID:               "note-2",
				Title:                "Retro",
				OccurredAt:           "2026-07-08T09:00:00Z",
				TranscriptGeneration: 1,
				Segments: []model.Segment{
					{StartMS: 0, EndMS: 500, Text: "What went well?", Source: "mic", Speaker: "Alice"},
				},
			},
		},
		Template: TemplatePayload{Sections: []model.TemplateSection{{Heading: "Summary", Instruction: "Summarize."}}},
		Config:   json.RawMessage(`{}`),
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Transcript is left nil (not coerced) when Documents is the chosen form;
	// it still serializes the key (Transcript has no omitempty, matching the
	// legacy "transcript": [] wire invariant) but as JSON null, which is how
	// validateGenerateRequest's exclusivity check tells the two forms apart.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if tr, ok := raw["transcript"]; !ok || string(tr) != "null" {
		t.Fatalf("expected \"transcript\": null when Documents is used, got %s", data)
	}
	if _, ok := raw["documents"]; !ok {
		t.Fatalf("expected \"documents\" present in JSON, got %s", data)
	}

	var got GenerateRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Documents) != 2 {
		t.Fatalf("Documents len = %d, want 2", len(got.Documents))
	}
	if got.Documents[0].NoteID != "note-1" || got.Documents[1].NoteID != "note-2" {
		t.Fatalf("document order not preserved: %+v", got.Documents)
	}
	if len(got.Documents[0].Segments) != 2 || got.Documents[0].Segments[1].Text != "Sounds good." {
		t.Fatalf("segment order not preserved: %+v", got.Documents[0].Segments)
	}
}

func TestValidateGenerateRequestDocumentsExclusivity(t *testing.T) {
	sections := TemplatePayload{Sections: []model.TemplateSection{{Heading: "H", Instruction: "I"}}}
	validDoc := Document{NoteID: "note-1", Segments: []model.Segment{}}

	cases := []struct {
		name    string
		req     GenerateRequest
		wantErr bool
	}{
		{
			name:    "transcript only (legacy)",
			req:     GenerateRequest{Transcript: []model.Segment{}, Template: sections, Config: json.RawMessage(`{}`)},
			wantErr: false,
		},
		{
			name:    "documents only",
			req:     GenerateRequest{Documents: []Document{validDoc}, Template: sections, Config: json.RawMessage(`{}`)},
			wantErr: false,
		},
		{
			name:    "both forms rejected",
			req:     GenerateRequest{Transcript: []model.Segment{}, Documents: []Document{validDoc}, Template: sections, Config: json.RawMessage(`{}`)},
			wantErr: true,
		},
		{
			name:    "neither form rejected",
			req:     GenerateRequest{Template: sections, Config: json.RawMessage(`{}`)},
			wantErr: true,
		},
		{
			name: "duplicate document note ids rejected",
			req: GenerateRequest{
				Documents: []Document{validDoc, validDoc},
				Template:  sections, Config: json.RawMessage(`{}`),
			},
			wantErr: true,
		},
		{
			name: "blank document note id rejected",
			req: GenerateRequest{
				Documents: []Document{{NoteID: "  ", Segments: []model.Segment{}}},
				Template:  sections, Config: json.RawMessage(`{}`),
			},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := validateGenerateRequest(tc.req)
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}
