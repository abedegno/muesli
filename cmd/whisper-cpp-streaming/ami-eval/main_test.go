package main

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/report"
	"github.com/abedegno/muesli/internal/pluginkit"
)

func TestParseFlagsDefaults(t *testing.T) {
	opts, err := parseFlags(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.manifest != "" || opts.cache != "" || opts.report != "" {
		t.Fatalf("expected empty defaults deferred to run(): %+v", opts)
	}
	if opts.workers != 1 {
		t.Fatalf("expected default workers=1, got %d", opts.workers)
	}
	if opts.offline || opts.check {
		t.Fatalf("expected offline/check false by default: %+v", opts)
	}
}

func TestParseFlagsOverrides(t *testing.T) {
	opts, err := parseFlags([]string{"--offline", "--check", "--workers", "4", "--cache", "/tmp/x"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.offline || !opts.check || opts.workers != 4 || opts.cache != "/tmp/x" {
		t.Fatalf("unexpected opts: %+v", opts)
	}
}

func TestParseFlagsHelp(t *testing.T) {
	_, err := parseFlags([]string{"--help"})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("expected flag.ErrHelp, got %v", err)
	}
}

func TestRunFailsOnMissingManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	err = run([]string{"--manifest", filepath.Join(dir, "does-not-exist.json")}, f)
	if err == nil {
		t.Fatal("expected failure for a missing manifest file")
	}
}

// TestRunOfflinePipelineFailsBeforePrepareOrEvaluate proves, at the whole-
// pipeline level (not just one acquire.EnsureObject call), that an offline
// run against an incomplete cache fails during acquisition and never
// reaches prepare or evaluate. prepare.Prepare and evaluate.RunMatrix are
// the only code that ever creates the cache's "prepared" and "results"
// directories, so their continued absence after a failed run is direct
// evidence neither stage was entered -- a filesystem-level stand-in for an
// invocation counter, without needing to inject spies into main's
// unconditional pipeline wiring.
func TestRunOfflinePipelineFailsBeforePrepareOrEvaluate(t *testing.T) {
	// Resolve the real committed, schema-valid manifest before changing the
	// working directory below.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	realManifest := filepath.Join(wd, "manifest.json")
	if _, err := os.Stat(realManifest); err != nil {
		t.Fatalf("expected committed manifest.json to exist: %v", err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cacheDir := filepath.Join(t.TempDir(), "empty-cache")
	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	err = run([]string{"--offline", "--manifest", realManifest, "--cache", cacheDir}, f)
	if err == nil {
		t.Fatal("expected offline run against an empty cache to fail")
	}
	if !strings.Contains(err.Error(), "acquire") {
		t.Fatalf("expected failure to originate in acquisition, got: %v", err)
	}

	for _, sub := range []string{"prepared", "results"} {
		if _, statErr := os.Stat(filepath.Join(cacheDir, sub)); !os.IsNotExist(statErr) {
			t.Fatalf("expected cache %q directory to never be created (prepare/evaluate must not run), stat err: %v", sub, statErr)
		}
	}
}

func TestRunRejectsDeniedCachePath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	err = run([]string{"--cache", filepath.Join(dir, "internal", "evil-cache")}, f)
	if err == nil {
		t.Fatal("expected cache path rejection before any manifest read is attempted")
	}
}

// TestCommittedReportDecisionMatchesProductionDefaults reads the committed
// report's machine-readable decision (no corpus needed) and requires its
// final threshold to equal both the runtime default and the live schema
// default, with the comparator still the historical 0.01.
func TestCommittedReportDecisionMatchesProductionDefaults(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "ami-vad-evaluation.md"))
	if err != nil {
		t.Fatalf("read committed report: %v", err)
	}
	blk, err := report.ParseDecisionBlock(doc)
	if err != nil {
		t.Fatalf("committed report has no valid decision block: %v", err)
	}
	if blk.HistoricalBaselineThreshold != 0.01 {
		t.Fatalf("historical baseline = %v, want 0.01", blk.HistoricalBaselineThreshold)
	}
	runtimeDefault := pluginkit.DefaultStreamingConfig().EnergyThreshold
	schemaDefault, err := report.LiveSchemaDefault()
	if err != nil {
		t.Fatal(err)
	}
	if err := report.CheckProductionConsistency(blk.FinalThreshold, runtimeDefault, schemaDefault); err != nil {
		t.Fatal(err)
	}
	if blk.RuntimeDefaultThreshold != runtimeDefault || blk.LiveSchemaDefault != schemaDefault {
		t.Fatalf("committed report recorded defaults %v/%v, live %v/%v: regenerate it", blk.RuntimeDefaultThreshold, blk.LiveSchemaDefault, runtimeDefault, schemaDefault)
	}
	switch blk.Outcome {
	case "ship":
		if blk.SelectedThreshold == nil || *blk.SelectedThreshold != blk.FinalThreshold {
			t.Fatalf("ship decision must ship the selected threshold: %+v", blk)
		}
	case "keep":
		if blk.FinalThreshold != 0.01 {
			t.Fatalf("keep decision must retain 0.01: %+v", blk)
		}
	default:
		t.Fatalf("unknown outcome %q", blk.Outcome)
	}
}
