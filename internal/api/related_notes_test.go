package api_test

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset, so it must not be run on the local CI runner.

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestRelatedNotesHandler(t *testing.T) {
	t.Parallel()

	t.Run("degrades to empty when embeddings are disabled", func(t *testing.T) {
		t.Parallel()
		srv, _ := newTestServer(t)
		_ = doJSON(t, srv, http.MethodPost, "/api/setup",
			map[string]string{"email": "related@example.com", "password": "password123"}, nil)
		rec := doJSON(t, srv, http.MethodPost, "/api/login",
			map[string]string{"email": "related@example.com", "password": "password123"}, nil)
		var login struct {
			Token string `json:"token"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &login)
		hdr := map[string]string{"Authorization": "Bearer " + login.Token}

		noAuth := doJSON(t, srv, http.MethodGet, "/api/notes/00000000-0000-0000-0000-000000000000/related", nil, nil)
		if noAuth.Code != http.StatusUnauthorized {
			t.Fatalf("missing auth status=%d body=%s", noAuth.Code, noAuth.Body.String())
		}

		invalid := doJSON(t, srv, http.MethodGet, "/api/notes/not-a-uuid/related", nil, hdr)
		if invalid.Code != http.StatusNotFound {
			t.Fatalf("invalid note id status=%d body=%s", invalid.Code, invalid.Body.String())
		}

		rec = doJSON(t, srv, http.MethodGet, "/api/notes/11111111-1111-1111-1111-111111111111/related", nil, hdr)
		if rec.Code != http.StatusOK {
			t.Fatalf("related disabled status=%d body=%s", rec.Code, rec.Body.String())
		}
		if got := rec.Body.String(); got != "[]\n" {
			t.Fatalf("related disabled body=%q, want %q", got, "[]\n")
		}
	})

	t.Run("returns not found for foreign notes", func(t *testing.T) {
		t.Parallel()
		srv, st := newSearchServer(t, fakeEmbedder{vec: fixedVec()})
		hdr := authHeader(t, srv, "owner@example.com")
		otherHdr := createSecondUserHdr(t, st, "other@example.com", "password123")

		foreign := createNote(t, srv, otherHdr, "Foreign note")
		rec := doJSON(t, srv, http.MethodGet, "/api/notes/"+foreign+"/related", nil, hdr)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("foreign note status=%d body=%s", rec.Code, rec.Body.String())
		}
	})
}
