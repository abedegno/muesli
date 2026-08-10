package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/auth"
	"github.com/abedegno/muesli/internal/model"
)

func TestNotesAPI(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	// Create.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes",
		map[string]string{"title": "Sprint planning"}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status %d body %s", rec.Code, rec.Body)
	}
	var created struct{ ID, Status string }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ID == "" || created.Status != "draft" {
		t.Fatalf("unexpected created note %+v", created)
	}

	// Starting capture advances the draft, and retrying the action is idempotent.
	for range 2 {
		rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+created.ID+"/start-capture", nil, hdr)
		if rec.Code != http.StatusOK {
			t.Fatalf("start capture status %d body %s", rec.Code, rec.Body)
		}
		var started struct{ Status string }
		_ = json.Unmarshal(rec.Body.Bytes(), &started)
		if started.Status != "recording" {
			t.Fatalf("start capture status=%q, want recording", started.Status)
		}
	}

	// Get.
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+created.ID, nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status %d", rec.Code)
	}

	// List.
	rec = doJSON(t, srv, http.MethodGet, "/api/notes", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status %d", rec.Code)
	}
	var list []map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 {
		t.Fatalf("list len %d, want 1", len(list))
	}

	// Unauthenticated request is rejected.
	rec = doJSON(t, srv, http.MethodGet, "/api/notes", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth list status %d, want 401", rec.Code)
	}
}

func TestUpdateNoteTitleHandler(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	// Create a note.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes",
		map[string]string{"title": "Original title"}, hdr)
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)

	// Rename via PATCH /api/notes/{id}.
	rec = doJSON(t, srv, http.MethodPatch, "/api/notes/"+note.ID,
		map[string]string{"title": "Renamed"}, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch title status %d body %s", rec.Code, rec.Body)
	}
	// PATCH must return the updated note object.
	var patched struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &patched)
	if patched.ID != note.ID {
		t.Fatalf("patch response id=%q, want %q; body %s", patched.ID, note.ID, rec.Body)
	}
	if patched.Title != "Renamed" {
		t.Fatalf("patch response title=%q, want %q; body %s", patched.Title, "Renamed", rec.Body)
	}

	// Verify GET also returns the new title.
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID, nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("get after rename status %d", rec.Code)
	}
	var got struct{ Title string }
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Title != "Renamed" {
		t.Fatalf("title=%q, want %q", got.Title, "Renamed")
	}
}

func TestDuplicateNoteHandlerCopiesEditableContent(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	rec = doJSON(t, srv, http.MethodPost, "/api/notes",
		map[string]string{"title": "Original title"}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status %d body %s", rec.Code, rec.Body)
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	_ = doJSON(t, srv, http.MethodPut, "/api/notes/"+created.ID+"/body",
		map[string]string{"content": "# live notes"}, hdr)
	_ = doJSON(t, srv, http.MethodPost, "/api/notes/"+created.ID+"/tags",
		map[string]string{"name": "work"}, hdr)
	folderRec := doJSON(t, srv, http.MethodPost, "/api/folders",
		map[string]any{"name": "Clients"}, hdr)
	if folderRec.Code != http.StatusCreated {
		t.Fatalf("folder create status %d body %s", folderRec.Code, folderRec.Body)
	}
	var folder struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(folderRec.Body.Bytes(), &folder)
	_ = doJSON(t, srv, http.MethodPost, "/api/notes/"+created.ID+"/folders",
		map[string]string{"folder_id": folder.ID}, hdr)

	dupRec := doJSON(t, srv, http.MethodPost, "/api/notes/"+created.ID+"/duplicate", nil, hdr)
	if dupRec.Code != http.StatusCreated {
		t.Fatalf("duplicate status %d body %s", dupRec.Code, dupRec.Body)
	}
	var duplicated struct {
		ID        string   `json:"id"`
		Title     string   `json:"title"`
		Status    string   `json:"status"`
		Tags      []string `json:"tags"`
		FolderIDs []string `json:"folder_ids"`
	}
	_ = json.Unmarshal(dupRec.Body.Bytes(), &duplicated)
	if duplicated.ID == "" || duplicated.ID == created.ID {
		t.Fatalf("duplicate id=%q, original=%q", duplicated.ID, created.ID)
	}
	if duplicated.Title != "Copy of Original title" {
		t.Fatalf("duplicate title=%q", duplicated.Title)
	}
	if duplicated.Status != "draft" {
		t.Fatalf("duplicate status=%q, want draft", duplicated.Status)
	}
	if len(duplicated.Tags) != 1 || duplicated.Tags[0] != "work" {
		t.Fatalf("duplicate tags=%v", duplicated.Tags)
	}
	if len(duplicated.FolderIDs) != 1 || duplicated.FolderIDs[0] != folder.ID {
		t.Fatalf("duplicate folders=%v", duplicated.FolderIDs)
	}

	fullRec := doJSON(t, srv, http.MethodGet, "/api/notes/"+duplicated.ID+"/full", nil, hdr)
	if fullRec.Code != http.StatusOK {
		t.Fatalf("get full status %d body %s", fullRec.Code, fullRec.Body)
	}
	var full struct {
		Note struct {
			ID     string `json:"id"`
			Title  string `json:"title"`
			Status string `json:"status"`
		} `json:"note"`
		BodyMarkdown string `json:"body_markdown"`
	}
	_ = json.Unmarshal(fullRec.Body.Bytes(), &full)
	if full.Note.ID != duplicated.ID || full.Note.Title != duplicated.Title || full.Note.Status != "draft" {
		t.Fatalf("full note mismatch: %+v", full.Note)
	}
	if full.BodyMarkdown != "# live notes" {
		t.Fatalf("full body=%q", full.BodyMarkdown)
	}
}

func TestUpdateNoteTitleTrimsWhitespace(t *testing.T) {
	srv, _ := newTestServer(t)
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	// Create a note.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes",
		map[string]string{"title": "Original"}, hdr)
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)
	if note.ID == "" {
		t.Fatalf("no note id; body %s", rec.Body)
	}

	// PATCH with a whitespace-padded title.
	rec = doJSON(t, srv, http.MethodPatch, "/api/notes/"+note.ID,
		map[string]string{"title": "  Meeting notes  "}, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status %d body %s", rec.Code, rec.Body)
	}

	// The PATCH response must carry the trimmed title.
	var patchResp struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &patchResp)
	const want = "Meeting notes"
	if patchResp.Title != want {
		t.Fatalf("patch response title=%q, want %q; body %s", patchResp.Title, want, rec.Body)
	}
	if patchResp.ID != note.ID {
		t.Fatalf("patch response id=%q, want %q", patchResp.ID, note.ID)
	}

	// GET must also return the trimmed title (confirming what was stored).
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID, nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("get after trim-patch status %d", rec.Code)
	}
	var getResp struct{ Title string }
	_ = json.Unmarshal(rec.Body.Bytes(), &getResp)
	if getResp.Title != want {
		t.Fatalf("get title=%q after trim patch, want %q", getResp.Title, want)
	}
}

func TestUpdateNoteTitleCrossOwner(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()

	// Set up the owning user via the API.
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "owner@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "owner@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	ownerHdr := map[string]string{"Authorization": "Bearer " + login.Token}

	// Create a note as the owner.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes",
		map[string]string{"title": "Original title"}, ownerHdr)
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)

	// Create a second user directly via the store and mint a token for them.
	other, _ := st.CreateUser(ctx, "other@example.com", "h")
	raw, hash, _ := auth.GenerateToken()
	_ = st.CreateToken(ctx, other.ID, "session", hash, "session")
	otherHdr := map[string]string{"Authorization": "Bearer " + raw}

	// Second user attempts to rename the owner's note — must get 404.
	rec = doJSON(t, srv, http.MethodPatch, "/api/notes/"+note.ID,
		map[string]string{"title": "Hijacked"}, otherHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner PATCH status %d, want 404; body: %s", rec.Code, rec.Body)
	}

	// Title must remain unchanged.
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID, nil, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("get after cross-owner attempt status %d", rec.Code)
	}
	var got struct{ Title string }
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Title != "Original title" {
		t.Fatalf("title=%q after cross-owner PATCH, want %q", got.Title, "Original title")
	}
}

func TestSetupStatusAndBodyUpdate(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	// Before setup: needs_setup = true (no auth required).
	rec := doJSON(t, srv, http.MethodGet, "/api/setup/status", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status code %d", rec.Code)
	}
	var s1 struct {
		NeedsSetup bool `json:"needs_setup"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &s1)
	if !s1.NeedsSetup {
		t.Fatal("needs_setup should be true before setup")
	}

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)

	// After setup: needs_setup = false.
	rec = doJSON(t, srv, http.MethodGet, "/api/setup/status", nil, nil)
	var s2 struct {
		NeedsSetup bool `json:"needs_setup"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &s2)
	if s2.NeedsSetup {
		t.Fatal("needs_setup should be false after setup")
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	rec = doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "M"}, hdr)
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)

	// Body update requires auth.
	rec = doJSON(t, srv, http.MethodPut, "/api/notes/"+note.ID+"/body",
		map[string]string{"content": "# my notes"}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth body update status %d, want 401", rec.Code)
	}
	rec = doJSON(t, srv, http.MethodPut, "/api/notes/"+note.ID+"/body",
		map[string]string{"content": "# my notes"}, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("body update status %d body %s", rec.Code, rec.Body)
	}
}

func TestUpdateNoteBodyParsesMentionsIntoLinks(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()
	email := "mentions@example.com"

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": email, "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": email, "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	owner, err := st.GetUserByEmail(ctx, email)
	if err != nil {
		t.Fatalf("get owner: %v", err)
	}
	source, err := st.CreateNote(ctx, owner.ID, "Source note")
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	target, err := st.CreateNote(ctx, owner.ID, "Existing Title")
	if err != nil {
		t.Fatalf("create target: %v", err)
	}

	rec = doJSON(t, srv, http.MethodPut, "/api/notes/"+source.ID+"/body",
		map[string]string{"content": "See [[Existing Title]]"}, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("body update status %d body %s", rec.Code, rec.Body)
	}

	outgoing, err := st.OutgoingLinks(ctx, owner.ID, source.ID)
	if err != nil {
		t.Fatalf("outgoing links: %v", err)
	}
	if len(outgoing) != 1 || outgoing[0].ToNoteID != target.ID {
		t.Fatalf("outgoing=%+v, want one link to %s", outgoing, target.ID)
	}
}

func TestUpdateNoteBodySkipsAmbiguousMentions(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()
	email := "mentions-ambiguous@example.com"

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": email, "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": email, "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	owner, err := st.GetUserByEmail(ctx, email)
	if err != nil {
		t.Fatalf("get owner: %v", err)
	}
	source, err := st.CreateNote(ctx, owner.ID, "Source note")
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	if _, err := st.CreateNote(ctx, owner.ID, "Existing Title"); err != nil {
		t.Fatalf("create target 1: %v", err)
	}
	if _, err := st.CreateNote(ctx, owner.ID, "existing title"); err != nil {
		t.Fatalf("create target 2: %v", err)
	}

	rec = doJSON(t, srv, http.MethodPut, "/api/notes/"+source.ID+"/body",
		map[string]string{"content": "See [[Existing Title]]"}, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("body update status %d body %s", rec.Code, rec.Body)
	}

	outgoing, err := st.OutgoingLinks(ctx, owner.ID, source.ID)
	if err != nil {
		t.Fatalf("outgoing links: %v", err)
	}
	if len(outgoing) != 0 {
		t.Fatalf("outgoing=%+v, want no links", outgoing)
	}
}

func TestNoteResponsesIncludeTagsArray(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	// Create note — response must include tags:[].
	rec = doJSON(t, srv, http.MethodPost, "/api/notes",
		map[string]string{"title": "Tagged"}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status %d body %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"tags":`) {
		t.Errorf("create missing tags field: %s", rec.Body.String())
	}
	var created struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	noteID := created.ID

	// GET /api/notes/:id must include tags:[].
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+noteID, nil, hdr)
	if !strings.Contains(rec.Body.String(), `"tags":`) {
		t.Errorf("get missing tags field: %s", rec.Body.String())
	}

	// GET /api/notes must include tags:[].
	rec = doJSON(t, srv, http.MethodGet, "/api/notes", nil, hdr)
	if !strings.Contains(rec.Body.String(), `"tags":`) {
		t.Errorf("list missing tags field: %s", rec.Body.String())
	}

	// GET /api/notes/:id/full must include tags:[].
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+noteID+"/full", nil, hdr)
	if !strings.Contains(rec.Body.String(), `"tags":`) {
		t.Errorf("full missing tags field: %s", rec.Body.String())
	}
}

func TestDeleteNoteAPI(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "del@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "del@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	// Create a note.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes",
		map[string]string{"title": "To be deleted"}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status %d body %s", rec.Code, rec.Body)
	}
	var created struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// DELETE it — expect 200.
	rec = doJSON(t, srv, http.MethodDelete, "/api/notes/"+created.ID, nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status %d body %s", rec.Code, rec.Body)
	}

	// GET it — expect 404.
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+created.ID, nil, hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete status %d, want 404", rec.Code)
	}

	// DELETE again — expect 404.
	rec = doJSON(t, srv, http.MethodDelete, "/api/notes/"+created.ID, nil, hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete again status %d, want 404", rec.Code)
	}

	// DELETE malformed id — expect 404.
	rec = doJSON(t, srv, http.MethodDelete, "/api/notes/not-a-uuid", nil, hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("malformed id status %d, want 404", rec.Code)
	}
}

func TestTrashRoutes(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "trash@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "trash@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	// Create a note.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes",
		map[string]string{"title": "Trash me"}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status %d body %s", rec.Code, rec.Body)
	}
	var created struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	id := created.ID

	// Soft-delete it — expect 200.
	rec = doJSON(t, srv, http.MethodDelete, "/api/notes/"+id, nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status %d body %s", rec.Code, rec.Body)
	}

	// GET it — expect 404 (trashed).
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+id, nil, hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after trash status %d, want 404", rec.Code)
	}

	// GET /api/notes/trash — expect 200 and body contains the note id.
	// CRITICAL: this must route to handleListTrash, not handleGetNote.
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/trash", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("list trash status %d body %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), id) {
		t.Fatalf("trash list does not contain note id %s; body %s", id, rec.Body)
	}

	// Restore it — expect 200; then GET — expect 200.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+id+"/restore", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("restore status %d body %s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+id, nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("get after restore status %d, want 200", rec.Code)
	}

	// Trash again, then permanently purge — expect 200.
	rec = doJSON(t, srv, http.MethodDelete, "/api/notes/"+id, nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("re-delete status %d body %s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, http.MethodDelete, "/api/notes/"+id+"/permanent", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("purge status %d body %s", rec.Code, rec.Body)
	}

	// Restore after purge — expect 404 (gone for good).
	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+id+"/restore", nil, hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("restore after purge status %d, want 404; body %s", rec.Code, rec.Body)
	}
}

func TestResummarizeAPI(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()
	if err := st.SeedBuiltInTemplates(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	// Create a note via the API.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes",
		map[string]string{"title": "Retro"}, hdr)
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)
	if note.ID == "" {
		t.Fatalf("no note id; body %s", rec.Body)
	}

	// Without a transcript → 409.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/resummarize", nil, hdr)
	if rec.Code != http.StatusConflict {
		t.Fatalf("resummarize without transcript status %d, want 409; body %s", rec.Code, rec.Body)
	}

	// Give it a transcript via the store, plus a stale summary to be replaced.
	if _, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID:            note.ID,
		TranscriberPlugin: "test",
		Model:             "m",
		Segments:          []model.Segment{{StartMS: 0, EndMS: 1000, Text: "hi", Source: "mic"}},
	}); err != nil {
		t.Fatalf("SaveTranscript: %v", err)
	}
	tmpls, _ := st.BuiltInTemplates(ctx)
	staleID, err := st.CreatePendingSummary(ctx, note.ID, tmpls[0].ID)
	if err != nil {
		t.Fatalf("CreatePendingSummary: %v", err)
	}

	// With a transcript → 202.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/resummarize", nil, hdr)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("resummarize status %d, want 202; body %s", rec.Code, rec.Body)
	}
	var resp struct{ Status string }
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Status != "summarizing" {
		t.Fatalf("resummarize body status = %q, want summarizing", resp.Status)
	}

	// Stale summary was deleted and fresh pending summaries were created for the built-ins.
	tmpls, err = st.BuiltInTemplates(ctx)
	if err != nil {
		t.Fatalf("BuiltInTemplates: %v", err)
	}
	sums, _ := st.GetSummaries(ctx, note.ID)
	if len(sums) != len(tmpls) {
		t.Fatalf("summaries after resummarize = %d, want %d", len(sums), len(tmpls))
	}
	for _, s := range sums {
		if s.ID == staleID {
			t.Fatalf("stale summary %s should have been deleted", staleID)
		}
	}

	// Non-owner → 404.
	other, _ := st.CreateUser(ctx, "other@example.com", "h")
	raw, hash, _ := auth.GenerateToken()
	_ = st.CreateToken(ctx, other.ID, "session", hash, "session")
	otherHdr := map[string]string{"Authorization": "Bearer " + raw}
	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/resummarize", nil, otherHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner resummarize status %d, want 404; body %s", rec.Code, rec.Body)
	}

	// Missing / malformed id → 404.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes/not-a-uuid/resummarize", nil, hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("malformed id status %d, want 404", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// ListNotes filtering via HTTP handler
// ---------------------------------------------------------------------------

func TestListNotesFilteringAPI(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()

	// Create user and log in.
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "filter@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "filter@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	// Discover the owner's ID via the store.
	users, err := st.CountUsers(ctx)
	_ = users
	_ = err

	// Create two notes.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Alpha"}, hdr)
	var noteA struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &noteA)

	rec = doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Beta"}, hdr)
	var noteB struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &noteB)

	// GET /api/notes (no params): both notes returned.
	rec = doJSON(t, srv, http.MethodGet, "/api/notes", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status %d", rec.Code)
	}
	var all []map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &all)
	if len(all) != 2 {
		t.Fatalf("no-filter: want 2 notes, got %d", len(all))
	}

	// Tag note A with "foo".
	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+noteA.ID+"/tags",
		map[string]string{"name": "foo"}, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("add tag status %d body %s", rec.Code, rec.Body)
	}

	// GET /api/notes?tag=foo: only note A returned.
	rec = doJSON(t, srv, http.MethodGet, "/api/notes?tag=foo", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("tag filter status %d body %s", rec.Code, rec.Body)
	}
	var tagFiltered []map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &tagFiltered)
	if len(tagFiltered) != 1 {
		t.Fatalf("tag filter: want 1 note, got %d", len(tagFiltered))
	}
	if tagFiltered[0]["id"] != noteA.ID {
		t.Errorf("tag filter: got note %v, want %s", tagFiltered[0]["id"], noteA.ID)
	}

	// Advance note A to "ready" status via the store (bypasses the pipeline).
	// We need the ownerID from the store — use a direct store query.
	// Since the API doesn't expose status updates, we use the store directly.
	if err := st.SetNoteStatus(ctx, noteA.ID, "ready"); err != nil {
		t.Fatalf("set status: %v", err)
	}

	// GET /api/notes?status=ready: only note A returned.
	rec = doJSON(t, srv, http.MethodGet, "/api/notes?status=ready", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("status filter status %d body %s", rec.Code, rec.Body)
	}
	var statusFiltered []map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &statusFiltered)
	if len(statusFiltered) != 1 {
		t.Fatalf("status filter: want 1 note, got %d", len(statusFiltered))
	}
	if statusFiltered[0]["id"] != noteA.ID {
		t.Errorf("status filter: got note %v, want %s", statusFiltered[0]["id"], noteA.ID)
	}

	// Create a folder and put note A in it.
	rec = doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "Work"}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create folder status %d body %s", rec.Code, rec.Body)
	}
	var folder struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &folder)

	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+noteA.ID+"/folders",
		map[string]string{"folder_id": folder.ID}, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("add note to folder status %d body %s", rec.Code, rec.Body)
	}

	// GET /api/notes?folder_id=<uuid>: only note A returned.
	rec = doJSON(t, srv, http.MethodGet, "/api/notes?folder_id="+folder.ID, nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("folder filter status %d body %s", rec.Code, rec.Body)
	}
	var folderFiltered []map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &folderFiltered)
	if len(folderFiltered) != 1 {
		t.Fatalf("folder filter: want 1 note, got %d", len(folderFiltered))
	}
	if folderFiltered[0]["id"] != noteA.ID {
		t.Errorf("folder filter: got note %v, want %s", folderFiltered[0]["id"], noteA.ID)
	}

	// GET /api/notes?tag=foo&status=ready: intersection (only note A, which has both).
	rec = doJSON(t, srv, http.MethodGet, "/api/notes?tag=foo&status=ready", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("combined filter status %d body %s", rec.Code, rec.Body)
	}
	var combined []map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &combined)
	if len(combined) != 1 {
		t.Fatalf("combined filter: want 1 note, got %d", len(combined))
	}
	if combined[0]["id"] != noteA.ID {
		t.Errorf("combined filter: got note %v, want %s", combined[0]["id"], noteA.ID)
	}

	// GET /api/notes?folder_id=abc: 400 (not a valid UUID).
	rec = doJSON(t, srv, http.MethodGet, "/api/notes?folder_id=abc", nil, hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid folder_id: want 400, got %d body %s", rec.Code, rec.Body)
	}
}
