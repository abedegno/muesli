package noteexport

import (
	"bytes"
	"testing"

	"github.com/abedegno/muesli/internal/model"
)

func TestRenderNotePdf(t *testing.T) {
	t.Parallel()

	note := model.Note{Title: "Planning Review"}
	summaries := []model.SummarySection{
		{
			Heading:         "Overview",
			ContentMarkdown: "First paragraph.\n\nSecond line of the same section.",
		},
	}
	segments := []model.Segment{
		{Speaker: "SPEAKER_00", Text: "We should ship it."},
		{Text: "No speaker here."},
	}
	aliases := map[string]string{"SPEAKER_00": "Alice"}

	data, err := RenderNotePdf(note, summaries, segments, aliases)
	if err != nil {
		t.Fatalf("RenderNotePdf() error = %v", err)
	}
	if len(data) == 0 {
		t.Fatal("RenderNotePdf() returned empty data")
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		prefix := data
		if len(prefix) > 5 {
			prefix = prefix[:5]
		}
		t.Fatalf("RenderNotePdf() prefix = %q, want PDF header", prefix)
	}
}

func TestRenderNotePdfNotReadyNote(t *testing.T) {
	t.Parallel()

	note := model.Note{Title: "Draft"}
	segments := []model.Segment{
		{Text: "Working note"},
	}

	data, err := RenderNotePdf(note, nil, segments, nil)
	if err != nil {
		t.Fatalf("RenderNotePdf() error = %v", err)
	}
	if len(data) == 0 {
		t.Fatal("RenderNotePdf() returned empty data")
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		prefix := data
		if len(prefix) > 5 {
			prefix = prefix[:5]
		}
		t.Fatalf("RenderNotePdf() prefix = %q, want PDF header", prefix)
	}
}
