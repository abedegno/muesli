package api

import (
	"net/http"

	"github.com/abedegno/muesli/internal/embedded"
)

func (s *Server) handleEmbeddedStartup(w http.ResponseWriter, r *http.Request) {
	if !s.deps.Config.Embedded {
		writeJSON(w, http.StatusOK, embedded.Progress{
			Phase:    embedded.PhaseReady,
			Percent:  100,
			Ready:    true,
			Degraded: false,
		})
		return
	}

	progress := embedded.Progress{}
	if s.deps.EmbeddedProgress != nil {
		progress = s.deps.EmbeddedProgress.Snapshot()
	}

	writeJSON(w, http.StatusOK, progress)
}
