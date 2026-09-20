package execution

import (
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/model"
)

func preparedWithPromptBytes(t *testing.T, n int) PreparedExecution {
	t.Helper()
	input := ExecutionInput{
		Template:  model.Template{Sections: []model.TemplateSection{{Heading: "H", Instruction: "I"}}},
		Documents: []Document{{NoteID: "note-1", Title: "T", Segments: []model.Segment{{StartMS: 0, Text: "x"}}}},
	}
	prepared, err := PrepareDocuments(input)
	if err != nil {
		t.Fatalf("PrepareDocuments: %v", err)
	}
	// Overwrite with a synthetic prompt of an exact byte length for precise
	// boundary testing (independent of the real corpus's exact size).
	prepared.SectionPrompts = []SectionPrompt{{Heading: "H", Prompt: strings.Repeat("a", n)}}
	return prepared
}

func TestAdmitExactBoundary(t *testing.T) {
	cfg := AdmissionConfig{ContextTokens: 120, OutputReserveTokens: 20, ProviderFramingTokens: 0, ByteFallbackTokenizer: true}
	// effective = 100
	if err := Admit(preparedWithPromptBytes(t, 100), cfg); err != nil {
		t.Fatalf("expected admission exactly at the ceiling to succeed, got %v", err)
	}
	if err := Admit(preparedWithPromptBytes(t, 101), cfg); err != ErrContextBudget {
		t.Fatalf("expected one byte over the ceiling to fail with ErrContextBudget, got %v", err)
	}
}

func TestAdmitRejectsInvalidConfig(t *testing.T) {
	base := preparedWithPromptBytes(t, 10)
	cases := []struct {
		name string
		cfg  AdmissionConfig
	}{
		{"non-positive context", AdmissionConfig{ContextTokens: 0, ByteFallbackTokenizer: true}},
		{"negative context", AdmissionConfig{ContextTokens: -1, ByteFallbackTokenizer: true}},
		{"negative output reserve", AdmissionConfig{ContextTokens: 100, OutputReserveTokens: -1, ByteFallbackTokenizer: true}},
		{"negative framing reserve", AdmissionConfig{ContextTokens: 100, ProviderFramingTokens: -1, ByteFallbackTokenizer: true}},
		{"reserves consume entire context", AdmissionConfig{ContextTokens: 100, OutputReserveTokens: 60, ProviderFramingTokens: 40, ByteFallbackTokenizer: true}},
		{"reserves exceed context", AdmissionConfig{ContextTokens: 100, OutputReserveTokens: 80, ProviderFramingTokens: 40, ByteFallbackTokenizer: true}},
		{"missing byte-fallback capability fails closed", AdmissionConfig{ContextTokens: 100, ByteFallbackTokenizer: false}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if err := Admit(base, tc.cfg); err != ErrInvalidAdmissionConfig {
				t.Fatalf("got %v, want ErrInvalidAdmissionConfig", err)
			}
		})
	}
}

// TestAdmitByteCountsMultibyteText proves the bound counts UTF-8 BYTES, not
// runes or an average-bytes-per-token heuristic: ASCII, CJK (3 bytes/rune),
// and emoji (4 bytes/rune) all produce the byte length go's len([]byte(...))
// would report, and admission reacts accordingly.
func TestAdmitByteCountsMultibyteText(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"ascii", strings.Repeat("a", 30)},
		{"cjk", strings.Repeat("会議", 10)},           // 2 runes * 3 bytes = 6 bytes per repeat, 10x = 60 bytes
		{"emoji", strings.Repeat("\U0001F600", 10)}, // 4 bytes per rune, 10x = 40 bytes
		{"code and punctuation", strings.Repeat("if (x) { y(); } // ", 3)},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			input := ExecutionInput{
				Template:  model.Template{Sections: []model.TemplateSection{{Heading: "H", Instruction: "I"}}},
				Documents: []Document{{NoteID: "note-1", Segments: []model.Segment{{StartMS: 0, Text: "x"}}}},
			}
			prepared, err := PrepareDocuments(input)
			if err != nil {
				t.Fatalf("PrepareDocuments: %v", err)
			}
			prepared.SectionPrompts = []SectionPrompt{{Heading: "H", Prompt: tc.text}}
			wantBytes := len([]byte(tc.text))

			// Admits exactly at wantBytes.
			cfg := AdmissionConfig{ContextTokens: wantBytes, ByteFallbackTokenizer: true}
			if err := Admit(prepared, cfg); err != nil {
				t.Fatalf("expected admission at exact byte length %d to succeed, got %v", wantBytes, err)
			}
			// Rejects at wantBytes-1.
			if wantBytes > 0 {
				cfgTight := AdmissionConfig{ContextTokens: wantBytes - 1, ByteFallbackTokenizer: true}
				if err := Admit(prepared, cfgTight); err != ErrContextBudget {
					t.Fatalf("expected rejection one byte under budget, got %v", err)
				}
			}
		})
	}
}

// TestAdmitInvalidUTF8Replacement proves invalid UTF-8 bytes in a prompt are
// still counted at their raw byte length (Go strings are byte sequences;
// len([]byte(s)) never depends on validity), rather than panicking or
// silently under/over-counting via rune decoding.
func TestAdmitInvalidUTF8Replacement(t *testing.T) {
	invalid := "abc\xff\xfe" + strings.Repeat("z", 10)
	wantBytes := len([]byte(invalid))
	input := ExecutionInput{
		Template:  model.Template{Sections: []model.TemplateSection{{Heading: "H", Instruction: "I"}}},
		Documents: []Document{{NoteID: "note-1", Segments: []model.Segment{{StartMS: 0, Text: "x"}}}},
	}
	prepared, err := PrepareDocuments(input)
	if err != nil {
		t.Fatalf("PrepareDocuments: %v", err)
	}
	prepared.SectionPrompts = []SectionPrompt{{Heading: "H", Prompt: invalid}}
	if err := Admit(prepared, AdmissionConfig{ContextTokens: wantBytes, ByteFallbackTokenizer: true}); err != nil {
		t.Fatalf("expected admission at exact raw byte length %d to succeed, got %v", wantBytes, err)
	}
	if err := Admit(prepared, AdmissionConfig{ContextTokens: wantBytes - 1, ByteFallbackTokenizer: true}); err != ErrContextBudget {
		t.Fatalf("expected rejection one byte under budget, got %v", err)
	}
}
