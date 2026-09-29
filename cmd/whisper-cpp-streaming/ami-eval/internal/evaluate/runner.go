// Package evaluate feeds canonical AMI recordings through the real
// production StreamingSession, using an observation adapter to recover
// every 20ms VAD decision and the session's own final segments as the
// detector's predicted utterances (muesli#778). It never reimplements
// segmentation: internal/pluginkit supplies the session, detectors, and
// defaults, exactly as production uses them.
package evaluate

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/score"
	"github.com/abedegno/muesli/internal/pluginkit"
)

// EvaluationVersion identifies the evaluation semantics implemented here
// (matrix construction, scoring interpretation). It is an explicit
// constant, not a Git revision, and increments whenever interpretation
// changes in a way that must invalidate previously cached raw results.
//
// v2 (muesli#782): split-labelled matrices, the widened logarithmic grid
// with round-trip threshold IDs, the frozen historical 0.01 baseline, and
// tuning-only utterance-error selection gated on held-out evidence.
const EvaluationVersion = "v2"

// SampleRate is the canonical evaluation sample rate.
const SampleRate = 16000

// FeedSamples is exactly 200ms of audio at SampleRate: every
// StreamingSession.Feed call the harness makes -- for original audio or for
// flush silence -- contains exactly this many samples.
const FeedSamples = SampleRate / 5

// VADFrameSamples is the production 20ms VAD reframing quantum at
// SampleRate.
const VADFrameSamples = SampleRate / 50

// VADFactory constructs one fresh, independent VAD instance for one run.
// Matrix entries never share a VAD between recordings or detectors.
type VADFactory func() (pluginkit.VAD, error)

// Observation is one exactly-once 20ms VAD decision recorded by
// observingVAD, in absolute sample coordinates over everything fed to the
// session (original audio followed by flush silence).
type Observation struct {
	Start  int64
	End    int64
	Speech bool
}

// observingVAD wraps a production VAD, delegating every IsSpeech call
// exactly once, recording its span and Boolean decision, and returning the
// decision unchanged.
type observingVAD struct {
	inner        pluginkit.VAD
	cursor       int64
	observations []Observation
}

func (o *observingVAD) IsSpeech(frame []float32) bool {
	start := o.cursor
	end := start + int64(len(frame))
	o.cursor = end
	speech := o.inner.IsSpeech(frame)
	o.observations = append(o.observations, Observation{Start: start, End: end, Speech: speech})
	return speech
}

// constantTranscribe is the deterministic transcription sink: a constant
// non-empty token, so StreamingSession always treats a completed window as
// a real segment worth emitting. Running real ASR would add model
// acquisition and recognition behavior unrelated to detector evaluation.
func constantTranscribe(samples []float32) (string, error) {
	return "speech", nil
}

// callbackTracker enforces the harness's callback contract as a backstop --
// not a recovery mechanism for dropped finals, which the per-feed Wait
// discipline in feedAll prevents structurally. A callback with no Feed
// activity since the last utterance closed (which also catches a duplicate
// final firing with nothing fed in between), or a sink error, is fatal.
// An utterance left open when the run ends is also fatal.
type callbackTracker struct {
	feedsSinceClose int
	open            bool
	predicted       []score.Interval
	fatal           error
}

func (t *callbackTracker) onFeed() {
	t.feedsSinceClose++
}

func (t *callbackTracker) onCallback(seg pluginkit.StreamingSegment, err error) {
	if t.fatal != nil {
		return
	}
	if err != nil {
		t.fatal = fmt.Errorf("evaluate: transcription sink error: %w", err)
		return
	}
	if t.feedsSinceClose == 0 {
		t.fatal = fmt.Errorf("evaluate: callback (final=%v start=%dms end=%dms) arrived with no feed activity since the last utterance closed", seg.Final, seg.StartMS, seg.EndMS)
		return
	}
	t.open = true
	if seg.Final {
		t.predicted = append(t.predicted, score.Interval{
			Start: msToSamples(seg.StartMS),
			End:   msToSamples(seg.EndMS),
		})
		t.open = false
		t.feedsSinceClose = 0
	}
}

func (t *callbackTracker) finish() error {
	if t.fatal != nil {
		return t.fatal
	}
	if t.open {
		return fmt.Errorf("evaluate: trailing activity without a final callback after flush")
	}
	return nil
}

// msToSamples converts a StreamingSegment millisecond timestamp back to a
// sample index. StreamingSession's public segment API only exposes
// millisecond timestamps (itself derived from integer sample/ms division),
// so this is the best precision available without copying segmentation
// internals; the loss is at most a fraction of a millisecond.
func msToSamples(ms int64) int64 {
	return int64(math.Round(float64(ms) * float64(SampleRate) / 1000.0))
}

// Deadline computes the per-run deadline: min(30m, max(1m, 2*audioDuration)).
func Deadline(audioDuration time.Duration) time.Duration {
	d := 2 * audioDuration
	if d < time.Minute {
		d = time.Minute
	}
	if d > 30*time.Minute {
		d = 30 * time.Minute
	}
	return d
}

// RunResult is one recording's detector output: every observed frame
// decision (for frame scoring) and every final utterance interval (for
// utterance scoring), both in absolute sample coordinates over the
// original (unpadded) audio.
type RunResult struct {
	Frames    []score.Frame
	Predicted []score.Interval
}

// Run executes one fresh StreamingSession and one fresh VAD (from
// vadFactory) against one recording's canonical audio, under production
// defaults, and returns its detector output. audio must be canonical mono
// PCM at SampleRate; the harness itself performs no resampling.
func Run(ctx context.Context, audio []float32, vadFactory VADFactory, deadline time.Duration) (RunResult, error) {
	vad, err := vadFactory()
	if err != nil {
		return RunResult{}, fmt.Errorf("evaluate: construct detector: %w", err)
	}
	obs := &observingVAD{inner: vad}
	tracker := &callbackTracker{}

	cfg := pluginkit.DefaultStreamingConfig()
	session, err := pluginkit.NewStreamingSession(cfg, obs, constantTranscribe, tracker.onCallback)
	if err != nil {
		return RunResult{}, fmt.Errorf("evaluate: construct session: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- feedAll(session, tracker, audio, cfg)
	}()

	select {
	case err := <-done:
		if err != nil {
			return RunResult{}, err
		}
	case <-time.After(deadline):
		return RunResult{}, fmt.Errorf("evaluate: run exceeded deadline of %s", deadline)
	case <-ctx.Done():
		return RunResult{}, ctx.Err()
	}

	if err := tracker.finish(); err != nil {
		return RunResult{}, err
	}

	frames := framesFromObservations(obs.observations, int64(len(audio)))
	predicted := clipToAudioExtent(tracker.predicted, int64(len(audio)))
	return RunResult{Frames: frames, Predicted: predicted}, nil
}

// clipToAudioExtent clips every final session interval to [0, audioSamples].
// A committed final's End can legitimately extend past the original audio
// into hysteresis-tolerated trailing silence (SilenceHysteresis audio
// still joins the transcription window by design); utterance scoring must
// only ever see the original audio extent.
func clipToAudioExtent(predicted []score.Interval, audioSamples int64) []score.Interval {
	out := make([]score.Interval, 0, len(predicted))
	for _, p := range predicted {
		start, end := p.Start, p.End
		if start < 0 {
			start = 0
		}
		if end > audioSamples {
			end = audioSamples
		}
		if start > audioSamples {
			start = audioSamples
		}
		if end < start {
			end = start
		}
		out = append(out, score.Interval{Start: start, End: end})
	}
	return out
}

// feedAll feeds original audio in exactly FeedSamples/200ms calls (the
// last padded with digital silence if short), waiting after every call so
// each triggered transcription fully completes before the next feed can
// possibly trigger another -- this is what prevents the queued-final
// eviction pluginkit.StreamingSession's bounded backlog would otherwise
// risk (see runner_test.go). It then feeds successive full flush-silence
// calls, also waiting after each, until fed silence exceeds
// SilenceHysteresis + SilenceDuration + VADFrame, which is enough for the
// session's own hysteresis/silence-duration logic to finalize any trailing
// active utterance without ever calling Finish (which would force a
// commit outside the normal, evaluated code path).
func feedAll(session *pluginkit.StreamingSession, tracker *callbackTracker, audio []float32, cfg pluginkit.StreamingConfig) error {
	n := len(audio)
	for pos := 0; pos < n; pos += FeedSamples {
		end := pos + FeedSamples
		var chunk []float32
		if end <= n {
			chunk = audio[pos:end]
		} else {
			chunk = make([]float32, FeedSamples)
			copy(chunk, audio[pos:n])
		}
		session.Feed(chunk)
		tracker.onFeed()
		session.Wait()
		if tracker.fatal != nil {
			return tracker.fatal
		}
	}

	threshold := cfg.SilenceHysteresis + cfg.SilenceDuration + cfg.VADFrame
	perFeed := time.Duration(FeedSamples) * time.Second / time.Duration(SampleRate)
	silence := make([]float32, FeedSamples)
	for flushed := time.Duration(0); flushed <= threshold; flushed += perFeed {
		session.Feed(silence)
		tracker.onFeed()
		session.Wait()
		if tracker.fatal != nil {
			return tracker.fatal
		}
	}
	return nil
}

// framesFromObservations converts recorded VAD observations into
// score.Frame, zero-weighting any part of a span at or beyond
// validSamples (the original, unpadded audio length) -- this is the
// padding and flush silence, which must never contribute to frame scoring.
func framesFromObservations(obs []Observation, validSamples int64) []score.Frame {
	frames := make([]score.Frame, 0, len(obs))
	for _, o := range obs {
		valid := o.End - o.Start
		if o.Start >= validSamples {
			valid = 0
		} else if o.End > validSamples {
			valid = validSamples - o.Start
		}
		if valid < 0 {
			valid = 0
		}
		frames = append(frames, score.Frame{Start: o.Start, End: o.End, ValidSamples: valid, Predicted: o.Speech})
	}
	return frames
}
