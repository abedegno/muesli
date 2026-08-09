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
