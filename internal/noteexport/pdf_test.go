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

			text := extractPDFText(t, data)
			for _, want := range tc.wantContains {
				if !strings.Contains(text, want) {
					t.Fatalf("pdf text missing %q\n%s", want, text)
				}
			}
			for _, want := range tc.wantNotContain {
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
	for {
		start := bytes.Index(data, []byte("stream"))
		if start < 0 {
			break
		}
		data = data[start+len("stream"):]
		if len(data) > 0 && data[0] == '\r' {
			data = data[1:]
		}
		if len(data) > 0 && data[0] == '\n' {
			data = data[1:]
		}
		end := bytes.Index(data, []byte("endstream"))
		if end < 0 {
			break
		}
		stream := data[:end]
		data = data[end+len("endstream"):]

		decoded := stream
		if zr, err := zlib.NewReader(bytes.NewReader(stream)); err == nil {
			decoded, err = io.ReadAll(zr)
			_ = zr.Close()
			if err != nil {
				t.Fatalf("read compressed pdf stream: %v", err)
			}
		}
		for _, m := range pdfStringLiteralRE.FindAll(decoded, -1) {
			out.WriteString(unescapePDFString(string(m[1 : len(m)-1])))
			out.WriteByte('\n')
		}
	}
	return out.String()
}

var pdfStringLiteralRE = regexp.MustCompile(`\((?:\\.|[^\\()])*\)`)

func unescapePDFString(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			out.WriteByte(s[i])
			continue
		}
		if i+1 >= len(s) {
			break
		}
		i++
		switch s[i] {
		case 'n':
			out.WriteByte('\n')
		case 'r':
			out.WriteByte('\r')
		case 't':
			out.WriteByte('\t')
		case 'b':
			out.WriteByte('\b')
		case 'f':
			out.WriteByte('\f')
		case '\\', '(', ')':
			out.WriteByte(s[i])
		default:
			out.WriteByte(s[i])
		}
	}
	return out.String()
}
