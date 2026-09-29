// Package compare turns split-labelled matrix results into validated,
// recording-weighted evidence and the fixed-threshold decision (muesli#778,
// muesli#782).
//
// Evidence is built per split: BuildTuningEvidence and BuildHeldOutEvidence
// verify complete, non-duplicated coverage of every expected recording and
// detector against the manifest before any averaging, and reject undefined
// or non-finite decision metrics. Overall rates are the arithmetic mean of
// a split's four recording rates (two equally weighted meetings, each with
// two equally weighted microphones) -- never duration-weighted, never
// pooled counts. SelectThreshold sees only tuning evidence; Decide applies
// only the held-out gates to that frozen selection.
package compare

// CellKey identifies one descriptive meeting-class/microphone-condition
// cell.
type CellKey struct {
	Class string
	Mic   string
}

// AllCells returns the four descriptive cells in stable order.
func AllCells() []CellKey {
	return []CellKey{
		{Class: "scenario", Mic: "headset"},
		{Class: "scenario", Mic: "fixed_distant"},
		{Class: "non_scenario", Mic: "headset"},
		{Class: "non_scenario", Mic: "fixed_distant"},
	}
}
