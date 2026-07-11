package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
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
