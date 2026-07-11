package worker

import (
	"testing"

	"github.com/abedegno/muesli/internal/model"
)

func seg(speaker, text string) model.Segment {
	return model.Segment{Speaker: speaker, Text: text}
}

func TestApplySpeakerAliases_FullyAliased(t *testing.T) {
	segments := []model.Segment{
		seg("SPEAKER_00", "hello"),
		seg("SPEAKER_01", "hi there"),
		seg("SPEAKER_00", "how are you"),
	}
	aliases := map[string]string{
		"SPEAKER_00": "Elena",
		"SPEAKER_01": "Bob",
	}
	notesMarkdown := "# Meeting notes\n\nSome content."

	outSegments, outNotes := applySpeakerAliases(segments, notesMarkdown, aliases)

	wantSpeakers := []string{"Elena", "Bob", "Elena"}
	if len(outSegments) != len(wantSpeakers) {
		t.Fatalf("got %d segments, want %d", len(outSegments), len(wantSpeakers))
	}
	for i, want := range wantSpeakers {
		if outSegments[i].Speaker != want {
			t.Errorf("segment %d: got speaker %q, want %q", i, outSegments[i].Speaker, want)
		}
		if outSegments[i].Text != segments[i].Text {
			t.Errorf("segment %d: text mutated, got %q want %q", i, outSegments[i].Text, segments[i].Text)
		}
	}

	wantPreface := "Speakers: SPEAKER_00 -> Elena, SPEAKER_01 -> Bob\n" +
		"Use the provided speaker names verbatim; do not infer, abbreviate, or merge names.\n\n" +
		notesMarkdown
	if outNotes != wantPreface {
		t.Errorf("notes_markdown mismatch:\ngot:  %q\nwant: %q", outNotes, wantPreface)
	}

	// Original input must not be mutated.
	if segments[0].Speaker != "SPEAKER_00" || segments[1].Speaker != "SPEAKER_01" {
		t.Errorf("input segments were mutated: %+v", segments)
	}
}

func TestApplySpeakerAliases_PartiallyAliased(t *testing.T) {
	segments := []model.Segment{
		seg("SPEAKER_00", "hello"),
		seg("SPEAKER_01", "hi there"),
		seg("SPEAKER_02", "unaliased speaker"),
	}
	aliases := map[string]string{
		"SPEAKER_01": "Bob",
		// SPEAKER_00 and SPEAKER_02 intentionally have no alias entries here;
		// an alias for a label that never appears in the transcript must also
		// be excluded from the preface.
		"SPEAKER_99": "Ghost",
	}
	notesMarkdown := "notes body"

	outSegments, outNotes := applySpeakerAliases(segments, notesMarkdown, aliases)

	if outSegments[0].Speaker != "SPEAKER_00" {
		t.Errorf("segment 0: got %q, want unchanged SPEAKER_00", outSegments[0].Speaker)
	}
	if outSegments[1].Speaker != "Bob" {
		t.Errorf("segment 1: got %q, want Bob", outSegments[1].Speaker)
	}
	if outSegments[2].Speaker != "SPEAKER_02" {
		t.Errorf("segment 2: got %q, want unchanged SPEAKER_02", outSegments[2].Speaker)
	}

	wantPreface := "Speakers: SPEAKER_01 -> Bob\n" +
		"Use the provided speaker names verbatim; do not infer, abbreviate, or merge names.\n\n" +
		notesMarkdown
	if outNotes != wantPreface {
		t.Errorf("notes_markdown mismatch:\ngot:  %q\nwant: %q", outNotes, wantPreface)
	}
}

func TestApplySpeakerAliases_NoAliasesOrNoSpeakers(t *testing.T) {
	notesMarkdown := "unchanged body"

	t.Run("empty alias map", func(t *testing.T) {
		segments := []model.Segment{
			seg("SPEAKER_00", "hello"),
			seg("SPEAKER_01", "hi there"),
		}
		outSegments, outNotes := applySpeakerAliases(segments, notesMarkdown, map[string]string{})
		if outNotes != notesMarkdown {
			t.Errorf("notes_markdown changed: got %q, want unchanged %q", outNotes, notesMarkdown)
		}
		for i, s := range outSegments {
			if s.Speaker != segments[i].Speaker {
				t.Errorf("segment %d speaker changed: got %q, want %q", i, s.Speaker, segments[i].Speaker)
			}
		}
	})

	t.Run("nil alias map", func(t *testing.T) {
		segments := []model.Segment{seg("SPEAKER_00", "hello")}
		outSegments, outNotes := applySpeakerAliases(segments, notesMarkdown, nil)
		if outNotes != notesMarkdown {
			t.Errorf("notes_markdown changed: got %q, want unchanged %q", outNotes, notesMarkdown)
		}
		if outSegments[0].Speaker != "SPEAKER_00" {
			t.Errorf("segment speaker changed: got %q", outSegments[0].Speaker)
		}
	})

	t.Run("no speakers in transcript", func(t *testing.T) {
		segments := []model.Segment{
			seg("", "no speaker label at all"),
			seg("", "still no speaker"),
		}
		aliases := map[string]string{"SPEAKER_00": "Elena"}
		outSegments, outNotes := applySpeakerAliases(segments, notesMarkdown, aliases)
		if outNotes != notesMarkdown {
			t.Errorf("notes_markdown changed: got %q, want unchanged %q", outNotes, notesMarkdown)
		}
		for i, s := range outSegments {
			if s.Speaker != "" {
				t.Errorf("segment %d speaker changed: got %q, want empty", i, s.Speaker)
			}
		}
	})
}
