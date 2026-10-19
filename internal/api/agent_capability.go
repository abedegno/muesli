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

func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	configured, err := s.agentConfigured(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	uid, _ := userIDFromContext(r.Context())
	admin, err := s.deps.Store.IsAdmin(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not check account permissions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"agent_configured": configured, "is_admin": admin, "user_id": uid})
}
