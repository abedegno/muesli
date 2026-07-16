// Package plugin is the server-side HTTP/JSON client for Muesli transcriber and
// agent plugins. It implements the pinned plugin contract (plugin_api 1).
package plugin

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

// PluginAPIVersion is the contract version this client speaks.
const PluginAPIVersion = "1"

// RequestTimeout is the maximum time a plugin request may block before the
// client treats it as failed.
const RequestTimeout = 10 * time.Minute

// Client calls a single plugin endpoint with a fixed bearer token.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New builds a client. Timeout is generous so scale-to-zero cold starts are not
// mistaken for failures.
func New(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: RequestTimeout},
	}
}

// Info is the common /info envelope.
type Info struct {
	Name         string          `json:"name"`
	Version      string          `json:"version"`
	PluginAPI    int             `json:"plugin_api"`
	Kind         string          `json:"kind"`
	ConfigSchema json.RawMessage `json:"config_schema,omitempty"`
}

// TranscribeRequest is the POST /transcribe body.
type TranscribeRequest struct {
	AudioURL     string          `json:"audio_url"`
	LanguageHint string          `json:"language_hint,omitempty"`
	Options      json.RawMessage `json:"options,omitempty"`
	Config       json.RawMessage `json:"config"`
}

// TranscribeResponse is the POST /transcribe reply.
type TranscribeResponse struct {
	Segments      []model.Segment `json:"segments"`
	Language      string          `json:"language"`
	Model         string          `json:"model"`
	DurationMS    int             `json:"duration_ms"`
	Partial       bool            `json:"partial"`
	PartialReason string          `json:"partial_reason,omitempty"`
}

// TemplatePayload mirrors the template shape sent to the agent.
type TemplatePayload struct {
	Sections []model.TemplateSection `json:"sections"`
}

// GenerateRequest is the POST /generate body.
//
// SystemPrompt, Model, and Temperature are optional per-template agent
// overrides (see model.Template), mirrored from pluginkit.GenerateRequest.
// They are absent/zero when the resolved template has no override set,
// preserving prior behaviour (the agent falls back to its own default system
// prompt / plugin Config values).
type GenerateRequest struct {
	Transcript    []model.Segment `json:"transcript"`
	NotesMarkdown string          `json:"notes_markdown"`
	Template      TemplatePayload `json:"template"`
	Options       json.RawMessage `json:"options,omitempty"`
	Config        json.RawMessage `json:"config"`
	SystemPrompt  string          `json:"system_prompt,omitempty"`
	Model         string          `json:"model,omitempty"`
	Temperature   *float64        `json:"temperature,omitempty"`
}

// SummaryPayload is the produced summary in a /generate reply.
type SummaryPayload struct {
	Sections []model.SummarySection `json:"sections"`
}

// GenerateUsage is an optional token-usage report a plugin may include in its
// /generate reply, letting the pipeline detect near-context-limit responses
// that are likely silently truncated. Most plugins today don't send this.
type GenerateUsage struct {
	TokensUsed int `json:"tokens_used"`
	MaxTokens  int `json:"max_tokens"`
}

// GenerateResponse is the POST /generate reply.
type GenerateResponse struct {
	Summary SummaryPayload `json:"summary"`
	Model   string         `json:"model"`
	// Usage is optional — nil when the plugin does not report token usage.
	Usage *GenerateUsage `json:"usage,omitempty"`
}

// HTTPError is a non-2xx response from a plugin. 5xx/timeouts are retryable.
type HTTPError struct {
	StatusCode int
	Body       string
}

// Error returns a safe summary of the plugin failure: the HTTP status code plus
// the first 80 characters of the response body (truncated with "... [truncated]"
// if longer). The raw Body field is preserved on the struct for programmatic
// inspection but intentionally omitted here so that log calls that format this
// error with %v never emit a full plugin response body, which might contain
// bearer tokens or other credentials that a misbehaving plugin echoes back.
func (e *HTTPError) Error() string {
	body := e.Body
	if len(body) > 80 {
		body = body[:80] + "... [truncated]"
	}
	return fmt.Sprintf("plugin returned %d: %s", e.StatusCode, body)
}

// Retryable reports whether the failure warrants a retry (5xx / 429).
func (e *HTTPError) Retryable() bool {
	return e.StatusCode >= 500 || e.StatusCode == http.StatusTooManyRequests
}

// AsHTTPError is a typed errors.As helper for callers/tests.
func AsHTTPError(err error, target **HTTPError) bool {
	return errors.As(err, target)
}

func (c *Client) Info(ctx context.Context) (Info, error) {
	var out Info
	err := c.doJSON(ctx, http.MethodGet, "/info", nil, &out)
	return out, err
}

// Health returns nil if the plugin reports ready (2xx on /health).
func (c *Client) Health(ctx context.Context) error {
	return c.doJSON(ctx, http.MethodGet, "/health", nil, nil)
}

func (c *Client) Transcribe(ctx context.Context, req TranscribeRequest) (TranscribeResponse, error) {
	var out TranscribeResponse
	err := c.doJSON(ctx, http.MethodPost, "/transcribe", req, &out)
	return out, err
}

func (c *Client) Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	// A nil transcript slice marshals to JSON null, which the agent contract
	// rejects (transcript must be a list). Coerce to an empty slice so we always
	// send "transcript": [] — e.g. for silent/very short audio with no segments.
	if req.Transcript == nil {
		req.Transcript = []model.Segment{}
	}
	var out GenerateResponse
	err := c.doJSON(ctx, http.MethodPost, "/generate", req, &out)
	return out, err
}

// doJSON performs a request with the pinned auth headers and decodes a 2xx JSON
// body into out (out may be nil to ignore the body, e.g. /health).
func (c *Client) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-Muesli-Plugin-API", PluginAPIVersion)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err // transport error (timeout, connection refused) — caller treats as retryable
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(respBody))}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(respBody, out)
}

// PluginStatus is the response from GET /status on a plugin.
type PluginStatus struct {
	Status  string `json:"status"`
	Model   string `json:"model"`
	Percent int    `json:"percent"`
}

// Status fetches the plugin's current model-download status.
// Returns a PluginStatus with Status="unknown" on any error (best-effort).
func (c *Client) Status(ctx context.Context) (PluginStatus, error) {
	var out PluginStatus
	err := c.doJSON(ctx, http.MethodGet, "/status", nil, &out)
	return out, err
}
