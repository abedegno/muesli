package pluginkit

import (
	"encoding/json"
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
