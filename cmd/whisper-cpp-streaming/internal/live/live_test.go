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

// streamingSamples converts a duration to a sample count at the given
// sample rate, matching pluginkit's internal duration/sample conversion.
func streamingSamples(d time.Duration, sampleRate int) int {
	return int(int64(d) * int64(sampleRate) / int64(time.Second))
}

func TestSessionProducesPartialAndFinal(t *testing.T) {
	eng := New(engine.Config{Model: "tiny.en", Language: "en"})
	if err := eng.whisper.EnsureReady(context.Background()); err != nil {
		t.Fatalf("prepare model: %v", err)
	}
	const sampleRate = 16_000
	req := pluginkit.StreamingStartRequest{Type: "start", SampleRate: sampleRate, Channels: 1}
	session, err := eng.StartStream(context.Background(), req)
	if err != nil {
		t.Fatalf("start stream: %v", err)
	}
	// StartStream configures the session with pluginkit.DefaultStreamingConfig;
	// derive this test's synthetic audio timing from those same fields so it
	// cannot silently drift out of sync with the config as it evolves.
	cfg := pluginkit.DefaultStreamingConfig()

	speech := make([]float32, streamingSamples(cfg.PartialInterval, sampleRate))
	for i := range speech {
		speech[i] = .25
	}
	events, err := session.WriteAudio(context.Background(), speech)
	if err != nil || len(events) != 1 || events[0].Final {
		t.Fatalf("partial events = %#v, err = %v", events, err)
	}
	// SilenceHysteresis tolerates a run of below-threshold audio before it
	// starts counting toward SilenceDuration, so genuine trailing silence
	// must span more than their sum to finalize; add a healthy margin so
	// this isn't balanced on the exact boundary.
	trailingSilence := streamingSamples(cfg.SilenceHysteresis+cfg.SilenceDuration+300*time.Millisecond, sampleRate)
	events, err = session.WriteAudio(context.Background(), make([]float32, trailingSilence))
	if err != nil || len(events) != 1 || !events[0].Final || events[0].Text == "" {
		t.Fatalf("final events = %#v, err = %v", events, err)
	}
}

// TestSessionCloseFlushesTrailingUtterance pins the fix for muesli#711's
// first loss path: session.Close used to only call stream.Wait, which blocks
// on transcription work already started but never starts one for an
// utterance that is still open when the stream ends. Speech shorter than
// PartialInterval never reaches a partial trigger, and with no trailing
// silence it never reaches the SilenceDuration finalize clock either, so
// nothing but an explicit end-of-stream flush can ever produce a final for
// it. Close must now force that flush (via stream.Finish) and return the
// resulting event instead of the trailing utterance simply vanishing.
func TestSessionCloseFlushesTrailingUtterance(t *testing.T) {
	eng := New(engine.Config{Model: "tiny.en", Language: "en"})
	if err := eng.whisper.EnsureReady(context.Background()); err != nil {
		t.Fatalf("prepare model: %v", err)
	}
	const sampleRate = 16_000
	req := pluginkit.StreamingStartRequest{Type: "start", SampleRate: sampleRate, Channels: 1}
	session, err := eng.StartStream(context.Background(), req)
	if err != nil {
		t.Fatalf("start stream: %v", err)
	}
	cfg := pluginkit.DefaultStreamingConfig()

	// Well short of PartialInterval, so WriteAudio produces nothing yet.
	speech := make([]float32, streamingSamples(cfg.PartialInterval, sampleRate)/3)
	for i := range speech {
		speech[i] = .25
	}
	events, err := session.WriteAudio(context.Background(), speech)
	if err != nil {
		t.Fatalf("write audio: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("events before close = %#v, want none yet", events)
	}

	events, err = session.Close(context.Background())
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if len(events) != 1 || !events[0].Final || events[0].Text == "" {
		t.Fatalf("close events = %#v, want exactly one non-empty final", events)
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
