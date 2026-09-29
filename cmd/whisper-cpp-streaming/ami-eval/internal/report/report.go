// Package report renders the committed AMI VAD fixed-threshold evaluation
// (muesli#778, muesli#782) as deterministic, Prettier-stable Markdown:
// stable ordering, four decimal places for displayed rates and boundary
// errors, shortest round-trip values for thresholds and every decision
// operand, "n/a" for undefined ratios, and no timestamps or local paths, so
// repeated runs with an identical recorded environment are byte-identical.
// Rounded displays never decide anything: the decision is computed upstream
// at full precision and only reported here.
package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/compare"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/evaluate"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/manifest"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/score"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/internal/live"
	"github.com/abedegno/muesli/internal/pluginkit"
)

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

// LiveSchemaDefault decodes the numeric vad_threshold default the
// streaming plugin publishes in its live config schema.
func LiveSchemaDefault() (float64, error) {
	raw := live.ConfigSchema(json.RawMessage(`{"type":"object","properties":{}}`))
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		return 0, fmt.Errorf("report: decode live config schema: %w", err)
	}
	prop, ok := schema.Properties["vad_threshold"]
	if !ok {
		return 0, fmt.Errorf("report: live config schema has no vad_threshold property")
	}
	var threshold struct {
		Default *float64 `json:"default"`
	}
	if err := json.Unmarshal(prop, &threshold); err != nil || threshold.Default == nil {
		return 0, fmt.Errorf("report: live config schema has no numeric vad_threshold default (%v)", err)
	}
	return *threshold.Default, nil
}

// Report is every structured input to the rendered document. Results is the
// same evidence envelope persisted as results.json.
type Report struct {
	Results                  compare.Results
	PreparationSchemaVersion int
	AnnotationSchemaVersion  int
	// Defaults are the live production defaults at render time; its
	// EnergyThreshold is the current runtime default.
	Defaults Defaults
	// LiveSchemaDefault is the live plugin schema's numeric vad_threshold
	// default at render time.
	LiveSchemaDefault   float64
	ReproductionCommand string
}

// DecisionBlock is the deterministic machine-readable decision summary
// embedded in the report as a fenced JSON block.
type DecisionBlock struct {
	Outcome                     string   `json:"outcome"`
	FinalThreshold              float64  `json:"final_threshold"`
	HistoricalBaselineThreshold float64  `json:"historical_baseline_threshold"`
	SelectedThreshold           *float64 `json:"selected_threshold"`
	RuntimeDefaultThreshold     float64  `json:"runtime_default_threshold"`
	LiveSchemaDefault           float64  `json:"live_schema_default"`
}

// DecisionBlockStart and DecisionBlockEnd delimit the fenced JSON decision
// block so tests and tooling can extract it from the committed report.
const (
	DecisionBlockStart = "```json\n"
	DecisionBlockEnd   = "```\n"
)

// ParseDecisionBlock extracts the first fenced JSON decision block from a
// rendered report.
func ParseDecisionBlock(doc []byte) (DecisionBlock, error) {
	s := string(doc)
	i := strings.Index(s, DecisionBlockStart)
	if i < 0 {
		return DecisionBlock{}, fmt.Errorf("report: no decision block")
	}
	rest := s[i+len(DecisionBlockStart):]
	j := strings.Index(rest, DecisionBlockEnd)
	if j < 0 {
		return DecisionBlock{}, fmt.Errorf("report: unterminated decision block")
	}
	dec := json.NewDecoder(strings.NewReader(rest[:j]))
	dec.DisallowUnknownFields()
	var b DecisionBlock
	if err := dec.Decode(&b); err != nil {
		return DecisionBlock{}, fmt.Errorf("report: decode decision block: %w", err)
	}
	return b, nil
}

func (r Report) decisionBlock() DecisionBlock {
	d := r.Results.Decision
	return DecisionBlock{
		Outcome:                     d.Outcome,
		FinalThreshold:              d.FinalThreshold,
		HistoricalBaselineThreshold: d.HistoricalBaselineThreshold,
		SelectedThreshold:           d.SelectedThreshold,
		RuntimeDefaultThreshold:     r.Defaults.EnergyThreshold,
		LiveSchemaDefault:           r.LiveSchemaDefault,
	}
}

// Validate checks that a report has everything required before it may
// replace the committed file, and that every reported aggregate agrees with
// the raw evidence it came from.
func Validate(r Report) error {
	res := r.Results
	if len(res.ManifestDigest) != 64 {
		return fmt.Errorf("report: manifest digest must be 64 hex characters, got %q", res.ManifestDigest)
	}
	if res.EvaluationVersion != evaluate.EvaluationVersion {
		return fmt.Errorf("report: evaluation version %q, want %q", res.EvaluationVersion, evaluate.EvaluationVersion)
	}
	if res.Environment.GoVersion == "" || res.Environment.GOOS == "" || res.Environment.GOARCH == "" {
		return fmt.Errorf("report: execution environment (Go version, OS, architecture) is required")
	}
	if r.ReproductionCommand == "" {
		return fmt.Errorf("report: reproduction command is required")
	}
	if res.HistoricalBaselineThreshold != evaluate.HistoricalBaselineThreshold {
		return fmt.Errorf("report: historical baseline must be %v", evaluate.HistoricalBaselineThreshold)
	}
	if err := evaluate.ValidateGrid(res.ThresholdGrid); err != nil {
		return fmt.Errorf("report: %w", err)
	}
	if res.RuntimeDefaultThreshold != r.Defaults.EnergyThreshold {
		return fmt.Errorf("report: evidence was produced under runtime default %v but the live default is %v; regenerate", res.RuntimeDefaultThreshold, r.Defaults.EnergyThreshold)
	}
	if len(res.Splits) != 2 || res.Splits[0].Split != manifest.SplitTuning || res.Splits[1].Split != manifest.SplitHeldOut {
		return fmt.Errorf("report: split inventory must list tuning then held_out")
	}
	for _, pair := range []struct {
		name    string
		summary compare.SplitSummary
		matrix  evaluate.Matrix
		members compare.SplitMembership
	}{
		{"tuning", res.Tuning, res.TuningMatrix, res.Splits[0]},
		{"held_out", res.HeldOut, res.HeldOutMatrix, res.Splits[1]},
	} {
		if err := validateSummary(pair.name, pair.summary, pair.matrix, pair.members, res.ThresholdGrid); err != nil {
			return err
		}
	}
	sel := res.Selection
	if len(sel.Candidates) != len(res.ThresholdGrid) {
		return fmt.Errorf("report: selection covers %d candidates, grid has %d", len(sel.Candidates), len(res.ThresholdGrid))
	}
	for i, c := range sel.Candidates {
		if c.Threshold != res.ThresholdGrid[i] {
			return fmt.Errorf("report: selection candidate %d is %v, grid has %v", i, c.Threshold, res.ThresholdGrid[i])
		}
		if !c.Eligible && len(c.RejectedBecause) == 0 {
			return fmt.Errorf("report: ineligible candidate %s has no rejection reason", evaluate.ThresholdLabel(c.Threshold))
		}
	}
	if (sel.Selected == nil) == (sel.NoEligibleReason == "") {
		return fmt.Errorf("report: selection must have exactly one of a selected threshold or a no-eligible reason")
	}
	if sel.Selected != nil && (sel.LowerNeighbor == nil || sel.HigherNeighbor == nil) {
		return fmt.Errorf("report: selected threshold must have both evaluated neighbours")
	}
	d := res.Decision
	switch d.Outcome {
	case compare.OutcomeShip:
		if sel.Selected == nil || d.FinalThreshold != *sel.Selected || d.UtteranceGate == nil || d.F1Gate == nil || !d.UtteranceGate.Passed || !d.F1Gate.Passed {
			return fmt.Errorf("report: a ship decision requires the selected threshold and two passed gates")
		}
	case compare.OutcomeKeep:
		if d.FinalThreshold != res.HistoricalBaselineThreshold {
			return fmt.Errorf("report: a keep decision must retain the historical baseline")
		}
		if d.GatesApplicable && (d.UtteranceGate == nil || d.F1Gate == nil) {
			return fmt.Errorf("report: applicable gates must be recorded")
		}
	default:
		return fmt.Errorf("report: unknown decision outcome %q", d.Outcome)
	}
	return nil
}

func validateSummary(name string, s compare.SplitSummary, m evaluate.Matrix, members compare.SplitMembership, grid []float64) error {
	if string(s.Split) != name || m.Split != name {
		return fmt.Errorf("report: %s summary/matrix split labels disagree (%q, %q)", name, s.Split, m.Split)
	}
	if len(s.Recordings) == 0 || len(s.Recordings) != len(members.RecordingIDs) {
		return fmt.Errorf("report: %s summary has %d recordings, inventory %d", name, len(s.Recordings), len(members.RecordingIDs))
	}
	for i, rec := range s.Recordings {
		if rec.RecordingID != members.RecordingIDs[i] {
			return fmt.Errorf("report: %s recording %d is %s, inventory has %s", name, i, rec.RecordingID, members.RecordingIDs[i])
		}
	}
	want := 1 + len(grid) + 1
	if s.WhisperAvailable {
		want++
	}
	if len(s.Detectors) != want {
		return fmt.Errorf("report: %s summary has %d detectors, want %d", name, len(s.Detectors), want)
	}
	entries := map[string]evaluate.MatrixEntry{}
	for _, e := range m.Entries {
		entries[e.RecordingID+"|"+e.DetectorID] = e
	}
	if len(entries) != len(m.Entries) || len(m.Entries) != want*len(s.Recordings) {
		return fmt.Errorf("report: %s matrix coverage is inconsistent with its summary", name)
	}
	for _, d := range s.Detectors {
		if len(d.PerRecording) != len(s.Recordings) || len(d.Cells) != len(compare.AllCells()) {
			return fmt.Errorf("report: %s detector %s has incomplete coverage", name, d.DetectorID)
		}
		for i, rec := range s.Recordings {
			e, ok := entries[rec.RecordingID+"|"+d.DetectorID]
			if !ok {
				return fmt.Errorf("report: %s raw evidence lacks %s/%s", name, rec.RecordingID, d.DetectorID)
			}
			got := d.PerRecording[i]
			if got.Frame != e.FrameMetrics || got.Utterance != e.UtteranceMetrics {
				return fmt.Errorf("report: %s summary for %s/%s disagrees with raw evidence", name, rec.RecordingID, d.DetectorID)
			}
		}
		var f1, uer float64
		for _, pr := range d.PerRecording {
			f1 += pr.Frame.F1.Value
			uer += pr.Utterance.UtteranceErrorRate.Value
		}
		n := float64(len(d.PerRecording))
		if d.Threshold != nil && (d.Overall.Frame.F1.Value != f1/n || d.Overall.Utterance.UtteranceErrorRate.Value != uer/n) {
			return fmt.Errorf("report: %s overall for %s is not the recording mean of its raw evidence", name, d.DetectorID)
		}
	}
	return nil
}

func ratioStr(r score.Ratio) string {
	if !r.Valid || math.IsNaN(r.Value) || math.IsInf(r.Value, 0) {
		return "n/a"
	}
	return fmt.Sprintf("%.4f", r.Value)
}

func floatStr4(f float64) string { return fmt.Sprintf("%.4f", f) }

// full renders a threshold or decision operand at shortest round-trip
// precision.
func full(f float64) string { return evaluate.ThresholdLabel(f) }

func fullPtr(f *float64) string {
	if f == nil {
		return "none"
	}
	return full(*f)
}

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

// metricColumn is one displayed metric of a group.
type metricColumn struct {
	label string
	get   func(compare.Metrics) score.Ratio
}

var (
	frameColumns = []metricColumn{
		{"Precision", func(m compare.Metrics) score.Ratio { return m.Frame.Precision }},
		{"Recall", func(m compare.Metrics) score.Ratio { return m.Frame.Recall }},
		{"F1", func(m compare.Metrics) score.Ratio { return m.Frame.F1 }},
		{"FPR", func(m compare.Metrics) score.Ratio { return m.Frame.FalsePositiveRate }},
		{"FNR", func(m compare.Metrics) score.Ratio { return m.Frame.FalseNegativeRate }},
	}
	utteranceColumns = []metricColumn{
		{"Miss", func(m compare.Metrics) score.Ratio { return m.Utterance.MissRate }},
		{"Split", func(m compare.Metrics) score.Ratio { return m.Utterance.SplitExtraRate }},
		{"Merge", func(m compare.Metrics) score.Ratio { return m.Utterance.MergeExtraRate }},
		{"Spurious", func(m compare.Metrics) score.Ratio { return m.Utterance.SpuriousRate }},
		{"UER", func(m compare.Metrics) score.Ratio { return m.Utterance.UtteranceErrorRate }},
	}
	timingColumns = []metricColumn{
		{"Start median", func(m compare.Metrics) score.Ratio { return m.Utterance.StartErrorMedian }},
		{"Start p95", func(m compare.Metrics) score.Ratio { return m.Utterance.StartErrorP95 }},
		{"End median", func(m compare.Metrics) score.Ratio { return m.Utterance.EndErrorMedian }},
		{"End p95", func(m compare.Metrics) score.Ratio { return m.Utterance.EndErrorP95 }},
	}
	metricGroups = []struct {
		title   string
		columns []metricColumn
	}{
		{"Frame metrics", frameColumns},
		{"Utterance metrics", utteranceColumns},
		{"Boundary timing (seconds)", timingColumns},
	}
)

// pairedHeaders returns "<label> T" / "<label> H" adjacent tuning and
// held-out columns for each metric.
func pairedHeaders(prefix []string, cols []metricColumn) []string {
	h := append([]string(nil), prefix...)
	for _, c := range cols {
		h = append(h, c.label+" T", c.label+" H")
	}
	return h
}

func pairedCells(cols []metricColumn, t, h compare.Metrics) []string {
	var out []string
	for _, c := range cols {
		out = append(out, ratioStr(c.get(t)), ratioStr(c.get(h)))
	}
	return out
}

func detectorLabel(d compare.DetectorSummary) string {
	switch {
	case d.DetectorID == evaluate.DetectorHistoricalFixed:
		return "historical 0.01"
	case d.Threshold != nil:
		return "fixed " + full(*d.Threshold)
	default:
		return d.DetectorID + " (descriptive)"
	}
}

// RenderToBytes renders the deterministic Markdown report.
func RenderToBytes(r Report) ([]byte, error) {
	if err := Validate(r); err != nil {
		return nil, err
	}
	res := r.Results
	var b bytes.Buffer
	p := func(lines ...string) {
		for _, l := range lines {
			b.WriteString(l)
			b.WriteString("\n")
		}
	}

	p("# AMI VAD Fixed-Threshold Evaluation",
		"",
		"Generated by `cmd/whisper-cpp-streaming/ami-eval` (muesli#778, muesli#782). A fixed",
		"energy threshold is selected on tuning meetings only, then that frozen selection is",
		"validated against the historical 0.01 threshold on separate held-out meetings. Every",
		"detector runs through the production `StreamingSession`, fed canonical 16kHz mono",
		"audio in 200ms calls. The evaluator computes and reports the decision; it never edits",
		"production source.",
		"")

	renderDecision(&b, r)
	renderCorpus(&b, res)
	renderGrid(&b, res)
	renderSelection(&b, res.Selection)
	renderMetricsSection(&b, res)
	renderProduction(&b, r)
	renderMetadata(&b, r)
	renderUsage(&b, r.ReproductionCommand)
	return b.Bytes(), nil
}

func renderDecision(b *bytes.Buffer, r Report) {
	res := r.Results
	d := res.Decision
	sel := res.Selection
	fmt.Fprintln(b, "## Decision")
	fmt.Fprintln(b)
	if d.Outcome == compare.OutcomeShip {
		fmt.Fprintf(b, "**Final fixed energy threshold: `%s` -- ship the tuning-selected threshold.**\n", full(d.FinalThreshold))
	} else {
		fmt.Fprintf(b, "**Final fixed energy threshold: `%s` -- keep the historical threshold.**\n", full(d.FinalThreshold))
	}
	fmt.Fprintln(b)
	fmt.Fprintf(b, "- Historical baseline (evaluation constant, independent of the runtime default): `%s`\n", full(res.HistoricalBaselineThreshold))
	if sel.Selected != nil {
		fmt.Fprintf(b, "- Tuning-selected threshold: `%s` (evaluated neighbours `%s` and `%s`)\n", full(*sel.Selected), full(*sel.LowerNeighbor), full(*sel.HigherNeighbor))
	} else {
		fmt.Fprintf(b, "- Tuning-selected threshold: none -- %s\n", sel.NoEligibleReason)
	}
	fmt.Fprintf(b, "- Current runtime default (`DefaultStreamingConfig().EnergyThreshold`): `%s`\n", full(r.Defaults.EnergyThreshold))
	fmt.Fprintf(b, "- Current live config schema `vad_threshold` default: `%s`\n", full(r.LiveSchemaDefault))
	if r.Defaults.EnergyThreshold == d.FinalThreshold && r.LiveSchemaDefault == d.FinalThreshold {
		fmt.Fprintln(b, "- Production matches this decision.")
	} else {
		fmt.Fprintf(b, "- **Production still differs from this decision**: the runtime default and live schema default must both become `%s` before `--check` passes.\n", full(d.FinalThreshold))
	}
	fmt.Fprintf(b, "- Reason: %s\n", d.Reason)
	fmt.Fprintln(b)

	if d.GatesApplicable {
		fmt.Fprintln(b, "Held-out gates, applied only to the frozen tuning selection against the historical 0.01")
		fmt.Fprintln(b, "results on the held-out meetings (full-precision operands):")
		fmt.Fprintln(b)
		var rows [][]string
		for _, g := range []*compare.Gate{d.UtteranceGate, d.F1Gate} {
			result := "pass"
			if !g.Passed {
				result = "fail"
			}
			rows = append(rows, []string{g.Name, g.Condition, full(g.Candidate), full(g.Baseline), result})
		}
		renderTable(b, []string{"Gate", "Condition", "Candidate", "Historical 0.01", "Result"}, rows)
		fmt.Fprintln(b)
		if len(d.FailedConditions) > 0 {
			fmt.Fprintln(b, "Failed conditions:")
			fmt.Fprintln(b)
			for _, f := range d.FailedConditions {
				fmt.Fprintf(b, "- %s\n", f)
			}
			fmt.Fprintln(b)
		}
	} else {
		fmt.Fprintln(b, "Held-out gates: not applicable -- no tuning candidate was eligible, so the historical")
		fmt.Fprintln(b, "threshold is retained. Held-out metrics below are descriptive only.")
		fmt.Fprintln(b)
	}
	fmt.Fprintln(b, "Machine-readable decision (read by the repository's default-consistency test):")
	fmt.Fprintln(b)
	blk, _ := json.MarshalIndent(r.decisionBlock(), "", "  ")
	b.WriteString(DecisionBlockStart)
	b.Write(blk)
	b.WriteString("\n")
	b.WriteString(DecisionBlockEnd)
	fmt.Fprintln(b)
	fmt.Fprintln(b, "Two held-out meetings are limited evidence. Passing both gates shows the selected")
	fmt.Fprintln(b, "threshold did better than 0.01 on these recordings; it is not a general guarantee across")
	fmt.Fprintln(b, "microphones, rooms, or deployments. Explicit `vad_threshold` settings are unaffected.")
	fmt.Fprintln(b)
}

func renderCorpus(b *bytes.Buffer, res compare.Results) {
	fmt.Fprintln(b, "## Corpus and splits")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "Splits are meeting-level: no meeting appears in both. Each meeting contributes its")
	fmt.Fprintln(b, "headset mix (Mix-Headset) and fixed single distant microphone (Array1-01) recordings.")
	fmt.Fprintln(b)
	var rows [][]string
	for _, pair := range []compare.SplitSummary{res.Tuning, res.HeldOut} {
		for _, rec := range pair.Recordings {
			rows = append(rows, []string{string(pair.Split), rec.MeetingID, rec.Class, rec.Mic, rec.RecordingID})
		}
	}
	renderTable(b, []string{"Split", "Meeting", "Class", "Microphone", "Recording"}, rows)
	fmt.Fprintln(b)
	if res.HeldOutReplacement != nil {
		rep := res.HeldOutReplacement
		fmt.Fprintf(b, "Held-out substitution: `%s` replaced `%s` because its annotations were unusable: %s\n",
			rep.ReplacementMeetingID, rep.ReplacedMeetingID, rep.AnnotationFailureReason)
		fmt.Fprintln(b, "The substitute keeps its actual meeting class; class/microphone summaries with no")
		fmt.Fprintln(b, "recording are shown as n/a and never reweight the decision.")
	} else {
		fmt.Fprintln(b, "Held-out substitution: none. Both requested held-out meetings had usable annotations,")
		fmt.Fprintln(b, "determined before any detector metric was inspected.")
	}
	fmt.Fprintln(b)
}

func renderGrid(b *bytes.Buffer, res compare.Results) {
	fmt.Fprintln(b, "## Threshold grid")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "The initial grid is five equal logarithmic intervals from 0.0001 to 0.001 (points")
	fmt.Fprintln(b, "`10^(-4 + k/5)`, k = 0..5, with exact endpoint constants) followed by every thousandth")
	fmt.Fprintln(b, "from 0.002 to 0.030. Thresholds are labelled at shortest round-trip precision.")
	fmt.Fprintln(b)
	labels := make([]string, len(res.ThresholdGrid))
	for i, t := range res.ThresholdGrid {
		labels[i] = "`" + full(t) + "`"
	}
	fmt.Fprintf(b, "- Final grid (%d points): %s\n", len(res.ThresholdGrid), strings.Join(labels, ", "))
	if len(res.GridExpansions) == 0 {
		fmt.Fprintln(b, "- Expansions: none -- the constrained tuning winner was interior on the initial grid.")
	} else {
		for i, e := range res.GridExpansions {
			added := make([]string, len(e.Added))
			for j, a := range e.Added {
				added[j] = "`" + full(a) + "`"
			}
			fmt.Fprintf(b, "- Expansion %d (%s end, from `%s` to `%s`): added %s\n", i+1, e.Direction, full(e.From), full(e.To), strings.Join(added, ", "))
		}
	}
	fmt.Fprintf(b, "- Historical baseline: `%s` (measured once per recording; grid point `%s` reuses that measurement)\n", full(res.HistoricalBaselineThreshold), full(res.HistoricalBaselineThreshold))
	fmt.Fprintf(b, "- Runtime default when the evidence was produced: `%s` (metadata only; it never selects what is measured)\n", full(res.RuntimeDefaultThreshold))
	if res.Tuning.WhisperAvailable {
		fmt.Fprintln(b, "- whisper.cpp detector: available (descriptive only)")
	} else {
		fmt.Fprintf(b, "- whisper.cpp detector: unavailable -- %s\n", res.Tuning.WhisperUnavailableReason)
	}
	fmt.Fprintln(b)
}

func renderSelection(b *bytes.Buffer, sel compare.Selection) {
	fmt.Fprintln(b, "## Tuning selection")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "Selection sees tuning evidence only. A grid point is eligible when its overall frame F1")
	fmt.Fprintf(b, "is at least the historical baseline's tuning F1 (`%s`) and its far-field FPR (mean of the\n", full(sel.BaselineF1))
	fmt.Fprintf(b, "two Array1-01 recordings) is at most `%s`. Among eligible points the lowest overall\n", full(sel.FarFieldFPRCeiling))
	fmt.Fprintln(b, "utterance error rate wins; exact ties at full stored precision choose the higher")
	fmt.Fprintln(b, "threshold. 0.01 is an ordinary candidate. The winner must have an evaluated lower and")
	fmt.Fprintln(b, "higher neighbour, otherwise the grid is expanded and tuning rerun before any held-out run.")
	fmt.Fprintln(b, "Displayed values are rounded to four decimals; eligibility and ranking use full precision")
	fmt.Fprintln(b, "(see the rejection reasons and `results.json`).")
	fmt.Fprintln(b)
	fmt.Fprintf(b, "- Historical baseline tuning: F1 `%s`, utterance error rate `%s`, far-field FPR `%s`\n",
		full(sel.BaselineF1), full(sel.BaselineUtteranceError), full(sel.BaselineFarFieldFPR))
	if sel.Selected != nil {
		fmt.Fprintf(b, "- Selected: `%s`; lower neighbour `%s`; higher neighbour `%s`\n", full(*sel.Selected), full(*sel.LowerNeighbor), full(*sel.HigherNeighbor))
	} else {
		fmt.Fprintf(b, "- Selected: none -- %s\n", sel.NoEligibleReason)
	}
	fmt.Fprintln(b)
	var rows [][]string
	for _, c := range sel.Candidates {
		status := "eligible"
		if !c.Eligible {
			status = "rejected"
		}
		if sel.Selected != nil && c.Threshold == *sel.Selected {
			status = "selected"
		}
		reason := strings.Join(c.RejectedBecause, "; ")
		if reason == "" {
			reason = "-"
		}
		rows = append(rows, []string{full(c.Threshold), floatStr4(c.F1), floatStr4(c.FarFieldFPR), floatStr4(c.UtteranceError), status, reason})
	}
	renderTable(b, []string{"Threshold", "F1", "Far-field FPR", "UER", "Status", "Rejection reasons"}, rows)
	fmt.Fprintln(b)
}

func renderMetricsSection(b *bytes.Buffer, res compare.Results) {
	fmt.Fprintln(b, "## Results")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "Every table pairs adjacent tuning (T) and held-out (H) columns. Overall values are the")
	fmt.Fprintln(b, "arithmetic mean of a split's four recording values (two equally weighted meetings, each")
	fmt.Fprintln(b, "with two equally weighted microphones) -- never duration-weighted or pooled. A mean with")
	fmt.Fprintln(b, "any undefined constituent is n/a. UER (utterance error rate) is misses + split extras +")
	fmt.Fprintln(b, "merge extras + spurious predictions over reference utterances and can exceed 1.")
	fmt.Fprintln(b, "Boundary timing covers one-to-one matched pairs only. Adaptive and whisper.cpp rows are")
	fmt.Fprintln(b, "descriptive and never enter eligibility, ranking, or the gates.")
	fmt.Fprintln(b)

	t, h := res.Tuning, res.HeldOut

	fmt.Fprintln(b, "### Overall")
	fmt.Fprintln(b)
	var ffRows [][]string
	for i, d := range t.Detectors {
		ffRows = append(ffRows, []string{detectorLabel(d), ratioStr(d.FarFieldFPR), ratioStr(h.Detectors[i].FarFieldFPR)})
	}
	fmt.Fprintln(b, "Far-field FPR (mean of the split's two Array1-01 recordings):")
	fmt.Fprintln(b)
	renderTable(b, []string{"Detector", "Far-field FPR T", "Far-field FPR H"}, ffRows)
	fmt.Fprintln(b)
	for _, g := range metricGroups {
		fmt.Fprintf(b, "%s:\n", g.title)
		fmt.Fprintln(b)
		var rows [][]string
		for i, d := range t.Detectors {
			rows = append(rows, append([]string{detectorLabel(d)}, pairedCells(g.columns, d.Overall, h.Detectors[i].Overall)...))
		}
		renderTable(b, pairedHeaders([]string{"Detector"}, g.columns), rows)
		fmt.Fprintln(b)
	}

	fmt.Fprintln(b, "### Class and microphone summaries")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "Descriptive only: each cell is the mean of that split's recordings in the cell (n/a when")
	fmt.Fprintln(b, "the split has none). Cells never reweight the decision.")
	fmt.Fprintln(b)
	for _, g := range metricGroups {
		fmt.Fprintf(b, "%s:\n", g.title)
		fmt.Fprintln(b)
		var rows [][]string
		for ci, cell := range compare.AllCells() {
			for i, d := range t.Detectors {
				hc := h.Detectors[i].Cells[ci]
				tc := d.Cells[ci]
				prefix := []string{cell.Class, cell.Mic, fmt.Sprintf("%d/%d", tc.RecordingCount, hc.RecordingCount), detectorLabel(d)}
				rows = append(rows, append(prefix, pairedCells(g.columns, tc.Metrics, hc.Metrics)...))
			}
		}
		renderTable(b, pairedHeaders([]string{"Class", "Mic", "Recordings T/H", "Detector"}, g.columns), rows)
		fmt.Fprintln(b)
	}

	fmt.Fprintln(b, "### Per recording")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "Recordings are paired by microphone and ordinal position within each split (sorted by")
	fmt.Fprintln(b, "meeting ID); both recording IDs are shown. Far-field rows are the Array1-01 recordings.")
	fmt.Fprintln(b)
	pairs := recordingPairs(t, h)
	for _, g := range metricGroups {
		fmt.Fprintf(b, "%s:\n", g.title)
		fmt.Fprintln(b)
		var rows [][]string
		for _, pr := range pairs {
			for i, d := range t.Detectors {
				tm := d.PerRecording[pr.t]
				hm := h.Detectors[i].PerRecording[pr.h]
				prefix := []string{t.Recordings[pr.t].Mic, t.Recordings[pr.t].RecordingID, h.Recordings[pr.h].RecordingID, detectorLabel(d)}
				rows = append(rows, append(prefix, pairedCells(g.columns, tm, hm)...))
			}
		}
		renderTable(b, pairedHeaders([]string{"Mic", "Tuning recording", "Held-out recording", "Detector"}, g.columns), rows)
		fmt.Fprintln(b)
	}
}

type recPair struct{ t, h int }

// recordingPairs pairs tuning and held-out recordings by microphone and
// ordinal position within that microphone.
func recordingPairs(t, h compare.SplitSummary) []recPair {
	byMic := func(s compare.SplitSummary, mic string) []int {
		var out []int
		for i, r := range s.Recordings {
			if r.Mic == mic {
				out = append(out, i)
			}
		}
		return out
	}
	var pairs []recPair
	for _, mic := range []string{manifest.MicHeadset, manifest.MicFixedDistant} {
		ti, hi := byMic(t, mic), byMic(h, mic)
		for k := 0; k < len(ti) && k < len(hi); k++ {
			pairs = append(pairs, recPair{t: ti[k], h: hi[k]})
		}
	}
	return pairs
}

func renderProduction(b *bytes.Buffer, r Report) {
	fmt.Fprintln(b, "## Production defaults")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "Read live from `internal/pluginkit.DefaultStreamingConfig`, the adaptive detector's")
	fmt.Fprintln(b, "defaults, and the streaming plugin's live config schema at render time. Local and hosted")
	fmt.Fprintln(b, "deployments share these defaults; a session's explicit `vad_threshold` always wins. The")
	fmt.Fprintln(b, "adaptive detector's warm-up fallback is the runtime default energy threshold, so adaptive")
	fmt.Fprintln(b, "(descriptive) rows follow that default; fixed rows and the 0.01 comparator do not.")
	fmt.Fprintln(b)
	d := r.Defaults
	renderTable(b, []string{"Setting", "Value"}, [][]string{
		{"Sample rate", fmt.Sprintf("%d Hz", d.SampleRate)},
		{"Max window", fmt.Sprintf("%d ms", d.MaxWindowMS)},
		{"Partial interval", fmt.Sprintf("%d ms", d.PartialIntervalMS)},
		{"Silence duration", fmt.Sprintf("%d ms", d.SilenceDurationMS)},
		{"Runtime default energy threshold", full(d.EnergyThreshold)},
		{"Live schema vad_threshold default", full(r.LiveSchemaDefault)},
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
	fmt.Fprintln(b)
}

func renderMetadata(b *bytes.Buffer, r Report) {
	res := r.Results
	fmt.Fprintln(b, "## Reproducibility metadata")
	fmt.Fprintln(b)
	fmt.Fprintf(b, "- Manifest digest: `%s`\n", res.ManifestDigest)
	fmt.Fprintf(b, "- Manifest schema version: `%d`\n", manifest.SchemaVersion)
	fmt.Fprintf(b, "- Evaluation revision: `%s`\n", res.EvaluationVersion)
	fmt.Fprintf(b, "- Preparation schema version: `%d`\n", r.PreparationSchemaVersion)
	fmt.Fprintf(b, "- Annotation schema version: `%d`\n", r.AnnotationSchemaVersion)
	fmt.Fprintf(b, "- Go version: `%s`\n", res.Environment.GoVersion)
	fmt.Fprintf(b, "- OS/architecture: `%s/%s`\n", res.Environment.GOOS, res.Environment.GOARCH)
	fmt.Fprintln(b)
	fmt.Fprintln(b, "## Corpus credit")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "This evaluation uses recordings and manual annotations from the")
	fmt.Fprintln(b, "[AMI Meeting Corpus](http://groups.inf.ed.ac.uk/ami/corpus/), made available by the")
	fmt.Fprintln(b, "AMI Consortium under a Creative Commons Attribution 4.0 International licence (CC BY")
	fmt.Fprintln(b, "4.0). See the [corpus overview](http://groups.inf.ed.ac.uk/ami/corpus/) and")
	fmt.Fprintln(b, "[manual annotation documentation](http://groups.inf.ed.ac.uk/ami/corpus/annotation.shtml)")
	fmt.Fprintln(b, "for full corpus and licensing details. No corpus audio or annotation is committed to")
	fmt.Fprintln(b, "this repository or used in continuous integration; see")
	fmt.Fprintln(b, "`cmd/whisper-cpp-streaming/ami-eval/manifest.json` for the exact pinned objects.")
	fmt.Fprintln(b)
}

func renderUsage(b *bytes.Buffer, command string) {
	fmt.Fprintln(b, "## Reproduction")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "```sh")
	fmt.Fprintln(b, command)
	fmt.Fprintln(b, "go run ./cmd/whisper-cpp-streaming/ami-eval --offline --workers 4 --check")
	fmt.Fprintln(b, "```")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "`make evaluate-ami-vad` (or `go run ./cmd/whisper-cpp-streaming/ami-eval`) runs validate ->")
	fmt.Fprintln(b, "acquire -> prepare/evaluate tuning -> select -> prepare/evaluate held-out -> gate ->")
	fmt.Fprintln(b, "render from an empty cache. Only one split's audio is in memory at a time. Flags:")
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
	fmt.Fprintln(b, "  replacing it; exits non-zero on any difference, or when the decision's final")
	fmt.Fprintln(b, "  threshold differs from the runtime default or the live schema default.")
	fmt.Fprintln(b, "- `--report` -- output report path (default: the committed")
	fmt.Fprintln(b, "  `docs/ami-vad-evaluation.md`).")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "A grid-edge winner, missing or duplicate evidence, an undefined or non-finite decision")
	fmt.Fprintln(b, "metric, a checksum failure, or a detector failure or timeout stops the run with context")
	fmt.Fprintln(b, "and leaves the existing report untouched. A failed held-out gate is a successful")
	fmt.Fprintln(b, "evaluation with a keep decision.")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "### Cache layout")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "```text")
	fmt.Fprintln(b, "<cache>/downloads/<sha256>                                   verified source objects")
	fmt.Fprintln(b, "<cache>/prepared/<manifest-digest>/<recording-id>/           canonical audio + reference + provenance")
	fmt.Fprintln(b, "<cache>/results/<manifest-digest>/<evaluation-version>/      results.json (both matrices, summaries, selection, decision)")
	fmt.Fprintln(b, "```")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "Downloads are sequential, HTTPS-only across redirects, subject to a 15-minute")
	fmt.Fprintln(b, "per-object timeout and a manifest byte ceiling enforced while streaming, and")
	fmt.Fprintln(b, "promoted atomically only after size and SHA-256 verification. A cached object that")
	fmt.Fprintln(b, "fails rehashing is quarantined (renamed aside) with a diagnostic rather than reused")
	fmt.Fprintln(b, "or silently deleted. Each recording/detector run has a deadline of")
	fmt.Fprintln(b, "`min(30m, max(1m, 2*audio duration))`; exceeding it is fatal.")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "No credentials are ever accepted or persisted by this tool; a protected source must")
	fmt.Fprintln(b, "be preseeded at its checksum-addressed cache location before an offline run.")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "### Reproducibility")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "Byte-identical report output is promised only for repeated runs with an identical")
	fmt.Fprintln(b, "corpus, manifest, evaluation revision, parameters, threshold grid, production defaults,")
	fmt.Fprintln(b, "and recorded execution environment (Go version and OS/architecture, both recorded")
	fmt.Fprintln(b, "above). A different supported environment can produce valid, auditable results while")
	fmt.Fprintln(b, "recording its own environment, without matching this report byte-for-byte.")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "### Continuous integration")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "CI never downloads the AMI corpus or runs this evaluation end to end. Its corpus-free")
	fmt.Fprintln(b, "tests -- manifest splits, grid construction, evidence validation, selection, gates,")
	fmt.Fprintln(b, "orchestration order, and report rendering -- run against synthetic audio and minimal")
	fmt.Fprintln(b, "annotation fixtures as part of the normal Go test suite.")
}

// CheckProductionConsistency requires the decision's final threshold to equal
// both the runtime default and the live schema default.
func CheckProductionConsistency(final, runtimeDefault, schemaDefault float64) error {
	if final != runtimeDefault || final != schemaDefault {
		return fmt.Errorf("report: decision final threshold %s disagrees with production: runtime default %s, live schema default %s",
			full(final), full(runtimeDefault), full(schemaDefault))
	}
	return nil
}

// WriteReport renders r and either compares it against the file at path
// (check=true, no replacement; additionally requires the decision's final
// threshold to equal the live runtime default and live schema default) or
// atomically writes it via a sibling temporary file and rename
// (check=false). changed reports whether the rendered content differs from
// what was previously on disk (or true if the file did not exist).
func WriteReport(path string, r Report, check bool) (changed bool, err error) {
	return writeReport(path, r, check, liveProduction)
}

// liveProduction reads the current runtime default and live schema default.
func liveProduction() (runtimeDefault, schemaDefault float64, err error) {
	schemaDefault, err = LiveSchemaDefault()
	return pluginkit.DefaultStreamingConfig().EnergyThreshold, schemaDefault, err
}

func writeReport(path string, r Report, check bool, production func() (float64, float64, error)) (changed bool, err error) {
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
		runtimeDefault, schemaDefault, err := production()
		if err != nil {
			return false, err
		}
		if err := CheckProductionConsistency(r.Results.Decision.FinalThreshold, runtimeDefault, schemaDefault); err != nil {
			return false, err
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
