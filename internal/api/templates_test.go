package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestTemplatesCRUDAPI(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	_ = doJSON(t, srv, http.MethodPost, "/api/setup", map[string]string{"email": "tpl@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login", map[string]string{"email": "tpl@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	body := map[string]any{"name": "Standup", "phase": "after", "sections": []map[string]string{{"heading": "Overview", "instruction": "Summarise it."}}}
	c := doJSON(t, srv, http.MethodPost, "/api/templates", body, hdr)
	if c.Code != http.StatusCreated {
		t.Fatalf("create=%d body=%s", c.Code, c.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(c.Body.Bytes(), &created)

	// list includes the created template (test server does not seed built-ins)
	l := doJSON(t, srv, http.MethodGet, "/api/templates", nil, hdr)
	if l.Code != http.StatusOK || !strings.Contains(l.Body.String(), "Standup") || !strings.Contains(l.Body.String(), "\"phase\":\"after\"") {
		t.Fatalf("list=%d body=%s", l.Code, l.Body.String())
	}

	// rename ok
	u := doJSON(t, srv, http.MethodPut, "/api/templates/"+created.ID, map[string]any{"name": "Standup2", "phase": "after", "sections": body["sections"]}, hdr)
	if u.Code != http.StatusOK {
		t.Fatalf("update=%d body=%s", u.Code, u.Body.String())
	}
	// duplicate name → 409
	_ = doJSON(t, srv, http.MethodPost, "/api/templates", map[string]any{"name": "Dupe", "phase": "after", "sections": body["sections"]}, hdr)
	d := doJSON(t, srv, http.MethodPost, "/api/templates", map[string]any{"name": "dupe", "phase": "after", "sections": body["sections"]}, hdr)
	if d.Code != http.StatusConflict {
		t.Fatalf("dup: want 409 got %d", d.Code)
	}
	// bad (no sections) → 400
	b := doJSON(t, srv, http.MethodPost, "/api/templates", map[string]any{"name": "X", "phase": "after", "sections": []any{}}, hdr)
	if b.Code != http.StatusBadRequest {
		t.Fatalf("bad: want 400 got %d", b.Code)
	}
	// updating a non-existent / built-in id → 404
	nf := doJSON(t, srv, http.MethodPut, "/api/templates/00000000-0000-0000-0000-000000000000", map[string]any{"name": "Y", "phase": "after", "sections": body["sections"]}, hdr)
	if nf.Code != http.StatusNotFound {
		t.Fatalf("notfound: want 404 got %d", nf.Code)
	}
	// delete
	del := doJSON(t, srv, http.MethodDelete, "/api/templates/"+created.ID, nil, hdr)
	if del.Code != http.StatusOK {
		t.Fatalf("delete=%d", del.Code)
	}
}
