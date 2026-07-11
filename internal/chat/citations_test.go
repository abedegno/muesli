package chat

import (
	"reflect"
	"testing"
)

func testSources() []TranscriptRef {
	return []TranscriptRef{
		{NoteID: "n1", NoteTitle: "Standup", SegmentIndex: 0, StartMS: 1000, Snippet: "first"},
		{NoteID: "n2", NoteTitle: "Retro", SegmentIndex: 2, StartMS: 5000, Snippet: "second"},
		{NoteID: "n3", NoteTitle: "Planning", SegmentIndex: 4, StartMS: 9000, Snippet: "third"},
	}
}

func TestParseCitations(t *testing.T) {
	sources := testSources()

	tests := []struct {
		name     string
		response string
		want     []Source
	}{
		{
			name:     "single marker",
			response: "We shipped the feature [1].",
			want: []Source{
				{N: 1, NoteID: "n1", SegmentIndex: 0, Timestamp: 1000, Snippet: "first"},
			},
		},
		{
			name:     "multiple distinct markers in appearance order",
			response: "First [2] then also [1] and [3].",
			want: []Source{
				{N: 2, NoteID: "n2", SegmentIndex: 2, Timestamp: 5000, Snippet: "second"},
				{N: 1, NoteID: "n1", SegmentIndex: 0, Timestamp: 1000, Snippet: "first"},
				{N: 3, NoteID: "n3", SegmentIndex: 4, Timestamp: 9000, Snippet: "third"},
			},
		},
		{
			name:     "duplicate marker deduped, kept once at first appearance",
			response: "As noted [1], and again [1] later.",
			want: []Source{
				{N: 1, NoteID: "n1", SegmentIndex: 0, Timestamp: 1000, Snippet: "first"},
			},
		},
		{
			name:     "out-of-range marker dropped without error",
			response: "This cites [5] which does not exist, but [2] does.",
			want: []Source{
				{N: 2, NoteID: "n2", SegmentIndex: 2, Timestamp: 5000, Snippet: "second"},
			},
		},
		{
			name:     "marker n=0 has no valid citation (0 is out of range)",
			response: "Bad marker [0] should be dropped.",
			want:     []Source{},
		},
		{
			name:     "no citation markers returns empty slice",
			response: "This response has no citations at all.",
			want:     []Source{},
		},
		{
			name:     "malformed brackets ignored",
			response: "Not a marker [abc] or unterminated [1 or empty [].",
			want:     []Source{},
		},
		{
			name:     "malformed alongside a valid marker",
			response: "Ignore [abc] but keep [1].",
			want: []Source{
				{N: 1, NoteID: "n1", SegmentIndex: 0, Timestamp: 1000, Snippet: "first"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseCitations(tc.response, sources)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseCitations(%q) = %+v, want %+v", tc.response, got, tc.want)
			}
		})
	}
}

func TestParseCitations_EmptySources(t *testing.T) {
	got := ParseCitations("Cites [1] and [2].", nil)
	if len(got) != 0 {
		t.Errorf("ParseCitations with no sources = %+v, want empty slice", got)
	}
}

func TestParseCitations_NeverNil(t *testing.T) {
	got := ParseCitations("no markers here", testSources())
	if got == nil {
		t.Errorf("ParseCitations must return a non-nil empty slice, got nil")
	}
}
