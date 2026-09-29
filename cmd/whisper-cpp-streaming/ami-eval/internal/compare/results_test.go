package compare

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/evaluate"
)

func fixtureResults(t *testing.T) Results {
	t.Helper()
	tm, hm, _ := fixtureMatrices(t)
	man := fixtureManifest()
	te, err := BuildTuningEvidence(man, tm)
	if err != nil {
		t.Fatal(err)
	}
	sel, err := SelectThreshold(te)
	if _, edge := IsGridEdge(err); err != nil && !edge {
		t.Fatal(err)
	}
	if err != nil {
		// The synthetic fixture's winner is irrelevant here; use a
		// no-eligible-style selection over the real grid for the envelope.
		sel.Selected, sel.LowerNeighbor, sel.HigherNeighbor = nil, nil, nil
		sel.NoEligibleReason = "fixture"
	}
	he, err := BuildHeldOutEvidence(man, hm)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := Decide(sel, he)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewResults(man, strings.Repeat("a", 64), Environment{GoVersion: "go1.25.11", GOOS: "linux", GOARCH: "amd64"}, tm, hm, sel, dec)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestResultsEnvelopeRoundTripsUnrounded(t *testing.T) {
	r := fixtureResults(t)
	if len(r.Splits) != 2 || r.Splits[0].Split != "tuning" || len(r.Splits[1].RecordingIDs) != 4 {
		t.Fatalf("split inventory: %+v", r.Splits)
	}
	dir := t.TempDir()
	if err := WriteResults(dir, r); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != ResultsFileName {
		t.Fatalf("unexpected files: %v", entries)
	}
	data, err := os.ReadFile(filepath.Join(dir, ResultsFileName))
	if err != nil {
		t.Fatal(err)
	}
	var back Results
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	for i, v := range r.ThresholdGrid {
		if back.ThresholdGrid[i] != v {
			t.Fatalf("grid[%d] not round-tripped exactly: %v vs %v", i, back.ThresholdGrid[i], v)
		}
	}
	d, _ := r.Tuning.Detector(evaluate.FixedDetectorID(r.ThresholdGrid[1]))
	bd, _ := back.Tuning.Detector(evaluate.FixedDetectorID(back.ThresholdGrid[1]))
	if d.Overall.Frame.F1.Value != bd.Overall.Frame.F1.Value || math.IsNaN(bd.Overall.Frame.F1.Value) {
		t.Fatal("overall F1 not round-tripped at full precision")
	}
	if back.HistoricalBaselineThreshold != 0.01 || back.EvaluationVersion != evaluate.EvaluationVersion || len(back.TuningMatrix.Entries) != len(r.TuningMatrix.Entries) {
		t.Fatalf("envelope metadata lost: %+v", back.EvaluationVersion)
	}
}

func TestNewResultsRejectsInconsistentInputs(t *testing.T) {
	tm, hm, _ := fixtureMatrices(t)
	man := fixtureManifest()
	r := fixtureResults(t)
	env := r.Environment
	if _, err := NewResults(man, r.ManifestDigest, env, hm, tm, r.Selection, r.Decision); err == nil {
		t.Fatal("swapped matrices must be rejected")
	}
	bad := r.Selection
	bad.ThresholdGrid = bad.ThresholdGrid[1:]
	if _, err := NewResults(man, r.ManifestDigest, env, tm, hm, bad, r.Decision); err == nil {
		t.Fatal("selection grid mismatch must be rejected")
	}
	hm2 := cloneMatrix(hm)
	hm2.RuntimeDefaultThreshold = 0.02
	if _, err := NewResults(man, r.ManifestDigest, env, tm, hm2, r.Selection, r.Decision); err == nil {
		t.Fatal("runtime default drift between splits must be rejected")
	}
	if _, err := NewResults(man, "short", env, tm, hm, r.Selection, r.Decision); err == nil {
		t.Fatal("bad digest must be rejected")
	}
}

// openHandlesUnder reports open file descriptors of this process whose target
// lies under dir (Linux /proc only; ok=false elsewhere). A leaked temp handle
// shows up here even after unlink, as "<path> (deleted)".
func openHandlesUnder(t *testing.T, dir string) (handles []string, ok bool) {
	t.Helper()
	fds, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return nil, false
	}
	for _, fd := range fds {
		target, err := os.Readlink(filepath.Join("/proc/self/fd", fd.Name()))
		if err != nil {
			continue
		}
		if strings.HasPrefix(target, dir+string(filepath.Separator)) {
			handles = append(handles, target)
		}
	}
	return handles, true
}

// assertCleanedUp checks the failure-after-acquisition contract: the temp
// handle is closed and no temp file is left beside results.json.
func assertCleanedUp(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != ResultsFileName {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
	if handles, ok := openHandlesUnder(t, dir); ok && len(handles) != 0 {
		t.Fatalf("temp results handle not closed: %v", handles)
	}
}

func TestWriteResultsEncodeFailureCleansUpAndPreservesExisting(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, ResultsFileName)
	prior := []byte("{\"prior\":true}\n")
	if err := os.WriteFile(dest, prior, 0o644); err != nil {
		t.Fatal(err)
	}
	r := fixtureResults(t)
	r.HistoricalBaselineThreshold = math.NaN() // json cannot encode NaN
	err := WriteResults(dir, r)
	if err == nil || !strings.Contains(err.Error(), "encode") {
		t.Fatalf("want encode error, got %v", err)
	}
	assertCleanedUp(t, dir)
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(prior) {
		t.Fatalf("pre-existing %s modified: %q", ResultsFileName, got)
	}
}

func TestWriteResultsRenameFailureCleansUpAndPreservesExisting(t *testing.T) {
	dir := t.TempDir()
	// A non-empty directory at the destination makes the final rename fail
	// after the temp file has been written, synced and closed.
	dest := filepath.Join(dir, ResultsFileName)
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(dest, "keep")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := WriteResults(dir, fixtureResults(t))
	if err == nil || !strings.Contains(err.Error(), "promote") {
		t.Fatalf("want promote error, got %v", err)
	}
	assertCleanedUp(t, dir)
	info, err := os.Stat(dest)
	if err != nil || !info.IsDir() {
		t.Fatalf("pre-existing %s replaced: %v", ResultsFileName, err)
	}
	got, err := os.ReadFile(sentinel)
	if err != nil || string(got) != "keep" {
		t.Fatalf("pre-existing %s contents changed: %q %v", ResultsFileName, got, err)
	}
}
