package embedded

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/abedegno/muesli/internal/config"
)

const (
	DefaultOllamaURL       = "http://127.0.0.1:11434"
	embeddedDegradedReason = "summaries & semantic search need Ollama"
	ollamaProbeTimeout     = 1500 * time.Millisecond
)

var ollamaHTTPClient = &http.Client{
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func normalizeOllamaBaseURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

// OllamaBaseURL returns the configured Ollama base URL, or the default local
// loopback address when the environment variable is unset or blank.
func OllamaBaseURL() string {
	if baseURL := normalizeOllamaBaseURL(os.Getenv("MUESLI_OLLAMA_URL")); baseURL != "" {
		return baseURL
	}
	return DefaultOllamaURL
}

// DetectOllama probes the Ollama version endpoint and reports whether the
// server is reachable and returns a 2xx response.
func DetectOllama(ctx context.Context, baseURL string) bool {
	baseURL = normalizeOllamaBaseURL(baseURL)
	if baseURL == "" {
		return false
	}

	probeCtx, cancel := context.WithTimeout(ctx, ollamaProbeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, baseURL+"/api/version", nil)
	if err != nil {
		return false
	}
	resp, err := ollamaHTTPClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// PullModel requests the named Ollama model and drains the streamed response
// to completion.
func PullModel(ctx context.Context, baseURL, model string) error {
	baseURL = normalizeOllamaBaseURL(baseURL)
	if baseURL == "" {
		return fmt.Errorf("empty ollama base url")
	}

	body, err := json.Marshal(map[string]string{"name": model})
	if err != nil {
		return fmt.Errorf("marshal ollama pull request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/pull", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ollamaHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ollama pull returned %s", resp.Status)
	}
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return err
	}
	return nil
}

// PullEmbeddingModel is the embedding-specific wrapper kept for existing call
// sites.
func PullEmbeddingModel(ctx context.Context, baseURL, model string) error {
	return PullModel(ctx, baseURL, model)
}

// ConfigureEmbeddedOllama updates runtime config based on whether Ollama was
// detected during embedded startup.
func ConfigureEmbeddedOllama(cfg *config.Config, baseURL string, detected bool) {
	if cfg == nil {
		return
	}

	if detected {
		cfg.EmbeddingsURL = normalizeOllamaBaseURL(baseURL)
		cfg.EmbeddedOllamaDetected = true
		cfg.EmbeddedDegraded = false
		cfg.EmbeddedDegradedReason = ""
		return
	}

	cfg.EmbeddingsURL = ""
	cfg.EmbeddedOllamaDetected = false
	cfg.EmbeddedDegraded = true
	cfg.EmbeddedDegradedReason = embeddedDegradedReason
}
