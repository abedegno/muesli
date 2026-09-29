package compare

import (
	"strings"
	"testing"

	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/evaluate"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/manifest"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/score"
)

// pt is one explicit numeric grid point for pure selector/gate tests.
type pt struct {
	th, f1, uer, fpr float64
}

func ratio(v float64) score.Ratio { return score.Ratio{Value: v, Valid: true} }

func numericDetector(id string, th *float64, f1, uer, fpr float64) DetectorSummary {
	return DetectorSummary{
		DetectorID: id, Threshold: th, MeasurementID: id,
		Overall: Metrics{
			Frame:     score.FrameMetrics{F1: ratio(f1), FalsePositiveRate: ratio(fpr)},
			Utterance: score.UtteranceMetrics{UtteranceErrorRate: ratio(uer)},
		},
		FarFieldFPR: ratio(fpr),
	}
}

func numericSummary(split manifest.Split, base pt, pts []pt) SplitSummary {
	s := SplitSummary{Split: split, HistoricalBaselineThreshold: evaluate.HistoricalBaselineThreshold}
	b := evaluate.HistoricalBaselineThreshold
	s.Detectors = append(s.Detectors, numericDetector(evaluate.DetectorHistoricalFixed, &b, base.f1, base.uer, base.fpr))
	for _, p := range pts {
		th := p.th
		s.ThresholdGrid = append(s.ThresholdGrid, th)
		s.Detectors = append(s.Detectors, numericDetector(evaluate.FixedDetectorID(th), &th, p.f1, p.uer, p.fpr))
	}
	return s
}

func numericTuning(base pt, pts ...pt) TuningEvidence {
	return TuningEvidence{ev: &splitEvidence{summary: numericSummary(manifest.SplitTuning, base, pts)}}
}

func numericHeldOut(base pt, pts ...pt) HeldOutEvidence {
	return HeldOutEvidence{ev: &splitEvidence{summary: numericSummary(manifest.SplitHeldOut, base, pts)}}
}

var baseTuning = pt{th: 0.01, f1: 0.5, uer: 1.0, fpr: 0.3}

func mustSelect(t *testing.T, ev TuningEvidence) Selection {
	t.Helper()
	sel, err := SelectThreshold(ev)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	return sel
}

func selected(t *testing.T, sel Selection) float64 {
	t.Helper()
	if sel.Selected == nil {
		t.Fatalf("expected a selection, got none (%s)", sel.NoEligibleReason)
	}
	return *sel.Selected
}

func TestSelectLowestUtteranceErrorBeatsHighestF1(t *testing.T) {
	sel := mustSelect(t, numericTuning(baseTuning,
		pt{0.001, 0.6, 0.9, 0.1},
		pt{0.002, 0.95, 0.5, 0.1}, // highest F1
		pt{0.003, 0.6, 0.2, 0.1},  // lowest UER
		pt{0.004, 0.6, 0.4, 0.1},
	))
	if got := selected(t, sel); got != 0.003 {
		t.Fatalf("selected %v, want 0.003 (lowest UER, not highest F1)", got)
	}
	if *sel.LowerNeighbor != 0.002 || *sel.HigherNeighbor != 0.004 {
		t.Fatalf("neighbors %v/%v", *sel.LowerNeighbor, *sel.HigherNeighbor)
	}
}

func TestSelectF1FloorConstraint(t *testing.T) {
	sel := mustSelect(t, numericTuning(baseTuning,
		pt{0.001, 0.6, 0.9, 0.1},
		pt{0.002, 0.6, 0.3, 0.1},
		pt{0.003, 0.4999999, 0.1, 0.1}, // lowest UER, F1 just below baseline
		pt{0.004, 0.6, 0.4, 0.1},
	))
	if got := selected(t, sel); got != 0.002 {
		t.Fatalf("selected %v, want 0.002", got)
	}
	c := sel.Candidates[2]
	if c.Eligible || c.MeetsF1Floor || !c.MeetsFPRCeiling || len(c.RejectedBecause) != 1 || !strings.Contains(c.RejectedBecause[0], "below the historical baseline F1") {
		t.Fatalf("0.003 candidate: %+v", c)
	}
}

func TestSelectFarFieldFPRConstraint(t *testing.T) {
	sel := mustSelect(t, numericTuning(baseTuning,
		pt{0.001, 0.6, 0.9, 0.1},
		pt{0.002, 0.6, 0.3, 0.1},
		pt{0.003, 0.9, 0.1, 0.5000001}, // lowest UER, far-field FPR just above ceiling
		pt{0.004, 0.6, 0.4, 0.1},
	))
	if got := selected(t, sel); got != 0.002 {
		t.Fatalf("selected %v, want 0.002", got)
	}
	c := sel.Candidates[2]
	if c.Eligible || !c.MeetsF1Floor || c.MeetsFPRCeiling || !strings.Contains(strings.Join(c.RejectedBecause, ";"), "exceeds the 0.5 ceiling") {
		t.Fatalf("0.003 candidate: %+v", c)
	}
}

func TestSelectEqualityAtBothConstraintsIsEligible(t *testing.T) {
	sel := mustSelect(t, numericTuning(baseTuning,
		pt{0.001, 0.6, 0.9, 0.1},
		pt{0.002, 0.5, 0.1, 0.5}, // F1 == baseline, FPR == 0.5 exactly
		pt{0.003, 0.6, 0.4, 0.1},
	))
	if got := selected(t, sel); got != 0.002 {
		t.Fatalf("selected %v, want 0.002 (equality passes both constraints)", got)
	}
}

func TestSelectExactTieChoosesHigherThreshold(t *testing.T) {
	// 0.01 (the baseline value) and 0.02 tie exactly; 0.02 wins even though
	// 0.01 is eligible -- no preference for the baseline.
	sel := mustSelect(t, numericTuning(baseTuning,
		pt{0.005, 0.6, 0.9, 0.1},
		pt{0.008, 0.6, 0.3, 0.1},
		pt{0.01, 0.5, 0.3, 0.3},
		pt{0.02, 0.6, 0.3, 0.1},
		pt{0.03, 0.6, 0.9, 0.1},
	))
	if got := selected(t, sel); got != 0.02 {
		t.Fatalf("selected %v, want 0.02 (higher threshold on exact tie)", got)
	}
}

func TestSelectSubDisplayPrecisionDifferenceDecides(t *testing.T) {
	// Differences far below the report's 4-decimal display still decide.
	sel := mustSelect(t, numericTuning(baseTuning,
		pt{0.001, 0.6, 0.9, 0.1},
		pt{0.002, 0.6, 0.30000000001, 0.1},
		pt{0.003, 0.6, 0.30000000002, 0.1},
		pt{0.004, 0.6, 0.9, 0.1},
	))
	if got := selected(t, sel); got != 0.002 {
		t.Fatalf("selected %v, want 0.002", got)
	}
}

func TestSelectNoEligibleCandidateKeepsBaseline(t *testing.T) {
	sel := mustSelect(t, numericTuning(baseTuning,
		pt{0.001, 0.4, 0.1, 0.9},
		pt{0.002, 0.4, 0.1, 0.1},
		pt{0.003, 0.6, 0.1, 0.6},
	))
	if sel.Selected != nil || sel.NoEligibleReason == "" {
		t.Fatalf("expected no selection with a reason, got %+v", sel)
	}
	for _, c := range sel.Candidates {
		if c.Eligible || len(c.RejectedBecause) == 0 {
			t.Fatalf("candidate %v must be ineligible with reasons: %+v", c.Threshold, c)
		}
	}
	d, err := Decide(sel, numericHeldOut(pt{0.01, 0.5, 1, 0.3}, pt{0.001, 1, 0, 0}, pt{0.002, 1, 0, 0}, pt{0.003, 1, 0, 0}))
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != OutcomeKeep || d.FinalThreshold != 0.01 || d.GatesApplicable || d.UtteranceGate != nil || d.F1Gate != nil {
		t.Fatalf("no-eligible decision: %+v", d)
	}
}

func TestSelectBaselineIsAnOrdinaryCandidate(t *testing.T) {
	sel := mustSelect(t, numericTuning(baseTuning,
		pt{0.005, 0.6, 0.9, 0.1},
		pt{0.01, 0.5, 0.2, 0.3}, // baseline value itself has the lowest UER
		pt{0.02, 0.6, 0.4, 0.1},
	))
	if got := selected(t, sel); got != 0.01 {
		t.Fatalf("selected %v, want 0.01", got)
	}
	// Selecting 0.01 necessarily fails the strict held-out UER gate.
	d, err := Decide(sel, numericHeldOut(pt{0.01, 0.5, 0.3, 0.3}, pt{0.005, 0.5, 0.3, 0.1}, pt{0.01, 0.5, 0.3, 0.3}, pt{0.02, 0.5, 0.3, 0.1}))
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != OutcomeKeep || d.UtteranceGate.Passed || !d.F1Gate.Passed || d.FinalThreshold != 0.01 {
		t.Fatalf("selecting the baseline must keep 0.01 via the strict UER gate: %+v", d)
	}
}

func TestSelectRejectsInvalidOrZeroEvidence(t *testing.T) {
	if _, err := SelectThreshold(TuningEvidence{}); err == nil {
		t.Fatal("zero-value evidence must be rejected")
	}
	ev := numericTuning(baseTuning, pt{0.001, 0.6, 0.9, 0.1}, pt{0.002, 0.6, 0.3, 0.1}, pt{0.003, 0.6, 0.9, 0.1})
	ev.ev.summary.Detectors[2].Overall.Utterance.UtteranceErrorRate = score.Ratio{}
	if _, err := SelectThreshold(ev); err == nil {
		t.Fatal("undefined overall UER must be an evaluation error, not ineligibility")
	}
	ev2 := numericTuning(baseTuning, pt{0.001, 0.6, 0.9, 0.1}, pt{0.002, 0.6, 0.3, 0.1}, pt{0.003, 0.6, 0.9, 0.1})
	ev2.ev.summary.Detectors = ev2.ev.summary.Detectors[:3] // drop the 0.003 candidate
	if _, err := SelectThreshold(ev2); err == nil {
		t.Fatal("missing candidate evidence must be an error")
	}
}

func TestSelectGridEdgesAreErrors(t *testing.T) {
	sel, err := SelectThreshold(numericTuning(baseTuning,
		pt{0.001, 0.6, 0.1, 0.1}, pt{0.002, 0.6, 0.3, 0.1}, pt{0.003, 0.6, 0.4, 0.1}))
	ge, ok := IsGridEdge(err)
	if !ok || ge.Direction != evaluate.ExpandLower || ge.Threshold != 0.001 || ge.GridMin != 0.001 || ge.GridMax != 0.003 {
		t.Fatalf("expected lower GridEdgeError, got %v", err)
	}
	if sel.Selected != nil || len(sel.Candidates) != 3 {
		t.Fatalf("edge selection must report candidates but select nothing: %+v", sel)
	}

	_, err = SelectThreshold(numericTuning(baseTuning,
		pt{0.001, 0.6, 0.5, 0.1}, pt{0.002, 0.6, 0.3, 0.1}, pt{0.003, 0.6, 0.3, 0.1}))
	if ge, ok := IsGridEdge(err); !ok || ge.Direction != evaluate.ExpandUpper || ge.Threshold != 0.003 {
		t.Fatalf("expected upper GridEdgeError (tie resolved to the higher endpoint), got %v", err)
	}
}

func TestSelectInteriorWinnerWithIneligibleNeighbors(t *testing.T) {
	sel := mustSelect(t, numericTuning(baseTuning,
		pt{0.001, 0.4, 0.05, 0.1}, // ineligible (F1)
		pt{0.002, 0.6, 0.3, 0.1},
		pt{0.003, 0.6, 0.05, 0.9}, // ineligible (FPR)
	))
	if selected(t, sel) != 0.002 || *sel.LowerNeighbor != 0.001 || *sel.HigherNeighbor != 0.003 {
		t.Fatalf("interior winner with ineligible neighbors: %+v", sel)
	}
}

// interiorSelection selects 0.002 from a 3-point grid.
func interiorSelection(t *testing.T) Selection {
	return mustSelect(t, numericTuning(baseTuning,
		pt{0.001, 0.6, 0.9, 0.1}, pt{0.002, 0.6, 0.3, 0.1}, pt{0.003, 0.6, 0.9, 0.1}))
}

// heldOutWith sets the held-out operands for the selected 0.002 and the
// baseline. The unselected 0.003 dominates every held-out metric, so any
// leak of held-out scores into selection or shipping would surface as 0.003.
func heldOutWith(candUER, candF1, baseUER, baseF1 float64) HeldOutEvidence {
	return numericHeldOut(pt{0.01, baseF1, baseUER, 0.3},
		pt{0.001, 0.1, 5, 0.9}, pt{0.002, candF1, candUER, 0.2}, pt{0.003, 0.99, 0.0, 0.0})
}

func TestDecideGateCombinations(t *testing.T) {
	sel := interiorSelection(t)
	cases := []struct {
		name                          string
		candUER, candF1, bUER, bF1    float64
		wantShip, wantUERok, wantF1ok bool
		wantFailed                    int
	}{
		{"both pass", 0.4, 0.7, 0.5, 0.6, true, true, true, 0},
		{"equal F1 passes", 0.4, 0.6, 0.5, 0.6, true, true, true, 0},
		{"equal UER fails", 0.5, 0.7, 0.5, 0.6, false, false, true, 1},
		{"F1 lower fails", 0.4, 0.59, 0.5, 0.6, false, true, false, 1},
		{"UER higher fails", 0.6, 0.7, 0.5, 0.6, false, false, true, 1},
		{"both fail", 0.6, 0.5, 0.5, 0.6, false, false, false, 2},
	}
	for _, tc := range cases {
		d, err := Decide(sel, heldOutWith(tc.candUER, tc.candF1, tc.bUER, tc.bF1))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if (d.Outcome == OutcomeShip) != tc.wantShip || d.UtteranceGate.Passed != tc.wantUERok || d.F1Gate.Passed != tc.wantF1ok || len(d.FailedConditions) != tc.wantFailed {
			t.Fatalf("%s: %+v (utt %+v f1 %+v)", tc.name, d, *d.UtteranceGate, *d.F1Gate)
		}
		wantFinal := 0.01
		if tc.wantShip {
			wantFinal = 0.002
		}
		if d.FinalThreshold != wantFinal || *d.SelectedThreshold != 0.002 || !d.GatesApplicable {
			t.Fatalf("%s: final %v selected %v", tc.name, d.FinalThreshold, *d.SelectedThreshold)
		}
		if d.UtteranceGate.Candidate != tc.candUER || d.UtteranceGate.Baseline != tc.bUER || d.F1Gate.Candidate != tc.candF1 || d.F1Gate.Baseline != tc.bF1 {
			t.Fatalf("%s: gate operands not recorded at full precision", tc.name)
		}
	}
}

func TestHeldOutScoresNeverChangeSelection(t *testing.T) {
	// Held-out evidence where 0.003 is overwhelmingly best: a leak would
	// move the selection or the shipped value to 0.003.
	sel := interiorSelection(t)
	leaky := numericHeldOut(pt{0.01, 0.6, 0.5, 0.3},
		pt{0.001, 0.1, 5, 0.9}, pt{0.002, 0.1, 0.9, 0.9}, pt{0.003, 0.99, 0.0, 0.0})
	d, err := Decide(sel, leaky)
	if err != nil {
		t.Fatal(err)
	}
	if *sel.Selected != 0.002 || *d.SelectedThreshold != 0.002 || d.Outcome != OutcomeKeep || d.FinalThreshold != 0.01 {
		t.Fatalf("held-out scores leaked into selection/fallback: sel=%v decision=%+v", *sel.Selected, d)
	}
}

func TestDecideRejectsMismatchedOrInvalidInputs(t *testing.T) {
	sel := interiorSelection(t)
	if _, err := Decide(sel, HeldOutEvidence{}); err == nil {
		t.Fatal("zero held-out evidence must be rejected")
	}
	other := numericHeldOut(pt{0.01, 0.6, 0.5, 0.3}, pt{0.001, 0.6, 0.4, 0.1}, pt{0.002, 0.6, 0.4, 0.1}, pt{0.004, 0.6, 0.4, 0.1})
	if _, err := Decide(sel, other); err == nil {
		t.Fatal("grid mismatch between selection and held-out must be rejected")
	}
	forged := sel
	edge := 0.001
	forged.Selected = &edge
	if _, err := Decide(forged, heldOutWith(0.4, 0.7, 0.5, 0.6)); err == nil {
		t.Fatal("an edge selection must never be validated")
	}
	moved := sel
	moved.HistoricalBaselineThreshold = 0.02
	if _, err := Decide(moved, heldOutWith(0.4, 0.7, 0.5, 0.6)); err == nil {
		t.Fatal("a moved comparator must be rejected")
	}
}
