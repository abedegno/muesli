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
	"github.com/abedegno/muesli/internal/pluginkit"
)

const (
	DefaultName    = "muesli-ollama-agent"
	DefaultVersion = "0.1.0"
	DefaultModel   = "llama3.2:3b"
	DefaultURL     = "http://127.0.0.1:11434"

	defaultTemperature = 0.2
)

var ConfigSchema = json.RawMessage(`{"type":"object","properties":{"model":{"type":"string","title":"Model","description":"Model name to send to Ollama","default":"llama3.2:3b"},"ollama_url":{"type":"string","title":"Ollama URL","description":"Base URL for the Ollama server","default":"http://127.0.0.1:11434"},"temperature":{"type":"number","title":"Temperature","minimum":0,"maximum":2,"default":0.2}},"additionalProperties":false}`)

type Config struct {
	OllamaURL   string
	Model       string
	Temperature float64
}

type Engine struct {
	cfg    Config
	client http.Client
}

func New(cfg Config) *Engine {
	return &Engine{
		cfg: cfg,
		client: http.Client{
			Timeout: 10 * time.Minute,
		},
	}
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

// Generate implements pluginkit.Agent by forwarding each section to Ollama and
// returning the combined summary response.
func (e *Engine) Generate(ctx context.Context, req pluginkit.GenerateRequest) (pluginkit.GenerateResponse, error) {
	cfg := e.effectiveConfig(req.Config)
	transcript := req.Transcript
	if transcript == nil {
		transcript = []model.Segment{}
	}

	// Per-request overrides (from the resolved template) win over the plugin's
	// Config defaults: an empty Model or nil Temperature on the request leaves
	// cfg (already resolved from req.Config / the plugin's own defaults)
	// untouched.
	if strings.TrimSpace(req.Model) != "" {
		cfg.Model = strings.TrimSpace(req.Model)
	}
	if req.Temperature != nil {
		cfg.Temperature = req.Temperature
	}
	sysPrompt := strings.TrimSpace(req.SystemPrompt)
	if sysPrompt == "" {
		sysPrompt = systemPrompt()
	}

	sections := make([]model.SummarySection, 0, len(req.Template.Sections))
	reportedModel := ""
	for idx, section := range req.Template.Sections {
		out, err := e.generateSection(ctx, cfg, sysPrompt, idx, section, transcript, req.NotesMarkdown, req.Options)
		if err != nil {
			return pluginkit.GenerateResponse{}, err
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

	resp := pluginkit.GenerateResponse{Model: cfg.Model}
	if reportedModel != "" {
		resp.Model = reportedModel
	}
	resp.Summary.Sections = sections
	return resp, nil
}

func (e *Engine) generateSection(ctx context.Context, cfg pluginConfig, sysPrompt string, index int, section model.TemplateSection, transcript []model.Segment, notesMarkdown string, options json.RawMessage) (sectionOutput, error) {
	prompt := buildPrompt(index, section, transcript, notesMarkdown, options)
	payload := ollamaChatRequest{
		Model:  cfg.Model,
		Stream: false,
		Format: "json",
		Messages: []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{
			{Role: "system", Content: sysPrompt},
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

	resp, err := e.client.Do(req)
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

func (e *Engine) effectiveConfig(raw json.RawMessage) pluginConfig {
	cfg := pluginConfig{
		Model:     strings.TrimSpace(e.cfg.Model),
		OllamaURL: strings.TrimSpace(e.cfg.OllamaURL),
	}
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}
	if cfg.OllamaURL == "" {
		cfg.OllamaURL = DefaultURL
	}
	temp := e.cfg.Temperature
	if temp <= 0 {
		temp = defaultTemperature
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
