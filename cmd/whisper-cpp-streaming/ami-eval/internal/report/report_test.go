package report

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/compare"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/evaluate"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/manifest"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/score"
	"github.com/abedegno/muesli/internal/pluginkit"
)

// --- fixtures built from real evaluator output on synthetic audio ----------

type scene struct{ medium, noise, farNoise float32 }

var (
	sceneInterior   = scene{medium: 0.0155, noise: 0.0045, farNoise: 0.0045} // tuning winner 0.015
	sceneShip       = scene{medium: 0.0155, noise: 0.012, farNoise: 0.012}   // 0.015 beats 0.01
	sceneNoEligible = scene{medium: 0.0155, noise: 0.0045, farNoise: 0.035}  // far-field FPR 1 everywhere
)

func sec(s float64) int { return int(math.Round(s * evaluate.SampleRate)) }

func sceneInput(r manifest.Recording, sc scene) evaluate.RecordingInput {
	a := make([]float32, 16*evaluate.SampleRate)
	noise := sc.noise
	if r.Mic == manifest.MicFixedDistant {
		noise = sc.farNoise
	}
	for i := range a {
		a[i] = noise
	}
	for i := sec(10); i < sec(11.9); i++ {
		a[i] = 0.5
	}
	for i := sec(14); i < sec(14.6); i++ {
		a[i] = sc.medium
	}
	return evaluate.RecordingInput{
		ID: r.ID, MeetingID: r.MeetingID, Class: r.Class, Split: string(r.Split), Mic: r.Mic, Audio: a,
		Reference: []score.Interval{{Start: int64(sec(10)), End: int64(sec(11.9))}, {Start: int64(sec(14)), End: int64(sec(14.6))}},
	}
}

func hexOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func fixtureManifest(substitute bool) *manifest.Manifest {
	sel0 := 0
	mk := func(id, class string, split manifest.Split) manifest.Meeting {
		obj := func(suffix string) manifest.AudioObject {
			return manifest.AudioObject{URL: "https://example.invalid/" + id + suffix, SizeBytes: 1, SHA256: hexOf(id + suffix),
				Channels: 1, ChannelPolicy: manifest.ChannelPolicyExplicit, SelectChannel: &sel0}
		}
		return manifest.Meeting{ID: id, Class: class, Split: split,
			HeadsetMix: obj(manifest.HeadsetAudioSuffix), FixedDistantMix: obj(manifest.FixedDistantAudioSuffix),
			Annotations: []manifest.AnnotationObject{{ParticipantID: "A", Member: "words/" + id + ".A.words.xml", SizeBytes: 1, SHA256: hexOf(id + "A")}}}
	}
	m := &manifest.Manifest{
		SchemaVersion:     manifest.SchemaVersion,
		AnnotationArchive: manifest.ArchiveObject{URL: "https://example.invalid/a.zip", SizeBytes: 1, SHA256: hexOf("archive")},
		Meetings: []manifest.Meeting{
			mk("ES2002a", manifest.ClassScenario, manifest.SplitTuning),
			mk("EN2001a", manifest.ClassNonScenario, manifest.SplitTuning),
			mk("ES2004a", manifest.ClassScenario, manifest.SplitHeldOut),
			mk("EN2002a", manifest.ClassNonScenario, manifest.SplitHeldOut),
		},
	}
	if substitute {
		m.Meetings[3] = mk("IS1009a", manifest.ClassScenario, manifest.SplitHeldOut)
		m.HeldOutReplacement = &manifest.HeldOutReplacement{ReplacedMeetingID: "EN2002a", ReplacementMeetingID: "IS1009a",
			AnnotationFailureReason: "synthetic: participant C word end precedes start"}
	}
	return m
}

func runSplit(t testing.TB, man *manifest.Manifest, split manifest.Split, sc scene) evaluate.Matrix {
	t.Helper()
	var in []evaluate.RecordingInput
	for _, r := range man.RecordingsForSplit(split) {
		in = append(in, sceneInput(r, sc))
	}
	m, err := evaluate.RunMatrix(context.Background(), in, evaluate.MatrixConfig{Workers: 4, Split: string(split)})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

type variant struct {
	tuning, heldOut scene
	substitute      bool
}

var (
	fixtureMu    sync.Mutex
	fixtureCache = map[variant]compare.Results{}
)

func fixtureResults(t testing.TB, v variant) compare.Results {
	t.Helper()
	fixtureMu.Lock()
	defer fixtureMu.Unlock()
	if r, ok := fixtureCache[v]; ok {
		return r
	}
	man := fixtureManifest(v.substitute)
	tm := runSplit(t, man, manifest.SplitTuning, v.tuning)
	hm := runSplit(t, man, manifest.SplitHeldOut, v.heldOut)
	te, err := compare.BuildTuningEvidence(man, tm)
	if err != nil {
		t.Fatal(err)
	}
	sel, err := compare.SelectThreshold(te)
	if err != nil {
		t.Fatal(err)
	}
	he, err := compare.BuildHeldOutEvidence(man, hm)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := compare.Decide(sel, he)
	if err != nil {
		t.Fatal(err)
	}
	r, err := compare.NewResults(man, strings.Repeat("a", 64), compare.Environment{GoVersion: "go1.25.11", GOOS: "linux", GOARCH: "amd64"}, tm, hm, sel, dec)
	if err != nil {
		t.Fatal(err)
	}
	fixtureCache[v] = r
	return r
}

var (
	shipVariant       = variant{tuning: sceneInterior, heldOut: sceneShip}
	failedGateVariant = variant{tuning: sceneInterior, heldOut: sceneInterior}
	noEligibleVariant = variant{tuning: sceneNoEligible, heldOut: sceneShip}
	substituteVariant = variant{tuning: sceneInterior, heldOut: sceneShip, substitute: true}
)

func fixtureReport(t testing.TB, v variant) Report {
	d := ProductionDefaults()
	return Report{
		Results:                  fixtureResults(t, v),
		PreparationSchemaVersion: 1,
		AnnotationSchemaVersion:  1,
		Defaults:                 d,
		LiveSchemaDefault:        d.EnergyThreshold,
		ReproductionCommand:      "make evaluate-ami-vad",
	}
}

func render(t *testing.T, r Report) string {
	t.Helper()
	b, err := RenderToBytes(r)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return string(b)
}

var spaces = regexp.MustCompile(` {2,}`)

// mustContain matches parts against doc with runs of spaces collapsed, so
// expectations are independent of table padding widths.
func mustContain(t *testing.T, doc string, parts ...string) {
	t.Helper()
	squashed := spaces.ReplaceAllString(doc, " ")
	for _, p := range parts {
		if !strings.Contains(squashed, spaces.ReplaceAllString(p, " ")) {
			t.Fatalf("report missing %q", p)
		}
	}
}

// --- tests -----------------------------------------------------------------

func TestProductionDefaultsMatchPluginkit(t *testing.T) {
	d := ProductionDefaults()
	cfg := pluginkit.DefaultStreamingConfig()
	if d.SampleRate != cfg.SampleRate || d.EnergyThreshold != cfg.EnergyThreshold ||
		d.SilenceDurationMS != cfg.SilenceDuration.Milliseconds() || d.VADFrameMS != cfg.VADFrame.Milliseconds() {
		t.Fatalf("defaults drifted from pluginkit: %+v", d)
	}
	schema, err := LiveSchemaDefault()
	if err != nil {
		t.Fatal(err)
	}
	if schema <= 0 {
		t.Fatalf("live schema default %v", schema)
	}
}

func TestRenderDecisionShip(t *testing.T) {
	r := fixtureReport(t, shipVariant)
	doc := render(t, r)
	g := r.Results.Decision
	mustContain(t, doc,
		"**Final fixed energy threshold: `0.015` -- ship the tuning-selected threshold.**",
		"Tuning-selected threshold: `0.015` (evaluated neighbours `0.014` and `0.016`)",
		"| held-out overall utterance error rate | candidate < historical baseline",
		full(g.UtteranceGate.Candidate), full(g.UtteranceGate.Baseline), full(g.F1Gate.Candidate), full(g.F1Gate.Baseline),
		"Two held-out meetings are limited evidence",
		"Creative Commons Attribution 4.0", "CC BY",
		"Manifest digest: `"+strings.Repeat("a", 64)+"`",
		"`--offline`", "### Cache layout", "results.json",
	)
	// Operands far beyond four decimals render exactly, never rounded.
	precise := r
	u, f := *g.UtteranceGate, *g.F1Gate
	u.Candidate, u.Baseline = 0.12345678901234566, 0.9876543210987654
	f.Candidate, f.Baseline = 0.7000000000000001, 0.7
	precise.Results.Decision.UtteranceGate, precise.Results.Decision.F1Gate = &u, &f
	mustContain(t, render(t, precise), "0.12345678901234566", "0.9876543210987654", "0.7000000000000001")
	blk, err := ParseDecisionBlock([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if blk.Outcome != "ship" || blk.FinalThreshold != 0.015 || blk.HistoricalBaselineThreshold != 0.01 || *blk.SelectedThreshold != 0.015 {
		t.Fatalf("decision block %+v", blk)
	}
}

func TestRenderFailedGateAndNoEligibleWording(t *testing.T) {
	doc := render(t, fixtureReport(t, failedGateVariant))
	mustContain(t, doc,
		"**Final fixed energy threshold: `0.01` -- keep the historical threshold.**",
		"Failed conditions:",
		"is not strictly lower than the historical baseline",
		"| fail   |",
	)
	doc2 := render(t, fixtureReport(t, noEligibleVariant))
	mustContain(t, doc2,
		"keep the historical threshold",
		"Tuning-selected threshold: none -- no grid threshold has tuning overall frame F1",
		"Held-out gates: not applicable",
	)
	if strings.Contains(doc2, "| Gate ") {
		t.Fatal("no-eligible report must not render gate operands")
	}
}

func TestRenderEveryRejectionReasonAndPreciseLabels(t *testing.T) {
	r := fixtureReport(t, shipVariant)
	doc := render(t, r)
	for _, c := range r.Results.Selection.Candidates {
		for _, reason := range c.RejectedBecause {
			if !strings.Contains(doc, reason) {
				t.Fatalf("rejection reason for %s missing: %q", full(c.Threshold), reason)
			}
		}
	}
	// Sub-0.001 thresholds are distinguishable, round-trip, and in numeric
	// order.
	last := -1
	for _, v := range r.Results.ThresholdGrid {
		row := "| " + full(v) + " "
		i := strings.Index(doc, row)
		if i < 0 {
			t.Fatalf("no selection row for %s", full(v))
		}
		if i <= last {
			t.Fatalf("selection rows not in numeric order at %s", full(v))
		}
		last = i
	}
	mustContain(t, doc, "`0.00015848931924611142`", "fixed 0.0003981071705534973")
}

func TestRenderAdjacentSplitColumnsAtEveryLevel(t *testing.T) {
	doc := render(t, fixtureReport(t, shipVariant))
	mustContain(t, doc,
		"| Detector | Far-field FPR T | Far-field FPR H |",
		"| Precision T | Precision H | Recall T | Recall H | F1 T | F1 H | FPR T | FPR H | FNR T | FNR H |",
		"| Miss T | Miss H | Split T | Split H | Merge T | Merge H | Spurious T | Spurious H | UER T | UER H |",
		"| Start median T | Start median H | Start p95 T | Start p95 H | End median T | End median H | End p95 T | End p95 H |",
		"| Class        | Mic           | Recordings T/H | Detector",
		"| Mic           | Tuning recording      | Held-out recording    | Detector",
		"| fixed_distant | EN2001a-fixed_distant | EN2002a-fixed_distant |",
		"| headset       | ES2002a-headset       | ES2004a-headset       |",
		"historical 0.01", "adaptive (descriptive)",
	)
	// Three metric groups at each of three levels.
	if n := strings.Count(doc, "Frame metrics:"); n != 3 {
		t.Fatalf("frame metric group rendered %d times, want 3", n)
	}
}

func TestRenderMembershipAndSubstitution(t *testing.T) {
	doc := render(t, fixtureReport(t, shipVariant))
	mustContain(t, doc, "| tuning   | EN2001a |", "| held_out | ES2004a |", "Held-out substitution: none.")

	sub := fixtureReport(t, substituteVariant)
	doc2 := render(t, sub)
	mustContain(t, doc2,
		"Held-out substitution: `IS1009a` replaced `EN2002a` because its annotations were unusable: synthetic: participant C word end precedes start",
		"| held_out | IS1009a | scenario     |",
		"| non_scenario | headset       | 1/0            |",
	)
	// The absent held-out class renders n/a in the held-out column.
	for _, line := range strings.Split(doc2, "\n") {
		if strings.HasPrefix(line, "| non_scenario | headset       | 1/0            | historical 0.01") && strings.Contains(line, "Precision") == false {
			cells := strings.Split(line, "|")
			if strings.TrimSpace(cells[6]) != "n/a" {
				t.Fatalf("absent held-out class must be n/a: %s", line)
			}
			return
		}
	}
	t.Fatal("no non_scenario/headset summary row found")
}

func TestRenderDeterministicUnderInputReordering(t *testing.T) {
	r := fixtureReport(t, shipVariant)
	a := render(t, r)
	shuffled := r
	shuffled.Results.TuningMatrix.Entries = append([]evaluate.MatrixEntry(nil), r.Results.TuningMatrix.Entries...)
	shuffled.Results.HeldOutMatrix.Entries = append([]evaluate.MatrixEntry(nil), r.Results.HeldOutMatrix.Entries...)
	rng := rand.New(rand.NewSource(1))
	rng.Shuffle(len(shuffled.Results.TuningMatrix.Entries), func(i, j int) {
		e := shuffled.Results.TuningMatrix.Entries
		e[i], e[j] = e[j], e[i]
	})
	rng.Shuffle(len(shuffled.Results.HeldOutMatrix.Entries), func(i, j int) {
		e := shuffled.Results.HeldOutMatrix.Entries
		e[i], e[j] = e[j], e[i]
	})
	if b := render(t, shuffled); a != b {
		t.Fatal("render depends on raw entry order")
	}
	if b := render(t, r); a != b {
		t.Fatal("render is not byte-identical for identical input")
	}
	if strings.Contains(a, "/tmp") || strings.Contains(a, "/home") || strings.Contains(a, "T0") {
		t.Fatal("report must not contain paths or timestamps")
	}
}

func TestValidateRejectsMissingOrInconsistentEvidence(t *testing.T) {
	base := fixtureReport(t, shipVariant)
	cases := map[string]func(r *Report){
		"digest":  func(r *Report) { r.Results.ManifestDigest = "" },
		"env":     func(r *Report) { r.Results.Environment.GoVersion = "" },
		"command": func(r *Report) { r.ReproductionCommand = "" },
		"missing raw entry": func(r *Report) {
			r.Results.HeldOutMatrix.Entries = r.Results.HeldOutMatrix.Entries[1:]
		},
		"summary disagrees with raw": func(r *Report) {
			e := append([]evaluate.MatrixEntry(nil), r.Results.TuningMatrix.Entries...)
			e[5].FrameMetrics.F1.Value += 1e-9
			r.Results.TuningMatrix.Entries = e
		},
		"missing detector summary": func(r *Report) {
			r.Results.HeldOut.Detectors = r.Results.HeldOut.Detectors[:3]
		},
		"reason removed": func(r *Report) {
			c := append([]compare.Candidate(nil), r.Results.Selection.Candidates...)
			c[0].RejectedBecause = nil
			r.Results.Selection.Candidates = c
		},
		"edge selection": func(r *Report) {
			r.Results.Selection.LowerNeighbor = nil
		},
		"ship without gates": func(r *Report) {
			r.Results.Decision.F1Gate = nil
		},
		"runtime default drift": func(r *Report) { r.Defaults.EnergyThreshold = r.Results.RuntimeDefaultThreshold + 0.001 },
	}
	for name, mutate := range cases {
		r := base
		mutate(&r)
		if _, err := RenderToBytes(r); err == nil {
			t.Errorf("%s: expected validation failure", name)
		}
	}
}

// consistent is an injected production reading that agrees with the report.
func consistent(r Report) func() (float64, float64, error) {
	return func() (float64, float64, error) {
		return r.Results.Decision.FinalThreshold, r.Results.Decision.FinalThreshold, nil
	}
}

func TestCheckModeByteComparisonNeverReplaces(t *testing.T) {
	r := fixtureReport(t, shipVariant)
	dir := t.TempDir()
	path := filepath.Join(dir, "report.md")
	changed, err := writeReport(path, r, false, consistent(r))
	if err != nil || !changed {
		t.Fatalf("first write: changed=%v err=%v", changed, err)
	}
	if _, err := writeReport(path, r, true, consistent(r)); err != nil {
		t.Fatalf("check on identical bytes: %v", err)
	}
	orig, _ := os.ReadFile(path)
	tampered := append([]byte(nil), orig...)
	tampered[len(tampered)/2] ^= 0x01
	if err := os.WriteFile(path, tampered, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = writeReport(path, r, true, consistent(r))
	if err == nil || !strings.Contains(err.Error(), "out of date") {
		t.Fatalf("expected content mismatch, got %v", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, tampered) {
		t.Fatal("check mode replaced the file")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("leftover files: %v", entries)
	}
	if changed, err := writeReport(path, r, false, consistent(r)); err != nil || !changed {
		t.Fatalf("rewrite: changed=%v err=%v", changed, err)
	}
	if changed, err := writeReport(path, r, false, consistent(r)); err != nil || changed {
		t.Fatalf("second identical write must be a no-op: changed=%v err=%v", changed, err)
	}
}

func TestCheckModeRequiresProductionConsistency(t *testing.T) {
	r := fixtureReport(t, shipVariant)
	path := filepath.Join(t.TempDir(), "report.md")
	if _, err := writeReport(path, r, false, consistent(r)); err != nil {
		t.Fatal(err)
	}
	final := r.Results.Decision.FinalThreshold
	for name, prod := range map[string][2]float64{
		"runtime differs": {0.01, final},
		"schema differs":  {final, 0.01},
		"both differ":     {0.02, 0.02},
	} {
		prod := prod
		_, err := writeReport(path, r, true, func() (float64, float64, error) { return prod[0], prod[1], nil })
		if err == nil || !strings.Contains(err.Error(), "disagrees with production") {
			t.Fatalf("%s: expected a production-consistency failure (not a content failure), got %v", name, err)
		}
	}
	// The exported path reads the live values.
	live := pluginkit.DefaultStreamingConfig().EnergyThreshold
	mismatched := r
	if final == live {
		mismatched = fixtureReport(t, failedGateVariant)
	}
	p2 := filepath.Join(t.TempDir(), "report.md")
	if _, err := WriteReport(p2, mismatched, false); err != nil {
		t.Fatal(err)
	}
	if mismatched.Results.Decision.FinalThreshold != live {
		if _, err := WriteReport(p2, mismatched, true); err == nil {
			t.Fatal("WriteReport check must compare against the live runtime default")
		}
	}
}

func TestParseDecisionBlockRejectsGarbage(t *testing.T) {
	if _, err := ParseDecisionBlock([]byte("# nothing")); err == nil {
		t.Fatal("expected missing block error")
	}
	if _, err := ParseDecisionBlock([]byte("```json\n{\"bogus\": 1}\n```\n")); err == nil {
		t.Fatal("expected unknown field rejection")
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
