package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

type folderMemberItemView struct {
	UserID    string `json:"user_id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

type folderMemberListView struct {
	Items      []folderMemberItemView `json:"items"`
	NextCursor string                 `json:"next_cursor"`
}

func TestFolderMembersUpsertOwnerOnly(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ownerHdr := setupLoginHdr(t, srv, "fmapi-owner@example.com")
	viewerID := createSecondUser(t, st, "fmapi-viewer@example.com", "password123")
	otherID := createSecondUser(t, st, "fmapi-other@example.com", "password123")
	otherHdr := loginHdr(t, srv, "fmapi-other@example.com", "password123")

	rec := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "Shared"}, ownerHdr)
	var folder struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &folder)

	// Non-owner cannot grant.
	rec = doJSON(t, srv, http.MethodPost, "/api/folders/"+folder.ID+"/members",
		map[string]any{"user_id": viewerID, "role": "viewer"}, otherHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-owner grant: status = %d, want 404", rec.Code)
	}

	// Owner grants.
	rec = doJSON(t, srv, http.MethodPost, "/api/folders/"+folder.ID+"/members",
		map[string]any{"user_id": viewerID, "role": "viewer"}, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner grant: status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	var member folderMemberItemView
	_ = json.Unmarshal(rec.Body.Bytes(), &member)
	if member.UserID != viewerID || member.Role != "viewer" || member.Email != "fmapi-viewer@example.com" || member.CreatedAt == "" {
		t.Fatalf("unexpected member: %+v", member)
	}

	// Self-grant rejected.
	ownerUser, err := st.GetUserByEmail(context.Background(), "fmapi-owner@example.com")
	if err != nil {
		t.Fatalf("lookup owner: %v", err)
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/folders/"+folder.ID+"/members",
		map[string]any{"user_id": ownerUser.ID, "role": "viewer"}, ownerHdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("self-grant: status = %d, want 400", rec.Code)
	}

	_ = otherID
}

func TestFolderMembersRevokeIdempotent(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ownerHdr := setupLoginHdr(t, srv, "fmrev-owner@example.com")
	viewerID := createSecondUser(t, st, "fmrev-viewer@example.com", "password123")

	rec := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "F"}, ownerHdr)
	var folder struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &folder)

	rec = doJSON(t, srv, http.MethodPost, "/api/folders/"+folder.ID+"/members",
		map[string]any{"user_id": viewerID, "role": "viewer"}, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("grant: status %d body %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, http.MethodDelete, "/api/folders/"+folder.ID+"/members/"+viewerID, nil, ownerHdr)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("first revoke: status = %d, want 204", rec.Code)
	}
	rec = doJSON(t, srv, http.MethodDelete, "/api/folders/"+folder.ID+"/members/"+viewerID, nil, ownerHdr)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("second revoke: status = %d, want 204 (idempotent)", rec.Code)
	}
}

func TestFolderMembersListOwnerAndMemberMalformedInput(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ownerHdr := setupLoginHdr(t, srv, "fmlist-owner@example.com")
	viewerID := createSecondUser(t, st, "fmlist-viewer@example.com", "password123")
	viewerHdr := loginHdr(t, srv, "fmlist-viewer@example.com", "password123")
	strangerID := createSecondUser(t, st, "fmlist-stranger@example.com", "password123")
	strangerHdr := loginHdr(t, srv, "fmlist-stranger@example.com", "password123")
	_ = strangerID

	rec := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "F"}, ownerHdr)
	var folder struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &folder)
	doJSON(t, srv, http.MethodPost, "/api/folders/"+folder.ID+"/members",
		map[string]any{"user_id": viewerID, "role": "viewer"}, ownerHdr)

	rec = doJSON(t, srv, http.MethodGet, "/api/folders/"+folder.ID+"/members", nil, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner list: status %d body %s", rec.Code, rec.Body)
	}
	var got folderMemberListView
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got.Items) != 1 || got.Items[0].UserID != viewerID {
		t.Fatalf("unexpected list: %+v", got)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/folders/"+folder.ID+"/members", nil, viewerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("member list: status %d body %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/folders/"+folder.ID+"/members", nil, strangerHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("stranger list: status = %d, want 404", rec.Code)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/folders/"+folder.ID+"/members?limit=0", nil, ownerHdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad limit: status = %d, want 400", rec.Code)
	}
	rec = doJSON(t, srv, http.MethodGet, "/api/folders/"+folder.ID+"/members?cursor=!!!bad", nil, ownerHdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad cursor: status = %d, want 400", rec.Code)
	}
}

func TestFolderMembersSoftDeletedFolder404(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ownerHdr := setupLoginHdr(t, srv, "fmsoft-owner@example.com")
	viewerID := createSecondUser(t, st, "fmsoft-viewer@example.com", "password123")

	rec := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "F"}, ownerHdr)
	var folder struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &folder)
	doJSON(t, srv, http.MethodPost, "/api/folders/"+folder.ID+"/members",
		map[string]any{"user_id": viewerID, "role": "viewer"}, ownerHdr)

	rec = doJSON(t, srv, http.MethodDelete, "/api/folders/"+folder.ID, nil, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("soft delete: status %d body %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/folders/"+folder.ID+"/members", nil, ownerHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("list after soft delete: status = %d, want 404", rec.Code)
	}
}
