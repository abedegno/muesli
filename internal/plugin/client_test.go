package plugin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugin"
)

func TestTranscribeSendsContractAndParsesResponse(t *testing.T) {
	var gotAuth, gotAPI, gotPath string
	var gotBody plugin.TranscribeRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAPI = r.Header.Get("X-Muesli-Plugin-API")
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(plugin.TranscribeResponse{
			Segments: []model.Segment{{StartMS: 0, EndMS: 500, Text: "hi", Source: "mic"}},
			Language: "en", Model: "base", DurationMS: 500,
		})
	}))
	defer srv.Close()

	c := plugin.New(srv.URL, "tok-123")
	resp, err := c.Transcribe(context.Background(), plugin.TranscribeRequest{
		AudioURL: "http://store/audio?sig=x",
		Config:   json.RawMessage(`{"k":"v"}`),
	})
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if gotPath != "/transcribe" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer tok-123" {
		t.Fatalf("auth header = %q", gotAuth)
	}
	if gotAPI != "1" {
		t.Fatalf("plugin-api header = %q", gotAPI)
	}
	if gotBody.AudioURL != "http://store/audio?sig=x" {
		t.Fatalf("audio_url not sent: %q", gotBody.AudioURL)
	}
	if len(resp.Segments) != 1 || resp.Segments[0].Text != "hi" || resp.Model != "base" {
		t.Fatalf("unexpected response %+v", resp)
	}
}

func TestGenerateSendsContractAndParsesResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/generate" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(plugin.GenerateResponse{
			Summary: plugin.SummaryPayload{Sections: []model.SummarySection{
				{Heading: "Overview", ContentMarkdown: "Done.", Refs: []int{0}},
			}},
			Model: "llama3",
		})
	}))
	defer srv.Close()

	c := plugin.New(srv.URL, "tok")
	resp, err := c.Generate(context.Background(), plugin.GenerateRequest{
		Transcript:    []model.Segment{{StartMS: 0, EndMS: 1, Text: "x", Source: "mic"}},
		NotesMarkdown: "- a note",
		Template:      plugin.TemplatePayload{Sections: []model.TemplateSection{{Heading: "Overview", Instruction: "Summarise."}}},
		Config:        json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(resp.Summary.Sections) != 1 || resp.Summary.Sections[0].Heading != "Overview" || resp.Model != "llama3" {
		t.Fatalf("unexpected response %+v", resp)
	}
}

func TestGenerateNilTranscriptSerializesAsEmptyArray(t *testing.T) {
	// A nil Go slice marshals to JSON null, which the agent's Pydantic schema
	// rejects with 422. The client must coerce it to an empty array so the agent
	// always sees a list.
	var raw map[string]json.RawMessage

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&raw)
		_ = json.NewEncoder(w).Encode(plugin.GenerateResponse{Model: "stub"})
	}))
	defer srv.Close()

	c := plugin.New(srv.URL, "tok")
	_, err := c.Generate(context.Background(), plugin.GenerateRequest{
		// Transcript intentionally left nil.
		NotesMarkdown: "- a note",
		Template:      plugin.TemplatePayload{Sections: []model.TemplateSection{{Heading: "Overview", Instruction: "Summarise."}}},
		Config:        json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	got, ok := raw["transcript"]
	if !ok {
		t.Fatal("transcript field missing from request body")
	}
	if string(got) != "[]" {
		t.Fatalf("transcript serialized as %q, want %q (never null)", string(got), "[]")
	}
}

func TestHealthAndInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/info":
			_ = json.NewEncoder(w).Encode(plugin.Info{
				Name: "whisper", Version: "1.0", PluginAPI: 1, Kind: "transcriber",
			})
		}
	}))
	defer srv.Close()

	c := plugin.New(srv.URL, "tok")
	if err := c.Health(context.Background()); err != nil {
		t.Fatalf("health: %v", err)
	}
	info, err := c.Info(context.Background())
	if err != nil || info.Name != "whisper" || info.PluginAPI != 1 {
		t.Fatalf("info = %+v err=%v", info, err)
	}
}

func TestNon2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := plugin.New(srv.URL, "tok")
	_, err := c.Transcribe(context.Background(), plugin.TranscribeRequest{AudioURL: "u", Config: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("expected error on 500")
	}
	var he *plugin.HTTPError
	if !plugin.AsHTTPError(err, &he) || he.StatusCode != 500 {
		t.Fatalf("want HTTPError 500, got %v", err)
	}
}

// TestHTTPErrorRedactsBody verifies that HTTPError.Error() never exposes a raw
// secret that appears in the plugin response body beyond the 80-char truncation
// boundary. A misbehaving plugin might echo back bearer tokens in an error
// message; the Error() string must not propagate them into log output.
func TestHTTPErrorRedactsBody(t *testing.T) {
	secret := "supersecret_token_value"
	// Body is longer than 80 chars with the secret starting at position 81, so it
	// falls entirely after the truncation boundary and must not appear in Error().
	// "error: plugin upstream auth: " is 30 chars; padding brings the start of the
	// secret to position 81.
	prefix := "error: plugin upstream auth: " + strings.Repeat("x", 52) // 81 chars total
	body := prefix + secret

	e := &plugin.HTTPError{StatusCode: 500, Body: body}
	got := e.Error()

	// Must contain the HTTP status code.
	if !strings.Contains(got, "500") {
		t.Errorf("Error() = %q, want it to contain status code 500", got)
	}

	// Must NOT contain the raw secret (it is beyond the 80-char cut).
	if strings.Contains(got, secret) {
		t.Errorf("Error() = %q, must not contain raw secret %q", got, secret)
	}

	// Body field on the struct is still the full value (for programmatic use).
	if e.Body != body {
		t.Errorf("Body field = %q, want %q", e.Body, body)
	}
}

// TestHTTPErrorTruncatesLongBody verifies that bodies longer than 80 characters
// are clipped and labelled so logs stay bounded.
func TestHTTPErrorTruncatesLongBody(t *testing.T) {
	long := strings.Repeat("x", 120) // 120 chars — well over the 80-char limit
	e := &plugin.HTTPError{StatusCode: 502, Body: long}
	got := e.Error()

	if strings.Contains(got, long) {
		t.Errorf("Error() = %q, must not contain full long body", got)
	}
	if !strings.Contains(got, "... [truncated]") {
		t.Errorf("Error() = %q, want \"... [truncated]\" suffix", got)
	}
	// The first 80 chars must still be present.
	if !strings.Contains(got, long[:80]) {
		t.Errorf("Error() = %q, want first 80 chars present", got)
	}
	// Body under 80 chars must not be truncated.
	short := strings.Repeat("y", 80)
	es := &plugin.HTTPError{StatusCode: 400, Body: short}
	if gs := es.Error(); strings.Contains(gs, "truncated") {
		t.Errorf("Error() for 80-char body = %q, should not be truncated", gs)
	}
}
