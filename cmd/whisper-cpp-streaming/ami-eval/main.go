// Command ami-eval runs the reproducible AMI VAD fixed-threshold evaluation
// (muesli#778, muesli#782): validate the pinned manifest, acquire and verify
// corpus objects into a git-ignored cache, then -- one split at a time --
// prepare and evaluate the tuning recordings, select a fixed threshold from
// tuning evidence alone, and only then prepare and evaluate the held-out
// recordings and apply the held-out gates. It persists the raw evidence and
// regenerates the committed report.
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
	check := fs.Bool("check", false, "compare the generated report against the committed file without replacing it, and require the decision's final threshold to equal the runtime and live schema defaults")
	reportPath := fs.String("report", "", "output report path (default: <module root>/docs/ami-vad-evaluation.md)")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "ami-eval selects a fixed VAD energy threshold on AMI tuning meetings and validates it on held-out meetings (muesli#782).")
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

// stages are the per-split work the orchestrator performs; production
// wiring uses prepare.Prepare and evaluate.RunMatrix, tests inject spies
// and faults.
type stages struct {
	prepare   func(acquire.AcquiredRecording) (evaluate.RecordingInput, error)
	runMatrix func(context.Context, []evaluate.RecordingInput, evaluate.MatrixConfig) (evaluate.Matrix, error)
	// selected, when non-nil, observes the frozen tuning selection the
	// moment it exists (a test seam for call ordering).
	selected func(compare.Selection)
}

// productionStages prepares from the verified cache and runs the real
// detector matrix.
func productionStages(cache acquire.Cache, digest string) stages {
	return stages{
		prepare: func(a acquire.AcquiredRecording) (evaluate.RecordingInput, error) {
			p, err := prepare.Prepare(cache, digest, a)
			if err != nil {
				return evaluate.RecordingInput{}, err
			}
			refs := make([]score.Interval, len(p.Reference.Intervals))
			for i, iv := range p.Reference.Intervals {
				refs[i] = score.Interval{Start: iv.Start, End: iv.End}
			}
			return evaluate.RecordingInput{
				ID: a.Recording.ID, MeetingID: a.Recording.MeetingID, Class: a.Recording.Class,
				Split: string(a.Recording.Split), Mic: a.Recording.Mic,
				Audio: p.Audio, Reference: refs,
			}, nil
		},
		runMatrix: evaluate.RunMatrix,
	}
}

// evaluation is the outcome of evaluateSplits.
type evaluation struct {
	tuning    evaluate.Matrix
	heldOut   evaluate.Matrix
	selection compare.Selection
	decision  compare.Decision
}

// evaluateSplits runs tuning to a frozen selection before any held-out
// recording is prepared or evaluated. A grid-edge winner (or any other
// tuning failure) stops before held-out work. With no eligible candidate
// the held-out split is still evaluated descriptively, and the decision
// keeps the historical baseline.
func evaluateSplits(ctx context.Context, man *manifest.Manifest, acquired []acquire.AcquiredRecording, workers int, st stages) (evaluation, error) {
	bySplit := map[manifest.Split][]acquire.AcquiredRecording{}
	for _, a := range acquired {
		if !a.Recording.Split.Valid() {
			return evaluation{}, fmt.Errorf("recording %s has unknown split %q", a.Recording.ID, a.Recording.Split)
		}
		bySplit[a.Recording.Split] = append(bySplit[a.Recording.Split], a)
	}
	for _, split := range manifest.Splits() {
		if want, got := len(man.RecordingsForSplit(split)), len(bySplit[split]); got != want {
			return evaluation{}, fmt.Errorf("acquired %d %s recordings, manifest requires %d", got, split, want)
		}
	}

	var out evaluation
	var err error
	out.tuning, err = runSplit(ctx, manifest.SplitTuning, bySplit[manifest.SplitTuning], workers, st)
	if err != nil {
		return evaluation{}, err
	}
	tuningEvidence, err := compare.BuildTuningEvidence(man, out.tuning)
	if err != nil {
		return evaluation{}, fmt.Errorf("validate tuning evidence: %w", err)
	}
	out.selection, err = compare.SelectThreshold(tuningEvidence)
	if err != nil {
		return evaluation{}, fmt.Errorf("select threshold on tuning split: %w", err)
	}
	if st.selected != nil {
		st.selected(out.selection)
	}

	out.heldOut, err = runSplit(ctx, manifest.SplitHeldOut, bySplit[manifest.SplitHeldOut], workers, st)
	if err != nil {
		return evaluation{}, err
	}
	heldOutEvidence, err := compare.BuildHeldOutEvidence(man, out.heldOut)
	if err != nil {
		return evaluation{}, fmt.Errorf("validate held-out evidence: %w", err)
	}
	out.decision, err = compare.Decide(out.selection, heldOutEvidence)
	if err != nil {
		return evaluation{}, fmt.Errorf("apply held-out gates: %w", err)
	}
	return out, nil
}

// runSplit prepares one split's recordings and evaluates them. The
// prepared audio lives only for the duration of this call: the returned
// matrix is metrics-only, so no split's audio is retained while the other
// split is loaded.
func runSplit(ctx context.Context, split manifest.Split, acquired []acquire.AcquiredRecording, workers int, st stages) (evaluate.Matrix, error) {
	inputs := make([]evaluate.RecordingInput, 0, len(acquired))
	for _, a := range acquired {
		in, err := st.prepare(a)
		if err != nil {
			return evaluate.Matrix{}, fmt.Errorf("prepare %s recording %s: %w", split, a.Recording.ID, err)
		}
		inputs = append(inputs, in)
	}
	m, err := st.runMatrix(ctx, inputs, evaluate.MatrixConfig{Workers: workers, Split: string(split)})
	if err != nil {
		return evaluate.Matrix{}, fmt.Errorf("run %s detector matrix: %w", split, err)
	}
	return m, nil
}

func run(args []string, stdout *os.File) error {
	return runWith(args, stdout, productionStages)
}

func runWith(args []string, stdout *os.File, newStages func(acquire.Cache, string) stages) error {
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

	ev, err := evaluateSplits(ctx, man, acquired, opts.workers, newStages(cache, digest))
	if err != nil {
		return err
	}

	env := compare.Environment{GoVersion: runtime.Version(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
	results, err := compare.NewResults(man, digest, env, ev.tuning, ev.heldOut, ev.selection, ev.decision)
	if err != nil {
		return fmt.Errorf("assemble results: %w", err)
	}
	if err := compare.WriteResults(cache.ResultsDir(digest, evaluate.EvaluationVersion), results); err != nil {
		return fmt.Errorf("write results: %w", err)
	}

	schemaDefault, err := report.LiveSchemaDefault()
	if err != nil {
		return err
	}
	rpt := report.Report{
		Results:                  results,
		LiveSchemaDefault:        schemaDefault,
		PreparationSchemaVersion: prepare.SchemaVersion,
		AnnotationSchemaVersion:  annotation.SchemaVersion,
		Defaults:                 report.ProductionDefaults(),
		ReproductionCommand:      "make evaluate-ami-vad",
	}

	printDecision(stdout, results)
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
	return nil
}

func printDecision(stdout *os.File, r compare.Results) {
	if r.Selection.Selected != nil {
		fmt.Fprintf(stdout, "selected threshold (tuning): %s\n", evaluate.ThresholdLabel(*r.Selection.Selected))
	} else {
		fmt.Fprintf(stdout, "selected threshold (tuning): none -- %s\n", r.Selection.NoEligibleReason)
	}
	fmt.Fprintf(stdout, "decision: %s %s\n", r.Decision.Outcome, evaluate.ThresholdLabel(r.Decision.FinalThreshold))
}
