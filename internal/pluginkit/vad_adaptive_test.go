package pluginkit

import (
	"math"
	"testing"
	"time"
)

// Percentile ladders (0th..100th in 5% steps) of 20ms frame RMS, measured from
// two real meeting recordings and split at each recording's separately measured
// best fixed threshold. They are recorded here rather than invented so the
// fixture describes audio that exists: a hand-written distribution would encode
// the very assumption these tests are meant to check. The recordings themselves
// are personal and are not part of the repo. See muesli#565.
//
// noisy: 90 minutes in a loud environment, 24.2% speech, noise floor 0.0132 --
// above the old fixed default of 0.01, which is why it finalized nothing.
// clean: 55 minutes in a quiet room, 25.1% speech, noise floor at true digital
// silence.
var (
	noisySpeech  = [21]float64{0.04000, 0.04109, 0.04226, 0.04356, 0.04496, 0.04648, 0.04811, 0.04995, 0.05196, 0.05413, 0.05661, 0.05949, 0.06268, 0.06636, 0.07074, 0.07604, 0.08279, 0.09179, 0.10608, 0.13430, 0.46276}
	noisySilence = [21]float64{0.00154, 0.01526, 0.01678, 0.01793, 0.01892, 0.01981, 0.02068, 0.02153, 0.02238, 0.02324, 0.02412, 0.02505, 0.02604, 0.02709, 0.02824, 0.02953, 0.03100, 0.03267, 0.03467, 0.03708, 0.04000}
	cleanSpeech  = [21]float64{0.02000, 0.02079, 0.02160, 0.02248, 0.02340, 0.02435, 0.02541, 0.02653, 0.02769, 0.02893, 0.03031, 0.03190, 0.03366, 0.03561, 0.03778, 0.04024, 0.04333, 0.04725, 0.05315, 0.06640, 0.28905}
	cleanSilence = [21]float64{0.00000, 0.00002, 0.00005, 0.00010, 0.00021, 0.00042, 0.00076, 0.00121, 0.00183, 0.00261, 0.00357, 0.00471, 0.00604, 0.00753, 0.00910, 0.01076, 0.01237, 0.01412, 0.01590, 0.01785, 0.02000}
)

const (
	testVADSampleRate = 16_000
	testVADFrame      = 20 * time.Millisecond
)

// sampleLadder draws from a measured distribution by inverse-CDF interpolation.
func sampleLadder(ladder [21]float64, u float64) float64 {
	x := u * 20
	i := int(x)
	if i >= 20 {
		return ladder[20]
	}
	return ladder[i] + (x-float64(i))*(ladder[i+1]-ladder[i])
}

type lcg uint32

func (r *lcg) next() float64 {
	*r = lcg(uint32(*r)*1664525 + 1013904223)
	return float64(uint32(*r)>>8&0xffffff) / float64(1<<24)
}

// frameOf returns a frame whose RMS is exactly rms. The within-frame waveform
// is irrelevant to a detector that only measures RMS, so a constant amplitude
// gives exact control of the quantity under test.
func frameOf(rms float64) []float32 {
	frame := make([]float32, testVADSampleRate*int(testVADFrame/time.Millisecond)/1000)
	for i := range frame {
		frame[i] = float32(rms)
	}
	return frame
}

// runLadder feeds `seconds` of audio mixed from the two populations at the
// given speech occupancy, and reports the settled threshold.
func runLadder(t *testing.T, speech, silence [21]float64, occupancy float64, seconds int) float64 {
	t.Helper()
	vad, err := NewAdaptiveEnergyVAD(DefaultAdaptiveVADConfig(testVADSampleRate, testVADFrame))
	if err != nil {
		t.Fatal(err)
	}
	rng := lcg(20260820)
	frames := seconds * int(time.Second/testVADFrame)
	for range frames {
		ladder := silence
		if rng.next() < occupancy {
			ladder = speech
		}
		vad.IsSpeech(frameOf(sampleLadder(ladder, rng.next())))
	}
	return vad.Threshold()
}

func TestAdaptiveVADConvergesToMeasuredThresholds(t *testing.T) {
	for _, tc := range []struct {
		name            string
		speech, silence [21]float64
		occupancy, want float64
	}{
		{"noisy recording", noisySpeech, noisySilence, 0.242, 0.040},
		{"clean recording", cleanSpeech, cleanSilence, 0.251, 0.020},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runLadder(t, tc.speech, tc.silence, tc.occupancy, 180)
			if math.Abs(got/tc.want-1) > 0.25 {
				t.Errorf("threshold %.4f is %.0f%% from the measured best %.4f",
					got, 100*(got/tc.want-1), tc.want)
			}
		})
	}
}

// TestAdaptiveVADIsIndependentOfSpeechOccupancy is the property that rules out
// a plain quantile over all frames. A P75-of-everything rule matches both
// recordings at their natural ~25% speech, but drifts by more than 60% once the
// same speech is surrounded by more or less silence, because it is really a
// 25%-positive-rate controller rather than a detector.
func TestAdaptiveVADIsIndependentOfSpeechOccupancy(t *testing.T) {
	for _, tc := range []struct {
		name            string
		speech, silence [21]float64
	}{
		{"noisy recording", noisySpeech, noisySilence},
		{"clean recording", cleanSpeech, cleanSilence},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lo, hi := math.Inf(1), 0.0
			for _, occupancy := range []float64{0.10, 0.25, 0.50, 0.70} {
				got := runLadder(t, tc.speech, tc.silence, occupancy, 180)
				t.Logf("occupancy %.0f%% -> threshold %.4f", occupancy*100, got)
				lo = math.Min(lo, got)
				hi = math.Max(hi, got)
			}
			if hi/lo > 1.5 {
				t.Errorf("threshold spans %.2fx across occupancies (%.4f..%.4f); "+
					"an occupancy-dependent rule is not a detector", hi/lo, lo, hi)
			}
		})
	}
}

// TestAdaptiveVADSuppressesStationaryNoise covers the failure a purely
// speech-referenced rule has on its own: with no speech present the
// distribution is unimodal, a threshold derived from it sits below the mode,
// and every frame of an empty room classifies as speech -- which would reset
// the silence clock forever and prevent any utterance from ever finalizing.
func TestAdaptiveVADSuppressesStationaryNoise(t *testing.T) {
	for _, level := range []float64{0.0005, 0.013, 0.05} {
		vad, err := NewAdaptiveEnergyVAD(DefaultAdaptiveVADConfig(testVADSampleRate, testVADFrame))
		if err != nil {
			t.Fatal(err)
		}
		rng := lcg(7)
		speech, total := 0, 180*int(time.Second/testVADFrame)
		for i := range total {
			// +/-30% stationary variation around the level.
			if vad.IsSpeech(frameOf(level*(0.7+0.6*rng.next()))) && i > total/2 {
				speech++
			}
		}
		if ratio := float64(speech) / float64(total/2); ratio > 0.05 {
			t.Errorf("level %.4f: %.1f%% of settled noise frames classified as speech", level, 100*ratio)
		}
	}
}

func TestAdaptiveVADUsesFallbackDuringWarmup(t *testing.T) {
	cfg := DefaultAdaptiveVADConfig(testVADSampleRate, testVADFrame)
	cfg.Fallback = 0.01
	vad, err := NewAdaptiveEnergyVAD(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// One frame either side of the fallback, well inside the warmup window.
	if !vad.IsSpeech(frameOf(0.02)) {
		t.Error("expected audio above the fallback to be speech during warmup")
	}
	if vad.IsSpeech(frameOf(0.005)) {
		t.Error("expected audio below the fallback to be silence during warmup")
	}
	if vad.Threshold() != cfg.Fallback {
		t.Errorf("threshold moved during warmup: %v", vad.Threshold())
	}
}

func TestAdaptiveVADIgnoresNonFiniteFrames(t *testing.T) {
	vad, err := NewAdaptiveEnergyVAD(DefaultAdaptiveVADConfig(testVADSampleRate, testVADFrame))
	if err != nil {
		t.Fatal(err)
	}
	before := vad.Threshold()
	for _, bad := range []float32{float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1))} {
		frame := frameOf(0.03)
		frame[0] = bad
		if vad.IsSpeech(frame) {
			t.Errorf("non-finite frame (%v) classified as speech", bad)
		}
	}
	if vad.Threshold() != before || !isFinite(vad.Threshold()) {
		t.Errorf("non-finite frames disturbed the threshold: %v", vad.Threshold())
	}
	if vad.IsSpeech(nil) {
		t.Error("empty frame classified as speech")
	}
}

func TestNewAdaptiveEnergyVADValidatesConfig(t *testing.T) {
	valid := DefaultAdaptiveVADConfig(testVADSampleRate, testVADFrame)
	for name, mutate := range map[string]func(*AdaptiveVADConfig){
		"zero sample rate":   func(c *AdaptiveVADConfig) { c.SampleRate = 0 },
		"zero frame":         func(c *AdaptiveVADConfig) { c.Frame = 0 },
		"negative fallback":  func(c *AdaptiveVADConfig) { c.Fallback = -1 },
		"quantile at 1":      func(c *AdaptiveVADConfig) { c.SpeechQuantile = 1 },
		"quantile at 0":      func(c *AdaptiveVADConfig) { c.NoiseQuantile = 0 },
		"zero speech factor": func(c *AdaptiveVADConfig) { c.SpeechFactor = 0 },
		"zero horizon":       func(c *AdaptiveVADConfig) { c.Horizon = 0 },
		"zero slew":          func(c *AdaptiveVADConfig) { c.MaxSlew = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := valid
			mutate(&cfg)
			if _, err := NewAdaptiveEnergyVAD(cfg); err == nil {
				t.Error("expected a validation error")
			}
		})
	}
	if _, err := NewAdaptiveEnergyVAD(valid); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
}

// TestAdaptiveVADUnderestimatesAtVeryLowOccupancy pins a known limitation
// rather than hiding it. Below roughly 10% speech occupancy -- a meeting with
// only a few minutes of talking in an hour -- the estimate falls well under the
// value measured for the same audio at normal occupancy.
//
// The cause is intrinsic to the estimator family rather than a badly chosen
// constant: the fixed point conditions on the frames above the threshold it is
// solving for, and when speech is scarce that set is dominated by the upper
// tail of the room tone, so the iteration settles between the noise and the
// speech instead of at the boundary. Sweeping the speech quantile over P80,
// P90, P95 and P98 does not remove it; every variant trades low-occupancy
// error against accuracy at normal occupancy.
//
// This is the main reason the adaptive detector ships opt-in rather than as the
// default, and why a trained detector is the eventual answer. If a change makes
// this test fail by *improving* the low-occupancy estimate, that is a win --
// tighten the bound rather than deleting the test.
func TestAdaptiveVADUnderestimatesAtVeryLowOccupancy(t *testing.T) {
	atNormal := runLadder(t, cleanSpeech, cleanSilence, 0.25, 180)
	atSparse := runLadder(t, cleanSpeech, cleanSilence, 0.05, 180)
	t.Logf("clean recording: 25%% occupancy -> %.4f, 5%% occupancy -> %.4f", atNormal, atSparse)

	if atSparse >= atNormal {
		t.Fatalf("expected the documented under-estimate at 5%% occupancy, got %.4f >= %.4f",
			atSparse, atNormal)
	}
	if ratio := atNormal / atSparse; ratio > 3.0 {
		t.Errorf("low-occupancy under-estimate widened to %.2fx (%.4f vs %.4f); "+
			"the documented envelope was about 2.4x", ratio, atSparse, atNormal)
	}
}

// TestAdaptiveVADBootstrapsFromAZeroFallback covers a configuration that the
// schema and the session parser both accept: vad_threshold of 0. The slew limit
// is multiplicative, so without a floor applied outside it a zero threshold
// clamps itself to zero on every update, and a zero threshold makes every frame
// speech -- so silence would never finalize and nothing would ever transcribe.
func TestAdaptiveVADBootstrapsFromAZeroFallback(t *testing.T) {
	cfg := DefaultAdaptiveVADConfig(testVADSampleRate, testVADFrame)
	cfg.Fallback = 0
	vad, err := NewAdaptiveEnergyVAD(cfg)
	if err != nil {
		t.Fatal(err)
	}

	rng := lcg(3)
	frames := 120 * int(time.Second/testVADFrame)
	speech := 0
	for i := range frames {
		// A quiet room: stationary noise well below any sensible threshold.
		if vad.IsSpeech(frameOf(0.0004*(0.7+0.6*rng.next()))) && i > frames/2 {
			speech++
		}
	}
	if vad.Threshold() < cfg.MinThreshold {
		t.Errorf("threshold %v never rose off zero; MinThreshold is %v", vad.Threshold(), cfg.MinThreshold)
	}
	if ratio := float64(speech) / float64(frames/2); ratio > 0.05 {
		t.Errorf("%.1f%% of a quiet room classified as speech; silence would never finalize", 100*ratio)
	}
}
