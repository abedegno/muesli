//go:build !whisper_cgo

package live

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
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

// buildBimodalPCM returns alternating bursts of moderate-amplitude "speech"
// and digital-silence "quiet" audio, repeated for `cycles` seconds. The
// bimodal shape -- rather than one constant amplitude -- is what lets the
// adaptive detector tell speech from noise at all: its noise-referenced guard
// and speech-referenced estimate would coincide on a single constant level,
// pinning the threshold near that level instead of below it. Whisper-cpp's
// non-CGO stub always returns non-empty text regardless of the samples it is
// given, so any triggered transcription is a usable, cheap proxy for "the
// detector classified enough of this as speech".
func buildBimodalPCM(sampleRate int, speechAmp float32, burst time.Duration, cycles int) []float32 {
	burstSamples := streamingSamples(burst, sampleRate)
	pcm := make([]float32, 0, cycles*2*burstSamples)
	for range cycles {
		for range burstSamples {
			pcm = append(pcm, speechAmp)
		}
		for range burstSamples {
			pcm = append(pcm, 0)
		}
	}
	return pcm
}

func startLiveStream(t *testing.T, eng *Engine, sampleRate int, mode string, threshold float64) pluginkit.StreamingEngineSession {
	t.Helper()
	cfg, err := json.Marshal(map[string]any{"vad": mode, "vad_threshold": threshold})
	if err != nil {
		t.Fatalf("marshal session config: %v", err)
	}
	session, err := eng.StartStream(context.Background(), pluginkit.StreamingStartRequest{
		Type: "start", SampleRate: sampleRate, Channels: 1, Config: cfg,
	})
	if err != nil {
		t.Fatalf("start %s stream at threshold %v: %v", mode, threshold, err)
	}
	return session
}

func hasNonEmptySegment(events []pluginkit.StreamingEvent) bool {
	for _, e := range events {
		if e.Type == "segment" && strings.TrimSpace(e.Text) != "" {
			return true
		}
	}
	return false
}

// TestStartStreamUsesSelectedVADBehavior starts a fixed and an adaptive
// session through the real, non-CGO live.Engine.StartStream with the same
// high fallback threshold, and feeds both identical bounded synthetic audio
// whose speech amplitude sits well below that threshold. A fixed detector can
// never see through a threshold set above the speech itself, so it must never
// emit; an adaptive one is expected to re-estimate its operating threshold
// from the audio and eventually cross PartialInterval's worth of correctly
// classified speech. Only the returned pluginkit.StreamingEvents are
// inspected -- no internal session or detector type is touched -- so this
// pins observable behavior rather than implementation.
func TestStartStreamUsesSelectedVADBehavior(t *testing.T) {
	eng := New(engine.Config{Model: "tiny.en", Language: "en"})
	if err := eng.whisper.EnsureReady(context.Background()); err != nil {
		t.Fatalf("prepare model: %v", err)
	}
	const sampleRate = 16_000
	const fallback = 0.5  // high: above the synthetic speech amplitude below
	const speechAmp = 0.2 // moderate: below fallback, comfortably above digital silence
	pcm := buildBimodalPCM(sampleRate, speechAmp, 500*time.Millisecond, 100)

	fixed := startLiveStream(t, eng, sampleRate, VADFixed, fallback)
	fixedEvents, err := fixed.WriteAudio(context.Background(), pcm)
	if err != nil {
		t.Fatalf("fixed write audio: %v", err)
	}
	if closeEvents, err := fixed.Close(context.Background()); err != nil {
		t.Fatalf("fixed close: %v", err)
	} else {
		fixedEvents = append(fixedEvents, closeEvents...)
	}
	for _, e := range fixedEvents {
		if e.Type == "segment" {
			t.Fatalf("fixed mode must never classify %.2f-amplitude audio as speech against a %.2f threshold, got %#v", speechAmp, fallback, fixedEvents)
		}
	}

	adaptive := startLiveStream(t, eng, sampleRate, VADAdaptive, fallback)
	adaptiveEvents, err := adaptive.WriteAudio(context.Background(), pcm)
	if err != nil {
		t.Fatalf("adaptive write audio: %v", err)
	}
	emitted := hasNonEmptySegment(adaptiveEvents)
	if closeEvents, err := adaptive.Close(context.Background()); err != nil {
		t.Fatalf("adaptive close: %v", err)
	} else if !emitted {
		emitted = hasNonEmptySegment(closeEvents)
	}
	if !emitted {
		t.Fatalf("adaptive mode should have re-estimated its threshold below %.2f and emitted a segment, got none", speechAmp)
	}
}

// TestStartStreamIsolatesConcurrentVADState guards two independent isolation
// properties of a shared live.Engine across concurrent sessions: that an
// adaptive detector's learned state belongs to one session and is never
// shared with (or raced by) another, and that each session's own fixed
// threshold is never overwritten by a sibling session started at the same
// time.
func TestStartStreamIsolatesConcurrentVADState(t *testing.T) {
	eng := New(engine.Config{Model: "tiny.en", Language: "en"})
	if err := eng.whisper.EnsureReady(context.Background()); err != nil {
		t.Fatalf("prepare model: %v", err)
	}
	const sampleRate = 16_000
	const fallback = 0.5
	const speechAmp = 0.2

	t.Run("adaptive detectors are not shared between sessions", func(t *testing.T) {
		a := startLiveStream(t, eng, sampleRate, VADAdaptive, fallback)
		defer func() { _, _ = a.Close(context.Background()) }()
		b := startLiveStream(t, eng, sampleRate, VADAdaptive, fallback)
		defer func() { _, _ = b.Close(context.Background()) }()

		// Precondition A only: enough bimodal audio for its threshold to fall
		// below speechAmp and fire at least one segment, then drain those
		// events. B is started but never fed anything, so it is still inside
		// its own warm-up window on the shared fallback threshold.
		training := buildBimodalPCM(sampleRate, speechAmp, 500*time.Millisecond, 100)
		preconditionEvents, err := a.WriteAudio(context.Background(), training)
		if err != nil {
			t.Fatalf("precondition A: %v", err)
		}
		if !hasNonEmptySegment(preconditionEvents) {
			t.Fatal("preconditioning A did not adapt and emit; test assumptions do not hold")
		}

		probe := make([]float32, streamingSamples(2*time.Second, sampleRate))
		for i := range probe {
			probe[i] = speechAmp
		}

		var barrier sync.WaitGroup
		barrier.Add(1)
		var wg sync.WaitGroup
		var aEvents, bEvents []pluginkit.StreamingEvent
		var aErr, bErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			barrier.Wait()
			aEvents, aErr = a.WriteAudio(context.Background(), probe)
		}()
		go func() {
			defer wg.Done()
			barrier.Wait()
			bEvents, bErr = b.WriteAudio(context.Background(), probe)
		}()
		barrier.Done()
		wg.Wait()

		if aErr != nil {
			t.Fatalf("A write audio: %v", aErr)
		}
		if bErr != nil {
			t.Fatalf("B write audio: %v", bErr)
		}
		if !hasNonEmptySegment(aEvents) {
			t.Errorf("A should still reflect its own learned threshold and emit on more of the same audio, got %#v", aEvents)
		}
		for _, e := range bEvents {
			if e.Type == "segment" {
				t.Errorf("B is still warming up on the shared fallback threshold and must not emit, got %#v", bEvents)
			}
		}
	})

	t.Run("fixed sessions do not share a threshold", func(t *testing.T) {
		const lowThreshold = 0.05 // below speechAmp: must detect it
		const highThreshold = 0.5 // above speechAmp: must not detect it
		signal := make([]float32, streamingSamples(2*time.Second, sampleRate))
		for i := range signal {
			signal[i] = speechAmp
		}

		var barrier sync.WaitGroup
		barrier.Add(1)
		var wg sync.WaitGroup
		var lowEvents, highEvents []pluginkit.StreamingEvent
		var lowErr, highErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			barrier.Wait()
			session := startLiveStream(t, eng, sampleRate, VADFixed, lowThreshold)
			defer func() { _, _ = session.Close(context.Background()) }()
			lowEvents, lowErr = session.WriteAudio(context.Background(), signal)
		}()
		go func() {
			defer wg.Done()
			barrier.Wait()
			session := startLiveStream(t, eng, sampleRate, VADFixed, highThreshold)
			defer func() { _, _ = session.Close(context.Background()) }()
			highEvents, highErr = session.WriteAudio(context.Background(), signal)
		}()
		barrier.Done()
		wg.Wait()

		if lowErr != nil {
			t.Fatalf("low-threshold start/write: %v", lowErr)
		}
		if highErr != nil {
			t.Fatalf("high-threshold start/write: %v", highErr)
		}
		if !hasNonEmptySegment(lowEvents) {
			t.Errorf("threshold %v is below the %v signal and should have emitted, got %#v", lowThreshold, speechAmp, lowEvents)
		}
		for _, e := range highEvents {
			if e.Type == "segment" {
				t.Errorf("threshold %v is above the %v signal and must not emit -- a shared/overwritten config would explain this, got %#v", highThreshold, speechAmp, highEvents)
			}
		}
	})
}
