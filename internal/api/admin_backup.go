package api

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/abedegno/muesli/internal/backup"
	"github.com/go-chi/chi/v5"
)

// backupRunner returns the injected backup.Runner, defaulting to the real
// pg_dump-shelling implementation when none was wired (production). Tests
// wire s.deps.BackupRunner to a stub so no test ever invokes pg_dump.
func (s *Server) backupRunner() backup.Runner {
	if s.deps.BackupRunner != nil {
		return s.deps.BackupRunner
	}
	return backup.PgDumpRunner{}
}

// handleCreateBackup runs a Postgres backup now via the injected Runner,
// prunes the backup dir to the configured retention count, and returns the
// new backup's metadata. Returns 400 (not 500) when the feature isn't
// configured.
func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	dir := s.deps.Config.BackupDir
	if dir == "" {
		writeError(w, http.StatusBadRequest, "backups are not configured (set MUESLI_BACKUP_DIR)")
		return
	}
	info, err := backup.Create(r.Context(), s.backupRunner(), s.deps.Config.DatabaseURL, dir)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin backup: create failed", "error", err)
		writeError(w, http.StatusInternalServerError, "backup failed")
		return
	}
	if err := backup.Prune(dir, s.deps.Config.BackupRetentionCount); err != nil {
		slog.ErrorContext(r.Context(), "admin backup: prune failed", "error", err)
	}
	writeJSON(w, http.StatusCreated, info)
}

// handleListBackups lists current backups newest-first. Returns an empty
// list (not an error) if the dir is empty, and 400 if backups are not
// configured.
func (s *Server) handleListBackups(w http.ResponseWriter, r *http.Request) {
	dir := s.deps.Config.BackupDir
	if dir == "" {
		writeError(w, http.StatusBadRequest, "backups are not configured (set MUESLI_BACKUP_DIR)")
		return
	}
	list, err := backup.List(dir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleDownloadBackup streams one backup file as an attachment. filename is
// validated strictly against the naming convention before it ever touches
// the filesystem, which also rejects path traversal.
func (s *Server) handleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	dir := s.deps.Config.BackupDir
	if dir == "" {
		writeError(w, http.StatusBadRequest, "backups are not configured (set MUESLI_BACKUP_DIR)")
		return
	}
	filename := chi.URLParam(r, "filename")
	if !backup.ValidFilename(filename) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	f, err := os.Open(filepath.Join(dir, filename))
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Length", strconv.FormatInt(fi.Size(), 10))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}

// backupListRunner returns the injected backup.ListRunner, defaulting to the
// real pg_restore-shelling implementation when none was wired (production).
// Tests wire s.deps.BackupListRunner to a stub so no test ever invokes
// pg_restore.
func (s *Server) backupListRunner() backup.ListRunner {
	if s.deps.BackupListRunner != nil {
		return s.deps.BackupListRunner
	}
	return backup.PgRestoreListRunner{}
}

// handleVerifyBackup verifies a chosen backup's integrity without restoring
// anything, by running pg_restore --list against the dump file (reads the
// archive's table-of-contents only — never touches a database).
func (s *Server) handleVerifyBackup(w http.ResponseWriter, r *http.Request) {
	dir := s.deps.Config.BackupDir
	if dir == "" {
		writeError(w, http.StatusBadRequest, "backups are not configured (set MUESLI_BACKUP_DIR)")
		return
	}
	filename := chi.URLParam(r, "filename")
	result, err := backup.Verify(r.Context(), s.backupListRunner(), dir, filename)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
