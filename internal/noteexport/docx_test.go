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
	tests := []struct {
		name            string
		segments        []model.Segment
		aliases         map[string]string
		options         Options
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "renders summary and transcript",
			segments: []model.Segment{
				{Speaker: "SPEAKER_00", Text: "We should ship it."},
				{Text: "No speaker here."},
			},
			aliases: map[string]string{"SPEAKER_00": "Alice"},
			options: Options{IncludeTranscript: true},
			wantContains: []string{
				"Planning Review",
				"Overview",
				"Alice: We should ship it.",
				"No speaker here.",
				"Transcript",
			},
		},
		{
			name: "redacts speakers",
			segments: []model.Segment{
				{Speaker: "SPEAKER_10", Text: "Hello"},
				{Text: "No speaker here."},
				{Speaker: "SPEAKER_20", Text: "Hi there"},
				{Speaker: "SPEAKER_10", Text: "Again"},
			},
			aliases: map[string]string{"SPEAKER_10": "Alice", "SPEAKER_20": "Bob"},
			options: Options{IncludeTranscript: true, RedactSpeakers: true},
			wantContains: []string{
				"Planning Review",
				"Overview",
				"Speaker 1: Hello",
				"No speaker here.",
				"Speaker 2: Hi there",
				"Speaker 1: Again",
			},
			wantNotContains: []string{"Alice", "Bob"},
		},
		{
			name:    "omits transcript",
			options: Options{IncludeTranscript: false},
			wantContains: []string{
				"Planning Review",
				"Overview",
				"First paragraph.",
				"Second line of the same section.",
			},
			wantNotContains: []string{"Transcript", "Alice", "We should ship it.", "No speaker here."},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data, err := RenderNoteDocx(note, summaries, tc.segments, tc.aliases, tc.options)
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

			for _, want := range tc.wantContains {
				if !strings.Contains(documentXML, want) {
					t.Fatalf("document.xml missing %q\n%s", want, documentXML)
				}
			}
			for _, want := range tc.wantNotContains {
				if strings.Contains(documentXML, want) {
					t.Fatalf("document.xml unexpectedly contains %q\n%s", want, documentXML)
				}
			}
		})
	}
}
