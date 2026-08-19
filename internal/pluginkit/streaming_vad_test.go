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

// TestStreamingDropsAllButOneFinalPerFeedCall pins a pre-existing limitation
// that reframing makes reachable rather than causes: Feed holds s.mu for the
// whole call, and the transcription goroutine needs that same lock to clear
// transcriptionInFlight, so at most one transcription can start per Feed call.
// A caller that sends a chunk spanning several utterances therefore loses all
// but the first. Production capture sends 200ms chunks while finalizing needs
// SilenceHysteresis+SilenceDuration of silence, so this is not reachable there
// today, but it is speech loss for any client that buffers more aggressively.
func TestStreamingDropsAllButOneFinalPerFeedCall(t *testing.T) {
	cfg := framingConfig()
	subframe := int(cfg.VADFrame) * cfg.SampleRate / int(time.Second)
	signal := framingSignal(subframe)

	perSubframe := collectFinals(t, cfg, signal, []int{subframe})
	if len(perSubframe) < 2 {
		t.Fatalf("fixture must span several utterances to show the loss, got %d", len(perSubframe))
	}

	whole := collectFinals(t, cfg, signal, []int{len(signal)})
	if len(whole) != 1 {
		t.Fatalf("expected exactly one final from a single chunk spanning %d utterances, got %d: %v",
			len(perSubframe), len(whole), whole)
	}
}
