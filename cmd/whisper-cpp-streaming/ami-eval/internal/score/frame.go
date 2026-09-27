// Package score computes detector performance from production streaming
// output against merged AMI reference intervals: duration-weighted frame
// metrics at the production 20ms quantum, and utterance-level overlap
// metrics from final session intervals (muesli#778). Every ratio with a
// zero denominator is explicitly n/a rather than a divide-by-zero NaN or a
// silently misleading zero.
package score

import "sort"

// Interval is a half-open [Start, End) span of sample indices.
type Interval struct {
	Start int64
	End   int64
}

func (iv Interval) valid() bool { return iv.End > iv.Start }

// overlaps reports whether two spans share a positive-duration
// intersection.
func overlaps(a, b Interval) bool {
	lo := a.Start
	if b.Start > lo {
		lo = b.Start
	}
	hi := a.End
	if b.End < hi {
		hi = b.End
	}
	return hi > lo
}

// Ratio is a rate or percentage with an explicit validity flag: Valid is
// false exactly when the ratio's denominator was zero, in which case
// callers must render "n/a" rather than a numeric value.
type Ratio struct {
	Value float64
	Valid bool
}

func ratio(numerator, denominator float64) Ratio {
	if denominator == 0 {
		return Ratio{}
	}
	return Ratio{Value: numerator / denominator, Valid: true}
}

// Frame is one 20ms-quantum VAD decision, expressed in absolute sample
// coordinates. ValidSamples is the count of samples within
// [Start, Start+ValidSamples) that are real, unpadded original audio;
// padding and flush frames report a smaller (possibly zero) ValidSamples
// than End-Start. A frame with ValidSamples == 0 contributes no weight.
type Frame struct {
	Start        int64
	End          int64
	ValidSamples int64
	Predicted    bool
}

// validSpan returns the frame's unpadded [Start, Start+ValidSamples) span,
// or false if it carries no scorable weight.
func (f Frame) validSpan() (Interval, bool) {
	if f.ValidSamples <= 0 {
		return Interval{}, false
	}
	span := Interval{Start: f.Start, End: f.Start + f.ValidSamples}
	if !span.valid() {
		return Interval{}, false
	}
	return span, true
}

// FrameCounts holds raw duration-weighted (in samples) confusion-matrix
// accumulations.
type FrameCounts struct {
	TP float64
	FP float64
	TN float64
	FN float64
}

// referencePositive reports whether any positive-duration part of span
// intersects the (assumed sorted, non-overlapping) merged reference
// intervals.
func referencePositive(span Interval, sortedRefs []Interval) bool {
	// First reference interval whose End exceeds span.Start.
	idx := sort.Search(len(sortedRefs), func(i int) bool { return sortedRefs[i].End > span.Start })
	for i := idx; i < len(sortedRefs) && sortedRefs[i].Start < span.End; i++ {
		if overlaps(span, sortedRefs[i]) {
			return true
		}
	}
	return false
}

// ScoreFrames accumulates duration-weighted TP/FP/TN/FN over every frame
// against the merged reference intervals. reference need not be
// pre-sorted; ScoreFrames sorts a copy.
func ScoreFrames(frames []Frame, reference []Interval) FrameCounts {
	refs := append([]Interval(nil), reference...)
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Start != refs[j].Start {
			return refs[i].Start < refs[j].Start
		}
		return refs[i].End < refs[j].End
	})

	var counts FrameCounts
	for _, f := range frames {
		span, ok := f.validSpan()
		if !ok {
			continue
		}
		weight := float64(f.ValidSamples)
		actual := referencePositive(span, refs)
		switch {
		case f.Predicted && actual:
			counts.TP += weight
		case f.Predicted && !actual:
			counts.FP += weight
		case !f.Predicted && actual:
			counts.FN += weight
		default:
			counts.TN += weight
		}
	}
	return counts
}

// FrameMetrics derives standard rates from raw weighted counts. Every
// ratio is n/a when its denominator is zero.
type FrameMetrics struct {
	Counts            FrameCounts
	Precision         Ratio
	Recall            Ratio
	F1                Ratio
	FalsePositiveRate Ratio
	FalseNegativeRate Ratio
}

// DeriveFrameMetrics computes precision, recall, F1, false-positive rate,
// and false-negative rate from raw weighted counts.
func DeriveFrameMetrics(c FrameCounts) FrameMetrics {
	precision := ratio(c.TP, c.TP+c.FP)
	recall := ratio(c.TP, c.TP+c.FN)
	var f1 Ratio
	if precision.Valid && recall.Valid && (precision.Value+recall.Value) > 0 {
		f1 = Ratio{Value: 2 * precision.Value * recall.Value / (precision.Value + recall.Value), Valid: true}
	}
	fpr := ratio(c.FP, c.FP+c.TN)
	fnr := ratio(c.FN, c.FN+c.TP)
	return FrameMetrics{
		Counts:            c,
		Precision:         precision,
		Recall:            recall,
		F1:                f1,
		FalsePositiveRate: fpr,
		FalseNegativeRate: fnr,
	}
}
