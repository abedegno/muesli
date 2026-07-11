// Package plugintest provides in-repo stub transcriber and agent plugins for
// driving worker/integration tests against the pinned plugin contract.
package plugintest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugin"
)

// Stub is a fake plugin HTTP server with optional failure injection.
type Stub struct {
	srv      *httptest.Server
	mu       sync.Mutex
	failNext int
	lastBody []byte
}

func (s *Stub) URL() string { return s.srv.URL }
func (s *Stub) Close()      { s.srv.Close() }

// FailNext makes the next n requests respond 500 before normal behaviour resumes.
func (s *Stub) FailNext(n int) {
	s.mu.Lock()
	s.failNext = n
	s.mu.Unlock()
}

// shouldFail reports (and consumes) one queued failure.
func (s *Stub) shouldFail() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNext > 0 {
		s.failNext--
		return true
	}
	return false
}

// LastBody returns the raw request body from the most recent transcribe call.
func (s *Stub) LastBody() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.lastBody...)
}

func (s *Stub) recordBody(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.lastBody = append(s.lastBody[:0], body...)
	s.mu.Unlock()
}

// NewTranscriber returns a stub that yields two fixed segments.
func NewTranscriber() *Stub {
	s := &Stub{}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(plugin.Info{Name: "stub-transcriber", Version: "0", PluginAPI: 1, Kind: "transcriber"})
	})
	mux.HandleFunc("/transcribe", func(w http.ResponseWriter, r *http.Request) {
		s.recordBody(r)
		if s.shouldFail() {
			http.Error(w, "injected failure", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(plugin.TranscribeResponse{
			Segments: []model.Segment{
				{StartMS: 0, EndMS: 1500, Text: "Welcome everyone.", Source: "mic"},
				{StartMS: 1500, EndMS: 3000, Text: "Let's begin.", Source: "system"},
			},
			Language: "en", Model: "stub", DurationMS: 3000,
		})
	})
	s.srv = httptest.NewServer(mux)
	return s
}

// NewEmptyTranscriber returns a stub that yields zero segments, simulating
// silent or very short audio.
func NewEmptyTranscriber() *Stub {
	s := &Stub{}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(plugin.Info{Name: "stub-transcriber", Version: "0", PluginAPI: 1, Kind: "transcriber"})
	})
	mux.HandleFunc("/transcribe", func(w http.ResponseWriter, r *http.Request) {
		s.recordBody(r)
		if s.shouldFail() {
			http.Error(w, "injected failure", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(plugin.TranscribeResponse{
			Segments: []model.Segment{}, Language: "en", Model: "stub", DurationMS: 0,
		})
	})
	s.srv = httptest.NewServer(mux)
	return s
}

// NewDiarizationTranscriber returns a stub that yields two fixed segments with
// non-empty Speaker labels, simulating a diarized transcription.
func NewDiarizationTranscriber() *Stub {
	s := &Stub{}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(plugin.Info{Name: "stub-diarization-transcriber", Version: "0", PluginAPI: 1, Kind: "transcriber"})
	})
	mux.HandleFunc("/transcribe", func(w http.ResponseWriter, r *http.Request) {
		if s.shouldFail() {
			http.Error(w, "injected failure", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(plugin.TranscribeResponse{
			Segments: []model.Segment{
				{StartMS: 0, EndMS: 1500, Text: "Welcome everyone.", Source: "mic", Speaker: "SPEAKER_00"},
				{StartMS: 1500, EndMS: 3000, Text: "Let's begin.", Source: "mic", Speaker: "SPEAKER_01"},
			},
			Language: "en", Model: "stub-diarized", DurationMS: 3000,
		})
	})
	s.srv = httptest.NewServer(mux)
	return s
}

// NewAgent returns a stub that echoes one section per requested template section.
func NewAgent() *Stub {
	s := &Stub{}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(plugin.Info{Name: "stub-agent", Version: "0", PluginAPI: 1, Kind: "agent"})
	})
	mux.HandleFunc("/generate", func(w http.ResponseWriter, r *http.Request) {
		if s.shouldFail() {
			http.Error(w, "injected failure", http.StatusInternalServerError)
			return
		}
		// Mirror the real agent contract: transcript must be a JSON list, never
		// null. Decode into a raw map first so we can reject "transcript": null
		// with 422 the way the Pydantic-backed agent does.
		var raw map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if t, ok := raw["transcript"]; !ok || string(t) == "null" {
			http.Error(w, `{"detail":[{"type":"list_type","loc":["body","transcript"],"msg":"Input should be a valid list","input":null}]}`, http.StatusUnprocessableEntity)
			return
		}
		var req plugin.GenerateRequest
		body, _ := json.Marshal(raw)
		_ = json.Unmarshal(body, &req)
		var sections []model.SummarySection
		for _, sec := range req.Template.Sections {
			sections = append(sections, model.SummarySection{
				Heading:         sec.Heading,
				ContentMarkdown: "Stub summary for " + sec.Heading + ".",
			})
		}
		_ = json.NewEncoder(w).Encode(plugin.GenerateResponse{
			Summary: plugin.SummaryPayload{Sections: sections},
			Model:   "stub",
		})
	})
	s.srv = httptest.NewServer(mux)
	return s
}

// NewTruncatedAgent returns a stub that echoes one section per requested
// template section, like NewAgent, but with content that does NOT end in
// terminal punctuation — simulating a response silently cut short by a
// context-window overflow (SUM02's DetectTruncation heuristic should flag it).
func NewTruncatedAgent() *Stub {
	s := &Stub{}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(plugin.Info{Name: "stub-truncated-agent", Version: "0", PluginAPI: 1, Kind: "agent"})
	})
	mux.HandleFunc("/generate", func(w http.ResponseWriter, r *http.Request) {
		if s.shouldFail() {
			http.Error(w, "injected failure", http.StatusInternalServerError)
			return
		}
		var raw map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		var req plugin.GenerateRequest
		body, _ := json.Marshal(raw)
		_ = json.Unmarshal(body, &req)
		var sections []model.SummarySection
		for _, sec := range req.Template.Sections {
			sections = append(sections, model.SummarySection{
				Heading:         sec.Heading,
				ContentMarkdown: "Stub summary for " + sec.Heading + " that just stops mid",
			})
		}
		_ = json.NewEncoder(w).Encode(plugin.GenerateResponse{
			Summary: plugin.SummaryPayload{Sections: sections},
			Model:   "stub",
		})
	})
	s.srv = httptest.NewServer(mux)
	return s
}

// NewPartialTranscriber returns a stub that yields two segments with Partial=true,
// simulating a mid-stream chunk failure in the plugin.
func NewPartialTranscriber(reason string) *Stub {
	s := &Stub{}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(plugin.Info{Name: "stub-partial-transcriber", Version: "0", PluginAPI: 1, Kind: "transcriber"})
	})
	mux.HandleFunc("/transcribe", func(w http.ResponseWriter, r *http.Request) {
		s.recordBody(r)
		if s.shouldFail() {
			http.Error(w, "injected failure", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(plugin.TranscribeResponse{
			Segments: []model.Segment{
				{StartMS: 0, EndMS: 1500, Text: "First chunk.", Source: "mixed"},
				{StartMS: 1500, EndMS: 3000, Text: "Second chunk.", Source: "mixed"},
			},
			Language:      "en",
			Model:         "stub-partial",
			DurationMS:    3000,
			Partial:       true,
			PartialReason: reason,
		})
	})
	s.srv = httptest.NewServer(mux)
	return s
}
