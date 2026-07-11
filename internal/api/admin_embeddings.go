package api

import (
	"net/http"
)

// EmbeddingsStatus holds the current embeddings configuration and state.
type EmbeddingsStatus struct {
	Enabled     bool    `json:"enabled"`
	Model       string  `json:"model"`
	Dim         int     `json:"dim"`
	MinScore    float64 `json:"minScore"`
	DocPrefix   string  `json:"docPrefix"`
	QueryPrefix string  `json:"queryPrefix"`
}

// handleAdminEmbeddingsStatus returns the current embeddings config (always
// populated from config, even when disabled, so the admin panel can show the
// configured-but-inactive model/dim). Never returns an error for the disabled case.
// Admin auth is enforced by the router middleware.
func (s *Server) handleAdminEmbeddingsStatus(w http.ResponseWriter, r *http.Request) {
	enabled := s.deps.Embedder != nil
	dim := s.deps.Config.EmbeddingsDim
	if dim <= 0 {
		dim = 768 // match the fallback in embed.New
	}

	status := EmbeddingsStatus{
		Enabled:     enabled,
		Model:       s.deps.Config.EmbeddingsModel,
		Dim:         dim,
		MinScore:    s.deps.Config.EmbeddingsMinScore,
		DocPrefix:   s.deps.Config.EmbeddingsDocPrefix,
		QueryPrefix: s.deps.Config.EmbeddingsQueryPrefix,
	}
	writeJSON(w, http.StatusOK, status)
}
