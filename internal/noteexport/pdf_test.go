package noteexport

import (
	"bytes"
	"compress/zlib"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/model"
)

func TestRenderNotePdf(t *testing.T) {
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
			data, err := RenderNotePdf(tc.note, tc.summaries, tc.segments, tc.aliases, tc.opts)
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

			text := extractPdfText(t, data)
			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Fatalf("pdf text missing %q\n%s", want, text)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(text, notWant) {
					t.Fatalf("pdf text unexpectedly contained %q\n%s", notWant, text)
				}
			}
		})
	}
}

func extractPdfText(t *testing.T, data []byte) string {
	t.Helper()

	streamRe := regexp.MustCompile(`(?s)stream\r?\n(.*?)\r?\nendstream`)
	matches := streamRe.FindAllSubmatch(data, -1)
	var out strings.Builder
	for _, match := range matches {
		stream := match[1]
		zr, err := zlib.NewReader(bytes.NewReader(stream))
		if err != nil {
			continue
		}
		body, err := io.ReadAll(zr)
		_ = zr.Close()
		if err != nil {
			continue
		}
		out.Write(body)
		out.WriteByte('\n')
	}
	return out.String()
}
