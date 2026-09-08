package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

type fullNoteView struct {
	Note struct {
		ID        string   `json:"id"`
		OwnerID   string   `json:"owner_id"`
		Tags      []string `json:"tags"`
		FolderIDs []string `json:"folder_ids"`
	} `json:"note"`
	BodyMarkdown string `json:"body_markdown"`
	Transcript   *struct {
		Segments []struct {
			Text    string `json:"text"`
			Speaker string `json:"speaker"`
		} `json:"segments"`
	} `json:"transcript"`
	Summaries []map[string]any `json:"summaries"`
}

func TestHandleGetNoteFullViewerAccess(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ownerHdr := setupLoginHdr(t, srv, "hgnf-owner@example.com")
	viewerID := createSecondUser(t, st, "hgnf-viewer@example.com", "password123")
	viewerHdr := loginHdr(t, srv, "hgnf-viewer@example.com", "password123")
	strangerID := createSecondUser(t, st, "hgnf-stranger@example.com", "password123")
	strangerHdr := loginHdr(t, srv, "hgnf-stranger@example.com", "password123")
	_ = strangerID

	rec := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "Shared"}, ownerHdr)
	var folder struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &folder)
	doJSON(t, srv, http.MethodPost, "/api/folders/"+folder.ID+"/members",
		map[string]any{"user_id": viewerID, "role": "viewer"}, ownerHdr)

	rec = doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Full"}, ownerHdr)
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)
	doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/folders",
		map[string]string{"folder_id": folder.ID}, ownerHdr)
	doJSON(t, srv, http.MethodPut, "/api/notes/"+note.ID+"/body", map[string]string{"content": "Hello viewer"}, ownerHdr)
	doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/tags", map[string]string{"name": "important"}, ownerHdr)

	// Owner sees everything.
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID+"/full", nil, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner full: status %d body %s", rec.Code, rec.Body)
	}

	// Viewer sees the same metadata/body/folders/tags, scoped to the shared folder.
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID+"/full", nil, viewerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer full: status %d body %s", rec.Code, rec.Body)
	}
	var full fullNoteView
	if err := json.Unmarshal(rec.Body.Bytes(), &full); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if full.BodyMarkdown != "Hello viewer" {
		t.Fatalf("body_markdown = %q", full.BodyMarkdown)
	}
	if len(full.Note.Tags) != 1 || full.Note.Tags[0] != "important" {
		t.Fatalf("tags = %v", full.Note.Tags)
	}
	if len(full.Note.FolderIDs) != 1 || full.Note.FolderIDs[0] != folder.ID {
		t.Fatalf("folder_ids = %v", full.Note.FolderIDs)
	}

	// Stranger gets an empty 404, not a partial body.
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID+"/full", nil, strangerHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("stranger full: status = %d, want 404", rec.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if _, hasBody := body["body_markdown"]; hasBody {
		t.Fatalf("404 response must not carry note content: %s", rec.Body)
	}
}

// TestHandleGetNoteFullSoftDeletedFolderAndNoteAreEmpty404 soft-deletes the
// authorizing folder (retaining membership) and separately the note itself
// (retaining component rows), and requires an empty 404 in both cases.
func TestHandleGetNoteFullSoftDeletedFolderAndNoteAreEmpty404(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ownerHdr := setupLoginHdr(t, srv, "hgnfd-owner@example.com")
	viewerID := createSecondUser(t, st, "hgnfd-viewer@example.com", "password123")
	viewerHdr := loginHdr(t, srv, "hgnfd-viewer@example.com", "password123")

	rec := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "F"}, ownerHdr)
	var folder struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &folder)
	doJSON(t, srv, http.MethodPost, "/api/folders/"+folder.ID+"/members",
		map[string]any{"user_id": viewerID, "role": "viewer"}, ownerHdr)
	rec = doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "N"}, ownerHdr)
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)
	doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/folders",
		map[string]string{"folder_id": folder.ID}, ownerHdr)

	// Sanity: viewer can read before deletion.
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID+"/full", nil, viewerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("before delete: status %d body %s", rec.Code, rec.Body)
	}

	doJSON(t, srv, http.MethodDelete, "/api/folders/"+folder.ID, nil, ownerHdr)
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID+"/full", nil, viewerHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("after folder soft delete: status = %d, want 404", rec.Code)
	}

	// Owner path: soft-delete the note itself; owner access also goes empty 404.
	rec = doJSON(t, srv, http.MethodDelete, "/api/notes/"+note.ID, nil, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner note delete: status %d body %s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID+"/full", nil, ownerHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("owner after note soft delete: status = %d, want 404", rec.Code)
	}
}
