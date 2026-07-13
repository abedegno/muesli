package api_test

// This file is DB-backed and CI-only: testutil.NewPool skips when
// TEST_DATABASE_URL is unset, so it must not be run on the local CI runner.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
)

func TestGetSharedNote(t *testing.T) {
	t.Parallel()

	srv, st := newShareTestServer(t)
	ctx := context.Background()

	if err := st.SeedBuiltInTemplates(ctx); err != nil {
		t.Fatalf("seed templates: %v", err)
	}
	tmpls, err := st.BuiltInTemplates(ctx)
	if err != nil {
		t.Fatalf("built-in templates: %v", err)
	}
	if len(tmpls) < 2 {
		t.Fatalf("need at least 2 built-in templates, got %d", len(tmpls))
	}

	owner, ownerHdr := createAuthedUser(t, st, "shared-owner@example.com")
	other, _ := createAuthedUser(t, st, "shared-other@example.com")

	sharedNote, err := st.CreateNote(ctx, owner.ID, "Shared note title")
	if err != nil {
		t.Fatalf("create shared note: %v", err)
	}
	startedAt := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	if _, err := st.Pool().Exec(ctx, `UPDATE notes SET started_at=$2 WHERE id=$1`, sharedNote.ID, startedAt); err != nil {
		t.Fatalf("set started_at: %v", err)
	}
	if _, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID:            sharedNote.ID,
		TranscriberPlugin: "test-transcriber",
		Model:             "test-model",
		Segments: []model.Segment{{
			StartMS: 0,
			EndMS:   1000,
			Text:    "Shared transcript content",
			Source:  "mic",
		}},
	}); err != nil {
		t.Fatalf("save transcript: %v", err)
	}
	readySummaryID, err := st.CreatePendingSummary(ctx, sharedNote.ID, tmpls[0].ID)
	if err != nil {
		t.Fatalf("create ready summary: %v", err)
	}
	if err := st.CompleteSummary(ctx, readySummaryID, "agent", "model", []model.SummarySection{{
		Heading:         "Overview",
		ContentMarkdown: "Ready summary content",
	}}, false); err != nil {
		t.Fatalf("complete ready summary: %v", err)
	}
	pendingSummaryID, err := st.CreatePendingSummary(ctx, sharedNote.ID, tmpls[1].ID)
	if err != nil {
		t.Fatalf("create pending summary: %v", err)
	}
	if pendingSummaryID == "" {
		t.Fatal("expected pending summary id")
	}

	foreignNote, err := st.CreateNote(ctx, other.ID, "Foreign note title")
	if err != nil {
		t.Fatalf("create foreign note: %v", err)
	}
	if _, err := st.SaveTranscript(ctx, model.Transcript{
		NoteID:            foreignNote.ID,
		TranscriberPlugin: "test-transcriber",
		Model:             "test-model",
		Segments: []model.Segment{{
			StartMS: 0,
			EndMS:   500,
			Text:    "Foreign transcript content",
			Source:  "mic",
		}},
	}); err != nil {
		t.Fatalf("save foreign transcript: %v", err)
	}
	foreignSummaryID, err := st.CreatePendingSummary(ctx, foreignNote.ID, tmpls[0].ID)
	if err != nil {
		t.Fatalf("create foreign summary: %v", err)
	}
	if err := st.CompleteSummary(ctx, foreignSummaryID, "agent", "model", []model.SummarySection{{
		Heading:         "Overview",
		ContentMarkdown: "Foreign summary content",
	}}, false); err != nil {
		t.Fatalf("complete foreign summary: %v", err)
	}

	shareRec := doJSON(t, srv, http.MethodPost, "/api/notes/"+sharedNote.ID+"/share", nil, ownerHdr)
	if shareRec.Code != http.StatusCreated {
		t.Fatalf("create share status %d body %s", shareRec.Code, shareRec.Body)
	}
	var created struct {
		Token string `json:"token"`
		ID    string `json:"id"`
	}
	if err := json.Unmarshal(shareRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode share response: %v", err)
	}
	if created.Token == "" {
		t.Fatal("expected token in share response")
	}
	share, err := st.GetActiveShare(ctx, created.Token)
	if err != nil {
		t.Fatalf("load created share: %v", err)
	}

	rec := doJSON(t, srv, http.MethodGet, "/api/shared/"+created.Token, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("shared note status %d body %s", rec.Code, rec.Body)
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode shared note: %v", err)
	}
	assertSharedNoteShape(t, rec.Body.Bytes())
	if _, ok := body["title"]; !ok {
		t.Fatal("missing title")
	}
	if _, ok := body["date"]; !ok {
		t.Fatal("missing date")
	}
	if _, ok := body["transcript"]; !ok {
		t.Fatal("missing transcript")
	}
	if _, ok := body["summary"]; !ok {
		t.Fatal("missing summary")
	}

	var got struct {
		Title      string `json:"title"`
		Date       string `json:"date"`
		Transcript struct {
			Segments []struct {
				Text string `json:"text"`
			} `json:"segments"`
		} `json:"transcript"`
		Summary struct {
			Sections []struct {
				ContentMarkdown string `json:"content_markdown"`
			} `json:"sections"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode shared note body: %v", err)
	}
	if got.Title != "Shared note title" {
		t.Fatalf("title = %q, want Shared note title", got.Title)
	}
	if got.Date != startedAt.Format(time.RFC3339) {
		t.Fatalf("date = %q, want %q", got.Date, startedAt.Format(time.RFC3339))
	}
	if len(got.Transcript.Segments) != 1 || got.Transcript.Segments[0].Text != "Shared transcript content" {
		t.Fatalf("shared transcript missing or wrong: %+v", got.Transcript)
	}
	if len(got.Summary.Sections) != 1 || got.Summary.Sections[0].ContentMarkdown != "Ready summary content" {
		t.Fatalf("shared summary missing or wrong: %+v", got.Summary)
	}

	raw := rec.Body.String()
	for _, unwanted := range []string{
		owner.ID,
		other.ID,
		sharedNote.ID,
		foreignNote.ID,
		share.ID,
		"Foreign note title",
		"Foreign transcript content",
		"Foreign summary content",
		"pending",
		"owner_id",
		"ownerId",
		"email",
		"note_id",
		"id",
		"deleted_at",
	} {
		if strings.Contains(raw, unwanted) {
			t.Fatalf("response leaked %q in %s", unwanted, raw)
		}
	}

	if rev := doJSON(t, srv, http.MethodDelete, "/api/shares/"+created.Token, nil, ownerHdr); rev.Code != http.StatusNoContent {
		t.Fatalf("revoke share status %d body %s", rev.Code, rev.Body)
	}

	for name, path := range map[string]string{
		"revoked": "/api/shared/" + created.Token,
		"expired": "/api/shared/expired-token",
		"garbage": "/api/shared/not-a-token",
	} {
		if name == "expired" {
			expiredAt := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
			expiredShare, err := st.CreateShare(ctx, owner.ID, sharedNote.ID, &expiredAt)
			if err != nil {
				t.Fatalf("create expired share: %v", err)
			}
			path = "/api/shared/" + expiredShare.Token
		}
		rec := doJSON(t, srv, http.MethodGet, path, nil, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s token status %d body %s", name, rec.Code, rec.Body)
		}
		if rec.Body.String() != `{"error":"not found"}`+"\n" {
			t.Fatalf("%s token body = %q, want identical not-found body", name, rec.Body.String())
		}
	}
}
