package evaluate

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// HistoricalBaselineThreshold is the fixed energy threshold every candidate
// is compared against (muesli#782). It is a named evaluation constant,
// deliberately independent of pluginkit.DefaultStreamingConfig: changing the
// production default must never move the comparator.
const HistoricalBaselineThreshold = 0.01

// Grid endpoints of the initial logarithmic lower segment.
const (
	initialLogMin = 0.0001
	initialLogMax = 0.001
)

// LogIntervalsPerDecade is the number of equal logarithmic intervals used
// for the initial lower segment and for every explicit decade expansion.
const LogIntervalsPerDecade = 5

// Grid expansion directions.
const (
	ExpandLower = "lower"
	ExpandUpper = "upper"
)

// gridExpansionSteps is the explicit, committed source-grid revision
// history. Each entry extends the grid by one decade at the named end with
// LogIntervalsPerDecade equal logarithmic intervals, preserving every
// existing point. It is edited only when a tuning run's constrained winner
// lands on an endpoint -- never from held-out evidence, and never by an
// automatic search.
var gridExpansionSteps = []string{}

// GridExpansion describes one committed decade expansion.
type GridExpansion struct {
	Direction string    `json:"direction"`
	From      float64   `json:"from_endpoint"`
	To        float64   `json:"to_endpoint"`
	Added     []float64 `json:"added"`
}

// initialGrid returns the initial sorted grid: 0.0001, the four interior
// points 10^(-4+k/5) for k=1..4, 0.001, then every existing thousandth
// point 0.002..0.030 (integer thousandths, as the historical grid used).
func initialGrid() []float64 {
	grid := []float64{initialLogMin}
	for k := 1; k < LogIntervalsPerDecade; k++ {
		grid = append(grid, math.Pow(10, -4+float64(k)/LogIntervalsPerDecade))
	}
	grid = append(grid, initialLogMax)
	for i := 2; i <= 30; i++ {
		grid = append(grid, float64(i)/1000.0)
	}
	return grid
}

// expandDecade returns the LogIntervalsPerDecade points one decade beyond
// endpoint in direction, nearest first: endpoint*10^(±k/5) for k=1..4,
// then the exact decimal decade endpoint (endpoint/10 or endpoint*10) as
// the last point.
func expandDecade(endpoint float64, direction string) ([]float64, error) {
	var sign float64
	switch direction {
	case ExpandLower:
		sign = -1
	case ExpandUpper:
		sign = 1
	default:
		return nil, fmt.Errorf("evaluate: unknown grid expansion direction %q", direction)
	}
	out := make([]float64, 0, LogIntervalsPerDecade)
	for k := 1; k < LogIntervalsPerDecade; k++ {
		out = append(out, endpoint*math.Pow(10, sign*float64(k)/LogIntervalsPerDecade))
	}
	exact, err := decadeShift(endpoint, int(sign))
	if err != nil {
		return nil, err
	}
	out = append(out, exact)
	return out, nil
}

// decadeShift returns the float64 nearest to the decimal value of x's
// shortest round-trip representation multiplied by 10^shift. Shifting the
// decimal exponent (rather than multiplying in binary) keeps chained decade
// endpoints exact constants: 0.0001 -> 1e-05 -> 1e-06, never
// 1.0000000000000002e-06.
func decadeShift(x float64, shift int) (float64, error) {
	s := strconv.FormatFloat(x, 'e', -1, 64)
	i := strings.IndexByte(s, 'e')
	if i < 0 {
		return 0, fmt.Errorf("evaluate: cannot shift %v by a decade", x)
	}
	exp, err := strconv.Atoi(s[i+1:])
	if err != nil {
		return 0, fmt.Errorf("evaluate: cannot shift %v by a decade: %w", x, err)
	}
	return strconv.ParseFloat(s[:i]+"e"+strconv.Itoa(exp+shift), 64)
}

// applyExpansions applies steps to base in order, returning the final
// sorted grid and the expansion metadata.
func applyExpansions(base []float64, steps []string) ([]float64, []GridExpansion, error) {
	grid := append([]float64(nil), base...)
	var history []GridExpansion
	for _, dir := range steps {
		if len(grid) == 0 {
			return nil, nil, fmt.Errorf("evaluate: cannot expand an empty grid")
		}
		endpoint := grid[0]
		if dir == ExpandUpper {
			endpoint = grid[len(grid)-1]
		}
		added, err := expandDecade(endpoint, dir)
		if err != nil {
			return nil, nil, err
		}
		if dir == ExpandLower {
			rev := make([]float64, 0, len(added)+len(grid))
			for i := len(added) - 1; i >= 0; i-- {
				rev = append(rev, added[i])
			}
			grid = append(rev, grid...)
		} else {
			grid = append(grid, added...)
		}
		history = append(history, GridExpansion{Direction: dir, From: endpoint, To: added[len(added)-1], Added: added})
		if err := ValidateGrid(grid); err != nil {
			return nil, nil, fmt.Errorf("evaluate: grid expansion %d (%s): %w", len(history), dir, err)
		}
	}
	return grid, history, nil
}

// ThresholdGrid returns the committed, validated fixed-threshold grid in
// ascending numeric order. It panics only if the committed source grid is
// itself invalid, which grid_test.go prevents.
func ThresholdGrid() []float64 {
	grid, _, err := applyExpansions(initialGrid(), gridExpansionSteps)
	if err != nil {
		panic(err)
	}
	return grid
}

// GridExpansions returns the committed expansion history (empty when the
// initial grid sufficed).
func GridExpansions() []GridExpansion {
	_, history, err := applyExpansions(initialGrid(), gridExpansionSteps)
	if err != nil {
		panic(err)
	}
	if history == nil {
		return []GridExpansion{}
	}
	return history
}

// ValidateGrid rejects an empty grid and any nonpositive, nonfinite,
// duplicate, or out-of-order value, and any pair of values whose detector
// IDs would collide.
func ValidateGrid(grid []float64) error {
	if len(grid) == 0 {
		return fmt.Errorf("threshold grid is empty")
	}
	ids := make(map[string]float64, len(grid))
	for i, t := range grid {
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return fmt.Errorf("threshold grid[%d] is not finite: %v", i, t)
		}
		if t <= 0 {
			return fmt.Errorf("threshold grid[%d] must be positive, got %v", i, t)
		}
		if i > 0 && !(t > grid[i-1]) {
			if t == grid[i-1] {
				return fmt.Errorf("threshold grid[%d] duplicates %s", i, ThresholdLabel(t))
			}
			return fmt.Errorf("threshold grid is not strictly increasing at [%d]: %s after %s", i, ThresholdLabel(t), ThresholdLabel(grid[i-1]))
		}
		id := FixedDetectorID(t)
		if prev, dup := ids[id]; dup {
			return fmt.Errorf("threshold grid detector id %q collides for %v and %v", id, prev, t)
		}
		ids[id] = t
	}
	return nil
}

// ThresholdLabel is the canonical shortest round-trip representation of a
// threshold: parsing it back yields exactly t.
func ThresholdLabel(t float64) string {
	return strconv.FormatFloat(t, 'g', -1, 64)
}

// FixedDetectorID names one fixed-threshold grid candidate, e.g.
// "fixed_0.01" or "fixed_0.00015848931924611142".
func FixedDetectorID(threshold float64) string {
	return "fixed_" + ThresholdLabel(threshold)
}
