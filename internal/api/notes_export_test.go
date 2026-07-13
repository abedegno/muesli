package api_test

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset, so these tests must not be run on the local runner.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestGetNoteExportQueryOptions(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "export@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "export@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	noteRec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Export Note"}, hdr)
	if noteRec.Code != http.StatusCreated {
		t.Fatalf("create note status=%d body=%s", noteRec.Code, noteRec.Body)
	}
	var note struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(noteRec.Body.Bytes(), &note)

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "defaults",
			path:       "/api/notes/" + note.ID + "/export?format=md",
			wantStatus: http.StatusOK,
		},
		{
			name:       "case insensitive true and false",
			path:       "/api/notes/" + note.ID + "/export?format=md&include_transcript=TRUE&redact_speakers=false",
			wantStatus: http.StatusOK,
		},
		{
			name:       "numeric values",
			path:       "/api/notes/" + note.ID + "/export?format=md&include_transcript=0&redact_speakers=1",
			wantStatus: http.StatusOK,
		},
		{
			name:       "empty uses defaults",
			path:       "/api/notes/" + note.ID + "/export?format=md&include_transcript=&redact_speakers=",
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid include transcript",
			path:       "/api/notes/" + note.ID + "/export?format=md&include_transcript=maybe",
			wantStatus: http.StatusBadRequest,
			wantBody:   "invalid option",
		},
		{
			name:       "invalid redact speakers",
			path:       "/api/notes/" + note.ID + "/export?format=md&redact_speakers=maybe",
			wantStatus: http.StatusBadRequest,
			wantBody:   "invalid option",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := doJSON(t, srv, http.MethodGet, tc.path, nil, hdr)
			if got.Code != tc.wantStatus {
				t.Fatalf("status=%d body=%s", got.Code, got.Body)
			}
			if tc.wantBody != "" && !strings.Contains(got.Body.String(), tc.wantBody) {
				t.Fatalf("response body %q does not contain %q", got.Body.String(), tc.wantBody)
			}
		})
	}
}
