package api_test

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset, so these tests must not be run on the local
// runner.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/auth"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
)

func TestGetNoteExportQueryOptions(t *testing.T) {
	t.Parallel()

	srv, st := newTestServer(t)
	ctx := context.Background()

	passwordHash, err := auth.HashPassword("password123")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user, err := st.CreateUser(ctx, "export-options@example.com", passwordHash)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	raw, hash, err := auth.GenerateToken()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if err := st.CreateToken(ctx, user.ID, "session", hash, "session"); err != nil {
		t.Fatalf("create token: %v", err)
	}
	hdr := map[string]string{"Authorization": "Bearer " + raw}

	note := seedExportNote(t, st, user.ID, "Exported Note")

	tests := []struct {
		name           string
		query          string
		wantStatus     int
		wantContains   []string
		wantNotContain []string
	}{
		{
			name:         "defaults include transcript and aliases",
			query:        "?format=md",
			wantStatus:   http.StatusOK,
			wantContains: []string{"## Overview", "Summary body", "Transcript", "Bob: First line", "Alice: Second line"},
		},
		{
			name:           "include transcript false omits transcript",
			query:          "?format=md&include_transcript=0",
			wantStatus:     http.StatusOK,
			wantContains:   []string{"## Overview", "Summary body"},
			wantNotContain: []string{"Transcript", "Bob: First line", "Alice: Second line"},
		},
		{
			name:           "redaction overrides aliases",
			query:          "?format=md&include_transcript=TRUE&redact_speakers=1",
			wantStatus:     http.StatusOK,
			wantContains:   []string{"Transcript", "Speaker 1: First line", "Speaker 2: Second line"},
			wantNotContain: []string{"Bob:", "Alice:"},
		},
		{
			name:       "invalid include transcript",
			query:      "?format=md&include_transcript=maybe",
			wantStatus: http.StatusBadRequest,
			wantContains: []string{
				"invalid option",
			},
		},
		{
			name:       "invalid redact speakers",
			query:      "?format=md&redact_speakers=maybe",
			wantStatus: http.StatusBadRequest,
			wantContains: []string{
				"invalid option",
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID+"/export"+tc.query, nil, hdr)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
			}
			body := rec.Body.String()
			for _, want := range tc.wantContains {
				if !containsString(body, want) {
					t.Fatalf("body missing %q\n%s", want, body)
				}
			}
			for _, want := range tc.wantNotContain {
				if containsString(body, want) {
					t.Fatalf("body unexpectedly contains %q\n%s", want, body)
				}
			}
		})
	}
}

func seedExportNote(t *testing.T, st *store.Store, ownerID, title string) model.Note {
	t.Helper()
	ctx := context.Background()

	note, err := st.CreateNote(ctx, ownerID, title)
	if err != nil {
		t.Fatalf("create note: %v", err)
	}

	if _, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID:            note.ID,
		TranscriberPlugin: "test",
		Model:             "test-model",
		Segments: []model.Segment{
			{StartMS: 0, EndMS: 1000, Text: "First line", Source: "test", Speaker: "SPEAKER_01"},
			{StartMS: 1000, EndMS: 2000, Text: "Second line", Source: "test", Speaker: "SPEAKER_00"},
		},
	}); err != nil {
		t.Fatalf("save transcript: %v", err)
	}

	sumID, err := st.CreatePendingSummary(ctx, note.ID, "template")
	if err != nil {
		t.Fatalf("create summary: %v", err)
	}
	if err := st.CompleteSummary(ctx, sumID, "agent", "model", []model.SummarySection{
		{Heading: "Overview", ContentMarkdown: "Summary body"},
	}, false); err != nil {
		t.Fatalf("complete summary: %v", err)
	}

	if err := st.UpsertSpeakerAlias(ctx, ownerID, note.ID, "SPEAKER_01", "Bob"); err != nil {
		t.Fatalf("upsert alias 1: %v", err)
	}
	if err := st.UpsertSpeakerAlias(ctx, ownerID, note.ID, "SPEAKER_00", "Alice"); err != nil {
		t.Fatalf("upsert alias 0: %v", err)
	}
	if err := st.SetNoteStatus(ctx, note.ID, model.NoteReady); err != nil {
		t.Fatalf("set note ready: %v", err)
	}

	return note
}

func containsString(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
