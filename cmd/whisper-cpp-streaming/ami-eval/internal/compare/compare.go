// Package compare aggregates per-recording matrix results into cell and
// overall detector comparisons and produces the evidence-based default
// recommendation (muesli#778). Aggregation never duration-weights: cells
// macro-average their recordings and the overall comparison equally
// macro-averages the four cells, so duration and speech prevalence cannot
// dominate. The optimum threshold is explicitly labeled in-sample, not a
// held-out validation result, and the recommendation changes no runtime
// setting.
package compare

import (
	"fmt"
	"sort"

	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/evaluate"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/score"
)

// EligibilityMargin is the one-sided floor: a candidate is eligible only if
// no cell's F1 is more than this many points below shipped fixed's F1 in
// that cell. There is no ceiling.
const EligibilityMargin = 0.05

// CellKey identifies one meeting-class/microphone-condition cell.
type CellKey struct {
	Class string
	Mic   string
}

// AllCells returns the four required cells in stable order.
func AllCells() []CellKey {
	return []CellKey{
		{Class: "scenario", Mic: "headset"},
		{Class: "scenario", Mic: "fixed_distant"},
		{Class: "non_scenario", Mic: "headset"},
		{Class: "non_scenario", Mic: "fixed_distant"},
	}
}

// averageRatio macro-averages the Valid values of rs. A ratio with an
// invalid (n/a) value does not contribute a zero; if no input is valid,
// the result is n/a.
func averageRatio(rs []score.Ratio) score.Ratio {
	var sum float64
	var n int
	for _, r := range rs {
		if r.Valid {
			sum += r.Value
			n++
		}
	}
	if n == 0 {
		return score.Ratio{}
	}
	return score.Ratio{Value: sum / float64(n), Valid: true}
}

func averageFrameMetrics(ms []score.FrameMetrics) score.FrameMetrics {
	get := func(f func(score.FrameMetrics) score.Ratio) score.Ratio {
		rs := make([]score.Ratio, len(ms))
		for i, m := range ms {
			rs[i] = f(m)
		}
		return averageRatio(rs)
	}
	var counts score.FrameCounts
	for _, m := range ms {
		counts.TP += m.Counts.TP
		counts.FP += m.Counts.FP
		counts.TN += m.Counts.TN
		counts.FN += m.Counts.FN
	}
	return score.FrameMetrics{
		Counts:            counts,
		Precision:         get(func(m score.FrameMetrics) score.Ratio { return m.Precision }),
		Recall:            get(func(m score.FrameMetrics) score.Ratio { return m.Recall }),
		F1:                get(func(m score.FrameMetrics) score.Ratio { return m.F1 }),
		FalsePositiveRate: get(func(m score.FrameMetrics) score.Ratio { return m.FalsePositiveRate }),
		FalseNegativeRate: get(func(m score.FrameMetrics) score.Ratio { return m.FalseNegativeRate }),
	}
}

func averageUtteranceMetrics(ms []score.UtteranceMetrics) score.UtteranceMetrics {
	get := func(f func(score.UtteranceMetrics) score.Ratio) score.Ratio {
		rs := make([]score.Ratio, len(ms))
		for i, m := range ms {
			rs[i] = f(m)
		}
		return averageRatio(rs)
	}
	var counts score.UtteranceCounts
	for _, m := range ms {
		counts.ReferenceCount += m.Counts.ReferenceCount
		counts.PredictionCount += m.Counts.PredictionCount
		counts.Misses += m.Counts.Misses
		counts.SplitExtras += m.Counts.SplitExtras
		counts.MergeExtras += m.Counts.MergeExtras
		counts.Spurious += m.Counts.Spurious
		counts.OneToOnePairs += m.Counts.OneToOnePairs
	}
	return score.UtteranceMetrics{
		Counts:             counts,
		MissRate:           get(func(m score.UtteranceMetrics) score.Ratio { return m.MissRate }),
		SplitExtraRate:     get(func(m score.UtteranceMetrics) score.Ratio { return m.SplitExtraRate }),
		MergeExtraRate:     get(func(m score.UtteranceMetrics) score.Ratio { return m.MergeExtraRate }),
		SpuriousRate:       get(func(m score.UtteranceMetrics) score.Ratio { return m.SpuriousRate }),
		UtteranceErrorRate: get(func(m score.UtteranceMetrics) score.Ratio { return m.UtteranceErrorRate }),
		StartErrorMedian:   get(func(m score.UtteranceMetrics) score.Ratio { return m.StartErrorMedian }),
		StartErrorP95:      get(func(m score.UtteranceMetrics) score.Ratio { return m.StartErrorP95 }),
		EndErrorMedian:     get(func(m score.UtteranceMetrics) score.Ratio { return m.EndErrorMedian }),
		EndErrorP95:        get(func(m score.UtteranceMetrics) score.Ratio { return m.EndErrorP95 }),
	}
}

// CellResult is one detector's macro-averaged result over the recordings in
// one cell.
type CellResult struct {
	Cell             CellKey
	DetectorID       string
	RecordingCount   int
	FrameMetrics     score.FrameMetrics
	UtteranceMetrics score.UtteranceMetrics
}

// AggregateCells groups matrix entries by (cell, detector) and
// macro-averages the recordings within each group.
func AggregateCells(entries []evaluate.MatrixEntry) []CellResult {
	type key struct {
		cell       CellKey
		detectorID string
	}
	groups := map[key][]evaluate.MatrixEntry{}
	var order []key
	for _, e := range entries {
		k := key{cell: CellKey{Class: e.Class, Mic: e.Mic}, detectorID: e.DetectorID}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], e)
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].cell != order[j].cell {
			if order[i].cell.Class != order[j].cell.Class {
				return order[i].cell.Class < order[j].cell.Class
			}
			return order[i].cell.Mic < order[j].cell.Mic
		}
		return order[i].detectorID < order[j].detectorID
	})

	results := make([]CellResult, 0, len(order))
	for _, k := range order {
		group := groups[k]
		frameMetrics := make([]score.FrameMetrics, len(group))
		utteranceMetrics := make([]score.UtteranceMetrics, len(group))
		for i, e := range group {
			frameMetrics[i] = e.FrameMetrics
			utteranceMetrics[i] = e.UtteranceMetrics
		}
		results = append(results, CellResult{
			Cell:             k.cell,
			DetectorID:       k.detectorID,
			RecordingCount:   len(group),
			FrameMetrics:     averageFrameMetrics(frameMetrics),
			UtteranceMetrics: averageUtteranceMetrics(utteranceMetrics),
		})
	}
	return results
}

// OverallResult is one detector's equal-weight macro-average over the four
// required cells.
type OverallResult struct {
	DetectorID       string
	FrameMetrics     score.FrameMetrics
	UtteranceMetrics score.UtteranceMetrics
	CellFrameF1      map[CellKey]score.Ratio
}

// AggregateOverall equally macro-averages every detector's four cells. It
// fails if any detector is missing any of the four required cells.
func AggregateOverall(cells []CellResult) ([]OverallResult, error) {
	byDetector := map[string][]CellResult{}
	var order []string
	seen := map[string]bool{}
	for _, c := range cells {
		if !seen[c.DetectorID] {
			seen[c.DetectorID] = true
			order = append(order, c.DetectorID)
		}
		byDetector[c.DetectorID] = append(byDetector[c.DetectorID], c)
	}
	sort.Strings(order)

	required := AllCells()
	results := make([]OverallResult, 0, len(order))
	for _, id := range order {
		group := byDetector[id]
		have := map[CellKey]CellResult{}
		for _, c := range group {
			have[c.Cell] = c
		}
		frameF1 := make(map[CellKey]score.Ratio, len(required))
		frameMetrics := make([]score.FrameMetrics, 0, len(required))
		utteranceMetrics := make([]score.UtteranceMetrics, 0, len(required))
		for _, want := range required {
			c, ok := have[want]
			if !ok {
				return nil, fmt.Errorf("compare: detector %q is missing cell %+v", id, want)
			}
			frameF1[want] = c.FrameMetrics.F1
			frameMetrics = append(frameMetrics, c.FrameMetrics)
			utteranceMetrics = append(utteranceMetrics, c.UtteranceMetrics)
		}
		results = append(results, OverallResult{
			DetectorID:       id,
			FrameMetrics:     averageFrameMetrics(frameMetrics),
			UtteranceMetrics: averageUtteranceMetrics(utteranceMetrics),
			CellFrameF1:      frameF1,
		})
	}
	return results, nil
}

// ThresholdPoint is one fixed-threshold candidate's overall macro frame F1.
type ThresholdPoint struct {
	Threshold float64
	F1        score.Ratio
}

// ThresholdCurve extracts the overall macro F1 for every fixed-threshold
// grid detector, sorted ascending by threshold. The optimum found here is
// in-sample: it is selected against the same recordings it is scored on,
// not held-out validation data.
func ThresholdCurve(overall []OverallResult, grid []float64) []ThresholdPoint {
	byID := make(map[string]OverallResult, len(overall))
	for _, o := range overall {
		byID[o.DetectorID] = o
	}
	curve := make([]ThresholdPoint, 0, len(grid))
	for _, t := range grid {
		o, ok := byID[evaluate.FixedDetectorID(t)]
		if !ok {
			continue
		}
		curve = append(curve, ThresholdPoint{Threshold: t, F1: o.FrameMetrics.F1})
	}
	sort.Slice(curve, func(i, j int) bool { return curve[i].Threshold < curve[j].Threshold })
	return curve
}

// SelectThreshold picks the maximum-F1 point on the curve. Exact ties (at
// full float64 precision) prefer the shipped threshold, then the lower
// threshold value.
func SelectThreshold(curve []ThresholdPoint, shippedThreshold float64) (ThresholdPoint, error) {
	if len(curve) == 0 {
		return ThresholdPoint{}, fmt.Errorf("compare: empty threshold curve")
	}
	best := curve[0]
	for _, p := range curve[1:] {
		switch {
		case !best.F1.Valid && p.F1.Valid:
			best = p
		case p.F1.Valid && best.F1.Valid && p.F1.Value > best.F1.Value:
			best = p
		case p.F1.Valid && best.F1.Valid && p.F1.Value == best.F1.Value:
			best = breakThresholdTie(best, p, shippedThreshold)
		}
	}
	return best, nil
}

func breakThresholdTie(a, b ThresholdPoint, shipped float64) ThresholdPoint {
	aShipped := a.Threshold == shipped
	bShipped := b.Threshold == shipped
	if aShipped != bShipped {
		if aShipped {
			return a
		}
		return b
	}
	if a.Threshold <= b.Threshold {
		return a
	}
	return b
}
