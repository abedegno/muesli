package pluginkit

import (
	"fmt"
	"reflect"
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

// TestStreamingSessionCommitsOnMaxWindowInsteadOfEvicting pins the fix for
// muesli#711's second loss path: a continuous talker who never pauses long
// enough to hit the silence finalize clock used to have the window silently
// slide once it passed MaxWindow, evicting the oldest audio -- everything
// before the trailing MaxWindow of a long utterance was lost, and only ever
// the tail got transcribed. Hitting MaxWindow must instead commit whatever
// has accumulated as a final segment and keep accumulating a fresh window for
// the rest of the same (still active) utterance, so a continuous talker's
// audio is fully covered by a sequence of finals instead of truncated to one.
func TestStreamingSessionCommitsOnMaxWindowInsteadOfEvicting(t *testing.T) {
	cfg := testStreamingConfig()
	cfg.MaxWindow = 30 * time.Millisecond
	cfg.PartialInterval = 40 * time.Millisecond // keep partials out of the way
	var mu sync.Mutex
	var finals []StreamingSegment
	session, err := NewStreamingSession(cfg, nil, func(samples []float32) (string, error) {
		return fmt.Sprintf("samples:%d", len(samples)), nil
	}, func(segment StreamingSegment, err error) {
		if err != nil {
			t.Errorf("emit error: %v", err)
			return
		}
		mu.Lock()
		finals = append(finals, segment)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}

	// 120 samples of continuous speech, four times the 30-sample MaxWindow,
	// fed and drained one 10-sample frame at a time so each max-window commit
	// completes before the next frame arrives.
	for range 12 {
		session.Feed(pcm(10, 0.5))
		session.Wait()
	}
	session.Wait()

	mu.Lock()
	defer mu.Unlock()
	want := []StreamingSegment{
		{StartMS: 0, EndMS: 40, Text: "samples:40", Final: true},
		{StartMS: 40, EndMS: 80, Text: "samples:40", Final: true},
		{StartMS: 80, EndMS: 120, Text: "samples:40", Final: true},
	}
	if !reflect.DeepEqual(finals, want) {
		t.Fatalf("finals = %+v, want %+v (every sample committed, none evicted)", finals, want)
	}
}

// TestStreamingSessionDropsPartialsButQueuesFinalDuringTranscription covers
// two invariants that must both hold while a transcription is in flight.
// First, the one that must never change: a transcription cannot be
// re-entered concurrently, so calls stays at 1 for as long as the first
// callback is blocked, no matter how many more triggers arrive. Second, the
// one muesli#711 fixes: of those triggers, the partial (cadence) ones are
// still dropped -- they're just a preview refresh, and the same audio is
// still sitting in the window for the next trigger to pick up -- but the
// final (commit) trigger is queued rather than dropped, so releasing the
// first callback lets it run and calls reaches 2. Before this fix every
// trigger reached while busy was dropped outright, so the final for this
// utterance would have disappeared even after the callback was released.
func TestStreamingSessionDropsPartialsButQueuesFinalDuringTranscription(t *testing.T) {
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
	session.Feed(pcm(20, 0)) // the final trigger; queued, not dropped
	// Give any incorrectly launched callbacks an opportunity to enter the fake.
	// Without the in-flight guard, each trigger above increments calls here.
	for range 100 {
		runtime.Gosched()
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls while first callback blocked = %d, want 1 (no concurrent re-entry)", got)
	}
	close(release)
	session.Wait()
	if got := calls.Load(); got != 2 {
		t.Fatalf("calls after release = %d, want 2 (the queued final must still run)", got)
	}
}

// TestStreamingEmitsQueuedFinalsInOrderNotConcurrently is a regression test
// for a race in launchTranscriptionLocked: a completing transcription goroutine
// used to hand off to the next queued commit -- launching its transcription
// goroutine -- BEFORE calling emit for the commit that just finished. That let
// the newly launched goroutine run its own (possibly much faster)
// transcription and call emit before the first goroutine got a chance to,
// reversing the delivery order of two queued finals (muesli#711 review
// finding). The fix moves the handoff to after emit, so the next queued
// commit's transcription cannot even start until the current one has been
// handed to emit. This test pins that: while the first result's emit call is
// deliberately kept blocked, the queued second transcription must not have
// started yet, and once everything unblocks, emit must have been called in
// queue order.
func TestStreamingEmitsQueuedFinalsInOrderNotConcurrently(t *testing.T) {
	cfg := testStreamingConfig()
	cfg.PartialInterval = 10 * time.Millisecond

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	emitPaused := make(chan struct{})
	releaseEmit := make(chan struct{})

	var calls atomic.Int64
	var mu sync.Mutex
	var emitOrder []string

	session, err := NewStreamingSession(cfg, nil, func([]float32) (string, error) {
		if calls.Add(1) == 1 {
			started <- struct{}{}
			<-release
			return "first", nil
		}
		return "second", nil
	}, func(segment StreamingSegment, err error) {
		mu.Lock()
		emitOrder = append(emitOrder, segment.Text)
		mu.Unlock()
		if segment.Text == "first" {
			close(emitPaused)
			<-releaseEmit
		}
	})
	if err != nil {
		t.Fatal(err)
	}

	// Start the first (in-flight) transcription.
	session.Feed(pcm(10, 0.5))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first transcription did not start")
	}

	// Trigger a final while the first is still in flight; this must be
	// queued rather than dropped (muesli#711 defect 3).
	session.Feed(pcm(20, 0)) // silence -> finalize -> queued behind the first

	// Let the first transcription's transcribe() return, so its goroutine
	// reaches the point where it either hands off to the queued commit or
	// (correctly) emits first.
	close(release)

	select {
	case <-emitPaused:
	case <-time.After(time.Second):
		t.Fatal("emit for the first result was not reached")
	}

	// Give a wrongly-launched second transcription goroutine ample real
	// time to run to completion (its fake transcribe does no blocking work
	// at all) before checking. This is only to make catching a regression
	// reliable; it does not weaken the assertion below for correct code,
	// where the handoff is gated on emit(first) returning -- a real
	// synchronization point, not a timing race -- so calls cannot advance
	// here no matter how long we wait.
	time.Sleep(5 * time.Millisecond)

	// While emit for the first result is still blocked inside the handler,
	// the queued second commit's transcription must not have started: the
	// handoff to it may only happen after the first has been emitted.
	if got := calls.Load(); got != 1 {
		t.Fatalf("second transcription started before first was emitted: calls=%d, want 1", got)
	}

	close(releaseEmit)
	session.Wait()

	mu.Lock()
	defer mu.Unlock()
	want := []string{"first", "second"}
	if !reflect.DeepEqual(emitOrder, want) {
		t.Fatalf("emit order = %v, want %v (queued finals must be delivered in order)", emitOrder, want)
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
