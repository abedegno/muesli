package compare

import (
	"math"
	"testing"

	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/evaluate"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/score"
)

func f1(v float64) score.FrameMetrics {
	return score.FrameMetrics{F1: score.Ratio{Value: v, Valid: true}}
}

func entry(recID, meeting, class, mic, detector string, f1v float64) evaluate.MatrixEntry {
	return evaluate.MatrixEntry{
		RecordingID:      recID,
		MeetingID:        meeting,
		Class:            class,
		Mic:              mic,
		DetectorID:       detector,
		FrameMetrics:     f1(f1v),
		UtteranceMetrics: score.UtteranceMetrics{UtteranceErrorRate: score.Ratio{Value: 1 - f1v, Valid: true}},
	}
}

func fourCellEntries(detector string, f1s [4]float64) []evaluate.MatrixEntry {
	cells := AllCells()
	out := make([]evaluate.MatrixEntry, 4)
	for i, c := range cells {
		out[i] = entry(c.Class+"-"+c.Mic, "M", c.Class, c.Mic, detector, f1s[i])
	}
	return out
}

func TestAggregateCellsMacroAveragesRecordings(t *testing.T) {
	entries := []evaluate.MatrixEntry{
		entry("r1", "M1", "scenario", "headset", "fixed_shipped", 0.8),
		entry("r2", "M2", "scenario", "headset", "fixed_shipped", 0.6),
	}
	cells := AggregateCells(entries)
	if len(cells) != 1 {
		t.Fatalf("expected 1 cell group, got %d", len(cells))
	}
	if math.Abs(cells[0].FrameMetrics.F1.Value-0.7) > 1e-9 {
		t.Fatalf("expected macro average 0.7, got %v", cells[0].FrameMetrics.F1.Value)
	}
	if cells[0].RecordingCount != 2 {
		t.Fatalf("expected 2 recordings, got %d", cells[0].RecordingCount)
	}
}

func TestAggregateOverallEqualCellWeight(t *testing.T) {
	// One cell has 1 recording, another has 3 -- overall must still weight
	// the two cells equally (not duration/count-weighted).
	entries := []evaluate.MatrixEntry{
		entry("r1", "M1", "scenario", "headset", "d", 1.0),
		entry("r2", "M2", "scenario", "fixed_distant", "d", 0.0),
		entry("r3", "M2", "scenario", "fixed_distant", "d", 0.0),
		entry("r4", "M2", "scenario", "fixed_distant", "d", 0.0),
		entry("r5", "M3", "non_scenario", "headset", "d", 1.0),
		entry("r6", "M4", "non_scenario", "fixed_distant", "d", 1.0),
	}
	cells := AggregateCells(entries)
	overall, err := AggregateOverall(cells)
	if err != nil {
		t.Fatal(err)
	}
	// Cells: scenario/headset=1.0, scenario/fixed_distant=0.0 (avg of three
	// zeros), non_scenario/headset=1.0, non_scenario/fixed_distant=1.0.
	// Equal-weight average = (1+0+1+1)/4 = 0.75, not (1+0+0+0+1+1)/6=0.5.
	want := 0.75
	if math.Abs(overall[0].FrameMetrics.F1.Value-want) > 1e-9 {
		t.Fatalf("expected equal-cell-weight overall F1 %v, got %v", want, overall[0].FrameMetrics.F1.Value)
	}
}

func TestAggregateOverallIncompleteCellFails(t *testing.T) {
	entries := []evaluate.MatrixEntry{
		entry("r1", "M1", "scenario", "headset", "d", 1.0),
		// Missing the other three cells for detector "d".
	}
	cells := AggregateCells(entries)
	if _, err := AggregateOverall(cells); err == nil {
		t.Fatal("expected incomplete-cell error")
	}
}

func TestThresholdCurveAndSelectMax(t *testing.T) {
	var entries []evaluate.MatrixEntry
	grid := evaluate.ThresholdGrid()
	for i, tt := range grid {
		f1v := 0.5
		if i == 5 {
			f1v = 0.9 // clear maximum
		}
		entries = append(entries, fourCellEntries(evaluate.FixedDetectorID(tt), [4]float64{f1v, f1v, f1v, f1v})...)
	}
	cells := AggregateCells(entries)
	overall, err := AggregateOverall(cells)
	if err != nil {
		t.Fatal(err)
	}
	curve := ThresholdCurve(overall, grid)
	if len(curve) != len(grid) {
		t.Fatalf("expected %d curve points, got %d", len(grid), len(curve))
	}
	best, err := SelectThreshold(curve, 0.01)
	if err != nil {
		t.Fatal(err)
	}
	if best.Threshold != grid[5] {
		t.Fatalf("expected best threshold %v, got %v", grid[5], best.Threshold)
	}
}

func TestSelectThresholdTieBreaksToShipped(t *testing.T) {
	curve := []ThresholdPoint{
		{Threshold: 0.005, F1: score.Ratio{Value: 0.8, Valid: true}},
		{Threshold: 0.010, F1: score.Ratio{Value: 0.8, Valid: true}}, // shipped
		{Threshold: 0.020, F1: score.Ratio{Value: 0.8, Valid: true}},
	}
	best, err := SelectThreshold(curve, 0.010)
	if err != nil {
		t.Fatal(err)
	}
	if best.Threshold != 0.010 {
		t.Fatalf("expected tie to prefer shipped threshold 0.010, got %v", best.Threshold)
	}
}

func TestSelectThresholdTieBreaksToLowerWhenNoShipped(t *testing.T) {
	curve := []ThresholdPoint{
		{Threshold: 0.015, F1: score.Ratio{Value: 0.8, Valid: true}},
		{Threshold: 0.005, F1: score.Ratio{Value: 0.8, Valid: true}},
	}
	best, err := SelectThreshold(curve, 0.010) // shipped not present in the tie
	if err != nil {
		t.Fatal(err)
	}
	if best.Threshold != 0.005 {
		t.Fatalf("expected tie to prefer the lower threshold, got %v", best.Threshold)
	}
}

func TestRecommendBaselineFallbackWhenNoneEligible(t *testing.T) {
	var entries []evaluate.MatrixEntry
	entries = append(entries, fourCellEntries("fixed_shipped", [4]float64{0.9, 0.9, 0.9, 0.9})...)
	entries = append(entries, fourCellEntries("adaptive", [4]float64{0.1, 0.1, 0.1, 0.1})...) // far below floor
	for _, tt := range evaluate.ThresholdGrid() {
		f1v := 0.9
		if tt == 0.01 {
			f1v = 0.9 // shipped grid point matches
		}
		entries = append(entries, fourCellEntries(evaluate.FixedDetectorID(tt), [4]float64{f1v, f1v, f1v, f1v})...)
	}
	cells := AggregateCells(entries)
	overall, err := AggregateOverall(cells)
	if err != nil {
		t.Fatal(err)
	}
	curve := ThresholdCurve(overall, evaluate.ThresholdGrid())
	best, err := SelectThreshold(curve, 0.01)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := Recommend(overall, best, 0.01, false)
	if err != nil {
		t.Fatal(err)
	}
	if rec.RecommendedDetectorID != "fixed_shipped" {
		t.Fatalf("expected baseline fallback, got %q", rec.RecommendedDetectorID)
	}
}

func TestRecommendEligibleCandidateBeatingShippedWins(t *testing.T) {
	var entries []evaluate.MatrixEntry
	entries = append(entries, fourCellEntries("fixed_shipped", [4]float64{0.7, 0.7, 0.7, 0.7})...)
	entries = append(entries, fourCellEntries("adaptive", [4]float64{0.9, 0.9, 0.9, 0.9})...) // beats shipped everywhere
	for _, tt := range evaluate.ThresholdGrid() {
		f1v := 0.7
		entries = append(entries, fourCellEntries(evaluate.FixedDetectorID(tt), [4]float64{f1v, f1v, f1v, f1v})...)
	}
	cells := AggregateCells(entries)
	overall, err := AggregateOverall(cells)
	if err != nil {
		t.Fatal(err)
	}
	curve := ThresholdCurve(overall, evaluate.ThresholdGrid())
	best, err := SelectThreshold(curve, 0.01)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := Recommend(overall, best, 0.01, false)
	if err != nil {
		t.Fatal(err)
	}
	if rec.RecommendedDetectorID != "adaptive" {
		t.Fatalf("expected adaptive to win by beating shipped everywhere, got %q", rec.RecommendedDetectorID)
	}
}

func TestRecommendOneSidedEligibilityMargin(t *testing.T) {
	// Candidate exactly 5 points below shipped in one cell stays eligible
	// (one-sided floor, inclusive); one point further below is disqualified.
	shipped := [4]float64{0.80, 0.80, 0.80, 0.80}
	atFloor := [4]float64{0.75, 0.80, 0.80, 0.80} // exactly shipped-0.05 in cell 0

	var entries []evaluate.MatrixEntry
	entries = append(entries, fourCellEntries("fixed_shipped", shipped)...)
	entries = append(entries, fourCellEntries("adaptive", atFloor)...)
	for _, tt := range evaluate.ThresholdGrid() {
		entries = append(entries, fourCellEntries(evaluate.FixedDetectorID(tt), shipped)...)
	}
	cells := AggregateCells(entries)
	overall, err := AggregateOverall(cells)
	if err != nil {
		t.Fatal(err)
	}
	var adaptiveOverall, shippedOverall OverallResult
	for _, o := range overall {
		if o.DetectorID == "adaptive" {
			adaptiveOverall = o
		}
		if o.DetectorID == "fixed_shipped" {
			shippedOverall = o
		}
	}
	if !eligibleAgainstShipped(adaptiveOverall, shippedOverall) {
		t.Fatal("expected candidate exactly at the 5-point floor to remain eligible")
	}

	belowFloor := [4]float64{0.74, 0.80, 0.80, 0.80} // 6 points below in cell 0
	var entries2 []evaluate.MatrixEntry
	entries2 = append(entries2, fourCellEntries("fixed_shipped", shipped)...)
	entries2 = append(entries2, fourCellEntries("adaptive", belowFloor)...)
	cells2 := AggregateCells(entries2)
	var adaptive2, shipped2 CellResult
	_ = adaptive2
	_ = shipped2
	overall2 := map[string]OverallResult{}
	for _, c := range cells2 {
		// Build partial per-detector CellFrameF1 map manually since these
		// two detectors don't have all 4 cells collectively in this
		// sub-test; we only need eligibility, which only reads CellFrameF1.
		o := overall2[c.DetectorID]
		if o.CellFrameF1 == nil {
			o.CellFrameF1 = map[CellKey]score.Ratio{}
			o.DetectorID = c.DetectorID
		}
		o.CellFrameF1[c.Cell] = c.FrameMetrics.F1
		overall2[c.DetectorID] = o
	}
	if eligibleAgainstShipped(overall2["adaptive"], overall2["fixed_shipped"]) {
		t.Fatal("expected candidate 6 points below the floor in one cell to be disqualified")
	}
}

func TestRecommendTieBreaksToLowerUtteranceErrorThenShipped(t *testing.T) {
	// adaptive and a grid threshold tie on overall F1; adaptive has a lower
	// utterance error rate and must win.
	shipped := [4]float64{0.80, 0.80, 0.80, 0.80}
	tie := [4]float64{0.80, 0.80, 0.80, 0.80}

	entries := fourCellEntries("fixed_shipped", shipped)
	adaptiveEntries := fourCellEntries("adaptive", tie)
	for i := range adaptiveEntries {
		adaptiveEntries[i].UtteranceMetrics.UtteranceErrorRate = score.Ratio{Value: 0.1, Valid: true}
	}
	entries = append(entries, adaptiveEntries...)
	for _, tt := range evaluate.ThresholdGrid() {
		entries = append(entries, fourCellEntries(evaluate.FixedDetectorID(tt), shipped)...)
	}
	cells := AggregateCells(entries)
	overall, err := AggregateOverall(cells)
	if err != nil {
		t.Fatal(err)
	}
	curve := ThresholdCurve(overall, evaluate.ThresholdGrid())
	best, err := SelectThreshold(curve, 0.01)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := Recommend(overall, best, 0.01, false)
	if err != nil {
		t.Fatal(err)
	}
	if rec.RecommendedDetectorID != "adaptive" {
		t.Fatalf("expected adaptive to win the F1 tie via lower utterance error rate, got %q", rec.RecommendedDetectorID)
	}
}

func TestRecommendShippedMeasurementReuseYieldsSameEvidence(t *testing.T) {
	shipped := [4]float64{0.5, 0.5, 0.5, 0.5}
	var entries []evaluate.MatrixEntry
	entries = append(entries, fourCellEntries("fixed_shipped", shipped)...)
	for _, tt := range evaluate.ThresholdGrid() {
		entries = append(entries, fourCellEntries(evaluate.FixedDetectorID(tt), shipped)...)
	}
	cells := AggregateCells(entries)
	overall, err := AggregateOverall(cells)
	if err != nil {
		t.Fatal(err)
	}
	var shippedO, gridO OverallResult
	for _, o := range overall {
		if o.DetectorID == "fixed_shipped" {
			shippedO = o
		}
		if o.DetectorID == "fixed_0.010" {
			gridO = o
		}
	}
	if shippedO.FrameMetrics.F1 != gridO.FrameMetrics.F1 {
		t.Fatalf("expected identical F1 for shipped and its reused grid point: %+v vs %+v", shippedO.FrameMetrics.F1, gridO.FrameMetrics.F1)
	}
}
