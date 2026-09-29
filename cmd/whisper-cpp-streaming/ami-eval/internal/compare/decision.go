package compare

import (
	"errors"
	"fmt"

	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/evaluate"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/score"
)

// FarFieldFPRCeiling is the tuning eligibility ceiling on the mean
// Array1-01 frame false-positive rate (inclusive).
const FarFieldFPRCeiling = 0.5

// Decision outcomes.
const (
	OutcomeShip = "ship"
	OutcomeKeep = "keep"
)

// Candidate is one tuning grid point's selection evidence, at full stored
// precision.
type Candidate struct {
	Threshold       float64  `json:"threshold"`
	DetectorID      string   `json:"detector_id"`
	F1              float64  `json:"overall_frame_f1"`
	UtteranceError  float64  `json:"overall_utterance_error_rate"`
	FarFieldFPR     float64  `json:"far_field_fpr"`
	MeetsF1Floor    bool     `json:"meets_f1_floor"`
	MeetsFPRCeiling bool     `json:"meets_fpr_ceiling"`
	Eligible        bool     `json:"eligible"`
	RejectedBecause []string `json:"rejected_because,omitempty"`
}

// Selection is the frozen tuning-only threshold selection.
type Selection struct {
	ThresholdGrid               []float64   `json:"threshold_grid"`
	HistoricalBaselineThreshold float64     `json:"historical_baseline_threshold"`
	BaselineF1                  float64     `json:"baseline_overall_frame_f1"`
	BaselineUtteranceError      float64     `json:"baseline_overall_utterance_error_rate"`
	BaselineFarFieldFPR         float64     `json:"baseline_far_field_fpr"`
	FarFieldFPRCeiling          float64     `json:"far_field_fpr_ceiling"`
	Candidates                  []Candidate `json:"candidates"`
	// Selected is nil when no candidate is eligible (or when the
	// constrained winner is a grid edge, which is an error).
	Selected         *float64 `json:"selected,omitempty"`
	LowerNeighbor    *float64 `json:"lower_neighbor,omitempty"`
	HigherNeighbor   *float64 `json:"higher_neighbor,omitempty"`
	NoEligibleReason string   `json:"no_eligible_reason,omitempty"`
}

// GridEdgeError reports that the constrained winner is the lowest or
// highest evaluated grid point. It is fatal: the grid must be deliberately
// expanded by a decade at that end and tuning rerun before any held-out
// detector run.
type GridEdgeError struct {
	Direction string // evaluate.ExpandLower or evaluate.ExpandUpper
	Threshold float64
	GridMin   float64
	GridMax   float64
}

func (e *GridEdgeError) Error() string {
	return fmt.Sprintf("compare: constrained tuning winner %s is the %s grid endpoint (grid %s..%s); expand the grid by one decade at the %s end (evaluate.gridExpansionSteps) and rerun tuning -- no held-out evaluation was run",
		evaluate.ThresholdLabel(e.Threshold), e.Direction, evaluate.ThresholdLabel(e.GridMin), evaluate.ThresholdLabel(e.GridMax), e.Direction)
}

// IsGridEdge reports whether err is (or wraps) a GridEdgeError.
func IsGridEdge(err error) (*GridEdgeError, bool) {
	var ge *GridEdgeError
	if errors.As(err, &ge) {
		return ge, true
	}
	return nil, false
}

func overallValue(r score.Ratio, what, detectorID string) (float64, error) {
	if err := requireRatio(r, 0, 0, false); err != nil {
		return 0, fmt.Errorf("compare: %s for %s: %w", what, detectorID, err)
	}
	return r.Value, nil
}

// SelectThreshold selects a fixed threshold from tuning evidence alone.
// A grid point is eligible when its overall frame F1 is at least the
// historical baseline's tuning F1 and its far-field FPR is at most
// FarFieldFPRCeiling. Among eligible points the lowest overall utterance
// error rate wins; exact ties at full precision choose the higher
// threshold. The baseline value 0.01 is an ordinary candidate. When no
// point is eligible, the Selection records why and selects nothing. When
// the winner is a grid endpoint, the Selection is returned together with a
// *GridEdgeError.
func SelectThreshold(ev TuningEvidence) (Selection, error) {
	s, err := ev.Summary()
	if err != nil {
		return Selection{}, err
	}
	base, ok := s.Detector(evaluate.DetectorHistoricalFixed)
	if !ok {
		return Selection{}, fmt.Errorf("compare: tuning evidence lacks the historical baseline")
	}
	sel := Selection{
		ThresholdGrid:               append([]float64(nil), s.ThresholdGrid...),
		HistoricalBaselineThreshold: s.HistoricalBaselineThreshold,
		FarFieldFPRCeiling:          FarFieldFPRCeiling,
	}
	if sel.BaselineF1, err = overallValue(base.Overall.Frame.F1, "tuning baseline overall F1", base.DetectorID); err != nil {
		return Selection{}, err
	}
	if sel.BaselineUtteranceError, err = overallValue(base.Overall.Utterance.UtteranceErrorRate, "tuning baseline overall UER", base.DetectorID); err != nil {
		return Selection{}, err
	}
	if sel.BaselineFarFieldFPR, err = overallValue(base.FarFieldFPR, "tuning baseline far-field FPR", base.DetectorID); err != nil {
		return Selection{}, err
	}

	best := -1
	for _, t := range s.ThresholdGrid {
		id := evaluate.FixedDetectorID(t)
		d, ok := s.Detector(id)
		if !ok {
			return Selection{}, fmt.Errorf("compare: tuning evidence lacks grid candidate %s", id)
		}
		c := Candidate{Threshold: t, DetectorID: id}
		if c.F1, err = overallValue(d.Overall.Frame.F1, "tuning overall F1", id); err != nil {
			return Selection{}, err
		}
		if c.UtteranceError, err = overallValue(d.Overall.Utterance.UtteranceErrorRate, "tuning overall UER", id); err != nil {
			return Selection{}, err
		}
		if c.FarFieldFPR, err = overallValue(d.FarFieldFPR, "tuning far-field FPR", id); err != nil {
			return Selection{}, err
		}
		c.MeetsF1Floor = c.F1 >= sel.BaselineF1
		c.MeetsFPRCeiling = c.FarFieldFPR <= FarFieldFPRCeiling
		c.Eligible = c.MeetsF1Floor && c.MeetsFPRCeiling
		if !c.MeetsF1Floor {
			c.RejectedBecause = append(c.RejectedBecause, fmt.Sprintf("overall frame F1 %s is below the historical baseline F1 %s",
				evaluate.ThresholdLabel(c.F1), evaluate.ThresholdLabel(sel.BaselineF1)))
		}
		if !c.MeetsFPRCeiling {
			c.RejectedBecause = append(c.RejectedBecause, fmt.Sprintf("far-field FPR %s exceeds the %s ceiling",
				evaluate.ThresholdLabel(c.FarFieldFPR), evaluate.ThresholdLabel(FarFieldFPRCeiling)))
		}
		sel.Candidates = append(sel.Candidates, c)
		if !c.Eligible {
			continue
		}
		// Grid is ascending, so "<=" lets an exact UER tie move to the
		// higher threshold.
		if best < 0 || c.UtteranceError <= sel.Candidates[best].UtteranceError {
			best = len(sel.Candidates) - 1
		}
	}

	if best < 0 {
		sel.NoEligibleReason = fmt.Sprintf("no grid threshold has tuning overall frame F1 >= the historical baseline F1 %s and far-field FPR <= %s; the historical %s is retained",
			evaluate.ThresholdLabel(sel.BaselineF1), evaluate.ThresholdLabel(FarFieldFPRCeiling), evaluate.ThresholdLabel(sel.HistoricalBaselineThreshold))
		return sel, nil
	}
	grid := sel.ThresholdGrid
	winner := grid[best]
	if best == 0 || best == len(grid)-1 {
		dir := evaluate.ExpandLower
		if best == len(grid)-1 {
			dir = evaluate.ExpandUpper
		}
		return sel, &GridEdgeError{Direction: dir, Threshold: winner, GridMin: grid[0], GridMax: grid[len(grid)-1]}
	}
	lo, hi := grid[best-1], grid[best+1]
	sel.Selected = &winner
	sel.LowerNeighbor = &lo
	sel.HigherNeighbor = &hi
	return sel, nil
}

// Gate is one held-out gate comparison at full precision.
type Gate struct {
	Name      string  `json:"name"`
	Condition string  `json:"condition"`
	Candidate float64 `json:"candidate"`
	Baseline  float64 `json:"baseline"`
	Passed    bool    `json:"passed"`
}

// Decision is the final ship/keep outcome.
type Decision struct {
	Outcome                     string   `json:"outcome"`
	FinalThreshold              float64  `json:"final_threshold"`
	HistoricalBaselineThreshold float64  `json:"historical_baseline_threshold"`
	SelectedThreshold           *float64 `json:"selected_threshold,omitempty"`
	GatesApplicable             bool     `json:"gates_applicable"`
	UtteranceGate               *Gate    `json:"utterance_error_gate,omitempty"`
	F1Gate                      *Gate    `json:"frame_f1_gate,omitempty"`
	FailedConditions            []string `json:"failed_conditions,omitempty"`
	Reason                      string   `json:"reason"`
}

// Decide applies only the held-out gates to a frozen selection: the
// selected threshold ships when its held-out overall utterance error rate
// is strictly lower than the historical baseline's and its held-out overall
// frame F1 is at least the baseline's. Otherwise the historical baseline is
// kept. A selection with no eligible candidate keeps the baseline with the
// gates marked not applicable.
func Decide(sel Selection, ev HeldOutEvidence) (Decision, error) {
	s, err := ev.Summary()
	if err != nil {
		return Decision{}, err
	}
	if sel.HistoricalBaselineThreshold != evaluate.HistoricalBaselineThreshold || s.HistoricalBaselineThreshold != sel.HistoricalBaselineThreshold {
		return Decision{}, fmt.Errorf("compare: selection baseline %v and held-out baseline %v must both be %v",
			sel.HistoricalBaselineThreshold, s.HistoricalBaselineThreshold, evaluate.HistoricalBaselineThreshold)
	}
	if len(sel.ThresholdGrid) == 0 || !equalFloats(sel.ThresholdGrid, s.ThresholdGrid) {
		return Decision{}, fmt.Errorf("compare: selection grid does not match the held-out evidence grid")
	}
	d := Decision{
		Outcome:                     OutcomeKeep,
		FinalThreshold:              sel.HistoricalBaselineThreshold,
		HistoricalBaselineThreshold: sel.HistoricalBaselineThreshold,
	}
	if sel.Selected == nil {
		if sel.NoEligibleReason == "" {
			return Decision{}, fmt.Errorf("compare: selection has neither a selected threshold nor a no-eligible reason")
		}
		d.Reason = "no eligible tuning candidate; held-out gates not applicable. " + sel.NoEligibleReason
		return d, nil
	}
	selected := *sel.Selected
	idx := -1
	for i, t := range sel.ThresholdGrid {
		if t == selected {
			idx = i
		}
	}
	if idx <= 0 || idx >= len(sel.ThresholdGrid)-1 {
		return Decision{}, fmt.Errorf("compare: selected threshold %s is not an interior grid point", evaluate.ThresholdLabel(selected))
	}
	d.SelectedThreshold = &selected
	d.GatesApplicable = true

	cand, ok := s.Detector(evaluate.FixedDetectorID(selected))
	if !ok {
		return Decision{}, fmt.Errorf("compare: held-out evidence lacks the selected candidate %s", evaluate.FixedDetectorID(selected))
	}
	base, ok := s.Detector(evaluate.DetectorHistoricalFixed)
	if !ok {
		return Decision{}, fmt.Errorf("compare: held-out evidence lacks the historical baseline")
	}
	cu, err := overallValue(cand.Overall.Utterance.UtteranceErrorRate, "held-out overall UER", cand.DetectorID)
	if err != nil {
		return Decision{}, err
	}
	bu, err := overallValue(base.Overall.Utterance.UtteranceErrorRate, "held-out overall UER", base.DetectorID)
	if err != nil {
		return Decision{}, err
	}
	cf, err := overallValue(cand.Overall.Frame.F1, "held-out overall F1", cand.DetectorID)
	if err != nil {
		return Decision{}, err
	}
	bf, err := overallValue(base.Overall.Frame.F1, "held-out overall F1", base.DetectorID)
	if err != nil {
		return Decision{}, err
	}
	d.UtteranceGate = &Gate{Name: "held-out overall utterance error rate", Condition: "candidate < historical baseline", Candidate: cu, Baseline: bu, Passed: cu < bu}
	d.F1Gate = &Gate{Name: "held-out overall frame F1", Condition: "candidate >= historical baseline", Candidate: cf, Baseline: bf, Passed: cf >= bf}
	if !d.UtteranceGate.Passed {
		d.FailedConditions = append(d.FailedConditions, fmt.Sprintf("held-out utterance error rate %s is not strictly lower than the historical baseline %s",
			evaluate.ThresholdLabel(cu), evaluate.ThresholdLabel(bu)))
	}
	if !d.F1Gate.Passed {
		d.FailedConditions = append(d.FailedConditions, fmt.Sprintf("held-out frame F1 %s is below the historical baseline %s",
			evaluate.ThresholdLabel(cf), evaluate.ThresholdLabel(bf)))
	}
	if d.UtteranceGate.Passed && d.F1Gate.Passed {
		d.Outcome = OutcomeShip
		d.FinalThreshold = selected
		d.Reason = fmt.Sprintf("selected threshold %s passed both held-out gates", evaluate.ThresholdLabel(selected))
	} else {
		d.Reason = fmt.Sprintf("selected threshold %s failed %d held-out gate(s); the historical %s is retained",
			evaluate.ThresholdLabel(selected), len(d.FailedConditions), evaluate.ThresholdLabel(sel.HistoricalBaselineThreshold))
	}
	return d, nil
}
