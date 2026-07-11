package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/auth"
)

func TestAddAndRemoveTagHandler(t *testing.T) {
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
		map[string]string{"title": "1on1 with Alice"}, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body)
	}
	var created struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	noteID := created.ID

	// POST /api/notes/{id}/tags — add tag "1on1".
	add := doJSON(t, srv, http.MethodPost, "/api/notes/"+noteID+"/tags",
		map[string]string{"name": "1on1"}, hdr)
	if add.Code != http.StatusOK {
		t.Fatalf("add status=%d body=%s", add.Code, add.Body.String())
	}
	if !strings.Contains(add.Body.String(), `"name":"1on1"`) {
		t.Fatalf("add body missing tag name: %s", add.Body.String())
	}

	// GET /api/notes/{id}/full — tag should appear.
	full := doJSON(t, srv, http.MethodGet, "/api/notes/"+noteID+"/full", nil, hdr)
	if full.Code != http.StatusOK {
		t.Fatalf("full status=%d body=%s", full.Code, full.Body.String())
	}
	if !strings.Contains(full.Body.String(), `"1on1"`) {
		t.Errorf("tag not on note after add: %s", full.Body.String())
	}

	// DELETE /api/notes/{id}/tags?name=1on1 — remove tag.
	del := doJSON(t, srv, http.MethodDelete, "/api/notes/"+noteID+"/tags?name=1on1", nil, hdr)
	if del.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", del.Code, del.Body.String())
	}

	// GET /api/notes/{id}/full — tag should be gone.
	full2 := doJSON(t, srv, http.MethodGet, "/api/notes/"+noteID+"/full", nil, hdr)
	if full2.Code != http.StatusOK {
		t.Fatalf("full2 status=%d body=%s", full2.Code, full2.Body.String())
	}
	if strings.Contains(full2.Body.String(), `"1on1"`) {
		t.Errorf("tag still present after remove: %s", full2.Body.String())
	}
}

func TestListTagsHandler(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "tags@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "tags@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	// Two notes; "alpha" on both, "beta" on one.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "N1"}, hdr)
	var n1 struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &n1)
	rec = doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "N2"}, hdr)
	var n2 struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &n2)

	for _, tt := range []struct{ id, name string }{
		{n1.ID, "alpha"}, {n2.ID, "alpha"}, {n1.ID, "beta"},
	} {
		r := doJSON(t, srv, http.MethodPost, "/api/notes/"+tt.id+"/tags",
			map[string]string{"name": tt.name}, hdr)
		if r.Code != http.StatusOK {
			t.Fatalf("add %s: status=%d body=%s", tt.name, r.Code, r.Body.String())
		}
	}

	got := doJSON(t, srv, http.MethodGet, "/api/tags", nil, hdr)
	if got.Code != http.StatusOK {
		t.Fatalf("list tags status=%d body=%s", got.Code, got.Body.String())
	}
	var tags []struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &tags); err != nil {
		t.Fatalf("unmarshal tags: %v body=%s", err, got.Body.String())
	}
	if len(tags) != 2 {
		t.Fatalf("got %d tags %+v, want 2", len(tags), tags)
	}
	if tags[0].Name != "alpha" || tags[0].Count != 2 {
		t.Errorf("tags[0] = %+v, want {alpha 2}", tags[0])
	}
	if tags[1].Name != "beta" || tags[1].Count != 1 {
		t.Errorf("tags[1] = %+v, want {beta 1}", tags[1])
	}
}

func TestListTagsEmptyReturnsArray(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "empty@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "empty@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	got := doJSON(t, srv, http.MethodGet, "/api/tags", nil, hdr)
	if got.Code != http.StatusOK {
		t.Fatalf("status=%d", got.Code)
	}
	if strings.TrimSpace(got.Body.String()) != "[]" {
		t.Errorf("empty tags body = %q, want []", got.Body.String())
	}
}

func TestAddTagRejectsBlankName(t *testing.T) {
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
		map[string]string{"title": "Some note"}, hdr)
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)

	// Blank name (whitespace-only) — expect 400.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/tags",
		map[string]string{"name": "   "}, hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("blank name status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestAddTagCrossOwnerReturns404(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "owner@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "owner@example.com", "password": "password123"}, nil)
	var ownerLogin struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &ownerLogin)
	ownerHdr := map[string]string{"Authorization": "Bearer " + ownerLogin.Token}

	// Owner creates a note.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes",
		map[string]string{"title": "Owner note"}, ownerHdr)
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)

	// Create a second user via store and mint a token.
	other, _ := st.CreateUser(ctx, "other@example.com", "h")
	raw, hash, _ := auth.GenerateToken()
	_ = st.CreateToken(ctx, other.ID, "session", hash, "session")
	otherHdr := map[string]string{"Authorization": "Bearer " + raw}

	// Other user tries to add tag to owner's note — must get 404.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/tags",
		map[string]string{"name": "hacked"}, otherHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner add tag status=%d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestRemoveTagMissingNameReturns400(t *testing.T) {
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
		map[string]string{"title": "Some note"}, hdr)
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)

	// DELETE without ?name= — expect 400.
	rec = doJSON(t, srv, http.MethodDelete, "/api/notes/"+note.ID+"/tags", nil, hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing name status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestRenameTagHandler(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "owner@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "owner@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	// Two notes both carrying "old"; plus a second tag "keep" for the duplicate case.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "N1"}, hdr)
	var n1 struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &n1)
	rec = doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "N2"}, hdr)
	var n2 struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &n2)

	for _, nid := range []string{n1.ID, n2.ID} {
		if r := doJSON(t, srv, http.MethodPost, "/api/notes/"+nid+"/tags",
			map[string]string{"name": "old"}, hdr); r.Code != http.StatusOK {
			t.Fatalf("add old to %s status=%d", nid, r.Code)
		}
	}
	if r := doJSON(t, srv, http.MethodPost, "/api/notes/"+n1.ID+"/tags",
		map[string]string{"name": "keep"}, hdr); r.Code != http.StatusOK {
		t.Fatalf("add keep status=%d", r.Code)
	}

	// Resolve the "old" tag id via GET /api/tags.
	tagID := tagIDByName(t, srv, hdr, "old")

	// 200 + new name shows via GET note and GET /api/tags.
	put := doJSON(t, srv, http.MethodPut, "/api/tags/"+tagID,
		map[string]string{"name": "new"}, hdr)
	if put.Code != http.StatusOK {
		t.Fatalf("rename status=%d body=%s", put.Code, put.Body.String())
	}
	if !strings.Contains(put.Body.String(), `"name":"new"`) {
		t.Fatalf("rename body missing new name: %s", put.Body.String())
	}
	full := doJSON(t, srv, http.MethodGet, "/api/notes/"+n2.ID+"/full", nil, hdr)
	if !strings.Contains(full.Body.String(), `"new"`) || strings.Contains(full.Body.String(), `"old"`) {
		t.Errorf("note tag not renamed: %s", full.Body.String())
	}
	if tagIDByName(t, srv, hdr, "new") == "" {
		t.Errorf("renamed tag not in /api/tags")
	}

	// 409 for a duplicate (rename "new" -> "keep", case-insensitive).
	dup := doJSON(t, srv, http.MethodPut, "/api/tags/"+tagID,
		map[string]string{"name": "KEEP"}, hdr)
	if dup.Code != http.StatusConflict {
		t.Fatalf("duplicate rename status=%d, want 409; body=%s", dup.Code, dup.Body.String())
	}

	// 400 for a blank name.
	blank := doJSON(t, srv, http.MethodPut, "/api/tags/"+tagID,
		map[string]string{"name": "  "}, hdr)
	if blank.Code != http.StatusBadRequest {
		t.Fatalf("blank rename status=%d, want 400; body=%s", blank.Code, blank.Body.String())
	}

	// 404 for another owner's tag id.
	other, _ := st.CreateUser(ctx, "other@example.com", "h")
	raw, hash, _ := auth.GenerateToken()
	_ = st.CreateToken(ctx, other.ID, "session", hash, "session")
	otherHdr := map[string]string{"Authorization": "Bearer " + raw}
	nf := doJSON(t, srv, http.MethodPut, "/api/tags/"+tagID,
		map[string]string{"name": "hijack"}, otherHdr)
	if nf.Code != http.StatusNotFound {
		t.Fatalf("cross-owner rename status=%d, want 404; body=%s", nf.Code, nf.Body.String())
	}
}

// tagIDByName fetches GET /api/tags and returns the id of the tag with the given
// name (empty string if absent).
func tagIDByName(t *testing.T, srv *api.Server, hdr map[string]string, name string) string {
	t.Helper()
	got := doJSON(t, srv, http.MethodGet, "/api/tags", nil, hdr)
	if got.Code != http.StatusOK {
		t.Fatalf("list tags status=%d body=%s", got.Code, got.Body.String())
	}
	var tags []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &tags); err != nil {
		t.Fatalf("unmarshal tags: %v body=%s", err, got.Body.String())
	}
	for _, tc := range tags {
		if tc.Name == name {
			return tc.ID
		}
	}
	return ""
}

func TestDeleteTagHandler(t *testing.T) {
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

	// Create a note and attach a tag.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Tagged Note"}, hdr)
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)

	add := doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/tags",
		map[string]string{"name": "todelete"}, hdr)
	if add.Code != http.StatusOK {
		t.Fatalf("add tag status=%d body=%s", add.Code, add.Body.String())
	}
	var tag struct{ ID string }
	_ = json.Unmarshal(add.Body.Bytes(), &tag)

	// DELETE /api/tags/{id} — success path: 204.
	del := doJSON(t, srv, http.MethodDelete, "/api/tags/"+tag.ID, nil, hdr)
	if del.Code != http.StatusNoContent {
		t.Fatalf("delete tag status=%d body=%s", del.Code, del.Body.String())
	}

	// Confirm the tag is gone from ListTags.
	got := doJSON(t, srv, http.MethodGet, "/api/tags", nil, hdr)
	if got.Code != http.StatusOK {
		t.Fatalf("list tags status=%d body=%s", got.Code, got.Body.String())
	}
	if strings.Contains(got.Body.String(), "todelete") {
		t.Errorf("tag still present after delete: %s", got.Body.String())
	}
}

func TestDeleteTagNotFound(t *testing.T) {
	srv, _ := newTestServer(t)

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "delnf@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "delnf@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	// DELETE /api/tags/nonexistent-id — expect 404.
	del := doJSON(t, srv, http.MethodDelete, "/api/tags/nonexistent-id", nil, hdr)
	if del.Code != http.StatusNotFound {
		t.Fatalf("delete nonexistent tag status=%d, want 404; body=%s", del.Code, del.Body.String())
	}
}
