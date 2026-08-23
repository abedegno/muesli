package api_test

import (
	"context"
	"testing"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
)

func seedExportableNote(t *testing.T, st *store.Store, ownerID, noteID string) {
	t.Helper()
	ctx := context.Background()

	if err := st.SeedBuiltInTemplates(ctx); err != nil {
		t.Fatalf("seed built-in templates: %v", err)
	}
	templates, err := st.BuiltInTemplates(ctx)
	if err != nil {
		t.Fatalf("load built-in templates: %v", err)
	}
	if len(templates) == 0 {
		t.Fatal("expected at least one built-in template")
	}

	tr := model.Transcript{
		NoteID:            noteID,
		TranscriberPlugin: "stub-transcriber",
		Model:             "stub-model",
		Segments: []model.Segment{
			{Speaker: "SPEAKER_00", Text: "We should ship it."},
			{Speaker: "SPEAKER_01", Text: "Then we can announce it."},
			{Speaker: "SPEAKER_00", Text: "Agreed."},
		},
	}
	if _, err := st.SaveTranscript(ctx, tr, 0); err != nil {
		t.Fatalf("save transcript: %v", err)
	}
	for _, alias := range []struct {
		label string
		name  string
	}{
		{label: "SPEAKER_00", name: "Alice"},
		{label: "SPEAKER_01", name: "Bob"},
	} {
		if err := st.UpsertSpeakerAlias(ctx, ownerID, noteID, alias.label, alias.name); err != nil {
			t.Fatalf("upsert alias %s: %v", alias.label, err)
		}
	}
	sumID, err := st.CreatePendingSummary(ctx, noteID, templates[0].ID)
	if err != nil {
		t.Fatalf("create pending summary: %v", err)
	}
	if err := st.CompleteSummary(ctx, sumID, "stub-agent", "stub-model", []model.SummarySection{
		{Heading: "Overview", ContentMarkdown: "Summary line."},
	}, false); err != nil {
		t.Fatalf("complete summary: %v", err)
	}
	if err := st.SetNoteStatus(ctx, noteID, model.NoteReady); err != nil {
		t.Fatalf("set note ready: %v", err)
	}
}
