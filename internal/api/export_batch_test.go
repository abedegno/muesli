package api_test

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset, so these tests must not be run on the local
// runner.

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/auth"
)

func TestBatchExportFolderZip(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "batch@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "batch@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	folderRec := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "Projects"}, hdr)
	if folderRec.Code != http.StatusCreated {
		t.Fatalf("create folder status=%d body=%s", folderRec.Code, folderRec.Body)
	}
	var folder struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(folderRec.Body.Bytes(), &folder)

	mkNote := func(title string) string {
		rec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]any{"title": title}, hdr)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create note status=%d body=%s", rec.Code, rec.Body)
		}
		var note struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &note)
		added := doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/folders",
			map[string]any{"folder_id": folder.ID}, hdr)
		if added.Code != http.StatusOK {
			t.Fatalf("add note to folder status=%d body=%s", added.Code, added.Body)
		}
		return note.ID
	}

	_ = mkNote("Team Update")
	_ = mkNote("Team Update")

	exportRec := doJSON(t, srv, http.MethodPost, "/api/export/batch",
		map[string]any{"folder_id": folder.ID, "format": "md"}, hdr)
	if exportRec.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", exportRec.Code, exportRec.Body)
	}
	if got := exportRec.Header().Get("Content-Type"); got != "application/zip" {
		t.Fatalf("content-type = %q, want application/zip", got)
	}
	if got := exportRec.Header().Get("Content-Disposition"); got == "" {
		t.Fatal("missing content-disposition")
	}

	zr, err := zip.NewReader(bytes.NewReader(exportRec.Body.Bytes()), int64(exportRec.Body.Len()))
	if err != nil {
		t.Fatalf("zip reader: %v", err)
	}
	if len(zr.File) != 2 {
		t.Fatalf("zip entries = %d, want 2", len(zr.File))
	}

	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open zip entry %s: %v", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read zip entry %s: %v", f.Name, err)
		}
		if len(data) == 0 {
			t.Fatalf("zip entry %s is empty", f.Name)
		}
	}
	if !names["team-update.md"] || !names["team-update-2.md"] {
		t.Fatalf("unexpected zip names: %v", names)
	}
}

func TestBatchExportFolderOwnerScopeAndValidation(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "owner1@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "owner1@example.com", "password": "password123"}, nil)
	var login1 struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login1)
	hdr1 := map[string]string{"Authorization": "Bearer " + login1.Token}

	other, err := st.CreateUser(ctx, "owner2@example.com", "password123")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	raw, hash, err := auth.GenerateToken()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if err := st.CreateToken(ctx, other.ID, "session", hash, "session"); err != nil {
		t.Fatalf("create token: %v", err)
	}
	login2 := struct {
		Token string `json:"token"`
	}{Token: raw}
	hdr2 := map[string]string{"Authorization": "Bearer " + login2.Token}

	folderRec := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "Private"}, hdr1)
	if folderRec.Code != http.StatusCreated {
		t.Fatalf("create folder status=%d body=%s", folderRec.Code, folderRec.Body)
	}
	var folder struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(folderRec.Body.Bytes(), &folder)

	nf := doJSON(t, srv, http.MethodPost, "/api/export/batch",
		map[string]any{"folder_id": folder.ID, "format": "md"}, hdr2)
	if nf.Code != http.StatusNotFound {
		t.Fatalf("cross-owner export status=%d body=%s", nf.Code, nf.Body)
	}

	missing := doJSON(t, srv, http.MethodPost, "/api/export/batch",
		map[string]any{"folder_id": "00000000-0000-0000-0000-000000000000", "format": "md"}, hdr1)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing folder status=%d body=%s", missing.Code, missing.Body)
	}

	badFmt := doJSON(t, srv, http.MethodPost, "/api/export/batch",
		map[string]any{"folder_id": folder.ID, "format": "csv"}, hdr1)
	if badFmt.Code != http.StatusBadRequest {
		t.Fatalf("bad format status=%d body=%s", badFmt.Code, badFmt.Body)
	}
}

func TestBatchExportNoteIDs(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "noteids@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "noteids@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	mkNote := func(title string) string {
		rec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]any{"title": title}, hdr)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create note status=%d body=%s", rec.Code, rec.Body)
		}
		var note struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &note)
		return note.ID
	}

	firstID := mkNote("Alpha")
	secondID := mkNote("Alpha")

	exportRec := doJSON(t, srv, http.MethodPost, "/api/export/batch",
		map[string]any{"note_ids": []string{firstID, secondID}, "format": "md"}, hdr)
	if exportRec.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", exportRec.Code, exportRec.Body)
	}

	zr, err := zip.NewReader(bytes.NewReader(exportRec.Body.Bytes()), int64(exportRec.Body.Len()))
	if err != nil {
		t.Fatalf("zip reader: %v", err)
	}
	if len(zr.File) != 2 {
		t.Fatalf("zip entries = %d, want 2", len(zr.File))
	}

	other, err := st.CreateUser(ctx, "other-noteids@example.com", "password123")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	raw, hash, err := auth.GenerateToken()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if err := st.CreateToken(ctx, other.ID, "session", hash, "session"); err != nil {
		t.Fatalf("create token: %v", err)
	}
	otherHdr := map[string]string{"Authorization": "Bearer " + raw}
	otherNoteRec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]any{"title": "Foreign"}, otherHdr)
	if otherNoteRec.Code != http.StatusCreated {
		t.Fatalf("create other note status=%d body=%s", otherNoteRec.Code, otherNoteRec.Body)
	}
	var otherNote struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(otherNoteRec.Body.Bytes(), &otherNote)

	cross := doJSON(t, srv, http.MethodPost, "/api/export/batch",
		map[string]any{"note_ids": []string{otherNote.ID}, "format": "md"}, hdr)
	if cross.Code != http.StatusNotFound {
		t.Fatalf("cross-owner note export status=%d body=%s", cross.Code, cross.Body)
	}

	missing := doJSON(t, srv, http.MethodPost, "/api/export/batch",
		map[string]any{"note_ids": []string{"00000000-0000-0000-0000-000000000000"}, "format": "md"}, hdr)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing note status=%d body=%s", missing.Code, missing.Body)
	}

	empty := doJSON(t, srv, http.MethodPost, "/api/export/batch",
		map[string]any{"note_ids": []string{}, "format": "md"}, hdr)
	if empty.Code != http.StatusBadRequest {
		t.Fatalf("empty note_ids status=%d body=%s", empty.Code, empty.Body)
	}
}

func TestBatchExportOptions(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "batch-options@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "batch-options@example.com", "password": "password123"}, nil)
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

	exportRec := doJSON(t, srv, http.MethodPost, "/api/export/batch",
		map[string]any{
			"note_ids":           []string{note.ID},
			"format":             "md",
			"include_transcript": false,
		}, hdr)
	if exportRec.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", exportRec.Code, exportRec.Body)
	}
	zr, err := zip.NewReader(bytes.NewReader(exportRec.Body.Bytes()), int64(exportRec.Body.Len()))
	if err != nil {
		t.Fatalf("zip reader: %v", err)
	}
	if len(zr.File) != 1 {
		t.Fatalf("zip entries = %d, want 1", len(zr.File))
	}
	rc, err := zr.File[0].Open()
	if err != nil {
		t.Fatalf("open zip entry: %v", err)
	}
	body, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatalf("read zip entry: %v", err)
	}
	content := string(body)
	if !strings.Contains(content, "Overview") || !strings.Contains(content, "Summary line.") {
		t.Fatalf("batch export missing summary content:\n%s", content)
	}
	if strings.Contains(content, "Transcript") || strings.Contains(content, "Alice:") {
		t.Fatalf("batch export unexpectedly included transcript:\n%s", content)
	}

	exportRec = doJSON(t, srv, http.MethodPost, "/api/export/batch",
		map[string]any{
			"note_ids":        []string{note.ID},
			"format":          "md",
			"redact_speakers": true,
		}, hdr)
	if exportRec.Code != http.StatusOK {
		t.Fatalf("redacted export status=%d body=%s", exportRec.Code, exportRec.Body)
	}
	zr, err = zip.NewReader(bytes.NewReader(exportRec.Body.Bytes()), int64(exportRec.Body.Len()))
	if err != nil {
		t.Fatalf("zip reader: %v", err)
	}
	rc, err = zr.File[0].Open()
	if err != nil {
		t.Fatalf("open zip entry: %v", err)
	}
	body, err = io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatalf("read zip entry: %v", err)
	}
	content = string(body)
	if !strings.Contains(content, "Speaker 1: We should ship it.") || !strings.Contains(content, "Speaker 2: Then we can announce it.") {
		t.Fatalf("batch export missing redacted speakers:\n%s", content)
	}
	if strings.Contains(content, "Alice:") || strings.Contains(content, "Bob:") {
		t.Fatalf("batch export unexpectedly used aliases:\n%s", content)
	}
}
