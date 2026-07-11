package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abedegno/muesli/internal/auth"
)

func TestConversationsCRUDAPI(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "conv-api@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "conv-api@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	// A second, unrelated user for owner-isolation checks. /api/setup only
	// allows a single first-run account, so create this one directly via the
	// store (and mint a session token the same way auth.Middleware expects),
	// mirroring the pattern used in dedup_test.go's owner_isolation subtest.
	ctx := context.Background()
	otherUser, err := st.CreateUser(ctx, "conv-other@example.com", "h")
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}
	otherRaw, otherHash, err := auth.GenerateToken()
	if err != nil {
		t.Fatalf("generate other token: %v", err)
	}
	if err := st.CreateToken(ctx, otherUser.ID, "session", otherHash, "session"); err != nil {
		t.Fatalf("create other token: %v", err)
	}
	otherHdr := map[string]string{"Authorization": "Bearer " + otherRaw}

	// create a note to scope a conversation to
	noteRec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]any{"title": "Standup"}, hdr)
	if noteRec.Code != http.StatusCreated {
		t.Fatalf("create note=%d body=%s", noteRec.Code, noteRec.Body.String())
	}
	var note struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(noteRec.Body.Bytes(), &note)

	// create global conversation
	c := doJSON(t, srv, http.MethodPost, "/api/conversations", map[string]any{"title": "General"}, hdr)
	if c.Code != http.StatusCreated {
		t.Fatalf("create=%d body=%s", c.Code, c.Body.String())
	}
	var created struct {
		ID     string  `json:"id"`
		NoteID *string `json:"note_id"`
	}
	_ = json.Unmarshal(c.Body.Bytes(), &created)
	if created.ID == "" {
		t.Fatal("expected id")
	}

	// create note-scoped conversation
	sc := doJSON(t, srv, http.MethodPost, "/api/conversations",
		map[string]any{"title": "About standup", "note_id": note.ID}, hdr)
	if sc.Code != http.StatusCreated {
		t.Fatalf("scoped create=%d body=%s", sc.Code, sc.Body.String())
	}
	var scoped struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(sc.Body.Bytes(), &scoped)

	// list all -> both
	l := doJSON(t, srv, http.MethodGet, "/api/conversations", nil, hdr)
	if l.Code != http.StatusOK {
		t.Fatalf("list=%d", l.Code)
	}
	var all []map[string]any
	_ = json.Unmarshal(l.Body.Bytes(), &all)
	if len(all) != 2 {
		t.Fatalf("list all: want 2 got %d body=%s", len(all), l.Body.String())
	}

	// list filtered by note -> only the scoped one
	lf := doJSON(t, srv, http.MethodGet, "/api/conversations?note_id="+note.ID, nil, hdr)
	if lf.Code != http.StatusOK {
		t.Fatalf("list filtered=%d", lf.Code)
	}
	var filtered []map[string]any
	_ = json.Unmarshal(lf.Body.Bytes(), &filtered)
	if len(filtered) != 1 {
		t.Fatalf("list filtered: want 1 got %d body=%s", len(filtered), lf.Body.String())
	}

	// get -> 200
	g := doJSON(t, srv, http.MethodGet, "/api/conversations/"+created.ID, nil, hdr)
	if g.Code != http.StatusOK {
		t.Fatalf("get=%d body=%s", g.Code, g.Body.String())
	}

	// get nonexistent -> 404
	gnf := doJSON(t, srv, http.MethodGet, "/api/conversations/00000000-0000-0000-0000-000000000000", nil, hdr)
	if gnf.Code != http.StatusNotFound {
		t.Fatalf("get nonexistent: want 404 got %d", gnf.Code)
	}

	// owner isolation: other user cannot see it
	go1 := doJSON(t, srv, http.MethodGet, "/api/conversations/"+created.ID, nil, otherHdr)
	if go1.Code != http.StatusNotFound {
		t.Fatalf("other-owner get: want 404 got %d", go1.Code)
	}
	lo := doJSON(t, srv, http.MethodGet, "/api/conversations", nil, otherHdr)
	var otherList []map[string]any
	_ = json.Unmarshal(lo.Body.Bytes(), &otherList)
	if len(otherList) != 0 {
		t.Fatalf("other-owner list: want 0 got %d", len(otherList))
	}

	// messages: empty at first
	msgs := doJSON(t, srv, http.MethodGet, "/api/conversations/"+created.ID+"/messages", nil, hdr)
	if msgs.Code != http.StatusOK {
		t.Fatalf("messages=%d body=%s", msgs.Code, msgs.Body.String())
	}
	var msgList []map[string]any
	_ = json.Unmarshal(msgs.Body.Bytes(), &msgList)
	if len(msgList) != 0 {
		t.Fatalf("expected no messages yet, got %+v", msgList)
	}

	// messages: 404 for nonexistent conversation
	msgsNF := doJSON(t, srv, http.MethodGet, "/api/conversations/00000000-0000-0000-0000-000000000000/messages", nil, hdr)
	if msgsNF.Code != http.StatusNotFound {
		t.Fatalf("messages nonexistent: want 404 got %d", msgsNF.Code)
	}

	// messages: owner isolation
	msgsOther := doJSON(t, srv, http.MethodGet, "/api/conversations/"+created.ID+"/messages", nil, otherHdr)
	if msgsOther.Code != http.StatusNotFound {
		t.Fatalf("other-owner messages: want 404 got %d", msgsOther.Code)
	}

	// delete -> 204
	d := doJSON(t, srv, http.MethodDelete, "/api/conversations/"+created.ID, nil, hdr)
	if d.Code != http.StatusNoContent {
		t.Fatalf("delete=%d body=%s", d.Code, d.Body.String())
	}

	// delete again -> 404 (already gone)
	dnf := doJSON(t, srv, http.MethodDelete, "/api/conversations/"+created.ID, nil, hdr)
	if dnf.Code != http.StatusNotFound {
		t.Fatalf("delete again: want 404 got %d", dnf.Code)
	}

	// delete not-owned -> 404
	dOther := doJSON(t, srv, http.MethodDelete, "/api/conversations/"+scoped.ID, nil, otherHdr)
	if dOther.Code != http.StatusNotFound {
		t.Fatalf("delete not-owned: want 404 got %d", dOther.Code)
	}
}
