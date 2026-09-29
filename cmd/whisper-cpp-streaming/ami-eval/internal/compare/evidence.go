package compare

import (
	"fmt"
	"math"

	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/evaluate"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/manifest"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/score"
)

// RecordingInfo identifies one recording of a split, in manifest order.
type RecordingInfo struct {
	RecordingID string `json:"recording_id"`
	MeetingID   string `json:"meeting_id"`
	Class       string `json:"class"`
	Mic         string `json:"mic"`
}

// Metrics is one frame + utterance metric set: either a single recording's
// measurement or an aggregate of recording ratios.
type Metrics struct {
	Frame     score.FrameMetrics     `json:"frame"`
	Utterance score.UtteranceMetrics `json:"utterance"`
}

// CellSummary is one descriptive class/microphone summary: the mean of the
// recording ratios in that cell, or n/a when the split has no recording in
// it (for example after a same-class held-out substitution). Cell summaries
// never feed the decision.
type CellSummary struct {
	Class          string  `json:"class"`
	Mic            string  `json:"mic"`
	RecordingCount int     `json:"recording_count"`
	Metrics        Metrics `json:"metrics"`
}

// DetectorSummary is one detector's results over one split.
type DetectorSummary struct {
	DetectorID    string   `json:"detector_id"`
	Threshold     *float64 `json:"threshold,omitempty"`
	MeasurementID string   `json:"measurement_id"`
	// PerRecording is aligned with SplitSummary.Recordings.
	PerRecording []Metrics `json:"per_recording"`
	// Cells is aligned with AllCells().
	Cells []CellSummary `json:"cells"`
	// Overall is the arithmetic mean of the split's recording ratios: two
	// equally weighted meetings, each with two equally weighted
	// microphones. Never pooled counts, never a mean of cell means.
	Overall Metrics `json:"overall"`
	// FarFieldFPR is the arithmetic mean of the split's fixed-distant
	// (Array1-01) recording frame false-positive rates.
	FarFieldFPR score.Ratio `json:"far_field_fpr"`
}

// SplitSummary is one split's validated, recording-weighted evidence.
type SplitSummary struct {
	Split                       manifest.Split  `json:"split"`
	MeetingIDs                  []string        `json:"meeting_ids"`
	Recordings                  []RecordingInfo `json:"recordings"`
	ThresholdGrid               []float64       `json:"threshold_grid"`
	HistoricalBaselineThreshold float64         `json:"historical_baseline_threshold"`
	WhisperAvailable            bool            `json:"whisper_available"`
	WhisperUnavailableReason    string          `json:"whisper_unavailable_reason,omitempty"`
	// Detectors is ordered: historical baseline, fixed grid ascending,
	// adaptive, then whisper.cpp when available.
	Detectors []DetectorSummary `json:"detectors"`
}

// Detector returns the summary for detectorID.
func (s SplitSummary) Detector(detectorID string) (DetectorSummary, bool) {
	for _, d := range s.Detectors {
		if d.DetectorID == detectorID {
			return d, true
		}
	}
	return DetectorSummary{}, false
}

// splitEvidence is the private, validated payload behind TuningEvidence and
// HeldOutEvidence.
type splitEvidence struct {
	summary SplitSummary
}

// TuningEvidence is validated evidence from the tuning split only. It can be
// constructed only by BuildTuningEvidence; its zero value is rejected by
// every consumer. The threshold selector accepts nothing else.
type TuningEvidence struct{ ev *splitEvidence }

// HeldOutEvidence is validated evidence from the held-out split only. It can
// be constructed only by BuildHeldOutEvidence; it never reaches selection.
type HeldOutEvidence struct{ ev *splitEvidence }

// Summary returns a copy of the validated tuning summary.
func (e TuningEvidence) Summary() (SplitSummary, error) {
	if e.ev == nil {
		return SplitSummary{}, fmt.Errorf("compare: tuning evidence is empty (not built by BuildTuningEvidence)")
	}
	return e.ev.summary, nil
}

// Summary returns a copy of the validated held-out summary.
func (e HeldOutEvidence) Summary() (SplitSummary, error) {
	if e.ev == nil {
		return SplitSummary{}, fmt.Errorf("compare: held-out evidence is empty (not built by BuildHeldOutEvidence)")
	}
	return e.ev.summary, nil
}

// BuildTuningEvidence validates a tuning-split matrix against the manifest
// and aggregates it. A matrix containing any held-out recording is rejected.
func BuildTuningEvidence(man *manifest.Manifest, m evaluate.Matrix) (TuningEvidence, error) {
	ev, err := buildSplitEvidence(man, m, manifest.SplitTuning)
	if err != nil {
		return TuningEvidence{}, err
	}
	return TuningEvidence{ev: ev}, nil
}

// BuildHeldOutEvidence validates a held-out-split matrix against the
// manifest and aggregates it. A matrix containing any tuning recording is
// rejected.
func BuildHeldOutEvidence(man *manifest.Manifest, m evaluate.Matrix) (HeldOutEvidence, error) {
	ev, err := buildSplitEvidence(man, m, manifest.SplitHeldOut)
	if err != nil {
		return HeldOutEvidence{}, err
	}
	return HeldOutEvidence{ev: ev}, nil
}

// expectedDetector is one detector every recording of a split must have.
type expectedDetector struct {
	id            string
	threshold     *float64
	measurementID string
	decision      bool // historical baseline or fixed grid candidate
}

func expectedDetectors(m evaluate.Matrix) []expectedDetector {
	baseline := m.HistoricalBaselineThreshold
	out := []expectedDetector{{
		id: evaluate.DetectorHistoricalFixed, threshold: &baseline,
		measurementID: evaluate.DetectorHistoricalFixed, decision: true,
	}}
	for _, t := range m.ThresholdGrid {
		t := t
		mid := evaluate.FixedDetectorID(t)
		if t == baseline {
			mid = evaluate.DetectorHistoricalFixed
		}
		out = append(out, expectedDetector{id: evaluate.FixedDetectorID(t), threshold: &t, measurementID: mid, decision: true})
	}
	out = append(out, expectedDetector{id: evaluate.DetectorAdaptive, measurementID: evaluate.DetectorAdaptive})
	if m.WhisperAvailable {
		out = append(out, expectedDetector{id: evaluate.DetectorWhisperCPP, measurementID: evaluate.DetectorWhisperCPP})
	}
	return out
}

func sameThreshold(a, b *float64) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	return a == nil || *a == *b
}

func buildSplitEvidence(man *manifest.Manifest, m evaluate.Matrix, split manifest.Split) (*splitEvidence, error) {
	if man == nil {
		return nil, fmt.Errorf("compare: %s evidence: manifest is required", split)
	}
	if err := man.Validate(); err != nil {
		return nil, fmt.Errorf("compare: %s evidence: %w", split, err)
	}
	if m.Split != string(split) {
		return nil, fmt.Errorf("compare: %s evidence: matrix is labelled split %q", split, m.Split)
	}
	if m.EvaluationVersion != evaluate.EvaluationVersion {
		return nil, fmt.Errorf("compare: %s evidence: matrix evaluation version %q, want %q", split, m.EvaluationVersion, evaluate.EvaluationVersion)
	}
	if m.HistoricalBaselineThreshold != evaluate.HistoricalBaselineThreshold {
		return nil, fmt.Errorf("compare: %s evidence: historical baseline %v, want %v", split, m.HistoricalBaselineThreshold, evaluate.HistoricalBaselineThreshold)
	}
	if err := evaluate.ValidateGrid(m.ThresholdGrid); err != nil {
		return nil, fmt.Errorf("compare: %s evidence: %w", split, err)
	}
	if !equalFloats(m.ThresholdGrid, evaluate.ThresholdGrid()) {
		return nil, fmt.Errorf("compare: %s evidence: matrix grid does not match the committed grid", split)
	}

	recs := man.RecordingsForSplit(split)
	recIndex := make(map[string]int, len(recs))
	infos := make([]RecordingInfo, len(recs))
	for i, r := range recs {
		recIndex[r.ID] = i
		infos[i] = RecordingInfo{RecordingID: r.ID, MeetingID: r.MeetingID, Class: r.Class, Mic: r.Mic}
	}
	detectors := expectedDetectors(m)
	detIndex := make(map[string]int, len(detectors))
	for i, d := range detectors {
		detIndex[d.id] = i
	}

	// grid[d][r] holds the single entry for detector d on recording r.
	grid := make([][]*evaluate.MatrixEntry, len(detectors))
	for i := range grid {
		grid[i] = make([]*evaluate.MatrixEntry, len(recs))
	}
	for i := range m.Entries {
		e := &m.Entries[i]
		ri, ok := recIndex[e.RecordingID]
		if !ok {
			return nil, fmt.Errorf("compare: %s evidence: unexpected recording %q (detector %s) -- not a %s recording of this manifest", split, e.RecordingID, e.DetectorID, split)
		}
		r := recs[ri]
		if e.Split != string(split) {
			return nil, fmt.Errorf("compare: %s evidence: recording %s detector %s labelled split %q", split, e.RecordingID, e.DetectorID, e.Split)
		}
		if e.MeetingID != r.MeetingID || e.Class != r.Class || e.Mic != r.Mic {
			return nil, fmt.Errorf("compare: %s evidence: recording %s detector %s metadata (meeting %q class %q mic %q) disagrees with manifest (meeting %q class %q mic %q)",
				split, e.RecordingID, e.DetectorID, e.MeetingID, e.Class, e.Mic, r.MeetingID, r.Class, r.Mic)
		}
		di, ok := detIndex[e.DetectorID]
		if !ok {
			return nil, fmt.Errorf("compare: %s evidence: recording %s has unexpected detector %q", split, e.RecordingID, e.DetectorID)
		}
		d := detectors[di]
		if !sameThreshold(e.Threshold, d.threshold) {
			return nil, fmt.Errorf("compare: %s evidence: recording %s detector %s threshold %v does not match its identity", split, e.RecordingID, e.DetectorID, e.Threshold)
		}
		if e.MeasurementID != d.measurementID {
			return nil, fmt.Errorf("compare: %s evidence: recording %s detector %s measurement %q, want %q", split, e.RecordingID, e.DetectorID, e.MeasurementID, d.measurementID)
		}
		if grid[di][ri] != nil {
			return nil, fmt.Errorf("compare: %s evidence: duplicate entry for recording %s detector %s", split, e.RecordingID, e.DetectorID)
		}
		grid[di][ri] = e
	}

	// Coverage before any averaging: a missing result must never improve
	// an average.
	for di, d := range detectors {
		for ri, r := range recs {
			if grid[di][ri] == nil {
				return nil, fmt.Errorf("compare: %s evidence: missing result for recording %s detector %s", split, r.ID, d.id)
			}
		}
	}

	// Decision metrics must be defined and finite for every fixed
	// candidate and the baseline on every recording.
	for di, d := range detectors {
		if !d.decision {
			continue
		}
		for ri, r := range recs {
			e := grid[di][ri]
			if err := requireRatio(e.FrameMetrics.F1, 0, 1, true); err != nil {
				return nil, fmt.Errorf("compare: %s evidence: recording %s detector %s frame F1: %w", split, r.ID, d.id, err)
			}
			if err := requireRatio(e.UtteranceMetrics.UtteranceErrorRate, 0, math.Inf(1), false); err != nil {
				return nil, fmt.Errorf("compare: %s evidence: recording %s detector %s utterance error rate: %w", split, r.ID, d.id, err)
			}
			if r.Mic == manifest.MicFixedDistant {
				if err := requireRatio(e.FrameMetrics.FalsePositiveRate, 0, 1, true); err != nil {
					return nil, fmt.Errorf("compare: %s evidence: recording %s detector %s far-field FPR: %w", split, r.ID, d.id, err)
				}
			}
		}
	}

	summary := SplitSummary{
		Split:                       split,
		MeetingIDs:                  man.MeetingIDs(split),
		Recordings:                  infos,
		ThresholdGrid:               append([]float64(nil), m.ThresholdGrid...),
		HistoricalBaselineThreshold: m.HistoricalBaselineThreshold,
		WhisperAvailable:            m.WhisperAvailable,
		WhisperUnavailableReason:    m.WhisperUnavailableReason,
	}
	for di, d := range detectors {
		per := make([]Metrics, len(recs))
		for ri := range recs {
			e := grid[di][ri]
			per[ri] = Metrics{Frame: e.FrameMetrics, Utterance: e.UtteranceMetrics}
		}
		ds := DetectorSummary{
			DetectorID:    d.id,
			Threshold:     d.threshold,
			MeasurementID: d.measurementID,
			PerRecording:  per,
			Overall:       meanMetrics(per),
		}
		var farField []score.Ratio
		for ri, r := range recs {
			if r.Mic == manifest.MicFixedDistant {
				farField = append(farField, per[ri].Frame.FalsePositiveRate)
			}
		}
		ds.FarFieldFPR = meanRatio(farField)
		for _, cell := range AllCells() {
			var in []Metrics
			for ri, r := range recs {
				if r.Class == cell.Class && r.Mic == cell.Mic {
					in = append(in, per[ri])
				}
			}
			cs := CellSummary{Class: cell.Class, Mic: cell.Mic, RecordingCount: len(in)}
			if len(in) > 0 {
				cs.Metrics = meanMetrics(in)
			}
			ds.Cells = append(ds.Cells, cs)
		}
		summary.Detectors = append(summary.Detectors, ds)
	}
	return &splitEvidence{summary: summary}, nil
}

// requireRatio rejects an undefined, non-finite, or out-of-range decision
// ratio. maxInclusive bounds F1/FPR to [0,1]; UER has no upper bound (split
// and spurious predictions can exceed the reference count).
func requireRatio(r score.Ratio, lo, hi float64, bounded bool) error {
	if !r.Valid {
		return fmt.Errorf("undefined (n/a)")
	}
	if math.IsNaN(r.Value) || math.IsInf(r.Value, 0) {
		return fmt.Errorf("not finite (%v)", r.Value)
	}
	if r.Value < lo || (bounded && r.Value > hi) {
		return fmt.Errorf("%v out of range", r.Value)
	}
	return nil
}

// meanRatio is the arithmetic mean of rs. It is valid only when every input
// is valid: an undefined constituent makes the aggregate n/a rather than
// being silently averaged away.
func meanRatio(rs []score.Ratio) score.Ratio {
	if len(rs) == 0 {
		return score.Ratio{}
	}
	var sum float64
	for _, r := range rs {
		if !r.Valid {
			return score.Ratio{}
		}
		sum += r.Value
	}
	return score.Ratio{Value: sum / float64(len(rs)), Valid: true}
}

// meanMetrics averages every ratio across ms (recording-weighted). Counts
// are summed for descriptive display only; no aggregate ratio is derived
// from pooled counts.
func meanMetrics(ms []Metrics) Metrics {
	pick := func(f func(Metrics) score.Ratio) score.Ratio {
		rs := make([]score.Ratio, len(ms))
		for i, m := range ms {
			rs[i] = f(m)
		}
		return meanRatio(rs)
	}
	var out Metrics
	for _, m := range ms {
		out.Frame.Counts.TP += m.Frame.Counts.TP
		out.Frame.Counts.FP += m.Frame.Counts.FP
		out.Frame.Counts.TN += m.Frame.Counts.TN
		out.Frame.Counts.FN += m.Frame.Counts.FN
		c := &out.Utterance.Counts
		mc := m.Utterance.Counts
		c.ReferenceCount += mc.ReferenceCount
		c.PredictionCount += mc.PredictionCount
		c.Misses += mc.Misses
		c.SplitExtras += mc.SplitExtras
		c.MergeExtras += mc.MergeExtras
		c.Spurious += mc.Spurious
		c.OneToOnePairs += mc.OneToOnePairs
	}
	out.Frame.Precision = pick(func(m Metrics) score.Ratio { return m.Frame.Precision })
	out.Frame.Recall = pick(func(m Metrics) score.Ratio { return m.Frame.Recall })
	out.Frame.F1 = pick(func(m Metrics) score.Ratio { return m.Frame.F1 })
	out.Frame.FalsePositiveRate = pick(func(m Metrics) score.Ratio { return m.Frame.FalsePositiveRate })
	out.Frame.FalseNegativeRate = pick(func(m Metrics) score.Ratio { return m.Frame.FalseNegativeRate })
	u := &out.Utterance
	u.MissRate = pick(func(m Metrics) score.Ratio { return m.Utterance.MissRate })
	u.SplitExtraRate = pick(func(m Metrics) score.Ratio { return m.Utterance.SplitExtraRate })
	u.MergeExtraRate = pick(func(m Metrics) score.Ratio { return m.Utterance.MergeExtraRate })
	u.SpuriousRate = pick(func(m Metrics) score.Ratio { return m.Utterance.SpuriousRate })
	u.UtteranceErrorRate = pick(func(m Metrics) score.Ratio { return m.Utterance.UtteranceErrorRate })
	u.StartErrorMedian = pick(func(m Metrics) score.Ratio { return m.Utterance.StartErrorMedian })
	u.StartErrorP95 = pick(func(m Metrics) score.Ratio { return m.Utterance.StartErrorP95 })
	u.EndErrorMedian = pick(func(m Metrics) score.Ratio { return m.Utterance.EndErrorMedian })
	u.EndErrorP95 = pick(func(m Metrics) score.Ratio { return m.Utterance.EndErrorP95 })
	return out
}

func equalFloats(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
