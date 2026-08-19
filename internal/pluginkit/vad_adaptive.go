package pluginkit

import (
	"errors"
	"math"
	"time"
)

// Adaptive detector defaults. The speech and noise coefficients are calibrated
// against two real meeting recordings with opposite characteristics (a noisy
// 90-minute recording whose noise floor sits above the old fixed default, and a
// quiet 55-minute one whose floor is digital silence). At 20ms framing the two
// recordings agree on the speech coefficient to within 0.2%, and the resulting
// thresholds land within 1% of each recording's separately measured best value.
// See muesli#565 for the measurements.
const (
	adaptiveBins   = 256
	adaptiveLogMin = -5.0 // 1e-5 RMS, below any real noise floor
	adaptiveLogMax = 0.0  // full scale

	// The speech-referenced arm. Conditioning on frames already classified as
	// speech is what makes this independent of speech occupancy: a quantile
	// over *all* frames is really an occupancy controller, and drifts by more
	// than 60% between a recording that is 25% speech and one that is 70%.
	DefaultAdaptiveSpeechQuantile = 0.90
	DefaultAdaptiveSpeechFactor   = 0.3767

	// The noise-referenced guard. Without it a speech-referenced threshold sits
	// below the mode of a unimodal distribution, so stationary noise with no
	// speech in it classifies entirely as speech. This arm dominates in that
	// case and suppresses it; on real speech the speech arm dominates.
	DefaultAdaptiveNoiseQuantile = 0.10
	DefaultAdaptiveNoiseFactor   = 1.8

	DefaultAdaptiveMinThreshold = 0.002

	// minSpeechMass is the smallest share of observations the speech class may
	// hold and still be worth conditioning on. Without it the fixed point can
	// run away: each rise leaves a smaller, louder tail above the threshold,
	// which raises the estimate again. Measured against a quiet recording at
	// 10% speech occupancy, an unguarded loop settled 5x too high.
	minSpeechMass              = 0.02
	DefaultAdaptiveWarmup      = 3 * time.Second
	DefaultAdaptiveUpdateEvery = 1 * time.Second
	DefaultAdaptiveHorizon     = 3 * time.Minute
	DefaultAdaptiveMaxSlew     = 0.10
)

// AdaptiveVADConfig configures an AdaptiveEnergyVAD.
type AdaptiveVADConfig struct {
	// Frame is the duration of one classified frame and SampleRate the audio
	// rate; together they convert the duration settings below into frame
	// counts. They must match the StreamingConfig the detector is used with.
	Frame      time.Duration
	SampleRate int

	// Fallback is the fixed threshold used until Warmup has elapsed, before
	// enough audio has been observed to estimate anything.
	Fallback float64

	SpeechQuantile float64
	SpeechFactor   float64
	NoiseQuantile  float64
	NoiseFactor    float64
	MinThreshold   float64

	Warmup      time.Duration
	UpdateEvery time.Duration
	// Horizon is the half-life of the observation histogram: older audio keeps
	// influencing the estimate but with exponentially decreasing weight, so the
	// detector tracks a room that changes without being whipped around by a
	// single loud passage.
	Horizon time.Duration
	// MaxSlew caps the fractional change in threshold per update, damping the
	// feedback loop between the threshold and the speech class it conditions on.
	MaxSlew float64
}

// DefaultAdaptiveVADConfig returns the calibrated defaults for a given framing.
func DefaultAdaptiveVADConfig(sampleRate int, frame time.Duration) AdaptiveVADConfig {
	return AdaptiveVADConfig{
		Frame:          frame,
		SampleRate:     sampleRate,
		Fallback:       DefaultStreamingConfig().EnergyThreshold,
		SpeechQuantile: DefaultAdaptiveSpeechQuantile,
		SpeechFactor:   DefaultAdaptiveSpeechFactor,
		NoiseQuantile:  DefaultAdaptiveNoiseQuantile,
		NoiseFactor:    DefaultAdaptiveNoiseFactor,
		MinThreshold:   DefaultAdaptiveMinThreshold,
		Warmup:         DefaultAdaptiveWarmup,
		UpdateEvery:    DefaultAdaptiveUpdateEvery,
		Horizon:        DefaultAdaptiveHorizon,
		MaxSlew:        DefaultAdaptiveMaxSlew,
	}
}

// AdaptiveEnergyVAD is an experimental RMS detector that estimates its own
// threshold from the audio it has heard, instead of comparing against a fixed
// constant that cannot span rooms with different noise floors.
//
// It is NOT safe to share one detector between sessions: it carries per-session
// state and StreamingSession serializes calls only within a single session.
// Construct one per stream.
//
// Known envelope, measured against two real recordings: between roughly 10% and
// 70% speech occupancy the estimate stays within about 1.3x across that range
// and lands within a few percent of each recording's separately measured best
// fixed threshold. Below about 10% occupancy it under-estimates -- see
// TestAdaptiveVADUnderestimatesAtVeryLowOccupancy for the mechanism. That
// limitation is why this ships opt-in and why a trained detector remains the
// intended destination.
type AdaptiveEnergyVAD struct {
	cfg AdaptiveVADConfig

	bins  [adaptiveBins]float64
	mass  float64
	decay float64 // applied to the whole histogram once per update

	warmupFrames int64
	updateFrames int64
	frames       int64
	sinceUpdate  int64

	threshold float64
}

// NewAdaptiveEnergyVAD validates cfg and returns a detector for one session.
func NewAdaptiveEnergyVAD(cfg AdaptiveVADConfig) (*AdaptiveEnergyVAD, error) {
	if cfg.SampleRate <= 0 {
		return nil, errors.New("adaptive vad sample rate must be positive")
	}
	if cfg.Frame <= 0 {
		return nil, errors.New("adaptive vad frame must be positive")
	}
	if cfg.Fallback < 0 || cfg.MinThreshold < 0 {
		return nil, errors.New("adaptive vad thresholds must not be negative")
	}
	if cfg.SpeechQuantile <= 0 || cfg.SpeechQuantile >= 1 ||
		cfg.NoiseQuantile <= 0 || cfg.NoiseQuantile >= 1 {
		return nil, errors.New("adaptive vad quantiles must lie strictly between 0 and 1")
	}
	if cfg.SpeechFactor <= 0 || cfg.NoiseFactor <= 0 {
		return nil, errors.New("adaptive vad factors must be positive")
	}
	if cfg.UpdateEvery <= 0 || cfg.Horizon <= 0 {
		return nil, errors.New("adaptive vad update interval and horizon must be positive")
	}
	if cfg.Warmup < 0 {
		return nil, errors.New("adaptive vad warmup must not be negative")
	}
	if cfg.MaxSlew <= 0 {
		return nil, errors.New("adaptive vad max slew must be positive")
	}

	perFrame := float64(cfg.Frame)
	v := &AdaptiveEnergyVAD{
		cfg:          cfg,
		warmupFrames: int64(math.Ceil(float64(cfg.Warmup) / perFrame)),
		updateFrames: int64(math.Ceil(float64(cfg.UpdateEvery) / perFrame)),
		threshold:    cfg.Fallback,
	}
	if v.updateFrames < 1 {
		v.updateFrames = 1
	}
	// Decay applied once per update so the histogram's half-life is Horizon.
	v.decay = math.Pow(0.5, float64(cfg.UpdateEvery)/float64(cfg.Horizon))
	return v, nil
}

// Threshold reports the detector's current operating threshold.
func (v *AdaptiveEnergyVAD) Threshold() float64 { return v.threshold }

// IsSpeech classifies one frame and folds it into the running estimate.
func (v *AdaptiveEnergyVAD) IsSpeech(frame []float32) bool {
	if len(frame) == 0 {
		return false
	}
	rms := frameRMS(frame)
	if !isFinite(rms) {
		// A non-finite frame tells us nothing; do not let it poison the
		// histogram or the threshold.
		return false
	}

	v.observe(rms)
	v.frames++
	v.sinceUpdate++
	if v.sinceUpdate >= v.updateFrames {
		v.sinceUpdate = 0
		v.recompute()
	}

	if v.frames <= v.warmupFrames {
		return rms >= v.cfg.Fallback
	}
	return rms >= v.threshold
}

func (v *AdaptiveEnergyVAD) observe(rms float64) {
	v.bins[binOf(rms)]++
	v.mass++
}

// recompute re-derives the threshold from the decayed histogram. It runs once
// per UpdateEvery rather than per frame: the fixed point costs a few hundred
// operations over the bins, never over the audio.
func (v *AdaptiveEnergyVAD) recompute() {
	for i := range v.bins {
		v.bins[i] *= v.decay
	}
	v.mass *= v.decay
	if v.frames <= v.warmupFrames || v.mass <= 0 {
		return
	}

	floor := math.Max(v.cfg.NoiseFactor*v.quantile(v.cfg.NoiseQuantile, 0), v.cfg.MinThreshold)

	// Fixed point: t = max(speechFactor * quantile of the mass at or above t,
	// noise guard). Conditioning on the speech class is what removes the
	// dependence on how much silence surrounds the speech.
	// The iteration is seeded from the noise guard and climbs, rather than from
	// the previous threshold. The map is non-decreasing in t, so climbing from
	// below lands on the *lowest* fixed point -- the boundary between noise and
	// speech. Seeding from the previous threshold instead lets the estimate
	// ratchet into a spurious high fixed point and never come back down.
	t := floor
	for range 40 {
		if v.massAbove(t) < minSpeechMass*v.mass {
			// The tail above t is too thin to describe speech; stop climbing
			// and keep the last threshold that still had support.
			break
		}
		next := math.Max(v.cfg.SpeechFactor*v.quantile(v.cfg.SpeechQuantile, t), floor)
		if next <= 0 || !isFinite(next) {
			return
		}
		if next <= t*1.001 {
			t = math.Max(t, next)
			break
		}
		t = next
	}

	// Slew limit: never move more than MaxSlew of the current threshold in one
	// update, so a transient cannot swing the detector.
	lo := v.threshold * (1 - v.cfg.MaxSlew)
	hi := v.threshold * (1 + v.cfg.MaxSlew)
	// MinThreshold is applied *after* the slew clamp, not inside it. The clamp
	// is multiplicative, so a threshold of zero has lo == hi == 0 and would pin
	// itself at zero forever -- and a zero threshold classifies every frame as
	// speech, so silence would never finalize. A configured fallback of zero is
	// valid input, so the floor has to be able to bootstrap it.
	v.threshold = math.Max(math.Min(math.Max(t, lo), hi), v.cfg.MinThreshold)
}

// quantile returns the q-th quantile of the histogram mass at or above `from`,
// which is the conditioning that makes the speech arm occupancy-independent.
// A `from` of zero makes it a plain quantile over every observation.
func (v *AdaptiveEnergyVAD) quantile(q, from float64) float64 {
	start := 0
	if from > 0 {
		start = binOf(from)
	}
	total := 0.0
	for i := start; i < adaptiveBins; i++ {
		total += v.bins[i]
	}
	if total <= 0 {
		return 0
	}
	target := q * total
	running := 0.0
	for i := start; i < adaptiveBins; i++ {
		running += v.bins[i]
		if running >= target {
			return valueOf(i)
		}
	}
	return valueOf(adaptiveBins - 1)
}

// massAbove reports the histogram mass at or above an RMS value.
func (v *AdaptiveEnergyVAD) massAbove(from float64) float64 {
	total := 0.0
	for i := binOf(from); i < adaptiveBins; i++ {
		total += v.bins[i]
	}
	return total
}

func frameRMS(frame []float32) float64 {
	var sum float64
	for _, sample := range frame {
		x := float64(sample)
		sum += x * x
	}
	return math.Sqrt(sum / float64(len(frame)))
}

func isFinite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

// binOf maps an RMS value onto the log-spaced histogram, clamping at both ends
// so that digital silence and full-scale samples both land in a real bin.
func binOf(rms float64) int {
	if rms <= 0 {
		return 0
	}
	l := math.Log10(rms)
	if l <= adaptiveLogMin {
		return 0
	}
	if l >= adaptiveLogMax {
		return adaptiveBins - 1
	}
	i := int((l - adaptiveLogMin) / (adaptiveLogMax - adaptiveLogMin) * float64(adaptiveBins-1))
	if i < 0 {
		return 0
	}
	if i >= adaptiveBins {
		return adaptiveBins - 1
	}
	return i
}

// valueOf returns the RMS at the centre of a bin.
func valueOf(bin int) float64 {
	l := adaptiveLogMin + (float64(bin)+0.5)/float64(adaptiveBins-1)*(adaptiveLogMax-adaptiveLogMin)
	return math.Pow(10, l)
}
