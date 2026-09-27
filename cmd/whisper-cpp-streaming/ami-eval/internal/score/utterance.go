package score

import (
	"math"
	"sort"
)

// UtteranceCounts holds the raw bipartite overlap classification between
// final predicted intervals and reference intervals.
type UtteranceCounts struct {
	ReferenceCount  int
	PredictionCount int
	Misses          int // references with zero overlapping predictions
	SplitExtras     int // sum over references of (overlapping predictions - 1), when >= 1
	MergeExtras     int // sum over predictions of (overlapping references - 1), when >= 1
	Spurious        int // predictions with zero overlapping references
	OneToOnePairs   int // references and predictions that overlap exactly each other
}

// UtteranceMetrics derives rates from UtteranceCounts, plus boundary-error
// statistics over one-to-one pairs.
type UtteranceMetrics struct {
	Counts             UtteranceCounts
	MissRate           Ratio
	SplitExtraRate     Ratio
	MergeExtraRate     Ratio
	SpuriousRate       Ratio
	UtteranceErrorRate Ratio
	StartErrorMedian   Ratio // seconds
	StartErrorP95      Ratio // seconds
	EndErrorMedian     Ratio // seconds
	EndErrorP95        Ratio // seconds
}

// ScoreUtterances builds the bipartite overlap graph between predictions
// (final session intervals, already clipped to original audio extent) and
// references (merged reference intervals) using positive-duration overlap,
// classifies misses/splits/merges/spurious predictions from each side's
// overlap degree, and reports median/95th-percentile absolute start/end
// boundary error (in seconds, using sampleRate) over one-to-one pairs --
// references and predictions that overlap exactly each other and nothing
// else.
func ScoreUtterances(predictions, references []Interval, sampleRate int) UtteranceMetrics {
	preds := append([]Interval(nil), predictions...)
	refs := append([]Interval(nil), references...)
	sort.Slice(preds, func(i, j int) bool { return preds[i].Start < preds[j].Start })
	sort.Slice(refs, func(i, j int) bool { return refs[i].Start < refs[j].Start })

	predDegree := make([]int, len(preds))
	refDegree := make([]int, len(refs))
	// oneToOne[i] records the matching ref index for prediction i, valid
	// only once both degrees are known to be exactly 1.
	predPartner := make([]int, len(preds))
	for i := range predPartner {
		predPartner[i] = -1
	}
	refPartner := make([]int, len(refs))
	for i := range refPartner {
		refPartner[i] = -1
	}

	// All-pairs overlap check. Utterance counts per recording are modest
	// (session-emitted final segments), so O(n*m) is simple and fast
	// enough; a merged/sorted sweep would only matter at far larger scale.
	for i, p := range preds {
		for j, r := range refs {
			if overlaps(p, r) {
				predDegree[i]++
				refDegree[j]++
				predPartner[i] = j
				refPartner[j] = i
			}
		}
	}

	var counts UtteranceCounts
	counts.ReferenceCount = len(refs)
	counts.PredictionCount = len(preds)

	for j := range refs {
		switch {
		case refDegree[j] == 0:
			counts.Misses++
		default:
			counts.SplitExtras += refDegree[j] - 1
		}
	}
	for i := range preds {
		switch {
		case predDegree[i] == 0:
			counts.Spurious++
		default:
			counts.MergeExtras += predDegree[i] - 1
		}
	}

	var startErrors, endErrors []float64
	for i, p := range preds {
		if predDegree[i] != 1 {
			continue
		}
		j := predPartner[i]
		if refDegree[j] != 1 || refPartner[j] != i {
			continue
		}
		r := refs[j]
		counts.OneToOnePairs++
		startErrors = append(startErrors, math.Abs(float64(p.Start-r.Start))/float64(sampleRate))
		endErrors = append(endErrors, math.Abs(float64(p.End-r.End))/float64(sampleRate))
	}

	refCount := float64(counts.ReferenceCount)
	predCount := float64(counts.PredictionCount)
	metrics := UtteranceMetrics{
		Counts:             counts,
		MissRate:           ratio(float64(counts.Misses), refCount),
		SplitExtraRate:     ratio(float64(counts.SplitExtras), refCount),
		MergeExtraRate:     ratio(float64(counts.MergeExtras), predCount),
		SpuriousRate:       ratio(float64(counts.Spurious), predCount),
		UtteranceErrorRate: ratio(float64(counts.Misses+counts.SplitExtras+counts.MergeExtras+counts.Spurious), refCount),
	}
	if len(startErrors) > 0 {
		metrics.StartErrorMedian = Ratio{Value: Median(startErrors), Valid: true}
		metrics.StartErrorP95 = Ratio{Value: P95NearestRank(startErrors), Valid: true}
		metrics.EndErrorMedian = Ratio{Value: Median(endErrors), Valid: true}
		metrics.EndErrorP95 = Ratio{Value: P95NearestRank(endErrors), Valid: true}
	}
	return metrics
}

// Median returns the middle value of values for an odd count, or the
// arithmetic mean of the two middle values for an even count. values need
// not be pre-sorted; Median sorts a copy. Callers must not call Median with
// an empty slice.
func Median(values []float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// P95NearestRank returns the 95th percentile by nearest rank: sorted index
// ceil(0.95*n), one-indexed. values need not be pre-sorted; P95NearestRank
// sorts a copy. Callers must not call P95NearestRank with an empty slice.
func P95NearestRank(values []float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	n := len(sorted)
	rank := int(math.Ceil(0.95 * float64(n)))
	if rank < 1 {
		rank = 1
	}
	if rank > n {
		rank = n
	}
	return sorted[rank-1]
}
