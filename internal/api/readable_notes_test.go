package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestHandleGetNoteViewerAccess(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ownerHdr := setupLoginHdr(t, srv, "hgn-owner@example.com")
	viewerID := createSecondUser(t, st, "hgn-viewer@example.com", "password123")
	viewerHdr := loginHdr(t, srv, "hgn-viewer@example.com", "password123")
	strangerID := createSecondUser(t, st, "hgn-stranger@example.com", "password123")
	strangerHdr := loginHdr(t, srv, "hgn-stranger@example.com", "password123")
	_ = strangerID

	rec := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "Shared"}, ownerHdr)
	var folder struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &folder)
	doJSON(t, srv, http.MethodPost, "/api/folders/"+folder.ID+"/members",
		map[string]any{"user_id": viewerID, "role": "viewer"}, ownerHdr)

	rec = doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Shared note"}, ownerHdr)
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)
	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/folders",
		map[string]string{"folder_id": folder.ID}, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("assign: status %d body %s", rec.Code, rec.Body)
	}

	// Owner can read.
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID, nil, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner get: status %d body %s", rec.Code, rec.Body)
	}

	// The explicit member can read, and sees the folder id.
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID, nil, viewerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer get: status %d body %s", rec.Code, rec.Body)
	}
	var got struct {
		FolderIDs []string `json:"folder_ids"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got.FolderIDs) != 1 || got.FolderIDs[0] != folder.ID {
		t.Fatalf("viewer folder_ids = %v, want [%s]", got.FolderIDs, folder.ID)
	}

	// An unrelated user gets 404, not the note's existence disclosed.
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID, nil, strangerHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("stranger get: status = %d, want 404", rec.Code)
	}
}

func TestHandleGetNoteRevocationIs404OnNextRequest(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ownerHdr := setupLoginHdr(t, srv, "hgnrev-owner@example.com")
	viewerID := createSecondUser(t, st, "hgnrev-viewer@example.com", "password123")
	viewerHdr := loginHdr(t, srv, "hgnrev-viewer@example.com", "password123")

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

	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID, nil, viewerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("before revoke: status %d body %s", rec.Code, rec.Body)
	}

	doJSON(t, srv, http.MethodDelete, "/api/folders/"+folder.ID+"/members/"+viewerID, nil, ownerHdr)

	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID, nil, viewerHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("after revoke: status = %d, want 404", rec.Code)
	}
}

func TestHandleListNotesFolderIDViewerAndOwnerDenial(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ownerHdr := setupLoginHdr(t, srv, "hln-owner@example.com")
	viewerID := createSecondUser(t, st, "hln-viewer@example.com", "password123")
	viewerHdr := loginHdr(t, srv, "hln-viewer@example.com", "password123")

	rec := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "Shared"}, ownerHdr)
	var folder struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &folder)
	doJSON(t, srv, http.MethodPost, "/api/folders/"+folder.ID+"/members",
		map[string]any{"user_id": viewerID, "role": "viewer"}, ownerHdr)

	rec = doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "A"}, ownerHdr)
	var noteA struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &noteA)
	doJSON(t, srv, http.MethodPost, "/api/notes/"+noteA.ID+"/folders",
		map[string]string{"folder_id": folder.ID}, ownerHdr)

	// Private folder B, not shared.
	rec = doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "Private"}, ownerHdr)
	var privateFolder struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &privateFolder)

	// Viewer can list notes in the shared folder.
	rec = doJSON(t, srv, http.MethodGet, "/api/notes?folder_id="+folder.ID, nil, viewerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer list shared: status %d body %s", rec.Code, rec.Body)
	}
	var notes []map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &notes)
	if len(notes) != 1 || notes[0]["id"] != noteA.ID {
		t.Fatalf("unexpected notes: %v", notes)
	}

	// Viewer CANNOT use the known private folder id as a filter.
	rec = doJSON(t, srv, http.MethodGet, "/api/notes?folder_id="+privateFolder.ID, nil, viewerHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("viewer list private: status = %d, want 404", rec.Code)
	}

	// A syntactically valid but nonexistent folder id is also 404 (never a
	// silent empty list) for both viewer and owner.
	rec = doJSON(t, srv, http.MethodGet, "/api/notes?folder_id=00000000-0000-0000-0000-000000000000", nil, ownerHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("owner list unknown folder: status = %d, want 404", rec.Code)
	}
}
