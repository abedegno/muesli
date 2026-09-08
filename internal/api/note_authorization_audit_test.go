package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestNoteAuthorizationAudit is a table-driven integration audit (issue
// #12, task 8): it grants a viewer explicit read-only access to a folder
// containing one note, then invokes every non-display sensitive path this
// slice deliberately left owner-only, and requires each one to deny the
// viewer non-disclosingly (404, matching an invisible object -- never 403,
// which would confirm the note's existence) and to mutate nothing. Finally
// it proves the SAME requests succeed for the owner, so a case is never
// "passing" merely because the request itself was malformed.
func TestNoteAuthorizationAudit(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ownerHdr := setupLoginHdr(t, srv, "audit-owner@example.com")
	viewerID := createSecondUser(t, st, "audit-viewer@example.com", "password123")
	viewerHdr := loginHdr(t, srv, "audit-viewer@example.com", "password123")

	rec := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "Shared"}, ownerHdr)
	var folder struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &folder)
	doJSON(t, srv, http.MethodPost, "/api/folders/"+folder.ID+"/members",
		map[string]any{"user_id": viewerID, "role": "viewer"}, ownerHdr)

	rec = doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Audited"}, ownerHdr)
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)
	doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/folders",
		map[string]string{"folder_id": folder.ID}, ownerHdr)
	doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/tags", map[string]string{"name": "t1"}, ownerHdr)

	cases := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"duplicate", http.MethodPost, "/api/notes/" + note.ID + "/duplicate", nil},
		{"export", http.MethodGet, "/api/notes/" + note.ID + "/export", nil},
		{"audio download url", http.MethodGet, "/api/notes/" + note.ID + "/audio-url", nil},
		{"audio upload url", http.MethodPost, "/api/notes/" + note.ID + "/audio-upload-url", nil},
		{"resummarize", http.MethodPost, "/api/notes/" + note.ID + "/resummarize", nil},
		{"retranscribe", http.MethodPost, "/api/notes/" + note.ID + "/retranscribe", nil},
		{"retry", http.MethodPost, "/api/notes/" + note.ID + "/retry", nil},
		{"process-next", http.MethodPost, "/api/notes/" + note.ID + "/process-next", nil},
		{"set event", http.MethodPost, "/api/notes/" + note.ID + "/event", map[string]string{"event_id": "00000000-0000-0000-0000-000000000000"}},
		{"update title", http.MethodPatch, "/api/notes/" + note.ID, map[string]string{"title": "hijacked"}},
		{"update body", http.MethodPut, "/api/notes/" + note.ID + "/body", map[string]string{"content": "hijacked"}},
		{"add tag", http.MethodPost, "/api/notes/" + note.ID + "/tags", map[string]string{"name": "hijacked"}},
		{"remove tag", http.MethodDelete, "/api/notes/" + note.ID + "/tags?name=t1", nil},
		{"add folder", http.MethodPost, "/api/notes/" + note.ID + "/folders", map[string]string{"folder_id": folder.ID}},
		{"remove folder", http.MethodDelete, "/api/notes/" + note.ID + "/folders/" + folder.ID, nil},
		{"list action items", http.MethodGet, "/api/notes/" + note.ID + "/action-items", nil},
		{"list people", http.MethodGet, "/api/notes/" + note.ID + "/people", nil},
		{"list speaker aliases", http.MethodGet, "/api/notes/" + note.ID + "/speaker-aliases", nil},
		{"diarization review", http.MethodGet, "/api/notes/" + note.ID + "/transcript/review", nil},
		{"delete", http.MethodDelete, "/api/notes/" + note.ID, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, srv, tc.method, tc.path, tc.body, viewerHdr)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("viewer %s %s: status = %d, want 404 (non-disclosing denial); body %s",
					tc.method, tc.path, rec.Code, rec.Body)
			}
		})
	}

	// The note must be entirely unaffected by the denied attempts: title,
	// tags, and folder membership all still reflect only the owner's setup.
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID, nil, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner sanity read: status %d body %s", rec.Code, rec.Body)
	}
	var got struct {
		Title     string   `json:"title"`
		Tags      []string `json:"tags"`
		FolderIDs []string `json:"folder_ids"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Title != "Audited" {
		t.Fatalf("title mutated by a denied viewer request: %q", got.Title)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "t1" {
		t.Fatalf("tags mutated by a denied viewer request: %v", got.Tags)
	}
	if len(got.FolderIDs) != 1 || got.FolderIDs[0] != folder.ID {
		t.Fatalf("folder membership mutated by a denied viewer request: %v", got.FolderIDs)
	}

	// Owner success sanity: a representative non-mutating case (export) and
	// the destructive one (delete, run last) both succeed for the owner,
	// proving the 404s above were authorization, not broken requests.
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID+"/export", nil, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner export: status %d body %s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, http.MethodDelete, "/api/notes/"+note.ID, nil, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner delete: status %d body %s", rec.Code, rec.Body)
	}
}

// TestBatchExportAuditsEachNote proves POST /api/export/batch denies a
// viewer-visible-but-not-owned note id even when it is only ONE entry among
// several, rather than silently skipping it.
func TestBatchExportAuditsEachNote(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ownerHdr := setupLoginHdr(t, srv, "batch-owner@example.com")
	viewerID := createSecondUser(t, st, "batch-viewer@example.com", "password123")
	viewerHdr := loginHdr(t, srv, "batch-viewer@example.com", "password123")

	rec := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "Shared"}, ownerHdr)
	var folder struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &folder)
	doJSON(t, srv, http.MethodPost, "/api/folders/"+folder.ID+"/members",
		map[string]any{"user_id": viewerID, "role": "viewer"}, ownerHdr)
	rec = doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Shared"}, ownerHdr)
	var sharedNote struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &sharedNote)
	doJSON(t, srv, http.MethodPost, "/api/notes/"+sharedNote.ID+"/folders",
		map[string]string{"folder_id": folder.ID}, ownerHdr)

	rec = doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Viewer's own"}, viewerHdr)
	var ownNote struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &ownNote)

	rec = doJSON(t, srv, http.MethodPost, "/api/export/batch", map[string]any{
		"note_ids": []string{ownNote.ID, sharedNote.ID},
		"format":   "md",
	}, viewerHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("batch export with a viewer-visible-only note: status = %d, want 404; body %s", rec.Code, rec.Body)
	}
}
