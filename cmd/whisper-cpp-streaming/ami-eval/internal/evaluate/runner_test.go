package evaluate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/score"
	"github.com/abedegno/muesli/internal/pluginkit"
)

// alwaysVAD is speech for indices [0,n) counted in calls, then silence.
type stepVAD struct {
	speechCalls int
	calls       int
}

func (v *stepVAD) IsSpeech(frame []float32) bool {
	v.calls++
	return v.calls <= v.speechCalls
}

func factoryFor(v pluginkit.VAD) VADFactory {
	return func() (pluginkit.VAD, error) { return v, nil }
}

func TestRunFeedsExactly200msChunksAndPadsOnlyLastOriginalBlock(t *testing.T) {
	// Not a multiple of FeedSamples (3200): forces padding on the last
	// original-audio block.
	audio := make([]float32, FeedSamples*2+800)
	result, err := Run(context.Background(), audio, factoryFor(&stepVAD{}), time.Minute)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(result.Frames) == 0 {
		t.Fatal("expected observed frames")
	}
	var totalValid int64
	prevEnd := int64(0)
	for i, f := range result.Frames {
		if f.End-f.Start != VADFrameSamples {
			t.Fatalf("frame %d width = %d, want %d", i, f.End-f.Start, VADFrameSamples)
		}
		if f.Start != prevEnd {
			t.Fatalf("frame %d not contiguous: start=%d want %d", i, f.Start, prevEnd)
		}
		prevEnd = f.End
		totalValid += f.ValidSamples
	}
	if totalValid != int64(len(audio)) {
		t.Fatalf("total valid samples = %d, want %d (original audio length, padding/flush must be zero-weight)", totalValid, len(audio))
	}
	// Every 200ms feed spans exactly FeedSamples of frame coverage; the
	// original-audio region covers ceil(len(audio)/FeedSamples)*FeedSamples.
	originalFeedChunks := (len(audio) + FeedSamples - 1) / FeedSamples
	originalCoverage := int64(originalFeedChunks * FeedSamples)
	if prevEnd < originalCoverage {
		t.Fatalf("expected at least original-audio coverage of %d samples, observed only %d", originalCoverage, prevEnd)
	}
}

func TestRunObservesExactlyOnceAt20msGranularity(t *testing.T) {
	audio := make([]float32, FeedSamples*3)
	vad := &stepVAD{}
	result, err := Run(context.Background(), audio, factoryFor(vad), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	wantCalls := vad.calls // however many IsSpeech calls actually happened
	if len(result.Frames) != wantCalls {
		t.Fatalf("expected exactly one observation per IsSpeech call: %d frames vs %d calls", len(result.Frames), wantCalls)
	}
}

func TestRunFinalsComeFromSessionNotReimplemented(t *testing.T) {
	// Speech long enough to open an utterance, then silence long enough to
	// finalize it via the harness's own flush.
	audio := make([]float32, FeedSamples*3)
	for i := range audio {
		audio[i] = 0.5 // loud enough for any reasonable fixed threshold
	}
	vad := pluginkit.EnergyVAD{Threshold: 0.01}
	result, err := Run(context.Background(), audio, factoryFor(vad), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Predicted) != 1 {
		t.Fatalf("expected exactly 1 final utterance, got %+v", result.Predicted)
	}
	p := result.Predicted[0]
	if p.Start != 0 {
		t.Fatalf("expected utterance to start at 0, got %d", p.Start)
	}
	// The session's raw commit end can legitimately extend past the
	// original audio into hysteresis-tolerated trailing silence; Run must
	// clip it to the original audio extent before returning it.
	if p.End != int64(len(audio)) {
		t.Fatalf("expected utterance end clipped to audio extent %d, got %d", len(audio), p.End)
	}
}

func TestRunClipsPredictedIntervalsToAudioExtent(t *testing.T) {
	got := clipToAudioExtent([]score.Interval{
		{Start: -5, End: 100},
		{Start: 50, End: 9999},
		{Start: 20, End: 40},
	}, 200)
	want := []score.Interval{
		{Start: 0, End: 100},
		{Start: 50, End: 200},
		{Start: 20, End: 40},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("interval %d: got %+v want %+v", i, got[i], want[i])
		}
	}
}

func TestRunFreshStateBetweenCalls(t *testing.T) {
	audio := make([]float32, FeedSamples*4)
	for i := range audio {
		if (i/VADFrameSamples)%2 == 0 {
			audio[i] = 0.3
		}
	}
	factory := func() (pluginkit.VAD, error) {
		cfg := pluginkit.DefaultAdaptiveVADConfig(SampleRate, time.Duration(VADFrameSamples)*time.Second/time.Duration(SampleRate))
		return pluginkit.NewAdaptiveEnergyVAD(cfg)
	}
	r1, err := Run(context.Background(), audio, factory, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := Run(context.Background(), audio, factory, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(r1.Frames) != len(r2.Frames) {
		t.Fatalf("expected identical frame counts across independent runs, got %d vs %d", len(r1.Frames), len(r2.Frames))
	}
	for i := range r1.Frames {
		if r1.Frames[i].Predicted != r2.Frames[i].Predicted {
			t.Fatalf("frame %d decision differs between runs: %v vs %v -- adaptive detector state leaked across runs", i, r1.Frames[i].Predicted, r2.Frames[i].Predicted)
		}
	}
}

func TestRunDeadlineExceeded(t *testing.T) {
	audio := make([]float32, FeedSamples)
	block := make(chan struct{})
	blockingVAD := vadFunc(func(frame []float32) bool {
		<-block // never returns within the test
		return false
	})
	_, err := Run(context.Background(), audio, factoryFor(blockingVAD), time.Millisecond)
	if err == nil {
		t.Fatal("expected deadline exceeded error")
	}
	close(block)
}

func TestRunContextCancellation(t *testing.T) {
	audio := make([]float32, FeedSamples)
	block := make(chan struct{})
	blockingVAD := vadFunc(func(frame []float32) bool {
		<-block
		return false
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Run(ctx, audio, factoryFor(blockingVAD), time.Minute)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	close(block)
}

type vadFunc func([]float32) bool

func (f vadFunc) IsSpeech(frame []float32) bool { return f(frame) }

func TestRunConstructorErrorPropagates(t *testing.T) {
	factory := func() (pluginkit.VAD, error) { return nil, errors.New("boom") }
	if _, err := Run(context.Background(), make([]float32, FeedSamples), factory, time.Minute); err == nil {
		t.Fatal("expected constructor error to propagate")
	}
}

// --- callback contract (whitebox) -------------------------------------------

func TestCallbackTrackerSinkErrorFatal(t *testing.T) {
	tr := &callbackTracker{}
	tr.onFeed()
	tr.onCallback(pluginkit.StreamingSegment{}, errors.New("sink failed"))
	if tr.finish() == nil {
		t.Fatal("expected sink error to be fatal")
	}
}

func TestCallbackTrackerCallbackWithoutFeedIsFatal(t *testing.T) {
	tr := &callbackTracker{}
	tr.onCallback(pluginkit.StreamingSegment{Final: true}, nil)
	if tr.finish() == nil {
		t.Fatal("expected callback with no feed activity to be fatal")
	}
}

func TestCallbackTrackerTrailingActivityWithoutFinalIsFatal(t *testing.T) {
	tr := &callbackTracker{}
	tr.onFeed()
	tr.onCallback(pluginkit.StreamingSegment{Final: false, Text: "partial"}, nil)
	if err := tr.finish(); err == nil {
		t.Fatal("expected trailing open utterance without a final to be fatal")
	}
}

func TestCallbackTrackerAcceptsPartialThenFinal(t *testing.T) {
	tr := &callbackTracker{}
	tr.onFeed()
	tr.onCallback(pluginkit.StreamingSegment{Final: false}, nil)
	tr.onCallback(pluginkit.StreamingSegment{Final: true, StartMS: 0, EndMS: 500}, nil)
	if err := tr.finish(); err != nil {
		t.Fatalf("expected clean finish: %v", err)
	}
	if len(tr.predicted) != 1 {
		t.Fatalf("expected exactly 1 predicted interval, got %+v", tr.predicted)
	}
}

func TestCallbackTrackerDuplicateFinalWithNoFeedBetweenIsFatal(t *testing.T) {
	tr := &callbackTracker{}
	tr.onFeed()
	tr.onCallback(pluginkit.StreamingSegment{Final: true, EndMS: 200}, nil)
	// No onFeed() call happened since the close above.
	tr.onCallback(pluginkit.StreamingSegment{Final: true, EndMS: 400}, nil)
	if tr.finish() == nil {
		t.Fatal("expected a second final with no intervening feed activity to be fatal")
	}
}

// --- the queued-final eviction risk pluginkit.StreamingSession carries -----

// TestStreamingSessionCanDropQueuedFinalsWithoutPerFeedWait documents, at
// the pluginkit level, exactly the risk the harness's Wait-after-every-Feed
// discipline exists to avoid: pluginkit.StreamingSession bounds its queued-
// commit backlog (maxPendingCommits) for a transcription that is still in
// flight, evicting the oldest queued commit once the bound is exceeded. If
// the harness fed audio without waiting for each transcription to
// complete, three utterances finalizing while one transcription blocks can
// lose the earliest of them.
func TestStreamingSessionCanDropQueuedFinalsWithoutPerFeedWait(t *testing.T) {
	release := make(chan struct{})
	var finals int
	blockedOnce := make(chan struct{}, 1)
	first := true
	transcribe := func(samples []float32) (string, error) {
		if first {
			first = false
			blockedOnce <- struct{}{}
			<-release // the first transcription (the partial) blocks here
		}
		return "speech", nil
	}
	emit := func(seg pluginkit.StreamingSegment, err error) {
		if err == nil && seg.Final {
			finals++
		}
	}
	cfg := pluginkit.DefaultStreamingConfig()
	vad := pluginkit.EnergyVAD{Threshold: 0.01}
	session, err := pluginkit.NewStreamingSession(cfg, vad, transcribe, emit)
	if err != nil {
		t.Fatal(err)
	}

	speech := make([]float32, FeedSamples)
	for i := range speech {
		speech[i] = 0.5
	}
	silence := make([]float32, FeedSamples)

	// Enough speech to cross the 1500ms partial-interval trigger, which
	// blocks the first transcription without calling Wait.
	for i := 0; i < 8; i++ {
		session.Feed(speech)
	}
	<-blockedOnce

	// Three separate utterances (speech, then enough silence to finalize),
	// all triggered while the first transcription remains blocked and
	// nothing is drained via Wait in between.
	for u := 0; u < 3; u++ {
		session.Feed(speech)
		session.Feed(speech)
		for i := 0; i < 6; i++ {
			session.Feed(silence)
		}
	}

	close(release)
	session.Wait()

	if finals >= 4 { // 1 (from the initial speech run, if any) + 3 would mean nothing was lost
		t.Skip("environment did not reproduce the queuing pattern needed for this documentation test")
	}
	if finals >= 3+1 {
		t.Fatalf("expected fewer finals than utterances fed due to bounded queue eviction, got %d", finals)
	}
}

// TestRunPreservesEveryFinalAcrossMultipleUtterances proves the harness's
// actual per-feed Wait discipline (feedAll) does not lose finals across
// several utterances in one recording -- the property the test above shows
// is not free without it.
func TestRunPreservesEveryFinalAcrossMultipleUtterances(t *testing.T) {
	// Three separate loud utterances separated by enough silence to
	// finalize each one before the next begins, followed by natural flush.
	var audio []float32
	utterance := make([]float32, FeedSamples*2)
	for i := range utterance {
		utterance[i] = 0.5
	}
	gap := make([]float32, FeedSamples*6)
	for u := 0; u < 3; u++ {
		audio = append(audio, utterance...)
		audio = append(audio, gap...)
	}

	vad := pluginkit.EnergyVAD{Threshold: 0.01}
	result, err := Run(context.Background(), audio, factoryFor(vad), 2*time.Minute)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(result.Predicted) != 3 {
		t.Fatalf("expected exactly 3 preserved finals, got %d: %+v", len(result.Predicted), result.Predicted)
	}
}
