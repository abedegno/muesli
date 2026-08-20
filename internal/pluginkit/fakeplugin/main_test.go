package main

import "testing"

func TestKindFromBinaryName(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "agent", path: "/tmp/fakeplugin-agent", want: "agent"},
		{name: "transcriber", path: "/tmp/fakeplugin-transcriber", want: "transcriber"},
		{name: "neither", path: "/tmp/fakeplugin", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := kindFromBinaryName(tt.path); got != tt.want {
				t.Fatalf("kindFromBinaryName(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestScriptedTranscript(t *testing.T) {
	t.Run("set", func(t *testing.T) {
		t.Setenv("MUESLI_FAKE_TRANSCRIPT", "  scripted words  ")
		if got := scriptedTranscript(); got != "scripted words" {
			t.Fatalf("scriptedTranscript() = %q, want %q", got, "scripted words")
		}
	})

	t.Run("unset", func(t *testing.T) {
		t.Setenv("MUESLI_FAKE_TRANSCRIPT", "")
		if got := scriptedTranscript(); got != "hello from fakeplugin" {
			t.Fatalf("scriptedTranscript() = %q, want default", got)
		}
	})
}

func TestScriptedSegments(t *testing.T) {
	t.Run("single line stays one segment", func(t *testing.T) {
		t.Setenv("MUESLI_FAKE_TRANSCRIPT", "just the one line")
		got := scriptedSegments()
		if len(got) != 1 {
			t.Fatalf("scriptedSegments() returned %d segments, want 1", len(got))
		}
		if got[0].Text != "just the one line" {
			t.Fatalf("segment text = %q, want %q", got[0].Text, "just the one line")
		}
		if got[0].StartMS != 0 || got[0].EndMS != segmentMS {
			t.Fatalf("segment timings = %d..%d, want 0..%d", got[0].StartMS, got[0].EndMS, segmentMS)
		}
	})

	t.Run("one segment per non-empty line, timings monotonic", func(t *testing.T) {
		t.Setenv("MUESLI_FAKE_TRANSCRIPT", "first line\n\n  second line  \nthird line\n")
		got := scriptedSegments()
		want := []string{"first line", "second line", "third line"}
		if len(got) != len(want) {
			t.Fatalf("scriptedSegments() returned %d segments, want %d", len(got), len(want))
		}
		for i, segment := range got {
			if segment.Text != want[i] {
				t.Fatalf("segment %d text = %q, want %q", i, segment.Text, want[i])
			}
			if segment.StartMS != i*segmentMS || segment.EndMS != (i+1)*segmentMS {
				t.Fatalf("segment %d timings = %d..%d, want %d..%d",
					i, segment.StartMS, segment.EndMS, i*segmentMS, (i+1)*segmentMS)
			}
			if segment.Source != "mic" {
				t.Fatalf("segment %d source = %q, want %q", i, segment.Source, "mic")
			}
		}
	})

	t.Run("blank script falls back to the default single segment", func(t *testing.T) {
		t.Setenv("MUESLI_FAKE_TRANSCRIPT", "\n \n")
		got := scriptedSegments()
		if len(got) != 1 || got[0].Text != "hello from fakeplugin" {
			t.Fatalf("scriptedSegments() = %+v, want the default single segment", got)
		}
	})
}
