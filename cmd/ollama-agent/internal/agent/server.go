package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/abedegno/muesli/internal/model"
)

const (
	DefaultName    = "muesli-ollama-agent"
	DefaultVersion = "0.1.0"
	DefaultModel   = "llama3.2:3b"

	defaultOllamaURL = "http://127.0.0.1:11434"
	pluginAPIVersion = "1"
)

var configSchema = json.RawMessage(`{"type":"object","properties":{"model":{"type":"string","title":"Model","description":"Model name to send to Ollama","default":"llama3.2:3b"},"ollama_url":{"type":"string","title":"Ollama URL","description":"Base URL for the Ollama server","default":"http://127.0.0.1:11434"},"temperature":{"type":"number","title":"Temperature","minimum":0,"maximum":2,"default":0.2}},"additionalProperties":false}`)

type Config struct {
	Addr        string
	Token       string
	OllamaURL   string
	Model       string
	Temperature float64
	Name        string
	Version     string
}

type Info struct {
	Name         string          `json:"name"`
	Version      string          `json:"version"`
	PluginAPI    int             `json:"plugin_api"`
	Kind         string          `json:"kind"`
	ConfigSchema json.RawMessage `json:"config_schema"`
}

type Template struct {
	Sections []model.TemplateSection `json:"sections"`
}

type GenerateRequest struct {
	Transcript    []model.Segment `json:"transcript"`
	NotesMarkdown string          `json:"notes_markdown"`
	Template      Template        `json:"template"`
	Options       json.RawMessage `json:"options,omitempty"`
	Config        json.RawMessage `json:"config"`
}

type GenerateResponse struct {
	Summary struct {
		Sections []model.SummarySection `json:"sections"`
	} `json:"summary"`
	Model string `json:"model"`
}

type pluginConfig struct {
	Model       string   `json:"model"`
	OllamaURL   string   `json:"ollama_url"`
	Temperature *float64 `json:"temperature"`
}

type sectionOutput struct {
	ContentMarkdown string `json:"content_markdown"`
	Refs            []int  `json:"refs,omitempty"`
	Model           string `json:"-"`
}

type ollamaChatRequest struct {
	Model    string `json:"model"`
	Stream   bool   `json:"stream"`
	Format   string `json:"format"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	Options map[string]any `json:"options,omitempty"`
}

type ollamaChatResponse struct {
	Model   string `json:"model"`
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Response string `json:"response"`
}

type Server struct {
	cfg    Config
	client http.Client
	mux    *http.ServeMux
}

func New(cfg Config) http.Handler {
	s := &Server{
		cfg: cfg,
		client: http.Client{
			Timeout: 10 * time.Minute,
		},
		mux: http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("/health", s.health)
	s.mux.Handle("/info", s.requireAuth(http.HandlerFunc(s.info)))
	s.mux.Handle("/generate", s.requireAuth(http.HandlerFunc(s.generate)))
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) info(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, Info{
		Name:         s.name(),
		Version:      s.version(),
		PluginAPI:    1,
		Kind:         "agent",
		ConfigSchema: configSchema,
	})
}

func (s *Server) generate(w http.ResponseWriter, r *http.Request) {
	var req GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	cfg := s.effectiveConfig(req.Config)
	transcript := req.Transcript
	if transcript == nil {
		transcript = []model.Segment{}
	}

	sections := make([]model.SummarySection, 0, len(req.Template.Sections))
	reportedModel := ""
	for idx, section := range req.Template.Sections {
		out, err := s.generateSection(r.Context(), cfg, idx, section, transcript, req.NotesMarkdown, req.Options)
		if err != nil {
			http.Error(w, fmt.Sprintf("ollama error: %v", err), http.StatusBadGateway)
			return
		}
		sections = append(sections, model.SummarySection{
			Heading:         section.Heading,
			ContentMarkdown: out.ContentMarkdown,
			Refs:            out.Refs,
		})
		if reportedModel == "" && out.Model != "" {
			reportedModel = out.Model
		}
	}

	resp := GenerateResponse{Model: cfg.Model}
	if reportedModel != "" {
		resp.Model = reportedModel
	}
	resp.Summary.Sections = sections
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) generateSection(ctx context.Context, cfg pluginConfig, index int, section model.TemplateSection, transcript []model.Segment, notesMarkdown string, options json.RawMessage) (sectionOutput, error) {
	prompt := buildPrompt(index, section, transcript, notesMarkdown, options)
	payload := ollamaChatRequest{
		Model:  cfg.Model,
		Stream: false,
		Format: "json",
		Messages: []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{
			{Role: "system", Content: systemPrompt()},
			{Role: "user", Content: prompt},
		},
	}
	if cfg.Temperature != nil {
		payload.Options = map[string]any{"temperature": *cfg.Temperature}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return sectionOutput{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.OllamaURL, "/")+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return sectionOutput{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return sectionOutput{}, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return sectionOutput{}, fmt.Errorf("upstream status %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}

	var upstream ollamaChatResponse
	if err := json.Unmarshal(respBody, &upstream); err != nil {
		return sectionOutput{}, fmt.Errorf("decode ollama response: %w", err)
	}
	content := strings.TrimSpace(upstream.Message.Content)
	if content == "" {
		content = strings.TrimSpace(upstream.Response)
	}
	if content == "" {
		return sectionOutput{}, errors.New("empty ollama response content")
	}

	out, err := decodeSectionOutput(content)
	if err != nil {
		return sectionOutput{}, err
	}
	out.Model = strings.TrimSpace(upstream.Model)
	return out, nil
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Muesli-Plugin-API") != pluginAPIVersion {
			http.Error(w, "missing or unsupported X-Muesli-Plugin-API", http.StatusBadRequest)
			return
		}
		authorization := r.Header.Get("Authorization")
		if !strings.HasPrefix(authorization, "Bearer ") || strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer ")) == "" {
			http.Error(w, "invalid or missing token", http.StatusUnauthorized)
			return
		}
		if strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer ")) != s.cfg.Token || s.cfg.Token == "" {
			http.Error(w, "invalid or missing token", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) effectiveConfig(raw json.RawMessage) pluginConfig {
	cfg := pluginConfig{
		Model:     strings.TrimSpace(s.cfg.Model),
		OllamaURL: strings.TrimSpace(s.cfg.OllamaURL),
	}
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}
	if cfg.OllamaURL == "" {
		cfg.OllamaURL = defaultOllamaURL
	}
	temp := s.cfg.Temperature
	if temp <= 0 {
		temp = 0.2
	}
	cfg.Temperature = &temp
	if len(raw) == 0 {
		return cfg
	}
	var override pluginConfig
	if err := json.Unmarshal(raw, &override); err != nil {
		return cfg
	}
	if strings.TrimSpace(override.Model) != "" {
		cfg.Model = strings.TrimSpace(override.Model)
	}
	if strings.TrimSpace(override.OllamaURL) != "" {
		cfg.OllamaURL = strings.TrimSpace(override.OllamaURL)
	}
	if override.Temperature != nil {
		cfg.Temperature = override.Temperature
	}
	return cfg
}

func buildPrompt(index int, section model.TemplateSection, transcript []model.Segment, notesMarkdown string, options json.RawMessage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Section %d\n", index+1)
	fmt.Fprintf(&b, "Heading: %s\n", section.Heading)
	fmt.Fprintf(&b, "Instruction: %s\n\n", section.Instruction)
	b.WriteString("Return only JSON with keys content_markdown and optional refs.\n")
	b.WriteString("refs must be 0-based transcript indices.\n\n")

	b.WriteString("Notes markdown:\n")
	if strings.TrimSpace(notesMarkdown) == "" {
		b.WriteString("(empty)\n")
	} else {
		b.WriteString(notesMarkdown)
		if !strings.HasSuffix(notesMarkdown, "\n") {
			b.WriteByte('\n')
		}
	}

	b.WriteString("\nTranscript:\n")
	if len(transcript) == 0 {
		b.WriteString("(empty)\n")
	} else {
		for i, seg := range transcript {
			fmt.Fprintf(&b, "[%d] %d-%d source=%s", i, seg.StartMS, seg.EndMS, seg.Source)
			if strings.TrimSpace(seg.Speaker) != "" {
				fmt.Fprintf(&b, " speaker=%s", seg.Speaker)
			}
			fmt.Fprintf(&b, ": %s\n", seg.Text)
		}
	}

	if len(options) > 0 {
		b.WriteString("\nOptions JSON:\n")
		b.Write(options)
		b.WriteByte('\n')
	}

	return b.String()
}

func decodeSectionOutput(content string) (sectionOutput, error) {
	var out sectionOutput
	if err := json.Unmarshal([]byte(content), &out); err == nil {
		return out, nil
	}
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		if err := json.Unmarshal([]byte(content[start:end+1]), &out); err == nil {
			return out, nil
		}
	}
	return sectionOutput{}, fmt.Errorf("invalid model json output: %s", content)
}

func systemPrompt() string {
	return "You are a note summarization agent. Return only JSON objects matching {\"content_markdown\":\"...\",\"refs\":[0,1]}. Use concise markdown. Keep refs optional."
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) name() string {
	if strings.TrimSpace(s.cfg.Name) != "" {
		return strings.TrimSpace(s.cfg.Name)
	}
	return DefaultName
}

func (s *Server) version() string {
	if strings.TrimSpace(s.cfg.Version) != "" {
		return strings.TrimSpace(s.cfg.Version)
	}
	return DefaultVersion
}
