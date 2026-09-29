// Command ami-eval runs the reproducible AMI VAD detector evaluation
// (muesli#778): validate the pinned manifest, acquire and verify corpus
// objects into a git-ignored cache, prepare canonical audio and reference
// intervals, run the production detector matrix, aggregate and recommend,
// and regenerate the committed comparison report.
//
// See docs/ami-vad-evaluation.md for usage, layout, and reproducibility
// details. Run via `make evaluate-ami-vad` or `go run
// ./cmd/whisper-cpp-streaming/ami-eval`.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/acquire"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/annotation"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/compare"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/evaluate"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/manifest"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/prepare"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/report"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/score"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "ami-eval:", err)
		os.Exit(1)
	}
}

type options struct {
	manifest string
	cache    string
	offline  bool
	workers  int
	check    bool
	report   string
}

func parseFlags(args []string) (options, error) {
	fs := flag.NewFlagSet("ami-eval", flag.ContinueOnError)
	manifestPath := fs.String("manifest", "", "path to the corpus manifest (default: <module root>/cmd/whisper-cpp-streaming/ami-eval/manifest.json)")
	cachePath := fs.String("cache", "", "cache directory (default: <module root>/.cache/ami-vad-eval); rejected if it aliases a repository source/output directory")
	offline := fs.Bool("offline", false, "forbid all network use; every required object must already be verified in the cache")
	workers := fs.Int("workers", 1, "concurrent recording workers, capped at 4")
	check := fs.Bool("check", false, "compare the generated report against the committed file without replacing it")
	reportPath := fs.String("report", "", "output report path (default: <module root>/docs/ami-vad-evaluation.md)")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "ami-eval acquires, prepares, evaluates, and reports on the AMI VAD detector matrix (muesli#778).")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	return options{
		manifest: *manifestPath,
		cache:    *cachePath,
		offline:  *offline,
		workers:  *workers,
		check:    *check,
		report:   *reportPath,
	}, nil
}

func run(args []string, stdout *os.File) error {
	opts, err := parseFlags(args)
	if err != nil {
		return err
	}

	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, err := acquire.FindModuleRoot(wd)
	if err != nil {
		return fmt.Errorf("locate module root: %w", err)
	}

	if opts.manifest == "" {
		opts.manifest = filepath.Join(root, "cmd", "whisper-cpp-streaming", "ami-eval", "manifest.json")
	}
	if opts.report == "" {
		opts.report = filepath.Join(root, "docs", "ami-vad-evaluation.md")
	}

	cacheRoot, err := acquire.ResolveCache(root, opts.cache)
	if err != nil {
		return err
	}
	cache := acquire.Cache{Root: cacheRoot}

	manifestBytes, err := os.ReadFile(opts.manifest)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	man, err := manifest.Load(manifestBytes)
	if err != nil {
		return fmt.Errorf("validate manifest: %w", err)
	}
	digest, err := man.Digest()
	if err != nil {
		return fmt.Errorf("compute manifest digest: %w", err)
	}

	ctx := context.Background()
	acquired, err := acquire.Acquire(ctx, cache, man, acquire.Options{Offline: opts.offline})
	if err != nil {
		return fmt.Errorf("acquire corpus objects: %w", err)
	}

	recordings := make([]evaluate.RecordingInput, 0, len(acquired))
	for _, a := range acquired {
		if a.Recording.Split != manifest.SplitTuning {
			continue
		}
		p, err := prepare.Prepare(cache, digest, a)
		if err != nil {
			return fmt.Errorf("prepare recording %s: %w", a.Recording.ID, err)
		}
		refs := make([]score.Interval, len(p.Reference.Intervals))
		for i, iv := range p.Reference.Intervals {
			refs[i] = score.Interval{Start: iv.Start, End: iv.End}
		}
		recordings = append(recordings, evaluate.RecordingInput{
			ID: a.Recording.ID, MeetingID: a.Recording.MeetingID, Class: a.Recording.Class, Split: string(a.Recording.Split), Mic: a.Recording.Mic,
			Audio: p.Audio, Reference: refs,
		})
	}

	matrix, err := evaluate.RunMatrix(ctx, recordings, evaluate.MatrixConfig{Workers: opts.workers, Split: string(manifest.SplitTuning)})
	if err != nil {
		return fmt.Errorf("run detector matrix: %w", err)
	}
	resultsDir := cache.ResultsDir(digest, evaluate.EvaluationVersion)
	if err := evaluate.WriteMatrix(resultsDir, matrix); err != nil {
		return fmt.Errorf("write matrix results: %w", err)
	}

	cells := compare.AggregateCells(matrix.Entries)
	overall, err := compare.AggregateOverall(cells)
	if err != nil {
		return fmt.Errorf("aggregate results: %w", err)
	}
	curve := compare.ThresholdCurve(overall, matrix.ThresholdGrid)
	best, err := compare.SelectThreshold(curve, matrix.HistoricalBaselineThreshold)
	if err != nil {
		return fmt.Errorf("select threshold: %w", err)
	}
	rec, err := compare.Recommend(overall, best, matrix.HistoricalBaselineThreshold, matrix.WhisperAvailable)
	if err != nil {
		return fmt.Errorf("build recommendation: %w", err)
	}

	rpt := report.Report{
		ManifestDigest:              digest,
		EvaluationVersion:           evaluate.EvaluationVersion,
		Environment:                 report.Environment{GoVersion: runtime.Version(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH},
		PreparationSchemaVersion:    prepare.SchemaVersion,
		AnnotationSchemaVersion:     annotation.SchemaVersion,
		Defaults:                    report.ProductionDefaults(),
		ThresholdGrid:               matrix.ThresholdGrid,
		HistoricalBaselineThreshold: matrix.HistoricalBaselineThreshold,
		WhisperAvailable:            matrix.WhisperAvailable,
		WhisperUnavailableReason:    matrix.WhisperUnavailableReason,
		CellResults:                 cells,
		OverallResults:              overall,
		ThresholdCurve:              curve,
		Recommendation:              rec,
		ReproductionCommand:         "make evaluate-ami-vad",
	}

	changed, err := report.WriteReport(opts.report, rpt, opts.check)
	if opts.check {
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, "report is up to date")
		return nil
	}
	if err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	if changed {
		fmt.Fprintf(stdout, "wrote %s\n", opts.report)
	} else {
		fmt.Fprintln(stdout, "report unchanged")
	}
	fmt.Fprintf(stdout, "recommended default: %s\n", rec.RecommendedDetectorID)
	return nil
}
