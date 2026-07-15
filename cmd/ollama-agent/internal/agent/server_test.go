package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/pluginkit"
)

func TestGenerateMultiSectionAndNilTranscript(t *testing.T) {
	t.Parallel()

	var calls int32
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/chat" {
			t.Fatalf("path = %q, want /api/chat", r.URL.Path)
		}

		var req struct {
			Model    string `json:"model"`
			Stream   bool   `json:"stream"`
			Format   string `json:"format"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			Options map[string]any `json:"options"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		atomic.AddInt32(&calls, 1)
		if req.Model != "test-model" {
			t.Fatalf("model = %q, want test-model", req.Model)
		}
		if req.Format != "json" {
			t.Fatalf("format = %q, want json", req.Format)
		}
		if got := req.Options["temperature"]; got != 0.7 {
			t.Fatalf("temperature = %v, want 0.7", got)
		}
		prompt := req.Messages[len(req.Messages)-1].Content

		var content string
		switch {
		case strings.Contains(prompt, "Heading: Summary"):
			content = `{"content_markdown":"summary body","refs":[0,1]}`
		case strings.Contains(prompt, "Heading: Decisions"):
			content = `{"content_markdown":"decisions body","refs":[1]}`
		default:
			content = `{"content_markdown":"fallback body"}`
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "test-model",
			"message": map[string]any{
				"content": content,
			},
		})
	}))
	defer ollama.Close()

	eng := New(Config{
		OllamaURL:   ollama.URL,
		Model:       "fallback-model",
		Temperature: 0.2,
	})

	reqBody := pluginkit.GenerateRequest{
		Transcript:    nil,
		NotesMarkdown: "notes",
		Template: pluginkit.TemplatePayload{Sections: []model.TemplateSection{
			{Heading: "Summary", Instruction: "Summarize the conversation."},
			{Heading: "Decisions", Instruction: "List decisions."},
		}},
		Options: json.RawMessage(`{"temperature":0.7}`),
		Config:  json.RawMessage(`{"model":"test-model","ollama_url":"` + ollama.URL + `","temperature":0.7}`),
	}

	resp, err := eng.Generate(context.Background(), reqBody)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Model != "test-model" {
		t.Fatalf("model = %q, want test-model", resp.Model)
	}
	if len(resp.Summary.Sections) != 2 {
		t.Fatalf("sections = %d, want 2", len(resp.Summary.Sections))
	}
	if resp.Summary.Sections[0].Heading != "Summary" || resp.Summary.Sections[0].ContentMarkdown != "summary body" {
		t.Fatalf("section 0 = %+v", resp.Summary.Sections[0])
	}
	if len(resp.Summary.Sections[0].Refs) != 2 || resp.Summary.Sections[0].Refs[0] != 0 || resp.Summary.Sections[0].Refs[1] != 1 {
		t.Fatalf("section 0 refs = %+v", resp.Summary.Sections[0].Refs)
	}
	if resp.Summary.Sections[1].Heading != "Decisions" || resp.Summary.Sections[1].ContentMarkdown != "decisions body" {
		t.Fatalf("section 1 = %+v", resp.Summary.Sections[1])
	}
	if len(resp.Summary.Sections[1].Refs) != 1 || resp.Summary.Sections[1].Refs[0] != 1 {
		t.Fatalf("section 1 refs = %+v", resp.Summary.Sections[1].Refs)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("ollama calls = %d, want 2", got)
	}
}

// TestGenerateUsesDefaultSystemPromptWhenUnset verifies that req.SystemPrompt
// empty (the common case today) still sends the agent's hardcoded default
// system prompt, i.e. current behaviour is unchanged.
func TestGenerateUsesDefaultSystemPromptWhenUnset(t *testing.T) {
	t.Parallel()

	var gotSystem string
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotSystem = req.Messages[0].Content
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]any{"content": `{"content_markdown":"ok"}`},
		})
	}))
	defer ollama.Close()

	eng := New(Config{OllamaURL: ollama.URL, Model: "fallback-model"})
	reqBody := pluginkit.GenerateRequest{
		Template: pluginkit.TemplatePayload{Sections: []model.TemplateSection{
			{Heading: "Summary", Instruction: "Summarize."},
		}},
		Config: json.RawMessage(`{"ollama_url":"` + ollama.URL + `"}`),
	}

	if _, err := eng.Generate(context.Background(), reqBody); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if gotSystem != systemPrompt() {
		t.Fatalf("system message = %q, want default systemPrompt(): %q", gotSystem, systemPrompt())
	}
}

// TestGenerateUsesRequestSystemPromptOverride verifies that a non-empty
// req.SystemPrompt (from a template override) replaces the hardcoded default
// system prompt in the outbound Ollama chat call.
func TestGenerateUsesRequestSystemPromptOverride(t *testing.T) {
	t.Parallel()

	const override = "You are a terse bullet-point summarizer. Never use full sentences."
	var gotSystem string
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotSystem = req.Messages[0].Content
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]any{"content": `{"content_markdown":"ok"}`},
		})
	}))
	defer ollama.Close()

	eng := New(Config{OllamaURL: ollama.URL, Model: "fallback-model"})
	reqBody := pluginkit.GenerateRequest{
		Template: pluginkit.TemplatePayload{Sections: []model.TemplateSection{
			{Heading: "Summary", Instruction: "Summarize."},
		}},
		Config:       json.RawMessage(`{"ollama_url":"` + ollama.URL + `"}`),
		SystemPrompt: override,
	}

	if _, err := eng.Generate(context.Background(), reqBody); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if gotSystem != override {
		t.Fatalf("system message = %q, want override %q", gotSystem, override)
	}
	if gotSystem == systemPrompt() {
		t.Fatalf("system message unexpectedly matches the hardcoded default")
	}
}

// TestGenerateRequestModelAndTemperatureOverridePluginConfig verifies that
// req.Model and req.Temperature (from a template override) win over the
// plugin's Config defaults in the actual outbound Ollama call.
func TestGenerateRequestModelAndTemperatureOverridePluginConfig(t *testing.T) {
	t.Parallel()

	var gotModel string
	var gotTemp any
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model   string         `json:"model"`
			Options map[string]any `json:"options"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotModel = req.Model
		gotTemp = req.Options["temperature"]
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   req.Model,
			"message": map[string]any{"content": `{"content_markdown":"ok"}`},
		})
	}))
	defer ollama.Close()

	eng := New(Config{OllamaURL: ollama.URL, Model: "fallback-model", Temperature: 0.2})
	temp := 0.9
	reqBody := pluginkit.GenerateRequest{
		Template: pluginkit.TemplatePayload{Sections: []model.TemplateSection{
			{Heading: "Summary", Instruction: "Summarize."},
		}},
		Config:      json.RawMessage(`{"model":"config-model","ollama_url":"` + ollama.URL + `","temperature":0.3}`),
		Model:       "override-model",
		Temperature: &temp,
	}

	resp, err := eng.Generate(context.Background(), reqBody)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if gotModel != "override-model" {
		t.Fatalf("outbound model = %q, want override-model (request override should win over config)", gotModel)
	}
	if gotTemp != 0.9 {
		t.Fatalf("outbound temperature = %v, want 0.9 (request override should win over config)", gotTemp)
	}
	if resp.Model != "override-model" {
		t.Fatalf("resp.Model = %q, want override-model", resp.Model)
	}
}

func TestGenerateNilTranscriptUsesEmptyList(t *testing.T) {
	t.Parallel()

	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if !strings.Contains(req.Messages[len(req.Messages)-1].Content, "Transcript:\n(empty)") {
			t.Fatalf("prompt missing empty transcript marker: %s", req.Messages[len(req.Messages)-1].Content)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]any{
				"content": `{"content_markdown":"empty transcript"}`,
			},
		})
	}))
	defer ollama.Close()

	eng := New(Config{
		OllamaURL: ollama.URL,
		Model:     "fallback-model",
	})

	reqBody := pluginkit.GenerateRequest{
		Transcript:    nil,
		NotesMarkdown: "",
		Template: pluginkit.TemplatePayload{Sections: []model.TemplateSection{
			{Heading: "Summary", Instruction: "Summarize."},
		}},
		Config: json.RawMessage(`{"ollama_url":"` + ollama.URL + `"}`),
	}

	resp, err := eng.Generate(context.Background(), reqBody)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(resp.Summary.Sections) != 1 || resp.Summary.Sections[0].ContentMarkdown != "empty transcript" {
		t.Fatalf("response = %+v", resp)
	}
}
