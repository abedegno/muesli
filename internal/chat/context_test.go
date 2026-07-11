package chat

import (
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/model"
)

func TestSystemDirective(t *testing.T) {
	lower := strings.ToLower(SystemDirective)
	if !strings.Contains(lower, "cite") {
		t.Errorf("SystemDirective must mention citing, got: %q", SystemDirective)
	}
	if !strings.Contains(lower, "verbatim") {
		t.Errorf("SystemDirective must mention verbatim speaker names, got: %q", SystemDirective)
	}
}

func TestRenderRetrievalBlock(t *testing.T) {
	tests := []struct {
		name    string
		sources []TranscriptRef
		want    []string // substrings expected, in order
	}{
		{
			name:    "empty sources",
			sources: nil,
			want:    []string{noSourcesNotice},
		},
		{
			name: "single source",
			sources: []TranscriptRef{
				{NoteID: "n1", NoteTitle: "Standup", StartMS: 65000, Snippet: "we shipped the thing"},
			},
			want: []string{"[1] Standup (01:05): we shipped the thing"},
		},
		{
			name: "multiple sources numbered in input order",
			sources: []TranscriptRef{
				{NoteID: "n1", NoteTitle: "Standup", StartMS: 0, Snippet: "first"},
				{NoteID: "n2", NoteTitle: "Retro", StartMS: 5000, Snippet: "second"},
				{NoteID: "n3", NoteTitle: "Planning", StartMS: 125000, Snippet: "third"},
			},
			want: []string{
				"[1] Standup (00:00): first",
				"[2] Retro (00:05): second",
				"[3] Planning (02:05): third",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RenderRetrievalBlock(tc.sources)
			lines := strings.Split(got, "\n")
			if len(tc.want) != len(lines) {
				t.Fatalf("RenderRetrievalBlock(%+v) = %q, want %d lines got %d", tc.sources, got, len(tc.want), len(lines))
			}
			for i, w := range tc.want {
				if lines[i] != w {
					t.Errorf("line %d = %q, want %q", i, lines[i], w)
				}
			}
			// Order must correspond 1:1 with the input slice order.
			for i := range tc.sources {
				marker := "[" + string(rune('1'+i)) + "]"
				if !strings.Contains(got, marker) {
					t.Errorf("expected marker %s in rendered block: %q", marker, got)
				}
			}
		})
	}
}

func TestFormatTimestamp(t *testing.T) {
	tests := []struct {
		ms   int
		want string
	}{
		{0, "00:00"},
		{999, "00:00"},
		{1000, "00:01"},
		{65000, "01:05"},
		{3661000, "61:01"},
		{-5000, "00:00"},
	}
	for _, tc := range tests {
		if got := formatTimestamp(tc.ms); got != tc.want {
			t.Errorf("formatTimestamp(%d) = %q, want %q", tc.ms, got, tc.want)
		}
	}
}

func TestBuildPrompt(t *testing.T) {
	sources := []TranscriptRef{
		{NoteID: "n1", NoteTitle: "Standup", StartMS: 0, Snippet: "first"},
		{NoteID: "n2", NoteTitle: "Retro", StartMS: 5000, Snippet: "second"},
	}
	history := []model.Message{
		{ID: "m1", Role: "user", Content: "what did we discuss last time?"},
		{ID: "m2", Role: "assistant", Content: "You discussed the roadmap [1]."},
	}

	prompt := BuildPrompt(sources, history, "anything new?")

	if len(prompt) != 2+len(history)+1 {
		t.Fatalf("BuildPrompt returned %d messages, want %d", len(prompt), 2+len(history)+1)
	}

	if prompt[0].Role != "system" || prompt[0].Content != SystemDirective {
		t.Errorf("prompt[0] = %+v, want system directive", prompt[0])
	}

	if prompt[1].Role != "system" {
		t.Errorf("prompt[1].Role = %q, want system", prompt[1].Role)
	}
	if !strings.Contains(prompt[1].Content, "[1] Standup") || !strings.Contains(prompt[1].Content, "[2] Retro") {
		t.Errorf("prompt[1].Content = %q, want numbered sources", prompt[1].Content)
	}

	for i, h := range history {
		got := prompt[2+i]
		if got.Role != h.Role || got.Content != h.Content {
			t.Errorf("prompt[%d] = %+v, want %+v", 2+i, got, h)
		}
	}

	last := prompt[len(prompt)-1]
	if last.Role != "user" || last.Content != "anything new?" {
		t.Errorf("last message = %+v, want the new user message", last)
	}
}

func TestBuildPrompt_EmptyHistory(t *testing.T) {
	sources := []TranscriptRef{{NoteID: "n1", NoteTitle: "Standup", StartMS: 0, Snippet: "first"}}
	prompt := BuildPrompt(sources, nil, "hello")

	if len(prompt) != 3 {
		t.Fatalf("BuildPrompt with empty history returned %d messages, want 3", len(prompt))
	}
	if prompt[0].Role != "system" {
		t.Errorf("prompt[0].Role = %q, want system", prompt[0].Role)
	}
	if prompt[1].Role != "system" {
		t.Errorf("prompt[1].Role = %q, want system", prompt[1].Role)
	}
	if prompt[2].Role != "user" || prompt[2].Content != "hello" {
		t.Errorf("prompt[2] = %+v, want the new user message", prompt[2])
	}
}

func TestBuildPrompt_EmptySources(t *testing.T) {
	history := []model.Message{{ID: "m1", Role: "user", Content: "hi"}}
	prompt := BuildPrompt(nil, history, "follow up")

	if len(prompt) != 4 {
		t.Fatalf("BuildPrompt with empty sources returned %d messages, want 4", len(prompt))
	}
	if prompt[1].Content != noSourcesNotice {
		t.Errorf("prompt[1].Content = %q, want the no-sources notice", prompt[1].Content)
	}
	if prompt[2].Role != "user" || prompt[2].Content != "hi" {
		t.Errorf("prompt[2] = %+v, want prior history message", prompt[2])
	}
	if prompt[3].Role != "user" || prompt[3].Content != "follow up" {
		t.Errorf("prompt[3] = %+v, want the new user message", prompt[3])
	}
}
