package compare

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/evaluate"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/manifest"
)

// ResultsFileName is the raw evidence file written under
// Cache.ResultsDir(digest, EvaluationVersion).
const ResultsFileName = "results.json"

// Environment records the execution-environment inputs that can affect
// floating-point and dependency behavior between otherwise-identical runs.
type Environment struct {
	GoVersion string `json:"go_version"`
	GOOS      string `json:"goos"`
	GOARCH    string `json:"goarch"`
}

// SplitMembership is one split's actual meeting and recording inventory.
type SplitMembership struct {
	Split        manifest.Split `json:"split"`
	MeetingIDs   []string       `json:"meeting_ids"`
	RecordingIDs []string       `json:"recording_ids"`
}

// Results is the complete, unrounded evidence for one evaluation: both
// metrics-only matrices, both validated summaries, the tuning selection,
// and the held-out decision. The report is rendered from exactly this.
type Results struct {
	ManifestDigest              string                       `json:"manifest_digest"`
	EvaluationVersion           string                       `json:"evaluation_version"`
	Environment                 Environment                  `json:"environment"`
	Splits                      []SplitMembership            `json:"splits"`
	HeldOutReplacement          *manifest.HeldOutReplacement `json:"held_out_replacement,omitempty"`
	HistoricalBaselineThreshold float64                      `json:"historical_baseline_threshold"`
	RuntimeDefaultThreshold     float64                      `json:"runtime_default_threshold"`
	ThresholdGrid               []float64                    `json:"threshold_grid"`
	GridExpansions              []evaluate.GridExpansion     `json:"grid_expansions"`
	TuningMatrix                evaluate.Matrix              `json:"tuning_matrix"`
	HeldOutMatrix               evaluate.Matrix              `json:"held_out_matrix"`
	Tuning                      SplitSummary                 `json:"tuning"`
	HeldOut                     SplitSummary                 `json:"held_out"`
	Selection                   Selection                    `json:"selection"`
	Decision                    Decision                     `json:"decision"`
}

// NewResults assembles the evidence envelope, re-deriving both summaries
// from the matrices (so the envelope can never carry summaries that
// disagree with its raw data) and checking the selection and decision
// against them.
func NewResults(man *manifest.Manifest, digest string, env Environment, tuning, heldOut evaluate.Matrix, sel Selection, dec Decision) (Results, error) {
	te, err := BuildTuningEvidence(man, tuning)
	if err != nil {
		return Results{}, err
	}
	he, err := BuildHeldOutEvidence(man, heldOut)
	if err != nil {
		return Results{}, err
	}
	ts, _ := te.Summary()
	hs, _ := he.Summary()
	if tuning.RuntimeDefaultThreshold != heldOut.RuntimeDefaultThreshold {
		return Results{}, fmt.Errorf("compare: runtime default changed between splits (%v vs %v)", tuning.RuntimeDefaultThreshold, heldOut.RuntimeDefaultThreshold)
	}
	if !equalFloats(sel.ThresholdGrid, ts.ThresholdGrid) || sel.HistoricalBaselineThreshold != ts.HistoricalBaselineThreshold {
		return Results{}, fmt.Errorf("compare: selection does not match the tuning evidence grid/baseline")
	}
	if dec.HistoricalBaselineThreshold != sel.HistoricalBaselineThreshold {
		return Results{}, fmt.Errorf("compare: decision baseline does not match the selection")
	}
	if len(digest) != 64 {
		return Results{}, fmt.Errorf("compare: manifest digest must be 64 hex characters")
	}
	if env.GoVersion == "" || env.GOOS == "" || env.GOARCH == "" {
		return Results{}, fmt.Errorf("compare: execution environment is required")
	}
	r := Results{
		ManifestDigest:              digest,
		EvaluationVersion:           evaluate.EvaluationVersion,
		Environment:                 env,
		HistoricalBaselineThreshold: evaluate.HistoricalBaselineThreshold,
		RuntimeDefaultThreshold:     tuning.RuntimeDefaultThreshold,
		ThresholdGrid:               append([]float64(nil), ts.ThresholdGrid...),
		GridExpansions:              append([]evaluate.GridExpansion{}, tuning.GridExpansions...),
		TuningMatrix:                tuning,
		HeldOutMatrix:               heldOut,
		Tuning:                      ts,
		HeldOut:                     hs,
		Selection:                   sel,
		Decision:                    dec,
	}
	if man.HeldOutReplacement != nil {
		rep := *man.HeldOutReplacement
		r.HeldOutReplacement = &rep
	}
	for _, split := range manifest.Splits() {
		m := SplitMembership{Split: split, MeetingIDs: man.MeetingIDs(split)}
		for _, rec := range man.RecordingsForSplit(split) {
			m.RecordingIDs = append(m.RecordingIDs, rec.ID)
		}
		r.Splits = append(r.Splits, m)
	}
	return r, nil
}

// WriteResults atomically writes results.json (sibling temp file, sync,
// rename) into dir, creating dir if needed.
func WriteResults(dir string, r Results) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("compare: create results dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ResultsFileName+".tmp-*")
	if err != nil {
		return fmt.Errorf("compare: create temp results file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			tmp.Close()
			os.Remove(tmpPath)
		}
	}()
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return fmt.Errorf("compare: encode %s: %w", ResultsFileName, err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("compare: sync %s: %w", ResultsFileName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("compare: close %s: %w", ResultsFileName, err)
	}
	if err := os.Rename(tmpPath, filepath.Join(dir, ResultsFileName)); err != nil {
		return fmt.Errorf("compare: promote %s: %w", ResultsFileName, err)
	}
	cleanup = false
	return nil
}
