package api_test

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset, so it must not be run on the local CI runner.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abedegno/muesli/internal/auth"
	"github.com/abedegno/muesli/internal/store"
)

func createSecondUserHdr(t *testing.T, st *store.Store, email, password string) map[string]string {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	u, err := st.CreateUser(context.Background(), email, hash)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	raw, hash, err := auth.GenerateToken()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if err := st.CreateToken(context.Background(), u.ID, "session", hash, "session"); err != nil {
		t.Fatalf("create token: %v", err)
	}
	return map[string]string{"Authorization": "Bearer " + raw}
}

func TestNoteLinkHandlers(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "links@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "links@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	fromRec := doJSON(t, srv, http.MethodPost, "/api/notes",
		map[string]string{"title": "From"}, hdr)
	if fromRec.Code != http.StatusCreated {
		t.Fatalf("create from status=%d body=%s", fromRec.Code, fromRec.Body.String())
	}
	var from struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(fromRec.Body.Bytes(), &from)

	toRec := doJSON(t, srv, http.MethodPost, "/api/notes",
		map[string]string{"title": "To"}, hdr)
	if toRec.Code != http.StatusCreated {
		t.Fatalf("create to status=%d body=%s", toRec.Code, toRec.Body.String())
	}
	var to struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(toRec.Body.Bytes(), &to)

	backRec := doJSON(t, srv, http.MethodPost, "/api/notes",
		map[string]string{"title": "Back"}, hdr)
	if backRec.Code != http.StatusCreated {
		t.Fatalf("create back status=%d body=%s", backRec.Code, backRec.Body.String())
	}
	var back struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(backRec.Body.Bytes(), &back)

	post := doJSON(t, srv, http.MethodPost, "/api/notes/"+from.ID+"/links",
		map[string]string{"to_note_id": to.ID}, hdr)
	if post.Code != http.StatusCreated {
		t.Fatalf("add link status=%d body=%s", post.Code, post.Body.String())
	}
	var created struct {
		ID         string `json:"id"`
		OwnerID    string `json:"owner_id"`
		FromNoteID string `json:"from_note_id"`
		ToNoteID   string `json:"to_note_id"`
	}
	_ = json.Unmarshal(post.Body.Bytes(), &created)
	if created.ID == "" || created.FromNoteID != from.ID || created.ToNoteID != to.ID {
		t.Fatalf("unexpected created link: %+v", created)
	}

	dup := doJSON(t, srv, http.MethodPost, "/api/notes/"+from.ID+"/links",
		map[string]string{"to_note_id": to.ID}, hdr)
	if dup.Code != http.StatusConflict {
		t.Fatalf("duplicate status=%d body=%s", dup.Code, dup.Body.String())
	}

	self := doJSON(t, srv, http.MethodPost, "/api/notes/"+from.ID+"/links",
		map[string]string{"to_note_id": from.ID}, hdr)
	if self.Code != http.StatusBadRequest {
		t.Fatalf("self-link status=%d body=%s", self.Code, self.Body.String())
	}

	otherHdr := createSecondUserHdr(t, st, "other-links@example.com", "password123")
	foreign := doJSON(t, srv, http.MethodPost, "/api/notes/"+from.ID+"/links",
		map[string]string{"to_note_id": back.ID}, otherHdr)
	if foreign.Code != http.StatusNotFound {
		t.Fatalf("foreign note status=%d body=%s", foreign.Code, foreign.Body.String())
	}

	getRec := doJSON(t, srv, http.MethodGet, "/api/notes/"+to.ID+"/links", nil, hdr)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get links status=%d body=%s", getRec.Code, getRec.Body.String())
	}
	var got struct {
		Outgoing []struct {
			ID string `json:"id"`
		} `json:"outgoing"`
		Backlinks []struct {
			ID string `json:"id"`
		} `json:"backlinks"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode get: %v body=%s", err, getRec.Body.String())
	}
	if len(got.Outgoing) != 0 || len(got.Backlinks) != 1 || got.Backlinks[0].ID != created.ID {
		t.Fatalf("unexpected get payload: %s", getRec.Body.String())
	}

	delRec := doJSON(t, srv, http.MethodDelete, "/api/notes/"+from.ID+"/links",
		map[string]string{"to_note_id": to.ID}, hdr)
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", delRec.Code, delRec.Body.String())
	}

	missing := doJSON(t, srv, http.MethodDelete, "/api/notes/"+from.ID+"/links",
		map[string]string{"to_note_id": to.ID}, hdr)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("delete missing status=%d body=%s", missing.Code, missing.Body.String())
	}

	foreignGet := doJSON(t, srv, http.MethodGet, "/api/notes/"+back.ID+"/links", nil, otherHdr)
	if foreignGet.Code != http.StatusNotFound {
		t.Fatalf("foreign get status=%d body=%s", foreignGet.Code, foreignGet.Body.String())
	}
}
