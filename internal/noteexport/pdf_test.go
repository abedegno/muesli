package noteexport

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"io"
	"strings"
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
				"Transcript",
				"Alice: We should ship it.",
				"No speaker here.",
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
				"Transcript",
				"Speaker 1: Hello",
				"Speaker 2: Hi there",
				"Speaker 1: Again",
				"No speaker here.",
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
			data, err := RenderNotePdf(note, summaries, tc.segments, tc.aliases, tc.options)
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

			text := extractPDFText(t, data)
			for _, want := range tc.wantContains {
				if !strings.Contains(text, want) {
					t.Fatalf("pdf text missing %q\n%s", want, text)
				}
			}
			for _, want := range tc.wantNotContains {
				if strings.Contains(text, want) {
					t.Fatalf("pdf text unexpectedly contains %q\n%s", want, text)
				}
			}
		})
	}
}

func TestRenderNotePdfNotReadyNote(t *testing.T) {
	t.Parallel()

	note := model.Note{Title: "Draft"}
	segments := []model.Segment{
		{Text: "Working note"},
	}

	data, err := RenderNotePdf(note, nil, segments, nil, Options{IncludeTranscript: true})
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

func extractPDFText(t *testing.T, data []byte) string {
	t.Helper()

	var out strings.Builder
	offset := 0
	for {
		start := bytes.Index(data[offset:], []byte("stream"))
		if start < 0 {
			break
		}
		start += offset
		start += len("stream")
		if start < len(data) {
			switch data[start] {
			case '\r':
				start++
				if start < len(data) && data[start] == '\n' {
					start++
				}
			case '\n':
				start++
			}
		}
		end := bytes.Index(data[start:], []byte("endstream"))
		if end < 0 {
			break
		}
		raw := bytes.TrimSpace(data[start : start+end])
		if len(raw) > 0 {
			if text, ok := decodePDFStream(raw); ok {
				out.Write(text)
				out.WriteByte('\n')
			}
		}
		offset = start + end + len("endstream")
	}
	return out.String()
}

func decodePDFStream(raw []byte) ([]byte, bool) {
	if zr, err := zlib.NewReader(bytes.NewReader(raw)); err == nil {
		defer zr.Close()
		text, err := io.ReadAll(zr)
		if err == nil {
			return text, true
		}
	}

	fr := flate.NewReader(bytes.NewReader(raw))
	defer fr.Close()
	text, err := io.ReadAll(fr)
	if err != nil {
		return nil, false
	}
	return text, true
}
