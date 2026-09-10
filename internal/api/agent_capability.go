package api

import (
	"errors"
	"net/http"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
)

const noDefaultAgentMessage = "no default agent configured"

func (s *Server) agentConfigured(r *http.Request) (bool, error) {
	_, err := s.deps.Store.DefaultPlugin(r.Context(), s.deps.Crypto, model.PluginAgent)
	return agentConfiguredFromLookup(err)
}

func agentConfiguredFromLookup(err error) (bool, error) {
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// teamSharingAvailable reports whether folder sharing is meaningful on this
// deployment: true once a second user exists (issue #12). A single-user
// deployment has no one else to share with, so the client hides the toggle
// even though the underlying API behavior is identical either way.
func (s *Server) teamSharingAvailable(r *http.Request) (bool, error) {
	n, err := s.deps.Store.CountUsers(r.Context())
	if err != nil {
		return false, err
	}
	return n >= 2, nil
}

func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	configured, err := s.agentConfigured(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	teamSharing, err := s.teamSharingAvailable(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{
		"agent_configured":       configured,
		"team_sharing_available": teamSharing,
	})
}
