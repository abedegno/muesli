package compare

import (
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/evaluate"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/score"
)

// CandidateEvidence is one recommendation candidate's overall evidence and
// its delta against historical fixed 0.01, for transparent, structured reporting.
type CandidateEvidence struct {
	DetectorID                string
	Threshold                 *float64
	Eligible                  bool
	OverallF1                 score.Ratio
	OverallUtteranceErrorRate score.Ratio
	F1DeltaVsHistorical       score.Ratio // candidate F1 - shipped F1 per cell would be more detailed; this is overall-level
}

// Recommendation is the evidence-based default recommendation. It changes
// no runtime setting; it is evidence for a human decision.
type Recommendation struct {
	RecommendedDetectorID string
	SelectedThreshold     *float64
	Candidates            []CandidateEvidence
}

// candidateSpec names one recommendation candidate by detector ID, with an
// optional threshold for display.
type candidateSpec struct {
	detectorID string
	threshold  *float64
}

// Recommend selects the default recommendation from overall results.
// Eligibility is the one-sided floor: a candidate qualifies only if no
// cell's frame F1 falls more than EligibilityMargin below historical fixed 0.01's
// F1 in that same cell; there is no ceiling, so a candidate that beats
// shipped by any amount remains eligible. Among eligible candidates, the
// highest overall macro F1 wins; ties (at full float64 precision) go to
// the lowest overall utterance error rate, then to historical fixed 0.01. If no
// non-baseline candidate is eligible, historical fixed 0.01 is recommended.
func Recommend(overall []OverallResult, selectedThreshold ThresholdPoint, historicalThreshold float64, whisperAvailable bool) (Recommendation, error) {
	byID := make(map[string]OverallResult, len(overall))
	for _, o := range overall {
		byID[o.DetectorID] = o
	}
	shipped, ok := byID["fixed_historical_0.01"]
	if !ok {
		return Recommendation{}, errMissingDetector("fixed_historical_0.01")
	}

	var specs []candidateSpec
	specs = append(specs, candidateSpec{detectorID: "fixed_historical_0.01", threshold: &historicalThreshold})
	selectedID := evaluate.FixedDetectorID(selectedThreshold.Threshold)
	if selectedThreshold.Threshold != historicalThreshold {
		t := selectedThreshold.Threshold
		specs = append(specs, candidateSpec{detectorID: selectedID, threshold: &t})
	}
	if _, ok := byID["adaptive"]; ok {
		specs = append(specs, candidateSpec{detectorID: "adaptive"})
	}
	if whisperAvailable {
		if _, ok := byID["whisper_cpp"]; ok {
			specs = append(specs, candidateSpec{detectorID: "whisper_cpp"})
		}
	}

	var candidates []CandidateEvidence
	for _, spec := range specs {
		o, ok := byID[spec.detectorID]
		if !ok {
			continue
		}
		eligible := spec.detectorID == "fixed_historical_0.01" || eligibleAgainstHistorical(o, shipped)
		candidates = append(candidates, CandidateEvidence{
			DetectorID:                spec.detectorID,
			Threshold:                 spec.threshold,
			Eligible:                  eligible,
			OverallF1:                 o.FrameMetrics.F1,
			OverallUtteranceErrorRate: o.UtteranceMetrics.UtteranceErrorRate,
			F1DeltaVsHistorical:       deltaRatio(o.FrameMetrics.F1, shipped.FrameMetrics.F1),
		})
	}

	best := candidates[0]
	for _, c := range candidates[1:] {
		if !c.Eligible {
			continue
		}
		if !best.Eligible {
			best = c
			continue
		}
		switch compareF1(c.OverallF1, best.OverallF1) {
		case 1:
			best = c
		case 0:
			switch compareUtteranceErrorAsc(c.OverallUtteranceErrorRate, best.OverallUtteranceErrorRate) {
			case -1:
				best = c
			case 0:
				if best.DetectorID != "fixed_historical_0.01" && c.DetectorID == "fixed_historical_0.01" {
					best = c
				}
			}
		}
	}

	rec := Recommendation{RecommendedDetectorID: best.DetectorID, SelectedThreshold: best.Threshold, Candidates: candidates}
	return rec, nil
}

// compareF1 returns 1 if a>b, -1 if a<b, 0 if equal-or-incomparable
// (invalid ratios sort as lowest).
func compareF1(a, b score.Ratio) int {
	if !a.Valid && !b.Valid {
		return 0
	}
	if !a.Valid {
		return -1
	}
	if !b.Valid {
		return 1
	}
	switch {
	case a.Value > b.Value:
		return 1
	case a.Value < b.Value:
		return -1
	default:
		return 0
	}
}

// compareUtteranceErrorAsc returns -1 if a is a strictly better (lower)
// utterance error rate than b, 1 if worse, 0 if equal-or-incomparable.
func compareUtteranceErrorAsc(a, b score.Ratio) int {
	if !a.Valid && !b.Valid {
		return 0
	}
	if !a.Valid {
		return 1 // n/a is not preferable to a known rate
	}
	if !b.Valid {
		return -1
	}
	switch {
	case a.Value < b.Value:
		return -1
	case a.Value > b.Value:
		return 1
	default:
		return 0
	}
}

func eligibleAgainstHistorical(candidate, shipped OverallResult) bool {
	for _, cell := range AllCells() {
		c, cok := candidate.CellFrameF1[cell]
		s, sok := shipped.CellFrameF1[cell]
		if !cok || !sok || !c.Valid || !s.Valid {
			return false
		}
		if c.Value < s.Value-EligibilityMargin {
			return false
		}
	}
	return true
}

func deltaRatio(a, b score.Ratio) score.Ratio {
	if !a.Valid || !b.Valid {
		return score.Ratio{}
	}
	return score.Ratio{Value: a.Value - b.Value, Valid: true}
}

type errMissingDetector string

func (e errMissingDetector) Error() string {
	return "compare: missing required detector overall result: " + string(e)
}
