package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestFoldersCRUDAPI(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "fld@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "fld@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	// create
	c := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "Clients"}, hdr)
	if c.Code != http.StatusCreated {
		t.Fatalf("create=%d body=%s", c.Code, c.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(c.Body.Bytes(), &created)
	if created.ID == "" {
		t.Fatal("expected id")
	}

	// list
	l := doJSON(t, srv, http.MethodGet, "/api/folders", nil, hdr)
	if l.Code != http.StatusOK {
		t.Fatalf("list=%d", l.Code)
	}

	// rename → 200 with the updated folder body
	u := doJSON(t, srv, http.MethodPut, "/api/folders/"+created.ID, map[string]any{"name": "Accounts"}, hdr)
	if u.Code != http.StatusOK {
		t.Fatalf("rename=%d body=%s", u.Code, u.Body.String())
	}
	var updated struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		CreatedAt string `json:"created_at"`
	}
	if err := json.Unmarshal(u.Body.Bytes(), &updated); err != nil {
		t.Fatalf("rename body unmarshal: %v body=%s", err, u.Body.String())
	}
	if updated.ID != created.ID {
		t.Fatalf("rename body id=%q want %q", updated.ID, created.ID)
	}
	if updated.Name != "Accounts" {
		t.Fatalf("rename body name=%q want Accounts", updated.Name)
	}
	if updated.CreatedAt == "" || updated.CreatedAt == "0001-01-01T00:00:00Z" {
		t.Fatalf("rename body created_at missing/zero: %q", updated.CreatedAt)
	}

	// duplicate name → 409
	_ = doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "Dup"}, hdr)
	dup := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "dup"}, hdr)
	if dup.Code != http.StatusConflict {
		t.Fatalf("dup: want 409 got %d", dup.Code)
	}

	// bad name → 400
	bad := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "  "}, hdr)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad name: want 400 got %d", bad.Code)
	}

	// not-owner update → 404 (random id)
	nf := doJSON(t, srv, http.MethodPut, "/api/folders/00000000-0000-0000-0000-000000000000", map[string]any{"name": "X"}, hdr)
	if nf.Code != http.StatusNotFound {
		t.Fatalf("not found: want 404 got %d", nf.Code)
	}

	// delete
	d := doJSON(t, srv, http.MethodDelete, "/api/folders/"+created.ID, nil, hdr)
	if d.Code != http.StatusOK {
		t.Fatalf("delete=%d", d.Code)
	}
}

func TestFolderNestingAPI(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	_ = doJSON(t, srv, http.MethodPost, "/api/setup", map[string]string{"email": "fn@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login", map[string]string{"email": "fn@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	pr := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "Parent"}, hdr)
	var parent struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(pr.Body.Bytes(), &parent)

	c := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "Child", "parent_id": parent.ID}, hdr)
	if c.Code != http.StatusCreated {
		t.Fatalf("nested create=%d body=%s", c.Code, c.Body.String())
	}
	// non-existent parent → 400
	bad := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "Orphan", "parent_id": "00000000-0000-0000-0000-000000000000"}, hdr)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad parent: want 400 got %d", bad.Code)
	}
}

func TestFolderReorderAPI(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "fr@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "fr@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	mk := func(name string) string {
		r := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": name}, hdr)
		if r.Code != http.StatusCreated {
			t.Fatalf("create %s=%d body=%s", name, r.Code, r.Body.String())
		}
		var f struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(r.Body.Bytes(), &f)
		return f.ID
	}
	listIDs := func() []string {
		l := doJSON(t, srv, http.MethodGet, "/api/folders", nil, hdr)
		if l.Code != http.StatusOK {
			t.Fatalf("list=%d", l.Code)
		}
		var fs []struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(l.Body.Bytes(), &fs)
		ids := make([]string, len(fs))
		for i, f := range fs {
			ids[i] = f.ID
		}
		return ids
	}

	a := mk("A")
	b := mk("B")
	c := mk("C")

	// Move c after a → order a, c, b.
	r := doJSON(t, srv, http.MethodPut, "/api/folders/"+c+"/reorder", map[string]any{"after_id": a}, hdr)
	if r.Code != http.StatusOK {
		t.Fatalf("reorder=%d body=%s", r.Code, r.Body.String())
	}
	if got := listIDs(); !(len(got) == 3 && got[0] == a && got[1] == c && got[2] == b) {
		t.Fatalf("after reorder order=%v want [a c b] (a=%s c=%s b=%s)", got, a, c, b)
	}

	// null after_id → moves to first.
	r = doJSON(t, srv, http.MethodPut, "/api/folders/"+b+"/reorder", map[string]any{"after_id": nil}, hdr)
	if r.Code != http.StatusOK {
		t.Fatalf("reorder first=%d body=%s", r.Code, r.Body.String())
	}
	if got := listIDs(); got[0] != b {
		t.Fatalf("after null reorder first=%v want b=%s", got, b)
	}

	// bad sibling (not in c's parent set — use a's parent? a IS a sibling; use a random uuid) → 400.
	bad := doJSON(t, srv, http.MethodPut, "/api/folders/"+c+"/reorder",
		map[string]any{"after_id": "00000000-0000-0000-0000-000000000000"}, hdr)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad sibling: want 400 got %d body=%s", bad.Code, bad.Body.String())
	}

	// unknown folder id → 404.
	nf := doJSON(t, srv, http.MethodPut, "/api/folders/00000000-0000-0000-0000-000000000000/reorder",
		map[string]any{"after_id": nil}, hdr)
	if nf.Code != http.StatusNotFound {
		t.Fatalf("unknown id: want 404 got %d", nf.Code)
	}
}

func TestNoteReorderInFolderAPI(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "nrf@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login", map[string]string{"email": "nrf@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	mkFolder := func(name string) string {
		r := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": name}, hdr)
		if r.Code != http.StatusCreated {
			t.Fatalf("create folder %s=%d body=%s", name, r.Code, r.Body.String())
		}
		var f struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(r.Body.Bytes(), &f)
		return f.ID
	}
	mkNote := func(title string) string {
		r := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]any{"title": title}, hdr)
		if r.Code != http.StatusCreated {
			t.Fatalf("create note %s=%d body=%s", title, r.Code, r.Body.String())
		}
		var n struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(r.Body.Bytes(), &n)
		return n.ID
	}
	f1 := mkFolder("Clients")
	f2 := mkFolder("Other")
	n1 := mkNote("One")
	n2 := mkNote("Two")
	n3 := mkNote("Three")
	_ = doJSON(t, srv, http.MethodPost, "/api/notes/"+n1+"/folders", map[string]any{"folder_id": f1}, hdr)
	_ = doJSON(t, srv, http.MethodPost, "/api/notes/"+n2+"/folders", map[string]any{"folder_id": f1}, hdr)
	_ = doJSON(t, srv, http.MethodPost, "/api/notes/"+n3+"/folders", map[string]any{"folder_id": f2}, hdr)

	listIDs := func(folderID string) []string {
		l := doJSON(t, srv, http.MethodGet, "/api/notes?folder_id="+folderID, nil, hdr)
		if l.Code != http.StatusOK {
			t.Fatalf("list notes=%d body=%s", l.Code, l.Body.String())
		}
		var ns []struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(l.Body.Bytes(), &ns)
		ids := make([]string, len(ns))
		for i, n := range ns {
			ids[i] = n.ID
		}
		return ids
	}

	r := doJSON(t, srv, http.MethodPut, "/api/folders/"+f1+"/notes/"+n2+"/reorder", map[string]any{"after_id": n1}, hdr)
	if r.Code != http.StatusOK {
		t.Fatalf("reorder=%d body=%s", r.Code, r.Body.String())
	}
	got := listIDs(f1)
	if len(got) != 2 || got[0] != n1 || got[1] != n2 {
		t.Fatalf("order after reorder=%v want [%s %s]", got, n1, n2)
	}

	first := doJSON(t, srv, http.MethodPut, "/api/folders/"+f1+"/notes/"+n2+"/reorder", map[string]any{"after_id": nil}, hdr)
	if first.Code != http.StatusOK {
		t.Fatalf("reorder first=%d body=%s", first.Code, first.Body.String())
	}
	got = listIDs(f1)
	if len(got) != 2 || got[0] != n2 || got[1] != n1 {
		t.Fatalf("order after front reorder=%v want [%s %s]", got, n2, n1)
	}

	self := doJSON(t, srv, http.MethodPut, "/api/folders/"+f1+"/notes/"+n1+"/reorder", map[string]any{"after_id": n1}, hdr)
	if self.Code != http.StatusBadRequest {
		t.Fatalf("self after: want 400 got %d body=%s", self.Code, self.Body.String())
	}

	cross := doJSON(t, srv, http.MethodPut, "/api/folders/"+f1+"/notes/"+n1+"/reorder", map[string]any{"after_id": n3}, hdr)
	if cross.Code != http.StatusBadRequest {
		t.Fatalf("cross-folder after: want 400 got %d body=%s", cross.Code, cross.Body.String())
	}

	missing := doJSON(t, srv, http.MethodPut, "/api/folders/"+f1+"/notes/"+n3+"/reorder", map[string]any{"after_id": nil}, hdr)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing membership: want 404 got %d body=%s", missing.Code, missing.Body.String())
	}
}

func TestFolderTrashRoutes(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	_ = doJSON(t, srv, http.MethodPost, "/api/setup", map[string]string{"email": "ft@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login", map[string]string{"email": "ft@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	// create root + child
	pr := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "Root"}, hdr)
	var parent struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(pr.Body.Bytes(), &parent)
	cr := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "Child", "parent_id": parent.ID}, hdr)
	if cr.Code != http.StatusCreated {
		t.Fatalf("child create=%d body=%s", cr.Code, cr.Body.String())
	}

	listIDs := func() []string {
		l := doJSON(t, srv, http.MethodGet, "/api/folders", nil, hdr)
		if l.Code != http.StatusOK {
			t.Fatalf("list=%d", l.Code)
		}
		var fs []struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(l.Body.Bytes(), &fs)
		ids := make([]string, len(fs))
		for i, f := range fs {
			ids[i] = f.ID
		}
		return ids
	}
	contains := func(ids []string, id string) bool {
		for _, x := range ids {
			if x == id {
				return true
			}
		}
		return false
	}

	// soft delete root → 200 {status:"trashed"}
	d := doJSON(t, srv, http.MethodDelete, "/api/folders/"+parent.ID, nil, hdr)
	if d.Code != http.StatusOK {
		t.Fatalf("delete=%d body=%s", d.Code, d.Body.String())
	}
	var ds struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(d.Body.Bytes(), &ds)
	if ds.Status != "trashed" {
		t.Fatalf("delete status=%q want trashed", ds.Status)
	}

	// absent from /api/folders
	if ids := listIDs(); contains(ids, parent.ID) {
		t.Fatalf("trashed folder still in /api/folders: %v", ids)
	}

	// present in /api/folders/trash — only the root (chi routes static segment, not {id})
	tr := doJSON(t, srv, http.MethodGet, "/api/folders/trash", nil, hdr)
	if tr.Code != http.StatusOK {
		t.Fatalf("trash list=%d body=%s", tr.Code, tr.Body.String())
	}
	var trashed []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(tr.Body.Bytes(), &trashed); err != nil {
		t.Fatalf("trash unmarshal: %v body=%s", err, tr.Body.String())
	}
	if len(trashed) != 1 || trashed[0].ID != parent.ID {
		t.Fatalf("trash roots = %+v, want only root %s", trashed, parent.ID)
	}

	// restore → 200, folder back in /api/folders
	rs := doJSON(t, srv, http.MethodPost, "/api/folders/"+parent.ID+"/restore", nil, hdr)
	if rs.Code != http.StatusOK {
		t.Fatalf("restore=%d body=%s", rs.Code, rs.Body.String())
	}
	if ids := listIDs(); !contains(ids, parent.ID) {
		t.Fatalf("restored folder missing from /api/folders: %v", ids)
	}

	// re-delete, then permanent delete → 200
	if d2 := doJSON(t, srv, http.MethodDelete, "/api/folders/"+parent.ID, nil, hdr); d2.Code != http.StatusOK {
		t.Fatalf("re-delete=%d", d2.Code)
	}
	pd := doJSON(t, srv, http.MethodDelete, "/api/folders/"+parent.ID+"/permanent", nil, hdr)
	if pd.Code != http.StatusOK {
		t.Fatalf("permanent delete=%d body=%s", pd.Code, pd.Body.String())
	}

	// restore after permanent delete → 404
	rs2 := doJSON(t, srv, http.MethodPost, "/api/folders/"+parent.ID+"/restore", nil, hdr)
	if rs2.Code != http.StatusNotFound {
		t.Fatalf("restore after purge: want 404 got %d", rs2.Code)
	}
}

func TestFolderNoteCountInAPI(t *testing.T) {
	srv, _ := newTestServer(t)
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "fnc@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "fnc@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	// Create parent and two children.
	prRes := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "Parent"}, hdr)
	if prRes.Code != http.StatusCreated {
		t.Fatalf("create parent=%d body=%s", prRes.Code, prRes.Body.String())
	}
	var parent struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(prRes.Body.Bytes(), &parent)

	ch1Res := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "Child1", "parent_id": parent.ID}, hdr)
	var child1 struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(ch1Res.Body.Bytes(), &child1)

	ch2Res := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "Child2", "parent_id": parent.ID}, hdr)
	var child2 struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(ch2Res.Body.Bytes(), &child2)

	// Create a note and put it in child1.
	noteRes := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]any{"title": "Note A"}, hdr)
	if noteRes.Code != http.StatusCreated {
		t.Fatalf("create note=%d body=%s", noteRes.Code, noteRes.Body.String())
	}
	var note struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(noteRes.Body.Bytes(), &note)

	addRes := doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/folders",
		map[string]any{"folder_id": child1.ID}, hdr)
	if addRes.Code != http.StatusOK {
		t.Fatalf("add note to folder=%d body=%s", addRes.Code, addRes.Body.String())
	}

	// GET /api/folders and check note_count fields.
	l := doJSON(t, srv, http.MethodGet, "/api/folders", nil, hdr)
	if l.Code != http.StatusOK {
		t.Fatalf("list=%d body=%s", l.Code, l.Body.String())
	}
	var folders []struct {
		ID        string `json:"id"`
		NoteCount int    `json:"note_count"`
	}
	if err := json.Unmarshal(l.Body.Bytes(), &folders); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, l.Body.String())
	}

	counts := map[string]int{}
	for _, f := range folders {
		counts[f.ID] = f.NoteCount
	}

	if got := counts[parent.ID]; got != 1 {
		t.Errorf("parent note_count via API: want 1 (recursive), got %d", got)
	}
	if got := counts[child1.ID]; got != 1 {
		t.Errorf("child1 note_count via API: want 1, got %d", got)
	}
	if got := counts[child2.ID]; got != 0 {
		t.Errorf("child2 note_count via API: want 0, got %d", got)
	}
}

func TestResolveFoldersAPI(t *testing.T) {
	t.Parallel()
	// newTestServer's /api/setup only ever provisions the FIRST account (see
	// auth_test.go: a second /api/setup call returns 409, so it would never
	// have created "rslv-other@example.com"). The second owner is created
	// directly through the store and authenticated with a real session
	// token, matching the authHeaderForUser pattern used by action_items_test.go.
	srv, st := newTestServer(t)
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "rslv@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "rslv@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	otherHdr, _ := authHeaderForUser(t, st, "rslv-other@example.com")

	mkFolder := func(h map[string]string, name string, parentID *string) string {
		body := map[string]any{"name": name}
		if parentID != nil {
			body["parent_id"] = *parentID
		}
		r := doJSON(t, srv, http.MethodPost, "/api/folders", body, h)
		if r.Code != http.StatusCreated {
			t.Fatalf("create %s=%d body=%s", name, r.Code, r.Body.String())
		}
		var f struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(r.Body.Bytes(), &f)
		return f.ID
	}

	live := mkFolder(hdr, "Live", nil)
	root := mkFolder(hdr, "Root", nil)
	child := mkFolder(hdr, "Child", &root)
	theirs := mkFolder(otherHdr, "Theirs", nil)

	if d := doJSON(t, srv, http.MethodDelete, "/api/folders/"+root, nil, hdr); d.Code != http.StatusOK {
		t.Fatalf("trash root=%d body=%s", d.Code, d.Body.String())
	}

	unknown := "00000000-0000-0000-0000-000000000000"
	res := doJSON(t, srv, http.MethodPost, "/api/folders/resolve",
		map[string]any{"ids": []string{live, live, root, child, theirs, unknown}}, hdr)
	if res.Code != http.StatusOK {
		t.Fatalf("resolve=%d body=%s", res.Code, res.Body.String())
	}
	var got []struct {
		ID        string  `json:"id"`
		Name      string  `json:"name"`
		ParentID  *string `json:"parent_id"`
		CreatedAt string  `json:"created_at"`
		DeletedAt *string `json:"deleted_at"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, res.Body.String())
	}
	if len(got) != 3 {
		t.Fatalf("want 3 rows (dedup + omit unknown/other-owner), got %d: %+v", len(got), got)
	}
	byID := map[string]struct {
		ID        string  `json:"id"`
		Name      string  `json:"name"`
		ParentID  *string `json:"parent_id"`
		CreatedAt string  `json:"created_at"`
		DeletedAt *string `json:"deleted_at"`
	}{}
	for _, f := range got {
		byID[f.ID] = f
	}
	if _, ok := byID[theirs]; ok {
		t.Errorf("other-owner folder present in response: %+v", got)
	}
	if _, ok := byID[unknown]; ok {
		t.Errorf("unknown folder present in response: %+v", got)
	}
	l, ok := byID[live]
	if !ok || l.DeletedAt != nil || l.Name != "Live" || l.CreatedAt == "" {
		t.Errorf("live folder row wrong: %+v (ok=%v)", l, ok)
	}
	r, ok := byID[root]
	if !ok || r.DeletedAt == nil {
		t.Errorf("trashed root row wrong: %+v (ok=%v)", r, ok)
	}
	c, ok := byID[child]
	if !ok || c.DeletedAt == nil || c.ParentID == nil || *c.ParentID != root {
		t.Errorf("auto-trashed descendant row wrong: %+v (ok=%v)", c, ok)
	}

	// Unauthenticated.
	unauth := doJSON(t, srv, http.MethodPost, "/api/folders/resolve", map[string]any{"ids": []string{live}}, nil)
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated: want 401 got %d", unauth.Code)
	}

	// Malformed JSON bytes.
	malformed := doRaw(t, srv, http.MethodPost, "/api/folders/resolve", "{not json", hdr)
	if malformed.Code != http.StatusBadRequest {
		t.Fatalf("malformed json: want 400 got %d", malformed.Code)
	}

	// Missing ids field.
	missing := doJSON(t, srv, http.MethodPost, "/api/folders/resolve", map[string]any{}, hdr)
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing ids: want 400 got %d body=%s", missing.Code, missing.Body.String())
	}

	// Null ids.
	nullIDs := doRaw(t, srv, http.MethodPost, "/api/folders/resolve", `{"ids":null}`, hdr)
	if nullIDs.Code != http.StatusBadRequest {
		t.Fatalf("null ids: want 400 got %d body=%s", nullIDs.Code, nullIDs.Body.String())
	}

	// Non-array ids.
	nonArray := doRaw(t, srv, http.MethodPost, "/api/folders/resolve", `{"ids":"nope"}`, hdr)
	if nonArray.Code != http.StatusBadRequest {
		t.Fatalf("non-array ids: want 400 got %d body=%s", nonArray.Code, nonArray.Body.String())
	}

	// Non-string item.
	nonString := doJSON(t, srv, http.MethodPost, "/api/folders/resolve", map[string]any{"ids": []any{live, 5}}, hdr)
	if nonString.Code != http.StatusBadRequest {
		t.Fatalf("non-string item: want 400 got %d body=%s", nonString.Code, nonString.Body.String())
	}

	// Empty string item.
	emptyString := doJSON(t, srv, http.MethodPost, "/api/folders/resolve", map[string]any{"ids": []string{""}}, hdr)
	if emptyString.Code != http.StatusBadRequest {
		t.Fatalf("empty string item: want 400 got %d body=%s", emptyString.Code, emptyString.Body.String())
	}

	// Invalid UUID.
	invalidUUID := doJSON(t, srv, http.MethodPost, "/api/folders/resolve", map[string]any{"ids": []string{"not-a-uuid"}}, hdr)
	if invalidUUID.Code != http.StatusBadRequest {
		t.Fatalf("invalid uuid: want 400 got %d body=%s", invalidUUID.Code, invalidUUID.Body.String())
	}

	// Empty array is valid -> [].
	emptyArr := doJSON(t, srv, http.MethodPost, "/api/folders/resolve", map[string]any{"ids": []string{}}, hdr)
	if emptyArr.Code != http.StatusOK {
		t.Fatalf("empty array: want 200 got %d body=%s", emptyArr.Code, emptyArr.Body.String())
	}
	var emptyGot []any
	if err := json.Unmarshal(emptyArr.Body.Bytes(), &emptyGot); err != nil {
		t.Fatalf("empty array unmarshal: %v body=%s", err, emptyArr.Body.String())
	}
	if len(emptyGot) != 0 {
		t.Fatalf("empty array result: want [], got %v", emptyGot)
	}

	// 100 unique ids succeeds.
	hundred := make([]string, 100)
	for i := range hundred {
		hundred[i] = uuid.NewString()
	}
	ok100 := doJSON(t, srv, http.MethodPost, "/api/folders/resolve", map[string]any{"ids": hundred}, hdr)
	if ok100.Code != http.StatusOK {
		t.Fatalf("100 unique ids: want 200 got %d body=%s", ok100.Code, ok100.Body.String())
	}

	// 101 unique ids rejected.
	hundredOne := append(append([]string{}, hundred...), uuid.NewString())
	rej101 := doJSON(t, srv, http.MethodPost, "/api/folders/resolve", map[string]any{"ids": hundredOne}, hdr)
	if rej101.Code != http.StatusBadRequest {
		t.Fatalf("101 unique ids: want 400 got %d body=%s", rej101.Code, rej101.Body.String())
	}

	// 101 entries deduplicating to 100 unique succeeds.
	dedupTo100 := append(append([]string{}, hundred...), hundred[0])
	ok101dedup := doJSON(t, srv, http.MethodPost, "/api/folders/resolve", map[string]any{"ids": dedupTo100}, hdr)
	if ok101dedup.Code != http.StatusOK {
		t.Fatalf("101 entries deduped to 100: want 200 got %d body=%s", ok101dedup.Code, ok101dedup.Body.String())
	}
}
