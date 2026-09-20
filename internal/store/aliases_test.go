package store

import (
	"reflect"
	"testing"

	"github.com/abedegno/muesli/internal/model"
)

// TestApplyAliasesNonMutating proves ApplyAliases never mutates its input
// segments slice, and is copy-on-write: no aliases in play returns the exact
// same slice (no allocation), while an applicable alias returns a new one.
func TestApplyAliasesNonMutating(t *testing.T) {
	segments := []model.Segment{
		{Speaker: "SPEAKER_00", Text: "hello"},
		{Speaker: "SPEAKER_01", Text: "hi"},
	}
	original := make([]model.Segment, len(segments))
	copy(original, segments)

	t.Run("no aliases returns identical slice", func(t *testing.T) {
		got, applied := ApplyAliases(segments, nil)
		if &got[0] != &segments[0] {
			t.Fatal("expected the exact same underlying slice when no aliases apply")
		}
		if applied != nil {
			t.Fatalf("expected nil applied map, got %+v", applied)
		}
	})

	t.Run("no matching alias returns identical slice", func(t *testing.T) {
		got, applied := ApplyAliases(segments, map[string]string{"SPEAKER_99": "Nobody"})
		if &got[0] != &segments[0] {
			t.Fatal("expected the exact same underlying slice when no alias matches a present speaker")
		}
		if applied != nil {
			t.Fatalf("expected nil applied map, got %+v", applied)
		}
	})

	t.Run("applicable alias substitutes without mutating input", func(t *testing.T) {
		got, applied := ApplyAliases(segments, map[string]string{"SPEAKER_00": "Alice"})
		if got[0].Speaker != "Alice" || got[1].Speaker != "SPEAKER_01" {
			t.Fatalf("unexpected substitution: %+v", got)
		}
		if applied["SPEAKER_00"] != "Alice" {
			t.Fatalf("unexpected applied map: %+v", applied)
		}
		if !reflect.DeepEqual(segments, original) {
			t.Fatalf("input segments mutated: got %+v, want %+v", segments, original)
		}
	})
}

func TestAliasPrefaceDeterministicOrder(t *testing.T) {
	applied := map[string]string{
		"SPEAKER_01": "Bob",
		"SPEAKER_00": "Alice",
	}
	want := "Speakers: SPEAKER_00 -> Alice, SPEAKER_01 -> Bob\n" + speakerAliasDirective
	if got := AliasPreface(applied); got != want {
		t.Fatalf("AliasPreface = %q, want %q", got, want)
	}
}
