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

	note, summaries, segments, aliases := exportTestFixture()

	tests := []struct {
		name      string
		note      model.Note
		summaries []model.SummarySection
		segments  []model.Segment
		aliases   map[string]string
		opts      Options
		want      []string
		notWant   []string
	}{
		{
			name:      "redacted transcript is stable",
			note:      note,
			summaries: summaries,
			segments:  segments,
			aliases:   aliases,
			opts:      Options{IncludeTranscript: true, RedactSpeakers: true},
			want:      []string{"Planning Review", "Overview", "Speaker 1: We should ship it.", "Speaker 2: Then we can announce it."},
			notWant:   []string{"Alice: We should ship it.", "Bob: Then we can announce it."},
		},
		{
			name:      "summary is preserved when transcript is omitted",
			note:      note,
			summaries: summaries,
			segments:  segments,
			aliases:   aliases,
			opts:      Options{IncludeTranscript: false},
			want:      []string{"Planning Review", "Overview", "First paragraph.", "Second line of the same section."},
			notWant:   []string{"Transcript", "Alice: We should ship it.", "Bob: Then we can announce it."},
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

			for _, want := range tc.want {
				if !strings.Contains(documentXML, want) {
					t.Fatalf("document.xml missing %q\n%s", want, documentXML)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(documentXML, notWant) {
					t.Fatalf("document.xml unexpectedly contained %q\n%s", notWant, documentXML)
				}
			}
		})
	}
}
