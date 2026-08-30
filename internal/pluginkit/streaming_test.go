package pluginkit

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testStreamingConfig() StreamingConfig {
	cfg := DefaultStreamingConfig()
	cfg.SampleRate = 1_000
	cfg.MaxWindow = 100 * time.Millisecond
	cfg.PartialInterval = 30 * time.Millisecond
	cfg.SilenceDuration = 20 * time.Millisecond
	// Disabled by default so the existing immediate-silence-finalizes tests
	// below keep their original, tightly timed expectations. Tests that
	// exercise the hysteresis tolerance set it explicitly.
	cfg.SilenceHysteresis = 0
	// Likewise disabled: these tests feed deliberately sized frames (10, 15,
	// 30, 70, 150 samples) and assert on their exact timing, so they pin the
	// caller-framed behavior rather than the reframed default.
	cfg.VADFrame = 0
	return cfg
}

func pcm(n int, value float32) []float32 {
	frame := make([]float32, n)
	for i := range frame {
		frame[i] = value
	}
	return frame
}

func TestStreamingSessionEmitsPartialAtCadence(t *testing.T) {
	cfg := testStreamingConfig()
	segments := make(chan StreamingSegment, 1)
	session, err := NewStreamingSession(cfg, nil, func(samples []float32) (string, error) {
		return fmt.Sprintf("samples:%d", len(samples)), nil
	}, func(segment StreamingSegment, err error) {
		if err != nil {
			t.Errorf("emit error: %v", err)
		}
		segments <- segment
	})
	if err != nil {
		t.Fatal(err)
	}

	for range 3 {
		session.Feed(pcm(10, 0.5))
	}
	segment := receiveSegment(t, segments)
	if segment.Final || segment.Text != "samples:30" || segment.StartMS != 0 || segment.EndMS != 30 {
		t.Fatalf("unexpected partial: %+v", segment)
	}
	session.Wait()
}

func TestStreamingSessionFinalizesOnceAndResets(t *testing.T) {
	cfg := testStreamingConfig()
	cfg.PartialInterval = time.Second
	var mu sync.Mutex
	var callSizes []int
	segments := make(chan StreamingSegment, 2)
	session, err := NewStreamingSession(cfg, nil, func(samples []float32) (string, error) {
		mu.Lock()
		callSizes = append(callSizes, len(samples))
		mu.Unlock()
		return fmt.Sprintf("samples:%d", len(samples)), nil
	}, func(segment StreamingSegment, err error) { segments <- segment })
	if err != nil {
		t.Fatal(err)
	}

	session.Feed(pcm(10, 0.5))
	session.Feed(pcm(10, 0))
	session.Feed(pcm(10, 0))
	session.Feed(pcm(30, 0)) // additional silence must not finalize again
	session.Wait()
	first := receiveSegment(t, segments)
	if !first.Final || first.Text != "samples:10" {
		t.Fatalf("unexpected first final: %+v", first)
	}

	session.Feed(pcm(15, 0.7))
	session.Feed(pcm(20, 0))
	session.Wait()
	second := receiveSegment(t, segments)
	if !second.Final || second.Text != "samples:15" {
		t.Fatalf("second utterance retained old audio: %+v", second)
	}
	select {
	case extra := <-segments:
		t.Fatalf("unexpected extra final: %+v", extra)
	default:
	}
	mu.Lock()
	defer mu.Unlock()
	if fmt.Sprint(callSizes) != "[10 15]" {
		t.Fatalf("transcribe call sizes = %v", callSizes)
	}
}

func TestStreamingSessionBoundsContinuousSpeechWindow(t *testing.T) {
	cfg := testStreamingConfig()
	cfg.MaxWindow = 30 * time.Millisecond
	cfg.PartialInterval = 40 * time.Millisecond
	var maxSeen atomic.Int64
	session, err := NewStreamingSession(cfg, nil, func(samples []float32) (string, error) {
		for {
			old := maxSeen.Load()
			if int64(len(samples)) <= old || maxSeen.CompareAndSwap(old, int64(len(samples))) {
				break
			}
		}
		return "bounded", nil
	}, func(StreamingSegment, error) {})
	if err != nil {
		t.Fatal(err)
	}
	for range 12 {
		session.Feed(pcm(10, 0.5))
		session.Wait()
	}
	session.Feed(pcm(20, 0))
	session.Wait()
	if got := maxSeen.Load(); got == 0 || got > 30 {
		t.Fatalf("largest transcription window = %d samples, want 1..30", got)
	}
}

func TestStreamingContinuousUtteranceExceedingMaxWindow(t *testing.T) {
	cfg := testStreamingConfig()
	cfg.MaxWindow = 30 * time.Millisecond
	cfg.PartialInterval = time.Second
	segments := make(chan StreamingSegment, 1)
	session, err := NewStreamingSession(cfg, nil, func(samples []float32) (string, error) {
		return fmt.Sprintf("samples:%d", len(samples)), nil
	}, func(segment StreamingSegment, err error) { segments <- segment })
	if err != nil {
		t.Fatal(err)
	}

	// This documents the current sliding-window behavior pending #711's
	// max-utterance commit-policy fix.
	for range 5 {
		session.Feed(pcm(10, 0.5))
	}
	select {
	case segment := <-segments:
		t.Fatalf("continuous speech unexpectedly emitted a segment: %+v", segment)
	default:
	}
	session.Feed(pcm(20, 0))
	session.Wait()
	segment := receiveSegment(t, segments)
	if !segment.Final || segment.Text != "samples:30" || segment.StartMS != 20 || segment.EndMS != 50 {
		t.Fatalf("unexpected final after continuous speech: %+v", segment)
	}
}

func TestStreamingSessionDropsTriggersDuringTranscription(t *testing.T) {
	cfg := testStreamingConfig()
	cfg.PartialInterval = 10 * time.Millisecond
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var calls atomic.Int64
	session, err := NewStreamingSession(cfg, nil, func([]float32) (string, error) {
		calls.Add(1)
		started <- struct{}{}
		<-release
		return "slow", nil
	}, func(StreamingSegment, error) {})
	if err != nil {
		t.Fatal(err)
	}

	session.Feed(pcm(10, 0.5))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("transcription did not start")
	}
	for range 5 {
		session.Feed(pcm(10, 0.5))
	}
	session.Feed(pcm(20, 0)) // the final trigger is also dropped, not deferred
	// Give any incorrectly launched callbacks an opportunity to enter the fake.
	// Without the in-flight guard, each trigger above increments calls here.
	for range 100 {
		runtime.Gosched()
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls while first callback blocked = %d, want 1", got)
	}
	close(release)
	session.Wait()
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls after release = %d, want 1 (triggers must not queue)", got)
	}
}

func TestStreamingSessionsDoNotCrossContaminate(t *testing.T) {
	cfg := testStreamingConfig()
	cfg.PartialInterval = time.Second
	outputs := [2]chan StreamingSegment{make(chan StreamingSegment, 1), make(chan StreamingSegment, 1)}
	sessions := make([]*StreamingSession, 2)
	for i := range sessions {
		index := i
		var err error
		sessions[i], err = NewStreamingSession(cfg, nil, func(samples []float32) (string, error) {
			return fmt.Sprintf("session-%d/value-%.1f", index, samples[0]), nil
		}, func(segment StreamingSegment, err error) { outputs[index] <- segment })
		if err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	for i, session := range sessions {
		wg.Add(1)
		go func(index int, session *StreamingSession) {
			defer wg.Done()
			session.Feed(pcm(10+index, float32(index+1)/10))
			session.Feed(pcm(20, 0))
			session.Wait()
		}(i, session)
	}
	wg.Wait()
	for i := range sessions {
		segment := receiveSegment(t, outputs[i])
		want := fmt.Sprintf("session-%d/value-0.%d", i, i+1)
		if segment.Text != want || !segment.Final {
			t.Fatalf("session %d output = %+v, want text %q final", i, segment, want)
		}
	}
}

// TestStreamingSessionTolerateSpeechDipsBeforeFinalizing simulates realistic
// continuous speech: an amplitude envelope that dips below the fixed energy
// threshold (a natural pause, a breath, a quieter word) partway through an
// utterance, then resumes at full volume, before eventually trailing off
// into genuine silence. The dip alone (80ms) outlasts SilenceDuration (70ms)
// but not SilenceHysteresis+SilenceDuration (30ms+70ms=100ms), so it must be
// tolerated as part of the same utterance rather than false-finalizing it.
// Before this hysteresis tolerance existed, a fixed single-frame RMS
// threshold would finalize as soon as continuous low-energy audio reached
// SilenceDuration -- well inside this dip and long before cumulative speech
// reached PartialInterval, so no partial (final:false) event was ever
// reachable in practice. This asserts the corrected ordering: a partial
// fires from the tolerated, still-continuous utterance, and only genuine
// trailing silence afterward produces a final.
func TestStreamingSessionTolerateSpeechDipsBeforeFinalizing(t *testing.T) {
	cfg := StreamingConfig{
		SampleRate:        1_000,
		MaxWindow:         5 * time.Second,
		PartialInterval:   150 * time.Millisecond,
		SilenceDuration:   70 * time.Millisecond,
		EnergyThreshold:   0.1,
		SilenceHysteresis: 30 * time.Millisecond,
	}
	segments := make(chan StreamingSegment, 2)
	session, err := NewStreamingSession(cfg, nil, func(samples []float32) (string, error) {
		return fmt.Sprintf("samples:%d", len(samples)), nil
	}, func(segment StreamingSegment, err error) {
		if err != nil {
			t.Errorf("emit error: %v", err)
		}
		segments <- segment
	})
	if err != nil {
		t.Fatal(err)
	}

	// Loud speech (30ms), then a below-threshold dip delivered as four 20ms
	// frames (matching how real audio arrives in small chunks): 80ms total,
	// longer than SilenceDuration (70ms) alone -- which is exactly what
	// would have false-finalized the utterance before this fix, since old,
	// hysteresis-free code would have accumulated 70ms of continuous
	// "silence" partway through the fourth dip frame -- but shorter than
	// SilenceHysteresis+SilenceDuration (30ms+70ms=100ms), so the fix must
	// tolerate it. Loud speech then resumes and continues until
	// PartialInterval (150ms) is reached counting only the loud stretches
	// plus the dip's first hysteresis-tolerated 20ms.
	session.Feed(pcm(30, 0.5))
	session.Feed(pcm(20, 0.02))
	session.Feed(pcm(20, 0.02))
	session.Feed(pcm(20, 0.02))
	session.Feed(pcm(20, 0.02))
	session.Feed(pcm(30, 0.5))
	session.Feed(pcm(70, 0.5))
	session.Wait()

	partial := receiveSegment(t, segments)
	if partial.Final {
		t.Fatalf("expected a partial (final:false) before any final, got %+v", partial)
	}
	if partial.Text == "" {
		t.Fatalf("partial segment had no text: %+v", partial)
	}

	// Genuine trailing silence: longer than SilenceHysteresis+SilenceDuration.
	session.Feed(pcm(150, 0))
	session.Wait()

	final := receiveSegment(t, segments)
	if !final.Final {
		t.Fatalf("expected a final after genuine trailing silence, got %+v", final)
	}

	select {
	case extra := <-segments:
		t.Fatalf("unexpected extra segment: %+v", extra)
	default:
	}
}

func receiveSegment(t *testing.T, segments <-chan StreamingSegment) StreamingSegment {
	t.Helper()
	select {
	case segment := <-segments:
		return segment
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for segment")
		return StreamingSegment{}
	}
}
