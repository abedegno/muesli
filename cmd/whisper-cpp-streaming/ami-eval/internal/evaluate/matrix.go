package evaluate

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/score"
	"github.com/abedegno/muesli/internal/pluginkit"
)

// Stable detector identifiers. DetectorHistoricalFixed is the frozen 0.01
// comparator (HistoricalBaselineThreshold), not whatever production
// currently ships.
const (
	DetectorHistoricalFixed = "fixed_historical_0.01"
	DetectorAdaptive        = "adaptive"
	DetectorWhisperCPP      = "whisper_cpp"
)

// MaxWorkers bounds concurrent recording execution; each worker owns at
// most one recording (and thus one detector, one session) at a time.
const MaxWorkers = 4

// OptionalDetectorFactory reports whether a compatible optional detector
// (currently, whisper.cpp) is available through the existing detector
// contract, and if so, a factory for it.
type OptionalDetectorFactory func() (factory VADFactory, available bool, reason string)

// ProductionWhisperCPPFactory reflects the pinned whisper.cpp binding: it
// exposes VAD setters but only transcription segments, never frame
// decisions or VAD intervals, so no compatible detector interface is
// available. This is the sole expected "unavailable" state -- claiming
// compatibility and then failing is fatal, not another unavailable path.
func ProductionWhisperCPPFactory() (VADFactory, bool, string) {
	return nil, false, "the pinned whisper.cpp binding exposes VAD setters but only transcription segments, not frame decisions or VAD intervals -- no compatible detector interface is available"
}

// RecordingInput is one canonical prepared recording ready for the
// detector matrix.
type RecordingInput struct {
	ID        string
	MeetingID string
	Class     string
	Split     string
	Mic       string
	Audio     []float32
	Reference []score.Interval
}

// MatrixEntry is one (recording, detector) cell's result.
type MatrixEntry struct {
	RecordingID      string                 `json:"recording_id"`
	MeetingID        string                 `json:"meeting_id"`
	Class            string                 `json:"class"`
	Split            string                 `json:"split"`
	Mic              string                 `json:"mic"`
	DetectorID       string                 `json:"detector_id"`
	Threshold        *float64               `json:"threshold,omitempty"`
	MeasurementID    string                 `json:"measurement_id"`
	FrameMetrics     score.FrameMetrics     `json:"frame_metrics"`
	UtteranceMetrics score.UtteranceMetrics `json:"utterance_metrics"`
}

// Matrix is one split's full raw evaluation result. The historical
// baseline and grid are the evaluation's own constants; the runtime
// default is recorded separately as metadata only and never selects what
// is measured.
type Matrix struct {
	EvaluationVersion           string          `json:"evaluation_version"`
	Split                       string          `json:"split"`
	ThresholdGrid               []float64       `json:"threshold_grid"`
	GridExpansions              []GridExpansion `json:"grid_expansions"`
	HistoricalBaselineThreshold float64         `json:"historical_baseline_threshold"`
	RuntimeDefaultThreshold     float64         `json:"runtime_default_threshold"`
	WhisperAvailable            bool            `json:"whisper_available"`
	WhisperUnavailableReason    string          `json:"whisper_unavailable_reason,omitempty"`
	Entries                     []MatrixEntry   `json:"entries"`
}

// MatrixConfig controls matrix execution.
type MatrixConfig struct {
	// Workers bounds concurrent recording execution; 0 or 1 is sequential.
	// Values above MaxWorkers are clamped.
	Workers int
	// WhisperFactory selects the optional whisper.cpp detector source.
	// Nil selects ProductionWhisperCPPFactory.
	WhisperFactory OptionalDetectorFactory
	// Split labels every entry and the matrix itself. All recordings must
	// carry this split.
	Split string
	// RuntimeDefaultThreshold, when non-nil, overrides the recorded
	// runtime-default metadata (a test seam). It never changes what is
	// measured: the baseline is always HistoricalBaselineThreshold.
	RuntimeDefaultThreshold *float64
}

type job struct {
	detectorID    string
	threshold     *float64
	measurementID string
	vadFactory    VADFactory
	reuseFrom     string // non-empty: copy the named detector's result instead of running
}

func fixedFactory(threshold float64) VADFactory {
	return func() (pluginkit.VAD, error) {
		return pluginkit.EnergyVAD{Threshold: threshold}, nil
	}
}

func adaptiveFactory() (pluginkit.VAD, error) {
	cfg := pluginkit.DefaultAdaptiveVADConfig(SampleRate, time.Duration(VADFrameSamples)*time.Second/time.Duration(SampleRate))
	return pluginkit.NewAdaptiveEnergyVAD(cfg)
}

// buildJobs returns the detector job list shared by every recording, given
// the validated grid, the historical baseline, and optional whisper.cpp
// availability resolved once up front. The baseline is measured exactly
// once per recording; a grid point equal to it reuses that measurement.
func buildJobs(grid []float64, baseline float64, whisperFactory VADFactory, whisperAvailable bool) []job {
	var jobs []job
	b := baseline
	jobs = append(jobs, job{
		detectorID: DetectorHistoricalFixed, threshold: &b, measurementID: DetectorHistoricalFixed,
		vadFactory: fixedFactory(b),
	})
	for _, t := range grid {
		t := t
		id := FixedDetectorID(t)
		if t == baseline {
			jobs = append(jobs, job{detectorID: id, threshold: &t, measurementID: DetectorHistoricalFixed, reuseFrom: DetectorHistoricalFixed})
			continue
		}
		jobs = append(jobs, job{detectorID: id, threshold: &t, measurementID: id, vadFactory: fixedFactory(t)})
	}
	jobs = append(jobs, job{detectorID: DetectorAdaptive, measurementID: DetectorAdaptive, vadFactory: adaptiveFactory})
	if whisperAvailable {
		jobs = append(jobs, job{detectorID: DetectorWhisperCPP, measurementID: DetectorWhisperCPP, vadFactory: whisperFactory})
	}
	return jobs
}

// RunMatrix runs the full detector matrix over every recording. Each
// recording is processed by exactly one worker, sequentially through its
// own job list (so a reused grid point always follows the canonical run it
// copies), fresh detector and session state for every job.
func RunMatrix(ctx context.Context, recordings []RecordingInput, cfg MatrixConfig) (Matrix, error) {
	grid := ThresholdGrid()
	if err := ValidateGrid(grid); err != nil {
		return Matrix{}, fmt.Errorf("evaluate: %w", err)
	}
	for _, r := range recordings {
		if r.Split != cfg.Split {
			return Matrix{}, fmt.Errorf("evaluate: recording %s has split %q, matrix split is %q", r.ID, r.Split, cfg.Split)
		}
	}
	runtimeDefault := pluginkit.DefaultStreamingConfig().EnergyThreshold
	if cfg.RuntimeDefaultThreshold != nil {
		runtimeDefault = *cfg.RuntimeDefaultThreshold
	}

	whisperFactoryFn := cfg.WhisperFactory
	if whisperFactoryFn == nil {
		whisperFactoryFn = ProductionWhisperCPPFactory
	}
	whisperVADFactory, whisperAvailable, whisperReason := whisperFactoryFn()
	if whisperAvailable && whisperVADFactory == nil {
		return Matrix{}, fmt.Errorf("evaluate: optional detector factory claimed availability but supplied no VADFactory")
	}

	jobs := buildJobs(grid, HistoricalBaselineThreshold, whisperVADFactory, whisperAvailable)

	workers := cfg.Workers
	if workers < 1 {
		workers = 1
	}
	if workers > MaxWorkers {
		workers = MaxWorkers
	}

	type outcome struct {
		entries []MatrixEntry
		err     error
	}
	results := make([]outcome, len(recordings))
	work := make(chan int)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range work {
				entries, err := runRecording(runCtx, recordings[idx], jobs)
				results[idx] = outcome{entries: entries, err: err}
				if err != nil {
					cancel()
				}
			}
		}()
	}
	for i := range recordings {
		work <- i
	}
	close(work)
	wg.Wait()

	m := Matrix{
		EvaluationVersion:           EvaluationVersion,
		Split:                       cfg.Split,
		ThresholdGrid:               grid,
		GridExpansions:              GridExpansions(),
		HistoricalBaselineThreshold: HistoricalBaselineThreshold,
		RuntimeDefaultThreshold:     runtimeDefault,
		WhisperAvailable:            whisperAvailable,
		WhisperUnavailableReason:    whisperReason,
	}
	for i, o := range results {
		if o.err != nil {
			return Matrix{}, fmt.Errorf("evaluate: recording %s: %w", recordings[i].ID, o.err)
		}
		m.Entries = append(m.Entries, o.entries...)
	}
	sort.Slice(m.Entries, func(i, j int) bool {
		a, b := m.Entries[i], m.Entries[j]
		if a.MeetingID != b.MeetingID {
			return a.MeetingID < b.MeetingID
		}
		if a.Mic != b.Mic {
			return a.Mic < b.Mic
		}
		return detectorLess(a, b)
	})
	return m, nil
}

// detectorRank orders detectors within one recording: the historical
// baseline, then fixed grid candidates in numeric threshold order, then
// adaptive, then whisper.cpp.
func detectorRank(e MatrixEntry) int {
	switch {
	case e.DetectorID == DetectorHistoricalFixed:
		return 0
	case e.Threshold != nil:
		return 1
	case e.DetectorID == DetectorAdaptive:
		return 2
	default:
		return 3
	}
}

func detectorLess(a, b MatrixEntry) bool {
	ra, rb := detectorRank(a), detectorRank(b)
	if ra != rb {
		return ra < rb
	}
	if ra == 1 && *a.Threshold != *b.Threshold {
		return *a.Threshold < *b.Threshold
	}
	return a.DetectorID < b.DetectorID
}

func runRecording(ctx context.Context, rec RecordingInput, jobs []job) ([]MatrixEntry, error) {
	audioDuration := time.Duration(float64(len(rec.Audio)) / float64(SampleRate) * float64(time.Second))
	deadline := Deadline(audioDuration)

	byDetector := make(map[string]RunResult, len(jobs))
	entries := make([]MatrixEntry, 0, len(jobs))
	for _, j := range jobs {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		var result RunResult
		if j.reuseFrom != "" {
			r, ok := byDetector[j.reuseFrom]
			if !ok {
				return nil, fmt.Errorf("evaluate: internal: detector %s has no prior measurement %s to reuse", j.detectorID, j.reuseFrom)
			}
			result = r
		} else {
			r, err := Run(ctx, rec.Audio, j.vadFactory, deadline)
			if err != nil {
				return nil, fmt.Errorf("detector %s: %w", j.detectorID, err)
			}
			result = r
		}
		byDetector[j.detectorID] = result

		frameCounts := score.ScoreFrames(result.Frames, rec.Reference)
		frameMetrics := score.DeriveFrameMetrics(frameCounts)
		utteranceMetrics := score.ScoreUtterances(result.Predicted, rec.Reference, SampleRate)

		entries = append(entries, MatrixEntry{
			RecordingID:      rec.ID,
			MeetingID:        rec.MeetingID,
			Class:            rec.Class,
			Split:            rec.Split,
			Mic:              rec.Mic,
			DetectorID:       j.detectorID,
			Threshold:        j.threshold,
			MeasurementID:    j.measurementID,
			FrameMetrics:     frameMetrics,
			UtteranceMetrics: utteranceMetrics,
		})
	}
	return entries, nil
}
