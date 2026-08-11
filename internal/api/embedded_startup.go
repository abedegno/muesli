package api

import (
	"net/http"

	"github.com/abedegno/muesli/internal/embedded"
)

func (s *Server) handleEmbeddedStartup(w http.ResponseWriter, r *http.Request) {
	if !s.deps.Config.Embedded {
		agentConfigured := false
		if s.deps.Store != nil {
			var err error
			agentConfigured, err = s.agentConfigured(r)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
		}
		writeJSON(w, http.StatusOK, embedded.Progress{
			Phase:    embedded.PhaseReady,
			Percent:  100,
			Ready:    true,
			Degraded: !agentConfigured,
		})
		return
	}

	progress := embedded.Progress{}
	if s.deps.EmbeddedProgress != nil {
		progress = s.deps.EmbeddedProgress.Snapshot()
	}
	if !progress.Ready || s.deps.Store == nil {
		writeJSON(w, http.StatusOK, progress)
		return
	}
	agentConfigured, err := s.agentConfigured(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// The startup banner and runtime feature gates share the exact capability
	// used by summarize jobs. Local Ollama detection remains startup progress,
	// but it does not decide whether AI features are available.
	progress.Degraded = !agentConfigured

	writeJSON(w, http.StatusOK, progress)
}
