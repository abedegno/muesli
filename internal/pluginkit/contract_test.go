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
	if string(gotGenReq) != `{"transcript":[{"start_ms":0,"end_ms":1,"text":"hi","source":"mic"}],"notes_markdown":"- note","template":{"sections":[{"heading":"Overview","instruction":"Summarise."}]},"options":{"temperature":0},"config":{"cfg":true}}` {
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
	if string(gotTRRes) != `{"segments":[{"start_ms":0,"end_ms":1,"text":"hi","source":"mic"}],"language":"en","model":"m","duration_ms":1}` {
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
