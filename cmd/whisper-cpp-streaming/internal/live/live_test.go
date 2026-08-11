//go:build !whisper_cgo

package live

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/pluginkit"
	"github.com/abedegno/muesli/internal/whispercpp/engine"
)

func TestSessionProducesPartialAndFinal(t *testing.T) {
	eng := New(engine.Config{Model: "tiny.en", Language: "en"})
	if err := eng.whisper.EnsureReady(context.Background()); err != nil {
		t.Fatalf("prepare model: %v", err)
	}
	req := pluginkit.StreamingStartRequest{Type: "start", SampleRate: 16_000, Channels: 1}
	session, err := eng.StartStream(context.Background(), req)
	if err != nil {
		t.Fatalf("start stream: %v", err)
	}
	speech := make([]float32, 24_000)
	for i := range speech {
		speech[i] = .25
	}
	events, err := session.WriteAudio(context.Background(), speech)
	if err != nil || len(events) != 1 || events[0].Final {
		t.Fatalf("partial events = %#v, err = %v", events, err)
	}
	// The default config now tolerates SilenceHysteresis (300ms) of
	// below-threshold audio before it starts counting toward
	// SilenceDuration (700ms), so genuine trailing silence must span more
	// than their sum (here 20,000 samples = 1.25s at 16kHz) to finalize.
	events, err = session.WriteAudio(context.Background(), make([]float32, 20_000))
	if err != nil || len(events) != 1 || !events[0].Final || events[0].Text == "" {
		t.Fatalf("final events = %#v, err = %v", events, err)
	}
}

func TestJoinTranscriptionSegmentsDropsWhisperSilenceTokens(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{name: "blank audio", text: "[BLANK_AUDIO]"},
		{name: "silence", text: "[SILENCE]"},
		{name: "parenthesized", text: "(blank_audio)"},
		{name: "inner and outer whitespace", text: " \t[ Silence ]\n"},
		{name: "case insensitive", text: "[bLaNk-AuDiO]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := joinTranscriptionSegments([]model.Segment{{Text: tt.text}})
			if got != "" {
				t.Fatalf("joinTranscriptionSegments(%q) = %q, want empty", tt.text, got)
			}
		})
	}
}

func TestJoinTranscriptionSegmentsPreservesSpeechContainingBrackets(t *testing.T) {
	const speech = "he said [pause] then left"
	got := joinTranscriptionSegments([]model.Segment{{Text: speech}})
	if got != speech {
		t.Fatalf("joinTranscriptionSegments() = %q, want %q", got, speech)
	}
}

func TestControlOnlyWindowDoesNotEmitAfterPriorSegment(t *testing.T) {
	cfg := pluginkit.StreamingConfig{
		SampleRate:      10,
		MaxWindow:       time.Second,
		PartialInterval: time.Second,
		SilenceDuration: 100 * time.Millisecond,
		EnergyThreshold: 0.01,
	}
	transcriptions := []string{"first spoken segment", joinTranscriptionSegments([]model.Segment{{Text: "[BLANK_AUDIO]"}})}
	var events []pluginkit.StreamingSegment
	stream, err := pluginkit.NewStreamingSession(cfg, nil, func([]float32) (string, error) {
		text := transcriptions[0]
		transcriptions = transcriptions[1:]
		return text, nil
	}, func(segment pluginkit.StreamingSegment, err error) {
		if err != nil {
			t.Errorf("unexpected streaming error: %v", err)
			return
		}
		events = append(events, segment)
	})
	if err != nil {
		t.Fatalf("NewStreamingSession: %v", err)
	}

	feedUtterance := func() {
		stream.Feed([]float32{0.25})
		stream.Feed([]float32{0})
		stream.Wait()
	}
	feedUtterance()
	want := []pluginkit.StreamingSegment{{StartMS: 0, EndMS: 100, Text: "first spoken segment", Final: true}}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events after speech = %#v, want %#v", events, want)
	}
	feedUtterance()
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events after control-only window = %#v, want prior events untouched: %#v", events, want)
	}
}
