package api

import (
	"net/http"

	"github.com/abedegno/muesli/internal/worker"
)

// reembedCap bounds how many ready notes a single admin-triggered re-embed
// enqueues. Matches the cap used by the `muesli reembed` CLI's non-dry-run
// path (cmd/muesli/reembed.go) so admin-triggered and CLI-triggered re-embeds
// behave identically.
const reembedCap = 100000

// ReembedStatus reports on-demand re-embed progress: whether embeddings are
// enabled, the configured (model, dim), and how many eligible notes already
// have a matching embedding. Distinct from EmbeddingsStatus (EMB01), which
// reports static config only; this endpoint reports live done/total counts
// backing the admin re-embed panel (EMB02).
type ReembedStatus struct {
	Enabled bool   `json:"enabled"`
	Model   string `json:"model"`
	Dim     int    `json:"dim"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
}

// ReembedResponse is returned by a successful POST /api/admin/embeddings/reembed.
type ReembedResponse struct {
	Status   string `json:"status"`
	Enqueued int    `json:"enqueued"`
}

// handleAdminReembedStatus reports live re-embed progress for the admin panel.
// Always 200, even when embeddings are disabled (enabled:false, zeroed fields)
// so the UI can render a clear disabled state instead of erroring.
func (s *Server) handleAdminReembedStatus(w http.ResponseWriter, r *http.Request) {
	if s.deps.Embedder == nil {
		writeJSON(w, http.StatusOK, ReembedStatus{Enabled: false})
		return
	}

	model := s.deps.Config.EmbeddingsModel
	dim := s.deps.Embedder.Dim()
	done, total, err := s.deps.Store.EmbeddingStatus(r.Context(), model, dim)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, ReembedStatus{
		Enabled: true,
		Model:   model,
		Dim:     dim,
		Done:    done,
		Total:   total,
	})
}

// handleAdminReembedAll triggers an on-demand full re-embed: it deletes every
// existing embedding for the current (model, dim) and enqueues fresh embed
// jobs for all eligible notes, reusing the same store/worker machinery as the
// `muesli reembed` CLI (cmd/muesli/reembed.go)'s non-dry-run path.
func (s *Server) handleAdminReembedAll(w http.ResponseWriter, r *http.Request) {
	if s.deps.Embedder == nil {
		writeError(w, http.StatusConflict, "embeddings are disabled")
		return
	}

	model := s.deps.Config.EmbeddingsModel
	dim := s.deps.Embedder.Dim()
	ctx := r.Context()

	if _, err := s.deps.Store.DeleteEmbeddingsForModel(ctx, model, dim); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	enqueued, err := worker.EnqueueBackfillEmbeds(ctx, s.deps.Store, model, dim, reembedCap)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusAccepted, ReembedResponse{Status: "queued", Enqueued: enqueued})
}
