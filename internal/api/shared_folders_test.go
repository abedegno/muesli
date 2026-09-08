package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestSharedFoldersListExcludesOwnFoldersAndPaginates(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ownerHdr := setupLoginHdr(t, srv, "sf-owner@example.com")
	viewerID := createSecondUser(t, st, "sf-viewer@example.com", "password123")
	viewerHdr := loginHdr(t, srv, "sf-viewer@example.com", "password123")

	// Viewer's own folder must never appear.
	doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "Mine"}, viewerHdr)

	var folderIDs []string
	for i := 0; i < 3; i++ {
		rec := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "Shared"}, ownerHdr)
		var f struct{ ID string }
		_ = json.Unmarshal(rec.Body.Bytes(), &f)
		rec = doJSON(t, srv, http.MethodPost, "/api/folders/"+f.ID+"/members",
			map[string]any{"user_id": viewerID, "role": "viewer"}, ownerHdr)
		if rec.Code != http.StatusOK {
			t.Fatalf("grant %d: status %d body %s", i, rec.Code, rec.Body)
		}
		folderIDs = append(folderIDs, f.ID)
	}

	rec := doJSON(t, srv, http.MethodGet, "/api/folders/shared", nil, viewerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	var got sharedFolderListView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Items) != 3 {
		t.Fatalf("expected 3 shared folders, got %d: %+v", len(got.Items), got.Items)
	}
	for _, it := range got.Items {
		if it.CountScope != "direct" || it.OwnerEmail != "sf-owner@example.com" {
			t.Fatalf("unexpected item: %+v", it)
		}
	}

	// Owner does not see their own folders through THIS endpoint.
	rec = doJSON(t, srv, http.MethodGet, "/api/folders/shared", nil, ownerHdr)
	var ownerSide sharedFolderListView
	_ = json.Unmarshal(rec.Body.Bytes(), &ownerSide)
	if len(ownerSide.Items) != 0 {
		t.Fatalf("owner should see 0 shared folders (they own these, not share into them): %+v", ownerSide.Items)
	}

	// Pagination.
	rec = doJSON(t, srv, http.MethodGet, "/api/folders/shared?limit=1", nil, viewerHdr)
	var page1 sharedFolderListView
	_ = json.Unmarshal(rec.Body.Bytes(), &page1)
	if len(page1.Items) != 1 || page1.NextCursor == "" {
		t.Fatalf("page1 = %+v, want 1 item + cursor", page1)
	}
	rec = doJSON(t, srv, http.MethodGet, "/api/folders/shared?limit=1&cursor="+page1.NextCursor, nil, viewerHdr)
	var page2 sharedFolderListView
	_ = json.Unmarshal(rec.Body.Bytes(), &page2)
	if len(page2.Items) != 1 || page2.Items[0].ID == page1.Items[0].ID {
		t.Fatalf("page2 = %+v, must not repeat page1's item", page2)
	}
}

type sharedFolderItemView struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	OwnerID    string `json:"owner_id"`
	OwnerEmail string `json:"owner_email"`
	NoteCount  int    `json:"note_count"`
	CountScope string `json:"count_scope"`
}

type sharedFolderListView struct {
	Items      []sharedFolderItemView `json:"items"`
	NextCursor string                 `json:"next_cursor"`
}

func TestSharedFoldersMalformedInput(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	hdr := setupLoginHdr(t, srv, "sf-malformed@example.com")

	rec := doJSON(t, srv, http.MethodGet, "/api/folders/shared?limit=0", nil, hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("limit=0: status = %d, want 400", rec.Code)
	}
	rec = doJSON(t, srv, http.MethodGet, "/api/folders/shared?limit=101", nil, hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("limit=101: status = %d, want 400", rec.Code)
	}
	rec = doJSON(t, srv, http.MethodGet, "/api/folders/shared?cursor=not-valid!!!", nil, hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad cursor: status = %d, want 400", rec.Code)
	}
}

func TestSharedFoldersRevocationAndSoftDelete(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ownerHdr := setupLoginHdr(t, srv, "sf-revoke-owner@example.com")
	viewerID := createSecondUser(t, st, "sf-revoke-viewer@example.com", "password123")
	viewerHdr := loginHdr(t, srv, "sf-revoke-viewer@example.com", "password123")

	rec := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "F"}, ownerHdr)
	var f struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &f)
	doJSON(t, srv, http.MethodPost, "/api/folders/"+f.ID+"/members",
		map[string]any{"user_id": viewerID, "role": "viewer"}, ownerHdr)

	rec = doJSON(t, srv, http.MethodGet, "/api/folders/shared", nil, viewerHdr)
	var before sharedFolderListView
	_ = json.Unmarshal(rec.Body.Bytes(), &before)
	if len(before.Items) != 1 {
		t.Fatalf("expected 1 shared folder before revocation, got %d", len(before.Items))
	}

	doJSON(t, srv, http.MethodDelete, "/api/folders/"+f.ID+"/members/"+viewerID, nil, ownerHdr)
	rec = doJSON(t, srv, http.MethodGet, "/api/folders/shared", nil, viewerHdr)
	var after sharedFolderListView
	_ = json.Unmarshal(rec.Body.Bytes(), &after)
	if len(after.Items) != 0 {
		t.Fatalf("expected 0 shared folders after revocation, got %d", len(after.Items))
	}

	// Re-grant, then soft-delete the folder: it must also drop out.
	doJSON(t, srv, http.MethodPost, "/api/folders/"+f.ID+"/members",
		map[string]any{"user_id": viewerID, "role": "viewer"}, ownerHdr)
	doJSON(t, srv, http.MethodDelete, "/api/folders/"+f.ID, nil, ownerHdr)
	rec = doJSON(t, srv, http.MethodGet, "/api/folders/shared", nil, viewerHdr)
	var afterDelete sharedFolderListView
	_ = json.Unmarshal(rec.Body.Bytes(), &afterDelete)
	if len(afterDelete.Items) != 0 {
		t.Fatalf("expected 0 shared folders after folder soft delete, got %d", len(afterDelete.Items))
	}
}

// TestListFoldersRemainsBareUnpaginatedArray is a regression test for the
// design's explicit constraint: GET /api/folders (the owner-anchored
// recursive tree) is NOT merged with shared folders and keeps returning a
// bare JSON array, not an {items,next_cursor} envelope.
func TestListFoldersRemainsBareUnpaginatedArray(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	hdr := setupLoginHdr(t, srv, "sf-bare-array@example.com")
	doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "A"}, hdr)
	doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "B"}, hdr)

	rec := doJSON(t, srv, http.MethodGet, "/api/folders", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	var arr []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &arr); err != nil {
		t.Fatalf("expected a bare JSON array, got: %s (%v)", rec.Body, err)
	}
	if len(arr) != 2 {
		t.Fatalf("expected 2 folders, got %d", len(arr))
	}
	for _, f := range arr {
		if _, ok := f["note_count"]; !ok {
			t.Fatalf("owner folder missing note_count: %+v", f)
		}
		if _, ok := f["count_scope"]; ok {
			t.Fatalf("owner folder must not carry count_scope (that's the shared-folder shape): %+v", f)
		}
	}
}
