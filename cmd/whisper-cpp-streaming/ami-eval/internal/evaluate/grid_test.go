package evaluate

import (
	"math"
	"strconv"
	"testing"
)

func TestThresholdGridInitialShape(t *testing.T) {
	grid := initialGrid()
	if len(grid) != 35 {
		t.Fatalf("expected 35 initial thresholds, got %d", len(grid))
	}
	if grid[0] != 0.0001 {
		t.Fatalf("expected exact lower endpoint 0.0001, got %v", grid[0])
	}
	if grid[5] != 0.001 {
		t.Fatalf("expected exact 0.001 at index 5, got %v", grid[5])
	}
	// Five equal logarithmic intervals between 0.0001 and 0.001.
	step := 1.0 / LogIntervalsPerDecade
	for k := 1; k <= 5; k++ {
		got := math.Log10(grid[k]) - math.Log10(grid[k-1])
		if math.Abs(got-step) > 1e-12 {
			t.Fatalf("log interval %d = %v, want %v", k, got, step)
		}
	}
	for k := 1; k <= 4; k++ {
		want := math.Pow(10, -4+float64(k)/5)
		if grid[k] != want {
			t.Fatalf("grid[%d] = %v, want 10^(-4+%d/5) = %v", k, grid[k], k, want)
		}
	}
	// Every legacy thousandth point 0.001..0.030 is preserved exactly.
	have := map[float64]bool{}
	for _, v := range grid {
		have[v] = true
	}
	for i := 1; i <= 30; i++ {
		legacy := float64(i) / 1000.0
		if !have[legacy] {
			t.Fatalf("legacy grid point %v missing", legacy)
		}
	}
	if grid[len(grid)-1] != 0.030 {
		t.Fatalf("expected last point 0.030, got %v", grid[len(grid)-1])
	}
	if !have[HistoricalBaselineThreshold] {
		t.Fatal("historical baseline 0.01 must be an ordinary grid point")
	}
}

func TestThresholdGridCommittedIsValid(t *testing.T) {
	grid := ThresholdGrid()
	if err := ValidateGrid(grid); err != nil {
		t.Fatalf("committed grid invalid: %v", err)
	}
	for i := 1; i < len(grid); i++ {
		if !(grid[i] > grid[i-1]) {
			t.Fatalf("grid not strictly increasing at %d", i)
		}
	}
	// The committed grid preserves every initial point.
	have := map[float64]bool{}
	for _, v := range grid {
		have[v] = true
	}
	for _, v := range initialGrid() {
		if !have[v] {
			t.Fatalf("committed grid dropped initial point %v", v)
		}
	}
	if len(grid) != len(initialGrid())+LogIntervalsPerDecade*len(GridExpansions()) {
		t.Fatalf("grid length %d inconsistent with %d expansions", len(grid), len(GridExpansions()))
	}
}

func TestThresholdLabelsRoundTripAndAreUnique(t *testing.T) {
	ids := map[string]float64{}
	for _, v := range ThresholdGrid() {
		label := ThresholdLabel(v)
		back, err := strconv.ParseFloat(label, 64)
		if err != nil || back != v {
			t.Fatalf("label %q does not round-trip to %v (got %v, %v)", label, v, back, err)
		}
		id := FixedDetectorID(v)
		if prev, dup := ids[id]; dup {
			t.Fatalf("detector id %q collides for %v and %v", id, prev, v)
		}
		ids[id] = v
	}
	// The sub-0.001 points must be distinguishable (three-decimal
	// formatting would collapse them).
	lower := initialGrid()[:6]
	seen := map[string]bool{}
	for _, v := range lower {
		seen[FixedDetectorID(v)] = true
	}
	if len(seen) != 6 {
		t.Fatalf("sub-0.001 detector ids collide: %v", seen)
	}
	if FixedDetectorID(0.01) != "fixed_0.01" {
		t.Fatalf("unexpected id for 0.01: %s", FixedDetectorID(0.01))
	}
}

func TestValidateGridRejectsInvalid(t *testing.T) {
	cases := map[string][]float64{
		"empty":    {},
		"dup":      {0.001, 0.002, 0.002},
		"unsorted": {0.002, 0.001},
		"zero":     {0, 0.001},
		"negative": {-0.001, 0.001},
		"nan":      {0.001, math.NaN()},
		"inf":      {0.001, math.Inf(1)},
	}
	for name, g := range cases {
		if err := ValidateGrid(g); err == nil {
			t.Fatalf("%s: expected rejection of %v", name, g)
		}
	}
	if err := ValidateGrid([]float64{0.0001, 0.001}); err != nil {
		t.Fatalf("expected valid grid: %v", err)
	}
}

func TestGridExpansionBothDirections(t *testing.T) {
	base := initialGrid()

	lower, hist, err := applyExpansions(base, []string{ExpandLower})
	if err != nil {
		t.Fatal(err)
	}
	if lower[0] != 1e-05 {
		t.Fatalf("lower expansion endpoint = %v, want exact 1e-05", lower[0])
	}
	if len(lower) != len(base)+5 || len(hist) != 1 || hist[0].Direction != ExpandLower || hist[0].From != 0.0001 {
		t.Fatalf("lower expansion metadata: len=%d hist=%+v", len(lower), hist)
	}
	for i, v := range base {
		if lower[i+5] != v {
			t.Fatalf("lower expansion moved existing point %v", v)
		}
	}
	for k := 1; k <= 5; k++ {
		got := math.Log10(lower[k]) - math.Log10(lower[k-1])
		if math.Abs(got-0.2) > 1e-12 {
			t.Fatalf("lower expansion interval %d = %v", k, got)
		}
	}

	upper, hist, err := applyExpansions(base, []string{ExpandUpper})
	if err != nil {
		t.Fatal(err)
	}
	if upper[len(upper)-1] != 0.3 {
		t.Fatalf("upper expansion endpoint = %v, want exact 0.3", upper[len(upper)-1])
	}
	for i, v := range base {
		if upper[i] != v {
			t.Fatalf("upper expansion moved existing point %v", v)
		}
	}
	if hist[0].Direction != ExpandUpper || hist[0].To != 0.3 {
		t.Fatalf("upper expansion metadata: %+v", hist)
	}

	// Chained expansions stay valid and ordered.
	both, hist, err := applyExpansions(base, []string{ExpandLower, ExpandUpper, ExpandLower})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateGrid(both); err != nil {
		t.Fatal(err)
	}
	if len(hist) != 3 || both[0] != 1e-06 || both[len(both)-1] != 0.3 {
		t.Fatalf("chained expansion: first=%v hist=%d", both[0], len(hist))
	}

	if _, _, err := applyExpansions(base, []string{"sideways"}); err == nil {
		t.Fatal("expected unknown direction rejection")
	}
}
