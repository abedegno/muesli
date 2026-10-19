package api

import (
	"encoding/json"
	"errors"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"net/http"
)

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid, ok := userIDFromContext(r.Context())
		if !ok {
			writeError(w, 401, "unauthorized")
			return
		}
		admin, err := s.deps.Store.IsAdmin(r.Context(), uid)
		if err != nil {
			writeError(w, 503, "could not check account permissions")
			return
		}
		if !admin {
			writeError(w, 403, "administrator permission required")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) handleSetUserRole(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.Email == "" || (req.Role != "admin" && req.Role != "member") {
		writeError(w, 400, "email and role (admin or member) required")
		return
	}
	user, err := s.deps.Store.SetUserRole(r.Context(), req.Email, req.Role)
	if errors.Is(err, store.ErrLastAdmin) {
		writeError(w, 409, err.Error())
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, 404, "account not found")
		return
	}
	if err != nil {
		writeError(w, 500, "could not change account role")
		return
	}
	writeJSON(w, 200, user)
}
func (s *Server) handleTranscriptionServices(w http.ResponseWriter, r *http.Request) {
	list, err := s.deps.Store.ListPlugins(r.Context(), s.deps.Crypto)
	if err != nil {
		writeError(w, 500, "could not load transcription services")
		return
	}
	result := make([]model.Plugin, 0)
	for _, p := range list {
		if p.Kind == model.PluginTranscriber || p.Kind == model.PluginStreamingTranscriber {
			p.Config = nil
			p.ConfigSchema = nil
			result = append(result, p)
		}
	}
	writeJSON(w, 200, result)
}
