package noteexport

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/model"
)

func TestRenderNoteDocx(t *testing.T) {
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

	data, err := RenderNoteDocx(note, summaries, segments, aliases)
	if err != nil {
		t.Fatalf("RenderNoteDocx() error = %v", err)
	}
	if len(data) == 0 {
		t.Fatal("RenderNoteDocx() returned empty data")
	}
	if len(data) < 4 || !bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		prefix := data
		if len(prefix) > 4 {
			prefix = prefix[:4]
		}
		t.Fatalf("RenderNoteDocx() prefix = %q, want ZIP header", prefix)
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip.NewReader() error = %v", err)
	}

	var documentXML string
	for _, file := range zr.File {
		if file.Name != "word/document.xml" {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open document.xml: %v", err)
		}
		body, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read document.xml: %v", err)
		}
		documentXML = string(body)
		break
	}
	if documentXML == "" {
		t.Fatal("word/document.xml missing from docx archive")
	}

	checks := []string{
		"Planning Review",
		"Overview",
		"Alice: We should ship it.",
	}
	for _, want := range checks {
		if !strings.Contains(documentXML, want) {
			t.Fatalf("document.xml missing %q\n%s", want, documentXML)
		}
	}
}
