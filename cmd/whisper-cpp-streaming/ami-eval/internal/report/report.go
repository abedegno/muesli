// Package report renders the committed AMI VAD evaluation comparison
// (muesli#778) as deterministic Markdown: stable sort order, exactly four
// decimal places for every rate/F1/percentage/boundary error, "n/a" for
// undefined ratios, and no timestamps or local filesystem paths, so
// byte-identical output is possible for repeated runs with an identical
// recorded environment.
package report

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/compare"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/score"
	"github.com/abedegno/muesli/internal/pluginkit"
)

// Environment records the execution-environment inputs that can affect
// floating-point and dependency behavior between otherwise-identical runs.
type Environment struct {
	GoVersion string
	GOOS      string
	GOARCH    string
}

// Defaults mirrors the production streaming and adaptive-detector defaults
// actually used, read live from pluginkit so this can never silently drift
// from what production ships.
type Defaults struct {
	SampleRate             int
	MaxWindowMS            int64
	PartialIntervalMS      int64
	SilenceDurationMS      int64
	EnergyThreshold        float64
	SilenceHysteresisMS    int64
	VADFrameMS             int64
	AdaptiveSpeechQuantile float64
	AdaptiveSpeechFactor   float64
	AdaptiveNoiseQuantile  float64
	AdaptiveNoiseFactor    float64
	AdaptiveMinThreshold   float64
	AdaptiveWarmupMS       int64
	AdaptiveUpdateEveryMS  int64
	AdaptiveHorizonMS      int64
	AdaptiveMaxSlew        float64
}

// ProductionDefaults reads the live production defaults from pluginkit.
func ProductionDefaults() Defaults {
	cfg := pluginkit.DefaultStreamingConfig()
	return Defaults{
		SampleRate:             cfg.SampleRate,
		MaxWindowMS:            cfg.MaxWindow.Milliseconds(),
		PartialIntervalMS:      cfg.PartialInterval.Milliseconds(),
		SilenceDurationMS:      cfg.SilenceDuration.Milliseconds(),
		EnergyThreshold:        cfg.EnergyThreshold,
		SilenceHysteresisMS:    cfg.SilenceHysteresis.Milliseconds(),
		VADFrameMS:             cfg.VADFrame.Milliseconds(),
		AdaptiveSpeechQuantile: pluginkit.DefaultAdaptiveSpeechQuantile,
		AdaptiveSpeechFactor:   pluginkit.DefaultAdaptiveSpeechFactor,
		AdaptiveNoiseQuantile:  pluginkit.DefaultAdaptiveNoiseQuantile,
		AdaptiveNoiseFactor:    pluginkit.DefaultAdaptiveNoiseFactor,
		AdaptiveMinThreshold:   pluginkit.DefaultAdaptiveMinThreshold,
		AdaptiveWarmupMS:       pluginkit.DefaultAdaptiveWarmup.Milliseconds(),
		AdaptiveUpdateEveryMS:  pluginkit.DefaultAdaptiveUpdateEvery.Milliseconds(),
		AdaptiveHorizonMS:      pluginkit.DefaultAdaptiveHorizon.Milliseconds(),
		AdaptiveMaxSlew:        pluginkit.DefaultAdaptiveMaxSlew,
	}
}

// Report is every structured input to the rendered comparison document.
type Report struct {
	ManifestDigest           string
	EvaluationVersion        string
	Environment              Environment
	PreparationSchemaVersion int
	AnnotationSchemaVersion  int
	Defaults                 Defaults
	ThresholdGrid            []float64
	ShippedThreshold         float64
	WhisperAvailable         bool
	WhisperUnavailableReason string
	CellResults              []compare.CellResult
	OverallResults           []compare.OverallResult
	ThresholdCurve           []compare.ThresholdPoint
	Recommendation           compare.Recommendation
	ReproductionCommand      string
}

// Validate checks that a report has everything required before it may
// replace the committed file: all four cells for every detector, AMI
// credit is implicit (rendered unconditionally), digest, environment,
// schemas, defaults, parameters, reproduction command, and recommendation
// evidence.
func Validate(r Report) error {
	if len(r.ManifestDigest) != 64 {
		return fmt.Errorf("report: manifest digest must be 64 hex characters, got %q", r.ManifestDigest)
	}
	if r.EvaluationVersion == "" {
		return fmt.Errorf("report: evaluation version is required")
	}
	if r.Environment.GoVersion == "" || r.Environment.GOOS == "" || r.Environment.GOARCH == "" {
		return fmt.Errorf("report: execution environment (Go version, OS, architecture) is required")
	}
	if len(r.ThresholdGrid) == 0 {
		return fmt.Errorf("report: threshold grid is required")
	}
	if r.ReproductionCommand == "" {
		return fmt.Errorf("report: reproduction command is required")
	}
	if r.Recommendation.RecommendedDetectorID == "" {
		return fmt.Errorf("report: recommendation is required")
	}
	if len(r.Recommendation.Candidates) == 0 {
		return fmt.Errorf("report: recommendation evidence is required")
	}
	required := compare.AllCells()
	haveCell := map[compare.CellKey]map[string]bool{}
	for _, c := range r.CellResults {
		if haveCell[c.Cell] == nil {
			haveCell[c.Cell] = map[string]bool{}
		}
		haveCell[c.Cell][c.DetectorID] = true
	}
	for _, cell := range required {
		if len(haveCell[cell]) == 0 {
			return fmt.Errorf("report: missing all results for cell %+v", cell)
		}
	}
	for _, o := range r.OverallResults {
		for _, cell := range required {
			if !haveCell[cell][o.DetectorID] {
				return fmt.Errorf("report: detector %q is missing cell %+v", o.DetectorID, cell)
			}
		}
	}
	return nil
}

func ratioStr(r score.Ratio) string {
	if !r.Valid {
		return "n/a"
	}
	return fmt.Sprintf("%.4f", r.Value)
}

func floatStr4(f float64) string { return fmt.Sprintf("%.4f", f) }

// renderTable writes a GFM Markdown table with Prettier-compatible column
// padding (each column padded to its widest cell, minimum width 3 to leave
// room for the "---" separator), so the generated report never needs a
// separate reformatting pass to satisfy the repository's Prettier check.
func renderTable(b *bytes.Buffer, headers []string, rows [][]string) {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}
	for i := range widths {
		if widths[i] < 3 {
			widths[i] = 3
		}
	}

	writeRow := func(cells []string) {
		b.WriteString("|")
		for i, w := range widths {
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			fmt.Fprintf(b, " %-*s |", w, cell)
		}
		b.WriteString("\n")
	}
	writeRow(headers)
	sep := make([]string, len(widths))
	for i, w := range widths {
		sep[i] = strings.Repeat("-", w)
	}
	writeRow(sep)
	for _, row := range rows {
		writeRow(row)
	}
}

// RenderToBytes renders the deterministic Markdown report.
func RenderToBytes(r Report) ([]byte, error) {
	if err := Validate(r); err != nil {
		return nil, err
	}
	var b bytes.Buffer

	fmt.Fprintln(&b, "# AMI VAD Detector Evaluation")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Generated by `cmd/whisper-cpp-streaming/ami-eval` (muesli#778). This report compares")
	fmt.Fprintln(&b, "voice-activity detectors used by the streaming transcription path against a pinned")
	fmt.Fprintln(&b, "AMI Meeting Corpus slice, through the production `StreamingSession` fed canonical")
	fmt.Fprintln(&b, "16kHz mono audio in 200ms calls. It changes no shipped detector selection,")
	fmt.Fprintln(&b, "configuration, threshold, framing, or unrelated utterance-loss behavior.")
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Reproduction")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "```")
	fmt.Fprintln(&b, r.ReproductionCommand)
	fmt.Fprintln(&b, "```")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Byte-identical output is promised only for repeated runs with an identical corpus,")
	fmt.Fprintln(&b, "manifest, evaluation revision, parameters, thresholds, and recorded execution")
	fmt.Fprintln(&b, "environment below. A different supported Go version or OS/architecture may produce")
	fmt.Fprintln(&b, "valid, auditable results that are not byte-identical.")
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Corpus credit")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "This evaluation uses recordings and manual annotations from the")
	fmt.Fprintln(&b, "[AMI Meeting Corpus](http://groups.inf.ed.ac.uk/ami/corpus/), made available by the")
	fmt.Fprintln(&b, "AMI Consortium under a Creative Commons Attribution 4.0 International licence. See")
	fmt.Fprintln(&b, "the [corpus overview](http://groups.inf.ed.ac.uk/ami/corpus/) and")
	fmt.Fprintln(&b, "[manual annotation documentation](http://groups.inf.ed.ac.uk/ami/corpus/annotation.shtml)")
	fmt.Fprintln(&b, "for full corpus and licensing details. No corpus audio or annotation is committed to")
	fmt.Fprintln(&b, "this repository or used in continuous integration; see `cmd/whisper-cpp-streaming/ami-eval/manifest.json`")
	fmt.Fprintln(&b, "for the exact pinned objects.")
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Reproducibility metadata")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- Manifest digest: `%s`\n", r.ManifestDigest)
	fmt.Fprintf(&b, "- Evaluation revision: `%s`\n", r.EvaluationVersion)
	fmt.Fprintf(&b, "- Preparation schema version: `%d`\n", r.PreparationSchemaVersion)
	fmt.Fprintf(&b, "- Annotation schema version: `%d`\n", r.AnnotationSchemaVersion)
	fmt.Fprintf(&b, "- Go version: `%s`\n", r.Environment.GoVersion)
	fmt.Fprintf(&b, "- OS/architecture: `%s/%s`\n", r.Environment.GOOS, r.Environment.GOARCH)
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Production defaults")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Read live from `internal/pluginkit.DefaultStreamingConfig` and the adaptive detector's")
	fmt.Fprintln(&b, "defaults, so this table cannot drift from what production ships.")
	fmt.Fprintln(&b)
	d := r.Defaults
	renderTable(&b, []string{"Setting", "Value"}, [][]string{
		{"Sample rate", fmt.Sprintf("%d Hz", d.SampleRate)},
		{"Max window", fmt.Sprintf("%d ms", d.MaxWindowMS)},
		{"Partial interval", fmt.Sprintf("%d ms", d.PartialIntervalMS)},
		{"Silence duration", fmt.Sprintf("%d ms", d.SilenceDurationMS)},
		{"Shipped energy threshold", floatStr4(d.EnergyThreshold)},
		{"Silence hysteresis", fmt.Sprintf("%d ms", d.SilenceHysteresisMS)},
		{"VAD frame", fmt.Sprintf("%d ms", d.VADFrameMS)},
		{"Adaptive speech quantile", floatStr4(d.AdaptiveSpeechQuantile)},
		{"Adaptive speech factor", floatStr4(d.AdaptiveSpeechFactor)},
		{"Adaptive noise quantile", floatStr4(d.AdaptiveNoiseQuantile)},
		{"Adaptive noise factor", floatStr4(d.AdaptiveNoiseFactor)},
		{"Adaptive min threshold", floatStr4(d.AdaptiveMinThreshold)},
		{"Adaptive warmup", fmt.Sprintf("%d ms", d.AdaptiveWarmupMS)},
		{"Adaptive update interval", fmt.Sprintf("%d ms", d.AdaptiveUpdateEveryMS)},
		{"Adaptive horizon", fmt.Sprintf("%d ms", d.AdaptiveHorizonMS)},
		{"Adaptive max slew", floatStr4(d.AdaptiveMaxSlew)},
	})
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Detector parameters")
	fmt.Fprintln(&b)
	grid := append([]float64(nil), r.ThresholdGrid...)
	sort.Float64s(grid)
	gridStrs := make([]string, len(grid))
	for i, t := range grid {
		gridStrs[i] = floatStr4(t)
	}
	fmt.Fprintf(&b, "- Fixed threshold grid: %s\n", strings.Join(gridStrs, ", "))
	fmt.Fprintf(&b, "- Shipped threshold: %s\n", floatStr4(r.ShippedThreshold))
	if r.WhisperAvailable {
		fmt.Fprintln(&b, "- whisper.cpp detector: available")
	} else {
		fmt.Fprintf(&b, "- whisper.cpp detector: unavailable -- %s\n", r.WhisperUnavailableReason)
	}
	fmt.Fprintln(&b)

	renderCellResults(&b, r.CellResults)
	renderOverallResults(&b, r.OverallResults)
	renderThresholdCurve(&b, r.ThresholdCurve, r.ShippedThreshold)
	renderRecommendation(&b, r.Recommendation)
	renderUsage(&b)

	return b.Bytes(), nil
}

func renderUsage(b *bytes.Buffer) {
	fmt.Fprintln(b, "## Usage")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "`make evaluate-ami-vad` (or `go run ./cmd/whisper-cpp-streaming/ami-eval`) runs the")
	fmt.Fprintln(b, "full validate -> acquire -> prepare -> evaluate -> compare -> render pipeline from an")
	fmt.Fprintln(b, "empty cache. Flags:")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "- `--manifest` -- path to the corpus manifest (default: the committed")
	fmt.Fprintln(b, "  `cmd/whisper-cpp-streaming/ami-eval/manifest.json`).")
	fmt.Fprintln(b, "- `--cache` -- cache directory (default: `.cache/ami-vad-eval/` under the module")
	fmt.Fprintln(b, "  root, which is git-ignored). Any other path may be used for a shared, hosted, or")
	fmt.Fprintln(b, "  preseeded cache, except that a path aliasing (or nested under) a repository")
	fmt.Fprintln(b, "  source/output directory is rejected before any cache use.")
	fmt.Fprintln(b, "- `--offline` -- forbid all network use; every required object must already be")
	fmt.Fprintln(b, "  present and verified in the cache, or the run fails closed before evaluation.")
	fmt.Fprintln(b, "- `--workers` -- concurrent recording workers, capped at 4; each worker owns at")
	fmt.Fprintln(b, "  most one recording (and its own fresh detector and session) at a time. Downloads")
	fmt.Fprintln(b, "  remain sequential regardless of this setting.")
	fmt.Fprintln(b, "- `--check` -- render the report and compare it against the committed file without")
	fmt.Fprintln(b, "  replacing it; exits non-zero on any difference. Used to verify the committed")
	fmt.Fprintln(b, "  report is current without mutating it.")
	fmt.Fprintln(b, "- `--report` -- output report path (default: the committed")
	fmt.Fprintln(b, "  `docs/ami-vad-evaluation.md`).")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "### Cache layout")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "```")
	fmt.Fprintln(b, "<cache>/downloads/<sha256>                                   verified source objects")
	fmt.Fprintln(b, "<cache>/prepared/<manifest-digest>/<recording-id>/           canonical audio + reference + provenance")
	fmt.Fprintln(b, "<cache>/results/<manifest-digest>/<evaluation-version>/      raw matrix.json")
	fmt.Fprintln(b, "```")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "Downloads are sequential, HTTPS-only across redirects, subject to a 15-minute")
	fmt.Fprintln(b, "per-object timeout and a manifest byte ceiling enforced while streaming, and")
	fmt.Fprintln(b, "promoted atomically only after size and SHA-256 verification. A cached object that")
	fmt.Fprintln(b, "fails rehashing is quarantined (renamed aside) with a diagnostic rather than reused")
	fmt.Fprintln(b, "or silently deleted. Each recording/detector run has a deadline of")
	fmt.Fprintln(b, "`min(30m, max(1m, 2*audio duration))`.")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "No credentials are ever accepted or persisted by this tool; a protected source must")
	fmt.Fprintln(b, "be preseeded at its checksum-addressed cache location before an offline run.")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "### Reproducibility")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "Byte-identical report output is promised only for repeated runs with an identical")
	fmt.Fprintln(b, "corpus, manifest, evaluation revision, parameters, threshold grid, and recorded")
	fmt.Fprintln(b, "execution environment (Go version and OS/architecture, both recorded above). A")
	fmt.Fprintln(b, "different supported environment can produce valid, auditable results while")
	fmt.Fprintln(b, "recording its own environment, without matching this report byte-for-byte.")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "### Continuous integration")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "CI never downloads the AMI corpus or runs this evaluation end to end. Its")
	fmt.Fprintln(b, "corpus-independent unit and mutation tests -- annotation interpretation, segment")
	fmt.Fprintln(b, "merging, WAV decoding, frame/utterance scoring, threshold selection, and report")
	fmt.Fprintln(b, "rendering -- run against minimized real-shape fixtures and generated audio, and are")
	fmt.Fprintln(b, "part of the normal Go test suite.")
}

func sortedCells(cells []compare.CellResult) []compare.CellResult {
	out := append([]compare.CellResult(nil), cells...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Cell.Class != out[j].Cell.Class {
			return out[i].Cell.Class < out[j].Cell.Class
		}
		if out[i].Cell.Mic != out[j].Cell.Mic {
			return out[i].Cell.Mic < out[j].Cell.Mic
		}
		return out[i].DetectorID < out[j].DetectorID
	})
	return out
}

func renderCellResults(b *bytes.Buffer, cells []compare.CellResult) {
	fmt.Fprintln(b, "## Per-cell results")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "Each cell macro-averages the recordings in that meeting-class/microphone-condition")
	fmt.Fprintln(b, "combination.")
	fmt.Fprintln(b)
	headers := []string{"Class", "Mic", "Detector", "Recordings", "Precision", "Recall", "F1", "FPR", "FNR", "Miss rate", "Split rate", "Merge rate", "Spurious rate", "Utterance error rate"}
	var rows [][]string
	for _, c := range sortedCells(cells) {
		rows = append(rows, []string{
			c.Cell.Class, c.Cell.Mic, c.DetectorID, fmt.Sprintf("%d", c.RecordingCount),
			ratioStr(c.FrameMetrics.Precision), ratioStr(c.FrameMetrics.Recall), ratioStr(c.FrameMetrics.F1),
			ratioStr(c.FrameMetrics.FalsePositiveRate), ratioStr(c.FrameMetrics.FalseNegativeRate),
			ratioStr(c.UtteranceMetrics.MissRate), ratioStr(c.UtteranceMetrics.SplitExtraRate),
			ratioStr(c.UtteranceMetrics.MergeExtraRate), ratioStr(c.UtteranceMetrics.SpuriousRate), ratioStr(c.UtteranceMetrics.UtteranceErrorRate),
		})
	}
	renderTable(b, headers, rows)
	fmt.Fprintln(b)

	fmt.Fprintln(b, "Boundary error is the absolute start/end timing error (seconds) for one-to-one")
	fmt.Fprintln(b, "matched reference/prediction pairs only; `n/a` when a cell/detector has none.")
	fmt.Fprintln(b)
	boundaryHeaders := []string{"Class", "Mic", "Detector", "Start median", "Start p95", "End median", "End p95"}
	var boundaryRows [][]string
	for _, c := range sortedCells(cells) {
		boundaryRows = append(boundaryRows, []string{
			c.Cell.Class, c.Cell.Mic, c.DetectorID,
			ratioStr(c.UtteranceMetrics.StartErrorMedian), ratioStr(c.UtteranceMetrics.StartErrorP95),
			ratioStr(c.UtteranceMetrics.EndErrorMedian), ratioStr(c.UtteranceMetrics.EndErrorP95),
		})
	}
	renderTable(b, boundaryHeaders, boundaryRows)
	fmt.Fprintln(b)
}

func sortedOverall(overall []compare.OverallResult) []compare.OverallResult {
	out := append([]compare.OverallResult(nil), overall...)
	sort.Slice(out, func(i, j int) bool { return out[i].DetectorID < out[j].DetectorID })
	return out
}

func renderOverallResults(b *bytes.Buffer, overall []compare.OverallResult) {
	fmt.Fprintln(b, "## Overall comparison")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "Equal-weight macro average over the four required cells, so duration and speech")
	fmt.Fprintln(b, "prevalence cannot dominate.")
	fmt.Fprintln(b)
	headers := []string{"Detector", "F1", "Precision", "Recall", "FPR", "FNR", "Miss rate", "Split rate", "Merge rate", "Spurious rate", "Utterance error rate"}
	var rows [][]string
	for _, o := range sortedOverall(overall) {
		rows = append(rows, []string{
			o.DetectorID, ratioStr(o.FrameMetrics.F1), ratioStr(o.FrameMetrics.Precision), ratioStr(o.FrameMetrics.Recall),
			ratioStr(o.FrameMetrics.FalsePositiveRate), ratioStr(o.FrameMetrics.FalseNegativeRate),
			ratioStr(o.UtteranceMetrics.MissRate), ratioStr(o.UtteranceMetrics.SplitExtraRate),
			ratioStr(o.UtteranceMetrics.MergeExtraRate), ratioStr(o.UtteranceMetrics.SpuriousRate), ratioStr(o.UtteranceMetrics.UtteranceErrorRate),
		})
	}
	renderTable(b, headers, rows)
	fmt.Fprintln(b)

	boundaryHeaders := []string{"Detector", "Start median", "Start p95", "End median", "End p95"}
	var boundaryRows [][]string
	for _, o := range sortedOverall(overall) {
		boundaryRows = append(boundaryRows, []string{
			o.DetectorID,
			ratioStr(o.UtteranceMetrics.StartErrorMedian), ratioStr(o.UtteranceMetrics.StartErrorP95),
			ratioStr(o.UtteranceMetrics.EndErrorMedian), ratioStr(o.UtteranceMetrics.EndErrorP95),
		})
	}
	renderTable(b, boundaryHeaders, boundaryRows)
	fmt.Fprintln(b)
}

func renderThresholdCurve(b *bytes.Buffer, curve []compare.ThresholdPoint, shipped float64) {
	fmt.Fprintln(b, "## Fixed-threshold curve (in-sample)")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "The optimum below is selected against this same evaluation slice -- it is an")
	fmt.Fprintln(b, "in-sample optimum, not a held-out validation result.")
	fmt.Fprintln(b)
	sorted := append([]compare.ThresholdPoint(nil), curve...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Threshold < sorted[j].Threshold })
	var rows [][]string
	for _, p := range sorted {
		marker := ""
		if p.Threshold == shipped {
			marker = "yes"
		}
		rows = append(rows, []string{floatStr4(p.Threshold), ratioStr(p.F1), marker})
	}
	renderTable(b, []string{"Threshold", "Overall macro F1", "Shipped"}, rows)
	fmt.Fprintln(b)
}

func renderRecommendation(b *bytes.Buffer, rec compare.Recommendation) {
	fmt.Fprintln(b, "## Recommendation")
	fmt.Fprintln(b)
	fmt.Fprintf(b, "**Recommended default: `%s`**", rec.RecommendedDetectorID)
	if rec.SelectedThreshold != nil {
		fmt.Fprintf(b, " (threshold %s)", floatStr4(*rec.SelectedThreshold))
	}
	fmt.Fprintln(b)
	fmt.Fprintln(b)
	fmt.Fprintln(b, "This recommendation is evidence for a human decision. It changes no runtime")
	fmt.Fprintln(b, "setting, shipped default, or configuration.")
	fmt.Fprintln(b)
	candidates := append([]compare.CandidateEvidence(nil), rec.Candidates...)
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].DetectorID < candidates[j].DetectorID })
	var rows [][]string
	for _, c := range candidates {
		threshold := ""
		if c.Threshold != nil {
			threshold = floatStr4(*c.Threshold)
		}
		rows = append(rows, []string{
			c.DetectorID, threshold, fmt.Sprintf("%v", c.Eligible), ratioStr(c.OverallF1), ratioStr(c.F1DeltaVsShipped), ratioStr(c.OverallUtteranceErrorRate),
		})
	}
	renderTable(b, []string{"Candidate", "Threshold", "Eligible", "Overall F1", "F1 delta vs shipped", "Utterance error rate"}, rows)
	fmt.Fprintln(b)
	fmt.Fprintln(b, "Eligibility: a candidate qualifies only if no cell's frame F1 falls more than 0.05")
	fmt.Fprintln(b, "below shipped fixed's F1 in that same cell (one-sided floor, no ceiling). Among")
	fmt.Fprintln(b, "eligible candidates, the highest overall macro F1 wins; ties go to the lowest")
	fmt.Fprintln(b, "overall utterance error rate, then to shipped fixed.")
	fmt.Fprintln(b)
}

// WriteReport renders r and either compares it against the file at path
// (check=true, no replacement) or atomically writes it via a sibling
// temporary file and rename (check=false). changed reports whether the
// rendered content differs from what was previously on disk (or true if
// the file did not exist).
func WriteReport(path string, r Report, check bool) (changed bool, err error) {
	content, err := RenderToBytes(r)
	if err != nil {
		return false, err
	}
	existing, readErr := os.ReadFile(path)
	changed = readErr != nil || !bytes.Equal(existing, content)

	if check {
		if changed {
			return true, fmt.Errorf("report: %s is out of date with the generated content", path)
		}
		return false, nil
	}

	if !changed {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("report: create report dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return false, fmt.Errorf("report: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			tmp.Close()
			os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(content); err != nil {
		return false, fmt.Errorf("report: write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return false, fmt.Errorf("report: sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return false, fmt.Errorf("report: close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return false, fmt.Errorf("report: promote report: %w", err)
	}
	cleanup = false
	return true, nil
}
