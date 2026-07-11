package api

import (
	"net/http"
	"os"

	"github.com/abedegno/muesli/internal/config"
)

// handleAdminConfig returns the effective, redacted MUESLI_* configuration
// for the admin panel (ADM06): an ordered list of {name, envVar, value,
// source} rows built from the already-loaded config.Config struct, never a
// raw os.Environ() dump. Secret-shaped fields (name/envVar matching
// token/key/password/secret, case-insensitive) are collapsed to
// "(set)"/"(unset)" by config.Effective. Admin auth is enforced by the
// router middleware (same authenticated group as the other /api/admin/*
// routes).
func (s *Server) handleAdminConfig(w http.ResponseWriter, r *http.Request) {
	entries := config.Effective(s.deps.Config, os.LookupEnv)
	writeJSON(w, http.StatusOK, entries)
}
