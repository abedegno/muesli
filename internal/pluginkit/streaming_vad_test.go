package pluginkit

import (
	"testing"
	"time"
)

// framingSignal builds a deterministic signal of alternating loud and quiet
// runs. Amplitudes vary within each run: a constant-amplitude signal has the
// same RMS at every frame size, so it cannot detect a detector that classifies
// whole caller-supplied chunks. Run lengths are whole 20ms subframes but are
// not multiples of larger chunk sizes, so an unreframed detector straddles
// transitions and segments the same audio differently.
func framingSignal(subframe int) []float32 {
	runs := []struct {
		subframes int
		loud      bool
	}{
		{6, true}, {5, false}, {3, true}, {8, false}, {4, true}, {9, false},
	}
	var out []float32
	state := uint32(12345)
	for _, r := range runs {
		for range r.subframes * subframe {
			state = state*1664525 + 1013904223
			jitter := float32(state>>16&0xff) / 255.0 // 0..1, deterministic
			if r.loud {
				out = append(out, 0.35+0.30*jitter)
			} else {
				out = append(out, 0.0005*jitter)
			}
		}
	}
	return out
}

func framingConfig() StreamingConfig {
	cfg := DefaultStreamingConfig()
	cfg.SampleRate = 1_000
	cfg.VADFrame = 20 * time.Millisecond
	cfg.MaxWindow = 2 * time.Second
	cfg.PartialInterval = 2 * time.Second // keep partials out of the comparison
	cfg.SilenceDuration = 60 * time.Millisecond
	cfg.SilenceHysteresis = 0
	cfg.EnergyThreshold = 0.05
	return cfg
}

// collectFinals feeds the signal in fixed-size chunks and returns the
// boundaries of every final segment.
func collectFinals(t *testing.T, cfg StreamingConfig, signal []float32, chunks []int) [][2]int64 {
	t.Helper()
	var finals [][2]int64
	session, err := NewStreamingSession(cfg, nil,
		func(samples []float32) (string, error) { return "x", nil },
		func(segment StreamingSegment, err error) {
			if err != nil {
				t.Errorf("emit error: %v", err)
				return
			}
			if segment.Final {
				finals = append(finals, [2]int64{segment.StartMS, segment.EndMS})
			}
		})
	if err != nil {
		t.Fatal(err)
	}
	for i, next := 0, 0; i < len(signal); next++ {
		size := chunks[next%len(chunks)]
		if i+size > len(signal) {
			size = len(signal) - i
		}
		session.Feed(signal[i : i+size])
		session.Wait() // deterministic: no dropped triggers from in-flight work
		i += size
	}
	session.Wait()
	return finals
}

// TestStreamingSegmentationIsIndependentOfChunkSize pins the property that
// identical audio must segment identically however the caller chunks it.
// Production capture sends 200ms frames while other clients may send anything.
func TestStreamingSegmentationIsIndependentOfChunkSize(t *testing.T) {
	cfg := framingConfig()
	subframe := int(cfg.VADFrame) * cfg.SampleRate / int(time.Second)
	signal := framingSignal(subframe)

	reference := collectFinals(t, cfg, signal, []int{subframe})
	if len(reference) == 0 {
		t.Fatal("reference produced no finals; the fixture cannot detect anything")
	}

	for _, tc := range []struct {
		name   string
		chunks []int
	}{
		{"production 200ms frames", []int{200}},
		{"irregular chunks", []int{7, 13, 31, 5, 44}},
		{"half-subframe chunks", []int{10}},
		{"ragged chunks around the subframe size", []int{19, 21, 20, 41}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := collectFinals(t, cfg, signal, tc.chunks)
			if len(got) != len(reference) {
				t.Fatalf("got %d finals, reference has %d: %v vs %v", len(got), len(reference), got, reference)
			}
			for i := range got {
				if got[i] != reference[i] {
					t.Errorf("final %d: got %v, reference %v", i, got[i], reference[i])
				}
			}
		})
	}
}

// TestStreamingQueuesFinalsAcrossAFeedCall pins the fix for muesli#711's
// third loss path, which TestStreamingDropsAllButOneFinalPerFeedCall used to
// pin as intentional: Feed holds s.mu for the whole call, so the
// transcription goroutine launched by the first final trigger cannot
// reacquire s.mu to clear transcriptionInFlight until Feed itself returns.
// Every later trigger reached inside that same Feed call used to see
// transcriptionInFlight still true and simply drop -- a caller that sends a
// chunk spanning several utterances lost all but the first. Those triggers
// now queue (bounded, see maxPendingCommits) instead, and run in order once
// Feed returns and the in-flight transcription (and each queued one after it)
// completes, so a chunk spanning several utterances produces the same finals
// a caller that fed the audio incrementally would have gotten.
//
// Production capture sends 200ms chunks while finalizing needs
// SilenceHysteresis+SilenceDuration (1s by default) of silence, so a single
// production chunk cannot contain two finals and this is unreachable from the
// desktop app. It is reachable for any client that buffers more aggressively --
// which before reframing got one RMS decision per whole chunk, so its
// segmentation was already unusable.
func TestStreamingQueuesFinalsAcrossAFeedCall(t *testing.T) {
	cfg := framingConfig()
	subframe := int(cfg.VADFrame) * cfg.SampleRate / int(time.Second)
	signal := framingSignal(subframe)

	reference := collectFinals(t, cfg, signal, []int{subframe})
	if len(reference) < 2 {
		t.Fatalf("fixture must span several utterances to show queuing, got %d", len(reference))
	}

	whole := collectFinals(t, cfg, signal, []int{len(signal)})
	if len(whole) != len(reference) {
		t.Fatalf("got %d finals from one chunk spanning %d utterances, want %d: %v vs %v",
			len(whole), len(reference), len(reference), whole, reference)
	}
	for i := range whole {
		if whole[i] != reference[i] {
			t.Errorf("final %d: got %v, reference %v", i, whole[i], reference[i])
		}
	}
}

// TestStreamingPendingBufferDoesNotRetainLargeChunks covers a hosted-server
// concern rather than a laptop one: compacting the reframing buffer in place
// reuses its backing array, so one oversized chunk would otherwise pin that
// allocation for the whole session, on every concurrent meeting.
func TestStreamingPendingBufferDoesNotRetainLargeChunks(t *testing.T) {
	cfg := framingConfig()
	subframe := int(cfg.VADFrame) * cfg.SampleRate / int(time.Second)
	session, err := NewStreamingSession(cfg, nil,
		func([]float32) (string, error) { return "x", nil },
		func(StreamingSegment, error) {})
	if err != nil {
		t.Fatal(err)
	}

	// One large chunk, deliberately not a whole number of subframes so a
	// remainder is carried.
	session.Feed(make([]float32, 500*subframe+7))
	session.Wait()

	session.mu.Lock()
	got := cap(session.pending)
	session.mu.Unlock()
	if got > 4*subframe {
		t.Errorf("pending buffer retained capacity %d after a large chunk; want at most %d",
			got, 4*subframe)
	}
}

// TestStreamingLeavesASubframeRemainderUnclassified pins what happens to audio
// shorter than one VAD frame at the end of a stream: it stays buffered and is
// never classified, so up to VADFrame-1 samples contribute to no utterance and
// no timestamp. The fixture in the chunk-independence test is an exact multiple
// of the subframe, which would hide this.
//
// Fixing it needs an explicit end-of-stream flush, tracked in muesli#711
// alongside the other paths that lose in-progress speech.
func TestStreamingLeavesASubframeRemainderUnclassified(t *testing.T) {
	cfg := framingConfig()
	subframe := int(cfg.VADFrame) * cfg.SampleRate / int(time.Second)

	for _, remainder := range []int{1, subframe / 2, subframe - 1} {
		session, err := NewStreamingSession(cfg, nil,
			func([]float32) (string, error) { return "x", nil },
			func(StreamingSegment, error) {})
		if err != nil {
			t.Fatal(err)
		}
		session.Feed(make([]float32, 3*subframe+remainder))
		session.Wait()

		session.mu.Lock()
		held, classified := len(session.pending), session.totalSamples
		session.mu.Unlock()

		if held != remainder {
			t.Errorf("remainder %d: %d samples held, want %d", remainder, held, remainder)
		}
		if classified != int64(3*subframe) {
			t.Errorf("remainder %d: accounted for %d samples, want %d",
				remainder, classified, 3*subframe)
		}
	}
}
