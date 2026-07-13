package api_test

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset, so it must not be run on the local runner.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestGetNoteExportOptions(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "export-options@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "export-options@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	noteRec := doJSON(t, srv, http.MethodPost, "/api/notes",
		map[string]any{"title": "Planning Review"}, hdr)
	if noteRec.Code != http.StatusCreated {
		t.Fatalf("create note status=%d body=%s", noteRec.Code, noteRec.Body)
	}
	var note struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(noteRec.Body.Bytes(), &note)
	ownerID, err := st.NoteOwnerID(context.Background(), note.ID)
	if err != nil {
		t.Fatalf("note owner id: %v", err)
	}
	seedExportableNote(t, st, ownerID, note.ID)

	exportRec := doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID+"/export?format=md&include_transcript=0",
		nil, hdr)
	if exportRec.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", exportRec.Code, exportRec.Body)
	}
	if got := exportRec.Header().Get("Content-Type"); got != "text/markdown; charset=utf-8" {
		t.Fatalf("content-type = %q", got)
	}
	if got := exportRec.Header().Get("Content-Disposition"); !strings.Contains(got, "planning-review.md") {
		t.Fatalf("content-disposition = %q", got)
	}
	body := exportRec.Body.String()
	if !strings.Contains(body, "Overview") || !strings.Contains(body, "Summary line.") {
		t.Fatalf("export missing summary content:\n%s", body)
	}
	if strings.Contains(body, "Transcript") || strings.Contains(body, "Alice:") {
		t.Fatalf("export unexpectedly included transcript:\n%s", body)
	}

	exportRec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID+"/export?format=md&redact_speakers=1",
		nil, hdr)
	if exportRec.Code != http.StatusOK {
		t.Fatalf("redacted export status=%d body=%s", exportRec.Code, exportRec.Body)
	}
	body = exportRec.Body.String()
	if !strings.Contains(body, "Speaker 1: We should ship it.") || !strings.Contains(body, "Speaker 2: Then we can announce it.") {
		t.Fatalf("export missing redacted transcript:\n%s", body)
	}
	if strings.Contains(body, "Alice:") || strings.Contains(body, "Bob:") {
		t.Fatalf("export unexpectedly used aliases:\n%s", body)
	}

	otherHdr, _ := authHeaderForUser(t, st, "other-export-options@example.com")
	nf := doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID+"/export", nil, otherHdr)
	if nf.Code != http.StatusNotFound {
		t.Fatalf("cross-owner export status=%d body=%s", nf.Code, nf.Body)
	}

	missing := doJSON(t, srv, http.MethodGet, "/api/notes/00000000-0000-0000-0000-000000000000/export", nil, hdr)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing note export status=%d body=%s", missing.Code, missing.Body)
	}
}
