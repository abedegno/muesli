package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugin"
	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
)

type pluginRequest struct {
	Kind         string          `json:"kind"`
	Name         string          `json:"name"`
	EndpointURL  string          `json:"endpoint_url"`
	Token        string          `json:"token"`
	Config       json.RawMessage `json:"config"`
	ConfigSchema json.RawMessage `json:"config_schema"`
	Enabled      *bool           `json:"enabled"`
	IsDefault    *bool           `json:"is_default"`
}

func (s *Server) handleListPlugins(w http.ResponseWriter, r *http.Request) {
	list, err := s.deps.Store.ListPlugins(r.Context(), s.deps.Crypto)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreatePlugin(w http.ResponseWriter, r *http.Request) {
	var req pluginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Kind != model.PluginTranscriber && req.Kind != model.PluginStreamingTranscriber && req.Kind != model.PluginAgent {
		writeError(w, http.StatusBadRequest, "kind must be transcriber, streaming-transcriber, or agent")
		return
	}
	if req.Name == "" || req.EndpointURL == "" {
		writeError(w, http.StatusBadRequest, "name and endpoint_url required")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if len(req.Config) == 0 {
		req.Config = json.RawMessage(`{}`)
	}
	p, err := s.deps.Store.CreatePlugin(r.Context(), s.deps.Crypto, model.Plugin{
		Kind:         req.Kind,
		Name:         req.Name,
		EndpointURL:  req.EndpointURL,
		Token:        req.Token,
		Config:       req.Config,
		ConfigSchema: req.ConfigSchema,
		Enabled:      enabled,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if req.IsDefault != nil && *req.IsDefault {
		if err := s.deps.Store.SetDefaultPlugin(r.Context(), p.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": p.ID})
}

func (s *Server) handlePatchPlugin(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validID(w, r, id) {
		return
	}
	var req pluginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	makeDefault := req.IsDefault != nil && *req.IsDefault
	updateFields := req.Name != "" || req.EndpointURL != "" || req.Token != "" || len(req.Config) > 0 || len(req.ConfigSchema) > 0 || req.Enabled != nil

	// Build the merged plugin (load-merge so partial PATCH works). We load even
	// when only setting default so PatchPlugin has the row's id; the merged config
	// is only written when updateFields is set.
	merged := model.Plugin{ID: id}
	if updateFields {
		existing, err := s.deps.Store.GetPlugin(r.Context(), s.deps.Crypto, id)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if req.Name != "" {
			existing.Name = req.Name
		}
		if req.EndpointURL != "" {
			existing.EndpointURL = req.EndpointURL
		}
		if req.Token != "" {
			existing.Token = req.Token
		}
		if len(req.Config) > 0 {
			existing.Config = req.Config
		}
		if len(req.ConfigSchema) > 0 {
			existing.ConfigSchema = req.ConfigSchema
		}
		if req.Enabled != nil {
			existing.Enabled = *req.Enabled
		}
		merged = existing
	}

	if !makeDefault && !updateFields {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	// Set-default + field-update run in a single transaction (no partial write).
	if err := s.deps.Store.PatchPlugin(r.Context(), s.deps.Crypto, merged, makeDefault, updateFields); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleDeletePlugin(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validID(w, r, id) {
		return
	}
	if err := s.deps.Store.DeletePlugin(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleGetPluginStatus proxies GET /status from the registered plugin and
// returns it as JSON. Returns {"status":"unknown"} on any error (best-effort).
func (s *Server) handleGetPluginStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validID(w, r, id) {
		return
	}
	p, err := s.deps.Store.GetPlugin(r.Context(), s.deps.Crypto, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	client := plugin.New(p.EndpointURL, p.Token)
	status, err := client.Status(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, plugin.PluginStatus{Status: "unknown"})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// handleCheckPluginHealth loads the plugin fully decrypted, builds a plugin
// client for its endpoint, and calls Health() on demand. An unhealthy plugin
// is reported inline via the JSON body (not an HTTP error) so the admin UI
// can render it without exception-handling ambiguity; only real request
// problems (bad id, not found) produce a non-2xx status.
func (s *Server) handleCheckPluginHealth(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validID(w, r, id) {
		return
	}
	p, err := s.deps.Store.GetPlugin(r.Context(), s.deps.Crypto, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	client := plugin.New(p.EndpointURL, p.Token)
	if err := client.Health(r.Context()); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"healthy": false, "error": pluginHealthErrorMessage(err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"healthy": true})
}

// pluginHealthErrorMessage builds the safe, user-facing error string for a
// failed Health() call. A *plugin.HTTPError's Body is plugin-controlled and
// may (if the plugin misbehaves) echo back the bearer token this server just
// sent it, so we never forward it to the admin UI — only the status code.
// Other error kinds (network/timeout/context) don't carry plugin-controlled
// bytes, so err.Error() is safe to surface as-is.
func pluginHealthErrorMessage(err error) string {
	var httpErr *plugin.HTTPError
	if plugin.AsHTTPError(err, &httpErr) {
		return fmt.Sprintf("plugin returned %d", httpErr.StatusCode)
	}
	return err.Error()
}

// handleGetDefaultTranscriberStatus looks up the default transcriber plugin
// and proxies its /status response. Returns {"status":"unknown"} if none is
// configured or the plugin is unreachable.
func (s *Server) handleGetDefaultTranscriberStatus(w http.ResponseWriter, r *http.Request) {
	plugins, err := s.deps.Store.ListPlugins(r.Context(), s.deps.Crypto)
	if err != nil {
		writeJSON(w, http.StatusOK, plugin.PluginStatus{Status: "unknown"})
		return
	}
	var def *model.Plugin
	for i := range plugins {
		if plugins[i].IsDefault && plugins[i].Kind == model.PluginTranscriber {
			def = &plugins[i]
			break
		}
	}
	if def == nil {
		writeJSON(w, http.StatusOK, plugin.PluginStatus{Status: "unknown"})
		return
	}
	client := plugin.New(def.EndpointURL, def.Token)
	status, err := client.Status(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, plugin.PluginStatus{Status: "unknown"})
		return
	}
	writeJSON(w, http.StatusOK, status)
}
