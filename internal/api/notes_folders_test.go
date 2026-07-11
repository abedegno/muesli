package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestNoteFolderMembershipAPI(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "nf@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "nf@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	// create a note
	nr := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "N"}, hdr)
	var note struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(nr.Body.Bytes(), &note)

	// create a folder
	fr := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "Clients"}, hdr)
	var folder struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(fr.Body.Bytes(), &folder)

	// add membership
	a := doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/folders", map[string]any{"folder_id": folder.ID}, hdr)
	if a.Code != http.StatusOK {
		t.Fatalf("add=%d body=%s", a.Code, a.Body.String())
	}

	// note list carries folder_ids including the folder
	l := doJSON(t, srv, http.MethodGet, "/api/notes", nil, hdr)
	if !strings.Contains(l.Body.String(), folder.ID) {
		t.Fatalf("folder_ids missing on note list: %s", l.Body.String())
	}

	// remove
	d := doJSON(t, srv, http.MethodDelete, "/api/notes/"+note.ID+"/folders/"+folder.ID, nil, hdr)
	if d.Code != http.StatusOK {
		t.Fatalf("remove=%d", d.Code)
	}

	// add to non-existent folder → 404
	nf := doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/folders",
		map[string]any{"folder_id": "00000000-0000-0000-0000-000000000000"}, hdr)
	if nf.Code != http.StatusNotFound {
		t.Fatalf("bad folder: want 404 got %d", nf.Code)
	}
}
