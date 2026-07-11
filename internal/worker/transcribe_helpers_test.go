package worker

import (
	"encoding/json"
	"testing"

	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/model"
)

func TestMergeModelOverride(t *testing.T) {
	t.Parallel()

	got, err := mergeModelOverride(json.RawMessage(`{"foo":"bar"}`), "large-v3")
	if err != nil {
		t.Fatalf("mergeModelOverride: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(got, &cfg); err != nil {
		t.Fatalf("unmarshal merged config: %v", err)
	}
	if cfg["foo"] != "bar" || cfg["model"] != "large-v3" {
		t.Fatalf("merged config = %#v", cfg)
	}

	got, err = mergeModelOverride(nil, "large-v3")
	if err != nil {
		t.Fatalf("mergeModelOverride(nil): %v", err)
	}
	if string(got) != `{"model":"large-v3"}` {
		t.Fatalf("mergeModelOverride(nil) = %s", got)
	}

	unchanged, err := mergeModelOverride(json.RawMessage(`{"foo":"bar"}`), "")
	if err != nil {
		t.Fatalf("mergeModelOverride empty override: %v", err)
	}
	if string(unchanged) != `{"foo":"bar"}` {
		t.Fatalf("mergeModelOverride empty override = %s", unchanged)
	}
}

func TestBuildTranscribeRequestAppliesOverrides(t *testing.T) {
	t.Parallel()

	req, err := buildTranscribeRequest(
		"https://example.test/audio",
		model.Plugin{Config: json.RawMessage(`{"foo":"bar"}`)},
		config.Config{TranscribeLanguage: "de", Diarization: true},
		transcribePayload{Model: "large-v3", Language: "fr"},
	)
	if err != nil {
		t.Fatalf("buildTranscribeRequest: %v", err)
	}
	if req.AudioURL != "https://example.test/audio" {
		t.Fatalf("AudioURL = %q", req.AudioURL)
	}
	if req.LanguageHint != "fr" {
		t.Fatalf("LanguageHint = %q, want fr", req.LanguageHint)
	}
	if string(req.Options) != `{"diarize":true}` {
		t.Fatalf("Options = %s, want diarize flag", req.Options)
	}
	var cfg map[string]any
	if err := json.Unmarshal(req.Config, &cfg); err != nil {
		t.Fatalf("unmarshal request config: %v", err)
	}
	if cfg["foo"] != "bar" || cfg["model"] != "large-v3" {
		t.Fatalf("Config = %#v", cfg)
	}
}
