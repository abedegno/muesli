package api

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/abedegno/muesli/internal/audiohash"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/storage"
	"github.com/abedegno/muesli/internal/store"
	"github.com/google/uuid"
)

func allowedAudioContentType(raw string, allowed []string) (string, bool) {
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil || mediaType == "" {
		return "", false
	}
	if len(allowed) == 0 {
		allowed = storage.DefaultAllowedContentTypes
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, t := range allowed {
		allowedSet[strings.ToLower(strings.TrimSpace(t))] = struct{}{}
	}
	_, ok := allowedSet[strings.ToLower(mediaType)]
	return mediaType, ok
}

func uploadWithinLimit(size, max int64) bool { return size <= max }

func titleFromFilename(filename string) string {
	base := strings.TrimSpace(filepath.Base(filename))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "Imported audio"
	}
	title := strings.TrimSpace(strings.TrimSuffix(base, filepath.Ext(base)))
	if title == "" {
		return "Imported audio"
	}
	return title
}

// handleImportNote accepts a multipart/form-data upload, stores the audio using
// the same storage object path and validation-bearing upload handler as live
// capture, and enqueues the same transcription job.
func (s *Server) handleImportNote(w http.ResponseWriter, r *http.Request) {
	ownerID, _ := userIDFromContext(r.Context())

	maxUpload := s.deps.Storage.MaxUploadBytes()
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "multipart form required")
		return
	}

	var title string
	var part *multipart.Part
	var rawContentType string
	var filename string

	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid multipart body")
			return
		}
		switch p.FormName() {
		case "title":
			if title == "" {
				b, err := io.ReadAll(io.LimitReader(p, 4096))
				if err != nil {
					writeError(w, http.StatusBadRequest, "invalid title")
					return
				}
				title = strings.TrimSpace(string(b))
			}
		case "file":
			if part != nil {
				writeError(w, http.StatusBadRequest, "multiple file parts are not supported")
				return
			}
			part = p
			rawContentType = p.Header.Get("Content-Type")
			filename = p.FileName()
			goto haveFile
		default:
			// Ignore unrecognized form fields.
		}
	}

haveFile:
	if part == nil {
		writeError(w, http.StatusBadRequest, "file required")
		return
	}
	defer part.Close()

	if _, ok := allowedAudioContentType(rawContentType, s.deps.Config.UploadAllowedContentTypes); !ok {
		writeError(w, http.StatusBadRequest, "unsupported or missing audio content type")
		return
	}

	tmp, err := os.CreateTemp("", "muesli-import-*.audio")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	limited := &io.LimitedReader{R: part, N: maxUpload + 1}
	hash, err := audiohash.HashRaw(io.TeeReader(limited, tmp))
	if err != nil {
		_ = tmp.Close()
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	size := maxUpload + 1 - limited.N
	if err := tmp.Close(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !uploadWithinLimit(size, maxUpload) {
		writeError(w, http.StatusRequestEntityTooLarge, "payload too large")
		return
	}

	notes, err := s.deps.Store.FindNotesByAudioHash(r.Context(), ownerID, hash, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if match, ok := firstAudioMatch(notes); ok {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "duplicate audio",
			"match": match,
		})
		return
	}

	if title == "" {
		title = titleFromFilename(filename)
	}
	note, err := s.deps.Store.CreateNote(r.Context(), ownerID, title)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	key := "notes/" + note.ID + "/audio/" + uuid.NewString()
	grant, err := s.deps.Storage.PresignUpload(key, 15*time.Minute)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	uploader, ok := s.deps.Storage.(interface{ UploadHandler() http.Handler })
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	src, err := os.Open(tmpName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer src.Close()

	putReq := httptest.NewRequest(http.MethodPut, grant.URL, src)
	putReq.Header.Set("Content-Type", rawContentType)
	putRec := httptest.NewRecorder()
	uploader.UploadHandler().ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusOK {
		switch putRec.Code {
		case http.StatusRequestEntityTooLarge:
			writeError(w, http.StatusRequestEntityTooLarge, "payload too large")
		case http.StatusUnsupportedMediaType, http.StatusBadRequest:
			writeError(w, http.StatusBadRequest, "audio upload rejected")
		default:
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	if err := s.deps.Store.SetNoteAudio(r.Context(), ownerID, note.ID, grant.Key); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := s.deps.Store.SetNoteHashesForOwner(r.Context(), ownerID, note.ID, hash, ""); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// The note was just created above, so it has no transcript yet — but read the
	// generation the same way every enqueue site does rather than assuming 0, so
	// this stays correct if note creation ever starts a transcript some other way.
	//
	// Deliberately unbound by a test, unlike the other two enqueue sites. The
	// note cannot have a transcript here, so 0 is the only value this read can
	// produce and an assertion on it would pass whether the read happened or
	// not. The sites where the value CAN differ are bound:
	// TestAudioUploadedEnqueuesCurrentGeneration (upload completion) and
	// TestRetranscribeEnqueuesCurrentGeneration (retranscribe).
	expectedGeneration, err := s.deps.Store.CurrentTranscriptGeneration(r.Context(), note.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	payload, _ := json.Marshal(map[string]any{"audio_key": grant.Key, "expected_generation": expectedGeneration})
	if _, err := s.deps.Store.EnqueueJob(r.Context(), note.ID, model.JobTranscribe, payload); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	created, err := s.deps.Store.GetNote(r.Context(), ownerID, note.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	created.Tags = []string{}
	created.FolderIDs = []string{}
	writeJSON(w, http.StatusCreated, created)
}
