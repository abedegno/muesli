// Package embed provides text-embedding generation for semantic search.
//
// Embeddings are optional and config-gated: New returns nil when no embeddings
// URL is configured, and callers treat a nil Embedder as "disabled" so the
// system degrades gracefully to lexical-only search.
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/abedegno/muesli/internal/config"
)

// Dim is nomic-embed-text's dimensionality. It is informational only — the
// note_embeddings.embedding column is unsized (`vector`) and stores any model's
// dimension; search is kept consistent by filtering on the model, not the dim.
const Dim = 768

// Embedder turns text into a fixed-dimension vector.
type Embedder interface {
	// Embed returns the embedding vector for text, or an error on failure.
	Embed(ctx context.Context, text string) ([]float32, error)
	// Dim returns the dimensionality of the vectors produced by Embed.
	Dim() int
}

// OllamaEmbedder talks to an Ollama server's /api/embeddings endpoint.
type OllamaEmbedder struct {
	url   string
	model string
	dim   int // configured dimension; falls back to Dim (768) when zero/unset
	http  *http.Client
}

// New builds an Embedder from config. It returns nil when cfg.EmbeddingsURL is
// empty, signalling that semantic embeddings are disabled.
func New(cfg config.Config) Embedder {
	if cfg.EmbeddingsURL == "" {
		return nil
	}
	dim := cfg.EmbeddingsDim
	if dim <= 0 {
		dim = Dim // fallback to 768
	}
	return &OllamaEmbedder{
		url:   cfg.EmbeddingsURL,
		model: cfg.EmbeddingsModel,
		dim:   dim,
		http:  &http.Client{Timeout: 60 * time.Second},
	}
}

// Dim returns the configured embedding dimension.
func (e *OllamaEmbedder) Dim() int { return e.dim }

// Embed POSTs {model, prompt} to {url}/api/embeddings and returns the resulting
// vector. It returns an error on network failure, a non-2xx status, or an empty
// embedding so callers can retry.
func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	reqBody, err := json.Marshal(struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
	}{Model: e.model, Prompt: text})
	if err != nil {
		return nil, fmt.Errorf("embed: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.url+"/api/embeddings", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("embed: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embed: unexpected status %d", resp.StatusCode)
	}

	var out struct {
		Embedding []float64 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("embed: decode response: %w", err)
	}
	if len(out.Embedding) == 0 {
		return nil, fmt.Errorf("embed: empty embedding")
	}
	// Validate that the returned vector length matches the configured dimension.
	if len(out.Embedding) != e.dim {
		return nil, fmt.Errorf("embed: model %q returned dim %d, want %d (check MUESLI_EMBEDDINGS_DIM)", e.model, len(out.Embedding), e.dim)
	}

	vec := make([]float32, len(out.Embedding))
	for i, v := range out.Embedding {
		// pgvector rejects NaN/±Inf, so reject them here with a clear error
		// rather than letting the DB cast fail cryptically downstream.
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("embed: non-finite component at index %d", i)
		}
		vec[i] = float32(v)
	}
	return vec, nil
}
