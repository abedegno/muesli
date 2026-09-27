package report

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/compare"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/score"
	"github.com/abedegno/muesli/internal/pluginkit"
)

func f1r(v float64) score.Ratio { return score.Ratio{Value: v, Valid: true} }

func sampleReport() Report {
	cells := []compare.CellResult{}
	overall := []compare.OverallResult{}
	detectors := []string{"fixed_shipped", "adaptive"}
	for _, det := range detectors {
		cellF1 := map[compare.CellKey]score.Ratio{}
		for _, cell := range compare.AllCells() {
			fm := score.FrameMetrics{Precision: f1r(0.9), Recall: f1r(0.8), F1: f1r(0.85), FalsePositiveRate: f1r(0.1), FalseNegativeRate: f1r(0.2)}
			um := score.UtteranceMetrics{MissRate: f1r(0.05), SpuriousRate: f1r(0.03), UtteranceErrorRate: f1r(0.1)}
			cells = append(cells, compare.CellResult{Cell: cell, DetectorID: det, RecordingCount: 1, FrameMetrics: fm, UtteranceMetrics: um})
			cellF1[cell] = fm.F1
		}
		overall = append(overall, compare.OverallResult{
			DetectorID:       det,
			FrameMetrics:     score.FrameMetrics{Precision: f1r(0.9), Recall: f1r(0.8), F1: f1r(0.85), FalsePositiveRate: f1r(0.1), FalseNegativeRate: f1r(0.2)},
			UtteranceMetrics: score.UtteranceMetrics{UtteranceErrorRate: f1r(0.1)},
			CellFrameF1:      cellF1,
		})
	}
	threshold := 0.01
	return Report{
		ManifestDigest:           strings.Repeat("a", 64),
		EvaluationVersion:        "v1",
		Environment:              Environment{GoVersion: "go1.25.11", GOOS: "linux", GOARCH: "amd64"},
		PreparationSchemaVersion: 1,
		AnnotationSchemaVersion:  1,
		Defaults:                 ProductionDefaults(),
		ThresholdGrid:            []float64{0.005, 0.01, 0.015},
		ShippedThreshold:         0.01,
		WhisperAvailable:         false,
		WhisperUnavailableReason: "no compatible interface",
		CellResults:              cells,
		OverallResults:           overall,
		ThresholdCurve: []compare.ThresholdPoint{
			{Threshold: 0.005, F1: f1r(0.7)},
			{Threshold: 0.01, F1: f1r(0.85)},
			{Threshold: 0.015, F1: f1r(0.6)},
		},
		Recommendation: compare.Recommendation{
			RecommendedDetectorID: "fixed_shipped",
			SelectedThreshold:     &threshold,
			Candidates: []compare.CandidateEvidence{
				{DetectorID: "fixed_shipped", Threshold: &threshold, Eligible: true, OverallF1: f1r(0.85), OverallUtteranceErrorRate: f1r(0.1), F1DeltaVsShipped: score.Ratio{}},
				{DetectorID: "adaptive", Eligible: true, OverallF1: f1r(0.85), OverallUtteranceErrorRate: f1r(0.1), F1DeltaVsShipped: f1r(0.0)},
			},
		},
		ReproductionCommand: "make evaluate-ami-vad",
	}
}

func TestProductionDefaultsMatchPluginkit(t *testing.T) {
	d := ProductionDefaults()
	cfg := pluginkit.DefaultStreamingConfig()
	if d.SampleRate != cfg.SampleRate {
		t.Fatalf("sample rate: %d vs %d", d.SampleRate, cfg.SampleRate)
	}
	if d.EnergyThreshold != cfg.EnergyThreshold {
		t.Fatalf("energy threshold: %v vs %v", d.EnergyThreshold, cfg.EnergyThreshold)
	}
	if d.SilenceDurationMS != cfg.SilenceDuration.Milliseconds() {
		t.Fatalf("silence duration: %d vs %d", d.SilenceDurationMS, cfg.SilenceDuration.Milliseconds())
	}
	if d.VADFrameMS != cfg.VADFrame.Milliseconds() {
		t.Fatalf("vad frame: %d vs %d", d.VADFrameMS, cfg.VADFrame.Milliseconds())
	}
	if d.AdaptiveSpeechFactor != pluginkit.DefaultAdaptiveSpeechFactor {
		t.Fatalf("adaptive speech factor mismatch")
	}
}

func TestRenderContainsRequiredSections(t *testing.T) {
	out, err := RenderToBytes(sampleReport())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	s := string(out)
	for _, want := range []string{
		"Manifest digest", "Evaluation revision", "Go version", "OS/architecture",
		"Preparation schema version", "Annotation schema version",
		"AMI Meeting Corpus", "Reproduction", "make evaluate-ami-vad",
		"Recommendation", "in-sample", "Fixed threshold grid", "Shipped threshold",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("expected report to contain %q", want)
		}
	}
}

func TestRenderFourDecimalPlaces(t *testing.T) {
	out, err := RenderToBytes(sampleReport())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "0.8500") {
		t.Fatalf("expected F1 rendered to 4 decimal places, got:\n%s", out)
	}
}

func TestRenderContainsUtteranceMetricColumns(t *testing.T) {
	out, err := RenderToBytes(sampleReport())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		"Miss rate", "Split rate", "Merge rate", "Spurious rate", "Utterance error rate",
		"Start median", "Start p95", "End median", "End p95",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("expected report to contain utterance-metric column %q, got:\n%s", want, s)
		}
	}
}

func TestRenderUtteranceMetricValuesToFourDecimalPlaces(t *testing.T) {
	r := sampleReport()
	r.CellResults[0].UtteranceMetrics.SplitExtraRate = f1r(0.125)
	r.CellResults[0].UtteranceMetrics.MergeExtraRate = f1r(0.375)
	r.CellResults[0].UtteranceMetrics.StartErrorMedian = f1r(0.02)
	r.CellResults[0].UtteranceMetrics.StartErrorP95 = f1r(0.05)
	r.CellResults[0].UtteranceMetrics.EndErrorMedian = f1r(0.03)
	r.CellResults[0].UtteranceMetrics.EndErrorP95 = f1r(0.06)
	out, err := RenderToBytes(r)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"0.1250", "0.3750", "0.0200", "0.0500", "0.0300", "0.0600"} {
		if !strings.Contains(s, want) {
			t.Fatalf("expected utterance metric value %q rendered to 4 decimal places, got:\n%s", want, s)
		}
	}
}

func TestRenderNAForUndefinedBoundaryError(t *testing.T) {
	// sampleReport leaves boundary-error fields at their zero value (no
	// one-to-one pairs observed), which must render as "n/a" rather than
	// "0.0000" or being silently omitted.
	r := sampleReport()
	out, err := RenderToBytes(r)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "Start median") || !strings.Contains(s, "n/a") {
		t.Fatalf("expected n/a rendering for undefined boundary error, got:\n%s", s)
	}
}

func TestRenderNAForInvalidRatio(t *testing.T) {
	r := sampleReport()
	r.CellResults[0].FrameMetrics.F1 = score.Ratio{}
	out, err := RenderToBytes(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "n/a") {
		t.Fatal("expected n/a rendering for invalid ratio")
	}
}

func TestRenderNoTimestampsOrPaths(t *testing.T) {
	out, err := RenderToBytes(sampleReport())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, forbidden := range []string{"/tmp/", "/home/", "/workspaces/", ":00 UTC", "202" /* year prefix, loose check */} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("expected no timestamps/paths, found %q in report", forbidden)
		}
	}
}

func TestRenderIsByteIdenticalForIdenticalInput(t *testing.T) {
	r := sampleReport()
	a, err := RenderToBytes(r)
	if err != nil {
		t.Fatal(err)
	}
	b, err := RenderToBytes(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatal("expected byte-identical output for identical input")
	}
}

func TestRenderStableSortOrderIndependentOfInputOrder(t *testing.T) {
	r1 := sampleReport()
	r2 := sampleReport()
	// Reverse cell and overall order.
	for i, j := 0, len(r2.CellResults)-1; i < j; i, j = i+1, j-1 {
		r2.CellResults[i], r2.CellResults[j] = r2.CellResults[j], r2.CellResults[i]
	}
	for i, j := 0, len(r2.OverallResults)-1; i < j; i, j = i+1, j-1 {
		r2.OverallResults[i], r2.OverallResults[j] = r2.OverallResults[j], r2.OverallResults[i]
	}
	a, err := RenderToBytes(r1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := RenderToBytes(r2)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatal("expected rendering to be independent of input slice ordering")
	}
}

func TestValidateFailsOnMissingCell(t *testing.T) {
	r := sampleReport()
	// Drop every result for the non_scenario/fixed_distant cell.
	var kept []compare.CellResult
	for _, c := range r.CellResults {
		if c.Cell.Class == "non_scenario" && c.Cell.Mic == "fixed_distant" {
			continue
		}
		kept = append(kept, c)
	}
	r.CellResults = kept
	if _, err := RenderToBytes(r); err == nil {
		t.Fatal("expected missing-cell validation failure")
	}
}

func TestValidateFailsOnMissingDigest(t *testing.T) {
	r := sampleReport()
	r.ManifestDigest = "short"
	if _, err := RenderToBytes(r); err == nil {
		t.Fatal("expected digest validation failure")
	}
}

func TestValidateFailsOnMissingRecommendation(t *testing.T) {
	r := sampleReport()
	r.Recommendation = compare.Recommendation{}
	if _, err := RenderToBytes(r); err == nil {
		t.Fatal("expected missing-recommendation validation failure")
	}
}

func TestWriteReportCheckModeDoesNotReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.md")
	if err := os.WriteFile(path, []byte("stale content"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := WriteReport(path, sampleReport(), true)
	if err == nil {
		t.Fatal("expected check mode to report a mismatch")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "stale content" {
		t.Fatal("expected check mode to leave the file untouched")
	}
}

func TestWriteReportWritesAndCheckThenPasses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.md")
	changed, err := WriteReport(path, sampleReport(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected first write to report changed=true")
	}
	if _, err := WriteReport(path, sampleReport(), true); err != nil {
		t.Fatalf("expected check to pass after a matching write: %v", err)
	}
	// No leftover temp files.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "report.md" {
			t.Fatalf("unexpected leftover file %q", e.Name())
		}
	}
}

func TestWriteReportSecondWriteNoOpWhenUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.md")
	if _, err := WriteReport(path, sampleReport(), false); err != nil {
		t.Fatal(err)
	}
	changed, err := WriteReport(path, sampleReport(), false)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("expected no-op write to report changed=false")
	}
}

func TestRenderTablePadsColumnsForPrettierCompatibility(t *testing.T) {
	var b bytes.Buffer
	renderTable(&b, []string{"A", "BB"}, [][]string{{"1", "22"}, {"333", "4"}})
	got := b.String()
	want := "| A   | BB  |\n| --- | --- |\n| 1   | 22  |\n| 333 | 4   |\n"
	if got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}
