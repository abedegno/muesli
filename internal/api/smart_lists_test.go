package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const ruleBody = `{"name":"Standups","rule":{"op":"and","children":[{"field":"title","operator":"contains","value":"standup"}]}}`

func TestSmartListCRUDHandlers(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	// Register a user and get a token.
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "sl@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "sl@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	// POST /api/smart-lists — create.
	c := doJSON(t, srv, http.MethodPost, "/api/smart-lists",
		json.RawMessage(ruleBody), hdr)
	if c.Code != http.StatusCreated {
		t.Fatalf("create=%d body=%s", c.Code, c.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(c.Body.Bytes(), &created); err != nil {
		t.Fatalf("parse create response: %v", err)
	}
	id := created.ID
	if id == "" {
		t.Fatal("expected non-empty id in create response")
	}

	// GET /api/smart-lists — list.
	l := doJSON(t, srv, http.MethodGet, "/api/smart-lists", nil, hdr)
	if l.Code != http.StatusOK {
		t.Fatalf("list=%d body=%s", l.Code, l.Body.String())
	}
	if !strings.Contains(l.Body.String(), "Standups") {
		t.Errorf("list missing Standups: %s", l.Body.String())
	}

	// PUT /api/smart-lists/{id} — update.
	u := doJSON(t, srv, http.MethodPut, "/api/smart-lists/"+id,
		json.RawMessage(`{"name":"Renamed","rule":{"op":"and","children":[]}}`), hdr)
	if u.Code != http.StatusOK {
		t.Fatalf("update=%d body=%s", u.Code, u.Body.String())
	}

	// DELETE /api/smart-lists/{id} — delete.
	d := doJSON(t, srv, http.MethodDelete, "/api/smart-lists/"+id, nil, hdr)
	if d.Code != http.StatusOK {
		t.Fatalf("delete=%d body=%s", d.Code, d.Body.String())
	}

	// List should now be empty.
	l2 := doJSON(t, srv, http.MethodGet, "/api/smart-lists", nil, hdr)
	if strings.Contains(l2.Body.String(), "Standups") {
		t.Errorf("list still contains deleted smart list: %s", l2.Body.String())
	}
}

func TestSmartListBadRule400(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "sl2@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "sl2@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	// Root not a group — expect 400.
	bad := json.RawMessage(`{"name":"X","rule":{"field":"tag","operator":"is","value":"y"}}`)
	r := doJSON(t, srv, http.MethodPost, "/api/smart-lists", bad, hdr)
	if r.Code != http.StatusBadRequest {
		t.Errorf("status=%d want 400; body=%s", r.Code, r.Body.String())
	}
}

func TestSmartListUnauthenticated401(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	r := doJSON(t, srv, http.MethodGet, "/api/smart-lists", nil, nil)
	if r.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated GET status=%d want 401", r.Code)
	}
}

func TestSmartListRecycleBinHandlers(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "slrb@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "slrb@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	// Create a smart list.
	c := doJSON(t, srv, http.MethodPost, "/api/smart-lists", json.RawMessage(ruleBody), hdr)
	if c.Code != http.StatusCreated {
		t.Fatalf("create=%d body=%s", c.Code, c.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(c.Body.Bytes(), &created)
	id := created.ID

	// DELETE → 200 {status:"trashed"}.
	d := doJSON(t, srv, http.MethodDelete, "/api/smart-lists/"+id, nil, hdr)
	if d.Code != http.StatusOK {
		t.Fatalf("delete=%d body=%s", d.Code, d.Body.String())
	}
	if !strings.Contains(d.Body.String(), "trashed") {
		t.Errorf("delete body=%s want status:trashed", d.Body.String())
	}

	// Absent from live list.
	l := doJSON(t, srv, http.MethodGet, "/api/smart-lists", nil, hdr)
	if strings.Contains(l.Body.String(), "Standups") {
		t.Errorf("live list still contains trashed: %s", l.Body.String())
	}

	// GET /api/smart-lists/trash routes correctly and contains the list.
	trash := doJSON(t, srv, http.MethodGet, "/api/smart-lists/trash", nil, hdr)
	if trash.Code != http.StatusOK {
		t.Fatalf("trash=%d body=%s", trash.Code, trash.Body.String())
	}
	if !strings.Contains(trash.Body.String(), "Standups") {
		t.Errorf("trash missing Standups: %s", trash.Body.String())
	}

	// POST restore → 200 {status:"restored"}.
	rest := doJSON(t, srv, http.MethodPost, "/api/smart-lists/"+id+"/restore", nil, hdr)
	if rest.Code != http.StatusOK || !strings.Contains(rest.Body.String(), "restored") {
		t.Fatalf("restore=%d body=%s", rest.Code, rest.Body.String())
	}
	// Back in live list.
	l2 := doJSON(t, srv, http.MethodGet, "/api/smart-lists", nil, hdr)
	if !strings.Contains(l2.Body.String(), "Standups") {
		t.Errorf("restored list missing from live list: %s", l2.Body.String())
	}

	// Trash again, then permanent delete → 200 {status:"deleted"}.
	_ = doJSON(t, srv, http.MethodDelete, "/api/smart-lists/"+id, nil, hdr)
	perm := doJSON(t, srv, http.MethodDelete, "/api/smart-lists/"+id+"/permanent", nil, hdr)
	if perm.Code != http.StatusOK || !strings.Contains(perm.Body.String(), "deleted") {
		t.Fatalf("permanent=%d body=%s", perm.Code, perm.Body.String())
	}

	// Restore/permanent on a now-gone list → 404.
	r404 := doJSON(t, srv, http.MethodPost, "/api/smart-lists/"+id+"/restore", nil, hdr)
	if r404.Code != http.StatusNotFound {
		t.Errorf("restore gone=%d want 404", r404.Code)
	}
	p404 := doJSON(t, srv, http.MethodDelete, "/api/smart-lists/"+id+"/permanent", nil, hdr)
	if p404.Code != http.StatusNotFound {
		t.Errorf("permanent gone=%d want 404", p404.Code)
	}
}
