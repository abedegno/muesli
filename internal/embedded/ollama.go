package embedded

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/abedegno/muesli/internal/config"
)

const (
	DefaultOllamaURL            = "http://127.0.0.1:11434"
	DefaultOllamaDetectAttempts = 5
	DefaultOllamaDetectInterval = 1 * time.Second
	embeddedDegradedReason      = "summaries & semantic search need Ollama"
	ollamaProbeTimeout          = 1500 * time.Millisecond
)

var ollamaHTTPClient = &http.Client{
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

var ollamaRetrySleep = func(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

func normalizeOllamaBaseURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

// PullProgressFunc receives the current percent complete for streamed Ollama
// model pulls.
type PullProgressFunc func(percent int)

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

// DetectOllamaWithRetry probes the Ollama version endpoint up to attempts
// times, sleeping between failures when the context remains active.
func DetectOllamaWithRetry(ctx context.Context, baseURL string, attempts int, interval time.Duration) bool {
	if ctx.Err() != nil {
		return false
	}

	if attempts <= 0 {
		attempts = 1
	}

	for i := 0; i < attempts; i++ {
		if ctx.Err() != nil {
			return false
		}
		if DetectOllama(ctx, baseURL) {
			return true
		}
		if i == attempts-1 {
			return false
		}
		if err := ollamaRetrySleep(ctx, interval); err != nil {
			return false
		}
	}

	return false
}

// PullModel requests the named Ollama model and drains the streamed response
// to completion. When onProgress is non-nil, it is called with parsed
// percentage updates from Ollama's streamed NDJSON response.
func PullModel(ctx context.Context, baseURL, model string, onProgress PullProgressFunc) error {
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

	type pullLine struct {
		Status    string `json:"status"`
		Total     int64  `json:"total"`
		Completed int64  `json:"completed"`
		Done      bool   `json:"done"`
	}

	dec := json.NewDecoder(resp.Body)
	lastPercent := -1
	for {
		var line pullLine
		if err := dec.Decode(&line); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}

		percent := lastPercent
		switch {
		case line.Done:
			percent = 100
		case line.Total > 0 && line.Completed >= 0:
			percent = int(line.Completed * 100 / line.Total)
		}
		if percent < 0 {
			continue
		}
		if percent > 100 {
			percent = 100
		}
		if onProgress != nil && percent != lastPercent {
			onProgress(percent)
		}
		lastPercent = percent
	}
	return nil
}

// PullEmbeddingModel is the embedding-specific wrapper kept for existing call
// sites.
func PullEmbeddingModel(ctx context.Context, baseURL, model string, onProgress PullProgressFunc) error {
	return PullModel(ctx, baseURL, model, onProgress)
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
