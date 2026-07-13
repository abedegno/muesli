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

	tests := []struct {
		name           string
		note           model.Note
		summaries      []model.SummarySection
		segments       []model.Segment
		aliases        map[string]string
		opts           Options
		wantContains   []string
		wantNotContain []string
	}{
		{
			name: "speaker attributed transcript",
			note: model.Note{Title: "Planning Review"},
			summaries: []model.SummarySection{
				{
					Heading:         "Overview",
					ContentMarkdown: "First paragraph.\n\nSecond line of the same section.",
				},
			},
			segments: []model.Segment{
				{Speaker: "SPEAKER_00", Text: "We should ship it."},
				{Text: "No speaker here."},
			},
			aliases:      map[string]string{"SPEAKER_00": "Alice"},
			opts:         Options{IncludeTranscript: true},
			wantContains: []string{"Planning Review", "Overview", "Alice: We should ship it.", "Transcript"},
		},
		{
			name: "redacted transcript",
			note: model.Note{Title: "Planning Review"},
			summaries: []model.SummarySection{
				{
					Heading:         "Overview",
					ContentMarkdown: "First paragraph.\n\nSecond line of the same section.",
				},
			},
			segments: []model.Segment{
				{Speaker: "SPEAKER_01", Text: "We should ship it."},
				{Speaker: "SPEAKER_00", Text: "No objections."},
				{Text: "No speaker here."},
			},
			aliases:        map[string]string{"SPEAKER_00": "Alice", "SPEAKER_01": "Bob"},
			opts:           Options{IncludeTranscript: true, RedactSpeakers: true},
			wantContains:   []string{"Planning Review", "Overview", "Speaker 1: We should ship it.", "Speaker 2: No objections.", "No speaker here.", "Transcript"},
			wantNotContain: []string{"Alice:", "Bob:"},
		},
		{
			name: "summary only omits transcript",
			note: model.Note{Title: "Draft"},
			summaries: []model.SummarySection{
				{
					Heading:         "Overview",
					ContentMarkdown: "First paragraph.\n\nSecond line of the same section.",
				},
			},
			segments: []model.Segment{
				{Speaker: "SPEAKER_00", Text: "Working note."},
			},
			aliases:        map[string]string{"SPEAKER_00": "Alice"},
			opts:           Options{IncludeTranscript: false},
			wantContains:   []string{"Draft", "Overview", "First paragraph.", "Second line of the same section."},
			wantNotContain: []string{"Transcript", "Working note."},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data, err := RenderNoteDocx(tc.note, tc.summaries, tc.segments, tc.aliases, tc.opts)
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

			documentXML := extractDocxDocumentXML(t, data)
			for _, want := range tc.wantContains {
				if !strings.Contains(documentXML, want) {
					t.Fatalf("document.xml missing %q\n%s", want, documentXML)
				}
			}
			for _, want := range tc.wantNotContain {
				if strings.Contains(documentXML, want) {
					t.Fatalf("document.xml unexpectedly contains %q\n%s", want, documentXML)
				}
			}
		})
	}
}

func extractDocxDocumentXML(t *testing.T, data []byte) string {
	t.Helper()

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip.NewReader() error = %v", err)
	}

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
		return string(body)
	}

	t.Fatal("word/document.xml missing from docx archive")
	return ""
}
