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
	b := 0.01
	g := 0.005
	det := func(id string, th *float64) compare.DetectorSummary {
		return compare.DetectorSummary{DetectorID: id, Threshold: th, Overall: compare.Metrics{
			Frame:     score.FrameMetrics{Precision: f1r(0.9), Recall: f1r(0.8), F1: f1r(0.85), FalsePositiveRate: f1r(0.1), FalseNegativeRate: f1r(0.2)},
			Utterance: score.UtteranceMetrics{MissRate: f1r(0.05), SpuriousRate: f1r(0.03), UtteranceErrorRate: f1r(0.1)},
		}, FarFieldFPR: f1r(0.2)}
	}
	return Report{
		Results: compare.Results{
			ManifestDigest:              strings.Repeat("a", 64),
			EvaluationVersion:           "v2",
			Environment:                 compare.Environment{GoVersion: "go1.25.11", GOOS: "linux", GOARCH: "amd64"},
			ThresholdGrid:               []float64{0.005, 0.01, 0.015},
			HistoricalBaselineThreshold: 0.01,
			Tuning: compare.SplitSummary{WhisperUnavailableReason: "no compatible interface", Detectors: []compare.DetectorSummary{
				det("fixed_historical_0.01", &b), det("fixed_0.005", &g), det("adaptive", nil),
			}},
			Selection: compare.Selection{Candidates: []compare.Candidate{{Threshold: 0.005, Eligible: true, UtteranceError: 0.1}}},
		},
		PreparationSchemaVersion: 1,
		AnnotationSchemaVersion:  1,
		Defaults:                 ProductionDefaults(),
		ReproductionCommand:      "make evaluate-ami-vad",
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

func TestRenderFourDecimalPlaces(t *testing.T) {
	out, err := RenderToBytes(sampleReport())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "0.8500") {
		t.Fatalf("expected F1 rendered to 4 decimal places, got:\n%s", out)
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

func TestValidateFailsOnMissingDigest(t *testing.T) {
	r := sampleReport()
	r.Results.ManifestDigest = "short"
	if _, err := RenderToBytes(r); err == nil {
		t.Fatal("expected digest validation failure")
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
