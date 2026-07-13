package agent

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/abedegno/muesli/internal/model"
)

func TestHealthAndInfoAuth(t *testing.T) {
	t.Parallel()

	srv := New(Config{
		Token:   "secret-token",
		Name:    "agent-name",
		Version: "agent-version",
	})

	t.Run("health is public", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
			t.Fatalf("body = %s, want ok status", rec.Body.String())
		}
	})

	cases := []struct {
		name      string
		apiHeader string
		auth      string
		wantCode  int
	}{
		{name: "missing api header", apiHeader: "", auth: "Bearer secret-token", wantCode: http.StatusBadRequest},
		{name: "wrong api header", apiHeader: "2", auth: "Bearer secret-token", wantCode: http.StatusBadRequest},
		{name: "missing bearer", apiHeader: pluginAPIVersion, auth: "", wantCode: http.StatusUnauthorized},
		{name: "wrong bearer", apiHeader: pluginAPIVersion, auth: "Bearer nope", wantCode: http.StatusUnauthorized},
		{name: "success", apiHeader: pluginAPIVersion, auth: "Bearer secret-token", wantCode: http.StatusOK},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/info", nil)
			if tc.apiHeader != "" {
				req.Header.Set("X-Muesli-Plugin-API", tc.apiHeader)
			}
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)

			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantCode)
			}
			if tc.wantCode == http.StatusOK {
				var info Info
				if err := json.NewDecoder(rec.Body).Decode(&info); err != nil {
					t.Fatalf("decode info: %v", err)
				}
				if info.Kind != "agent" {
					t.Fatalf("kind = %q, want agent", info.Kind)
				}
				if info.Name != "agent-name" || info.Version != "agent-version" || info.PluginAPI != 1 {
					t.Fatalf("info = %+v", info)
				}
			}
		})
	}
}

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

	handler := New(Config{
		Token:       "secret-token",
		Name:        "agent-name",
		Version:     "agent-version",
		OllamaURL:   ollama.URL,
		Model:       "fallback-model",
		Temperature: 0.2,
	})

	reqBody := GenerateRequest{
		Transcript: nil,
		NotesMarkdown: "notes",
		Template: Template{Sections: []model.TemplateSection{
			{Heading: "Summary", Instruction: "Summarize the conversation."},
			{Heading: "Decisions", Instruction: "List decisions."},
		}},
		Options: json.RawMessage(`{"temperature":0.7}`),
		Config:   json.RawMessage(`{"model":"test-model","ollama_url":"` + ollama.URL + `","temperature":0.7}`),
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/generate", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret-token")
	req.Header.Set("X-Muesli-Plugin-API", pluginAPIVersion)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp GenerateResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
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

	handler := New(Config{
		Token:     "secret-token",
		OllamaURL: ollama.URL,
		Model:     "fallback-model",
	})

	reqBody := GenerateRequest{
		Transcript: nil,
		NotesMarkdown: "",
		Template: Template{Sections: []model.TemplateSection{
			{Heading: "Summary", Instruction: "Summarize."},
		}},
		Config: json.RawMessage(`{"ollama_url":"` + ollama.URL + `"}`),
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/generate", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret-token")
	req.Header.Set("X-Muesli-Plugin-API", pluginAPIVersion)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp GenerateResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Summary.Sections) != 1 || resp.Summary.Sections[0].ContentMarkdown != "empty transcript" {
		t.Fatalf("response = %+v", resp)
	}
}
