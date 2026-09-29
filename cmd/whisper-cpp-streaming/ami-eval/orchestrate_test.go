package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/acquire"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/audio"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/compare"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/evaluate"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/manifest"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/score"
	"github.com/abedegno/muesli/internal/pluginkit"
)

// Synthetic scene geometry, aligned to 20ms VAD frames so every frame is
// uniformly one region. Participant A's utterance matches the merged
// reference from internal/annotation/testdata/ami.words.xml (10.0-11.9s);
// participant B's is a test-generated word annotation (14.0-14.6s).
const (
	sceneSeconds = 16
	refAStart    = 10.0
	refAEnd      = 11.9
	refBStart    = 14.0
	refBEnd      = 14.6
)

// scene sets synthetic amplitudes: A is always loud (0.5) so every grid
// threshold predicts something; B is at `medium`; silence carries `noise`
// (headset) or `farNoise` (fixed_distant).
type scene struct {
	medium, noise, farNoise float32
}

var (
	// Tuning: thresholds <= 0.004 detect the 0.0045 noise; 0.005..0.015
	// behave identically (best); >= 0.016 miss B. The exact UER tie
	// resolves to 0.015, an interior point.
	sceneInterior = scene{medium: 0.0155, noise: 0.0045, farNoise: 0.0045}
	// Held-out ship: 0.012 noise defeats the 0.01 baseline but not 0.015.
	sceneShip = scene{medium: 0.0155, noise: 0.012, farNoise: 0.012}
	// Everything loud and no noise: every threshold ties, so the tie
	// resolves to the upper grid endpoint.
	sceneEdge = scene{medium: 0.5}
	// Far-field noise above every threshold: far-field FPR is 1 for every
	// candidate, so nothing is eligible.
	sceneNoEligible = scene{medium: 0.0155, noise: 0.0045, farNoise: 0.035}
)

func sec(s float64) int { return int(math.Round(s * evaluate.SampleRate)) }

func sceneAudio(sc scene, mic string) []float32 {
	a := make([]float32, sceneSeconds*evaluate.SampleRate)
	noise := sc.noise
	if mic == manifest.MicFixedDistant {
		noise = sc.farNoise
	}
	for i := range a {
		a[i] = noise
	}
	for i := sec(refAStart); i < sec(refAEnd); i++ {
		a[i] = 0.5
	}
	for i := sec(refBStart); i < sec(refBEnd); i++ {
		a[i] = sc.medium
	}
	return a
}

func sceneReference() []score.Interval {
	return []score.Interval{
		{Start: int64(sec(refAStart)), End: int64(sec(refAEnd))},
		{Start: int64(sec(refBStart)), End: int64(sec(refBEnd))},
	}
}

func hexOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func testMeetingDef(id, class string, split manifest.Split, hash func(string) string, size func(string) int64, annotations []manifest.AnnotationObject) manifest.Meeting {
	sel0 := 0
	obj := func(suffix string) manifest.AudioObject {
		name := id + suffix
		return manifest.AudioObject{
			URL: "https://example.invalid/amicorpus/" + id + "/audio/" + name, SizeBytes: size(name), SHA256: hash(name),
			Channels: 1, ChannelPolicy: manifest.ChannelPolicyExplicit, SelectChannel: &sel0,
		}
	}
	return manifest.Meeting{
		ID: id, Class: class, Split: split,
		HeadsetMix: obj(manifest.HeadsetAudioSuffix), FixedDistantMix: obj(manifest.FixedDistantAudioSuffix),
		Annotations: annotations,
	}
}

var meetingDefs = []struct {
	id    string
	class string
	split manifest.Split
}{
	{"ES2002a", manifest.ClassScenario, manifest.SplitTuning},
	{"EN2001a", manifest.ClassNonScenario, manifest.SplitTuning},
	{"ES2004a", manifest.ClassScenario, manifest.SplitHeldOut},
	{"EN2002a", manifest.ClassNonScenario, manifest.SplitHeldOut},
}

// stubManifest is a valid manifest with synthetic pins, for injected-stage
// tests that never touch the cache.
func stubManifest() *manifest.Manifest {
	m := &manifest.Manifest{
		SchemaVersion:     manifest.SchemaVersion,
		AnnotationArchive: manifest.ArchiveObject{URL: "https://example.invalid/archive.zip", SizeBytes: 1, SHA256: hexOf([]byte("archive"))},
	}
	for _, d := range meetingDefs {
		anns := []manifest.AnnotationObject{{ParticipantID: "A", Member: "words/" + d.id + ".A.words.xml", SizeBytes: 1, SHA256: hexOf([]byte(d.id + "A"))}}
		m.Meetings = append(m.Meetings, testMeetingDef(d.id, d.class, d.split, func(n string) string { return hexOf([]byte(n)) }, func(string) int64 { return 1 }, anns))
	}
	return m
}

func stubAcquired(man *manifest.Manifest) []acquire.AcquiredRecording {
	var out []acquire.AcquiredRecording
	for _, r := range man.Recordings() {
		out = append(out, acquire.AcquiredRecording{Recording: r})
	}
	return out
}

// spyStages prepares synthetic audio per split scene and records every
// call in order.
type spyStages struct {
	mu     sync.Mutex
	events []string
	scenes map[manifest.Split]scene
}

func (s *spyStages) log(e string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

func (s *spyStages) stages() stages {
	return stages{
		prepare: func(a acquire.AcquiredRecording) (evaluate.RecordingInput, error) {
			s.log("prepare:" + string(a.Recording.Split) + ":" + a.Recording.ID)
			r := a.Recording
			return evaluate.RecordingInput{
				ID: r.ID, MeetingID: r.MeetingID, Class: r.Class, Split: string(r.Split), Mic: r.Mic,
				Audio: sceneAudio(s.scenes[r.Split], r.Mic), Reference: sceneReference(),
			}, nil
		},
		runMatrix: func(ctx context.Context, in []evaluate.RecordingInput, cfg evaluate.MatrixConfig) (evaluate.Matrix, error) {
			s.log("run:" + cfg.Split)
			return evaluate.RunMatrix(ctx, in, cfg)
		},
		selected: func(compare.Selection) { s.log("select") },
	}
}

func (s *spyStages) count(prefix string) int {
	n := 0
	for _, e := range s.events {
		if strings.HasPrefix(e, prefix) {
			n++
		}
	}
	return n
}

func TestEvaluateSplitsTuningBeforeHeldOut(t *testing.T) {
	spy := &spyStages{scenes: map[manifest.Split]scene{manifest.SplitTuning: sceneInterior, manifest.SplitHeldOut: sceneShip}}
	man := stubManifest()
	ev, err := evaluateSplits(context.Background(), man, stubAcquired(man), 4, spy.stages())
	if err != nil {
		t.Fatal(err)
	}
	// Every tuning prepare, the tuning run, and the frozen selection
	// precede the first held-out prepare.
	firstHeld, lastTuning, selectAt := -1, -1, -1
	for i, e := range spy.events {
		if strings.Contains(e, ":held_out") && firstHeld < 0 {
			firstHeld = i
		}
		if strings.Contains(e, ":tuning") {
			lastTuning = i
		}
		if e == "select" {
			selectAt = i
		}
	}
	if firstHeld < 0 || lastTuning > firstHeld || spy.events[lastTuning] != "run:tuning" || selectAt != lastTuning+1 || selectAt > firstHeld {
		t.Fatalf("held-out work began before the tuning selection was frozen: %v", spy.events)
	}
	if ev.selection.Selected == nil || *ev.selection.Selected != 0.015 {
		t.Fatalf("expected interior tuning selection 0.015, got %+v", ev.selection.Selected)
	}
	if *ev.selection.LowerNeighbor != 0.014 || *ev.selection.HigherNeighbor != 0.016 {
		t.Fatalf("neighbors %v/%v", *ev.selection.LowerNeighbor, *ev.selection.HigherNeighbor)
	}
	if ev.decision.Outcome != compare.OutcomeShip || ev.decision.FinalThreshold != 0.015 {
		t.Fatalf("expected ship 0.015, got %+v", ev.decision)
	}
}

func TestEvaluateSplitsGridEdgeRunsNoHeldOut(t *testing.T) {
	spy := &spyStages{scenes: map[manifest.Split]scene{manifest.SplitTuning: sceneEdge, manifest.SplitHeldOut: sceneShip}}
	man := stubManifest()
	_, err := evaluateSplits(context.Background(), man, stubAcquired(man), 4, spy.stages())
	ge, ok := compare.IsGridEdge(err)
	if !ok || ge.Direction != evaluate.ExpandUpper {
		t.Fatalf("expected an upper grid-edge error, got %v", err)
	}
	if n := spy.count("prepare:held_out") + spy.count("run:held_out"); n != 0 {
		t.Fatalf("expected zero held-out calls after an edge, got %d (%v)", n, spy.events)
	}
}

func TestEvaluateSplitsNoEligibleStillEvaluatesHeldOut(t *testing.T) {
	spy := &spyStages{scenes: map[manifest.Split]scene{manifest.SplitTuning: sceneNoEligible, manifest.SplitHeldOut: sceneShip}}
	man := stubManifest()
	ev, err := evaluateSplits(context.Background(), man, stubAcquired(man), 4, spy.stages())
	if err != nil {
		t.Fatal(err)
	}
	if ev.selection.Selected != nil || ev.selection.NoEligibleReason == "" {
		t.Fatalf("expected no eligible candidate, got %+v", ev.selection)
	}
	if spy.count("run:held_out") != 1 || len(ev.heldOut.Entries) == 0 {
		t.Fatal("held-out split must still be evaluated descriptively")
	}
	if ev.decision.Outcome != compare.OutcomeKeep || ev.decision.FinalThreshold != 0.01 || ev.decision.GatesApplicable {
		t.Fatalf("expected keep 0.01 with gates not applicable, got %+v", ev.decision)
	}
}

func TestEvaluateSplitsHeldOutNeverMovesSelection(t *testing.T) {
	man := stubManifest()
	var sels []compare.Selection
	var outcomes []string
	for _, held := range []scene{sceneInterior, sceneShip, sceneNoEligible} {
		spy := &spyStages{scenes: map[manifest.Split]scene{manifest.SplitTuning: sceneInterior, manifest.SplitHeldOut: held}}
		ev, err := evaluateSplits(context.Background(), man, stubAcquired(man), 4, spy.stages())
		if err != nil {
			t.Fatal(err)
		}
		sels = append(sels, ev.selection)
		outcomes = append(outcomes, ev.decision.Outcome+" "+evaluate.ThresholdLabel(ev.decision.FinalThreshold))
	}
	for i := 1; i < len(sels); i++ {
		if !reflect.DeepEqual(sels[0], sels[i]) {
			t.Fatalf("held-out data changed the tuning selection (run %d)", i)
		}
	}
	// Same selection; only the gate outcome differs. Equal held-out UER
	// (identical scenes) fails the strict gate.
	want := []string{"keep 0.01", "ship 0.015", "keep 0.01"}
	if !reflect.DeepEqual(outcomes, want) {
		t.Fatalf("outcomes %v, want %v", outcomes, want)
	}
}

func TestEvaluateSplitsSequentialAndParallelIdentical(t *testing.T) {
	man := stubManifest()
	var evs []evaluation
	for _, workers := range []int{1, 4} {
		spy := &spyStages{scenes: map[manifest.Split]scene{manifest.SplitTuning: sceneInterior, manifest.SplitHeldOut: sceneShip}}
		ev, err := evaluateSplits(context.Background(), man, stubAcquired(man), workers, spy.stages())
		if err != nil {
			t.Fatal(err)
		}
		evs = append(evs, ev)
	}
	if !reflect.DeepEqual(evs[0], evs[1]) {
		t.Fatal("sequential and 4-worker evaluations differ")
	}
}

func TestEvaluateSplitsInjectedFailures(t *testing.T) {
	man := stubManifest()
	base := &spyStages{scenes: map[manifest.Split]scene{manifest.SplitTuning: sceneInterior, manifest.SplitHeldOut: sceneShip}}
	faults := map[string]func(st stages) stages{
		"prepare": func(st stages) stages {
			st.prepare = func(acquire.AcquiredRecording) (evaluate.RecordingInput, error) {
				return evaluate.RecordingInput{}, errors.New("injected prepare failure")
			}
			return st
		},
		"detector timeout": func(st stages) stages {
			st.runMatrix = func(context.Context, []evaluate.RecordingInput, evaluate.MatrixConfig) (evaluate.Matrix, error) {
				return evaluate.Matrix{}, errors.New("evaluate: run exceeded deadline of 1m0s")
			}
			return st
		},
		"held-out coverage": func(st stages) stages {
			inner := st.runMatrix
			st.runMatrix = func(ctx context.Context, in []evaluate.RecordingInput, cfg evaluate.MatrixConfig) (evaluate.Matrix, error) {
				m, err := inner(ctx, in, cfg)
				if cfg.Split == string(manifest.SplitHeldOut) {
					m.Entries = m.Entries[1:]
				}
				return m, err
			}
			return st
		},
		"invalid tuning metric": func(st stages) stages {
			inner := st.runMatrix
			st.runMatrix = func(ctx context.Context, in []evaluate.RecordingInput, cfg evaluate.MatrixConfig) (evaluate.Matrix, error) {
				m, err := inner(ctx, in, cfg)
				m.Entries[3].FrameMetrics.F1.Value = math.NaN()
				return m, err
			}
			return st
		},
	}
	for name, fault := range faults {
		if _, err := evaluateSplits(context.Background(), man, stubAcquired(man), 4, fault(base.stages())); err == nil {
			t.Errorf("%s: expected failure", name)
		}
	}
}

// --- offline end-to-end ------------------------------------------------------

// participantB is a test-generated (not AMI) word annotation for B's
// utterance.
const participantB = `<?xml version="1.0" encoding="ISO-8859-1" standalone="yes"?>
<nite:root nite:id="synthetic.B.words" xmlns:nite="http://nite.sourceforge.net/">
   <w nite:id="synthetic.B.words0" starttime="14.00" endtime="14.30">synthetic</w>
   <w nite:id="synthetic.B.words1" starttime="14.30" endtime="14.60">words</w>
</nite:root>
`

type offlineCorpus struct {
	root, manifestPath, cacheDir, reportPath string
	man                                      *manifest.Manifest
}

// buildOfflineCorpus writes a module root, a manifest with real checksums of
// locally encoded WAVs and a locally built annotation archive, and a cache
// pre-populated at every Cache.DownloadPath. URLs point at an unresolvable
// host; runs must be --offline.
func buildOfflineCorpus(t *testing.T, scenes map[manifest.Split]scene) offlineCorpus {
	t.Helper()
	fixtureA, err := os.ReadFile(filepath.Join("internal", "annotation", "testdata", "ami.words.xml"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(t.TempDir(), "cache")
	cache := acquire.Cache{Root: cacheDir}
	put := func(data []byte) string {
		h := hexOf(data)
		p := cache.DownloadPath(h)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return h
	}

	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	annotations := map[string][]manifest.AnnotationObject{}
	for _, d := range meetingDefs {
		for _, p := range []struct {
			id   string
			data []byte
		}{{"A", fixtureA}, {"B", []byte(participantB)}} {
			member := "words/" + d.id + "." + p.id + ".words.xml"
			w, err := zw.Create(member)
			if err != nil {
				t.Fatal(err)
			}
			w.Write(p.data)
			annotations[d.id] = append(annotations[d.id], manifest.AnnotationObject{ParticipantID: p.id, Member: member, SizeBytes: int64(len(p.data)), SHA256: hexOf(p.data)})
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	archive := zbuf.Bytes()

	man := &manifest.Manifest{
		SchemaVersion:     manifest.SchemaVersion,
		AnnotationArchive: manifest.ArchiveObject{URL: "https://example.invalid/ami_public_manual.zip", SizeBytes: int64(len(archive)), SHA256: put(archive)},
	}
	hashes := map[string]string{}
	sizes := map[string]int64{}
	for _, d := range meetingDefs {
		for _, mic := range []struct{ suffix, mic string }{{manifest.HeadsetAudioSuffix, manifest.MicHeadset}, {manifest.FixedDistantAudioSuffix, manifest.MicFixedDistant}} {
			var wav bytes.Buffer
			samples := sceneAudio(scenes[d.split], mic.mic)
			// Make each WAV byte-distinct so no two recordings share a pin.
			samples[0] = float32(len(hashes)+1) / 32767
			if err := audio.Encode(&wav, samples, evaluate.SampleRate); err != nil {
				t.Fatal(err)
			}
			name := d.id + mic.suffix
			hashes[name] = put(wav.Bytes())
			sizes[name] = int64(wav.Len())
		}
		man.Meetings = append(man.Meetings, testMeetingDef(d.id, d.class, d.split,
			func(n string) string { return hashes[n] }, func(n string) int64 { return sizes[n] }, annotations[d.id]))
	}
	if err := man.Validate(); err != nil {
		t.Fatalf("offline manifest invalid: %v", err)
	}
	data, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "manifest.json")
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return offlineCorpus{root: root, manifestPath: manifestPath, cacheDir: cacheDir, reportPath: filepath.Join(root, "report.md"), man: man}
}

func (c offlineCorpus) run(t *testing.T, newStages func(acquire.Cache, string) stages, extra ...string) (string, error) {
	t.Helper()
	t.Chdir(c.root)
	out, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	args := append([]string{"--offline", "--workers", "4", "--manifest", c.manifestPath, "--cache", c.cacheDir, "--report", c.reportPath}, extra...)
	runErr := runWith(args, out, newStages)
	b, _ := os.ReadFile(out.Name())
	return string(b), runErr
}

func TestOfflineEndToEndShipAndKeep(t *testing.T) {
	for _, tc := range []struct {
		name    string
		heldOut scene
		want    string
	}{
		{"ship", sceneShip, "decision: ship 0.015"},
		{"failed gate keeps", sceneInterior, "decision: keep 0.01"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := buildOfflineCorpus(t, map[manifest.Split]scene{manifest.SplitTuning: sceneInterior, manifest.SplitHeldOut: tc.heldOut})
			out, err := c.run(t, productionStages)
			if err != nil {
				t.Fatalf("offline run: %v\n%s", err, out)
			}
			if !strings.Contains(out, "selected threshold (tuning): 0.015") || !strings.Contains(out, tc.want) {
				t.Fatalf("unexpected output:\n%s", out)
			}
			digest, _ := c.man.Digest()
			data, err := os.ReadFile(filepath.Join(acquire.Cache{Root: c.cacheDir}.ResultsDir(digest, evaluate.EvaluationVersion), compare.ResultsFileName))
			if err != nil {
				t.Fatalf("results.json not published: %v", err)
			}
			var r compare.Results
			if err := json.Unmarshal(data, &r); err != nil {
				t.Fatal(err)
			}
			if len(r.Tuning.Recordings) != 4 || len(r.HeldOut.Recordings) != 4 || r.Decision.Outcome+" "+evaluate.ThresholdLabel(r.Decision.FinalThreshold) != strings.TrimPrefix(tc.want, "decision: ") {
				t.Fatalf("results envelope: %+v", r.Decision)
			}
			if _, err := os.Stat(c.reportPath); err != nil {
				t.Fatalf("report not written: %v", err)
			}
		})
	}
}

const sentinel = "SENTINEL REPORT -- must survive failed runs\n"

func TestOfflineFailuresNeverReplaceReport(t *testing.T) {
	scenes := map[manifest.Split]scene{manifest.SplitTuning: sceneInterior, manifest.SplitHeldOut: sceneShip}
	faulty := func(mutate func(stages) stages) func(acquire.Cache, string) stages {
		return func(c acquire.Cache, d string) stages { return mutate(productionStages(c, d)) }
	}
	cases := map[string]func(acquire.Cache, string) stages{
		"prepare failure": faulty(func(st stages) stages {
			st.prepare = func(acquire.AcquiredRecording) (evaluate.RecordingInput, error) {
				return evaluate.RecordingInput{}, errors.New("injected prepare failure")
			}
			return st
		}),
		"detector timeout": faulty(func(st stages) stages {
			st.runMatrix = func(context.Context, []evaluate.RecordingInput, evaluate.MatrixConfig) (evaluate.Matrix, error) {
				return evaluate.Matrix{}, errors.New("evaluate: run exceeded deadline of 1m0s")
			}
			return st
		}),
		"missing coverage": faulty(func(st stages) stages {
			inner := st.runMatrix
			st.runMatrix = func(ctx context.Context, in []evaluate.RecordingInput, cfg evaluate.MatrixConfig) (evaluate.Matrix, error) {
				m, err := inner(ctx, in, cfg)
				m.Entries = m.Entries[:len(m.Entries)-1]
				return m, err
			}
			return st
		}),
		"invalid metric": faulty(func(st stages) stages {
			inner := st.runMatrix
			st.runMatrix = func(ctx context.Context, in []evaluate.RecordingInput, cfg evaluate.MatrixConfig) (evaluate.Matrix, error) {
				m, err := inner(ctx, in, cfg)
				m.Entries[1].UtteranceMetrics.UtteranceErrorRate.Value = math.Inf(1)
				return m, err
			}
			return st
		}),
		"duplicate entry": faulty(func(st stages) stages {
			inner := st.runMatrix
			st.runMatrix = func(ctx context.Context, in []evaluate.RecordingInput, cfg evaluate.MatrixConfig) (evaluate.Matrix, error) {
				m, err := inner(ctx, in, cfg)
				m.Entries = append(m.Entries, m.Entries[0])
				return m, err
			}
			return st
		}),
	}
	for name, st := range cases {
		t.Run(name, func(t *testing.T) {
			c := buildOfflineCorpus(t, scenes)
			if err := os.WriteFile(c.reportPath, []byte(sentinel), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := c.run(t, st); err == nil {
				t.Fatal("expected failure")
			}
			assertSentinel(t, c.reportPath)
		})
	}

	t.Run("grid edge", func(t *testing.T) {
		c := buildOfflineCorpus(t, map[manifest.Split]scene{manifest.SplitTuning: sceneEdge, manifest.SplitHeldOut: sceneShip})
		os.WriteFile(c.reportPath, []byte(sentinel), 0o644)
		_, err := c.run(t, productionStages)
		if _, ok := compare.IsGridEdge(err); !ok {
			t.Fatalf("expected grid-edge failure, got %v", err)
		}
		assertSentinel(t, c.reportPath)
	})

	t.Run("corrupt cached object", func(t *testing.T) {
		c := buildOfflineCorpus(t, scenes)
		os.WriteFile(c.reportPath, []byte(sentinel), 0o644)
		obj := c.man.Meetings[2].HeadsetMix
		p := acquire.Cache{Root: c.cacheDir}.DownloadPath(obj.SHA256)
		data, _ := os.ReadFile(p)
		data[len(data)-1] ^= 0xff
		os.WriteFile(p, data, 0o644)
		_, err := c.run(t, productionStages)
		if err == nil || !strings.Contains(err.Error(), "acquire") {
			t.Fatalf("expected an acquisition checksum failure, got %v", err)
		}
		assertSentinel(t, c.reportPath)
	})
}

func assertSentinel(t *testing.T, path string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != sentinel {
		t.Fatalf("report was replaced by a failed run: %q (%v)", got, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("leftover temp file %s", e.Name())
		}
	}
}

// sceneAtLiveDefault builds a scene whose tuning winner is exactly the live
// runtime default L: B sits just above L (below the next grid point) and the
// noise floor sits at L/2, so every grid point in (L/2, L] ties and the tie
// resolves to L. The decision therefore equals L whether it ships L or
// keeps 0.01 == L.
func sceneAtLiveDefault() scene {
	l := float32(pluginkit.DefaultStreamingConfig().EnergyThreshold)
	return scene{medium: l * 1.05, noise: l * 0.5, farNoise: l * 0.5}
}

func TestCommandCheckPassesOnIdenticalBytesAndFailsOnTamper(t *testing.T) {
	sc := sceneAtLiveDefault()
	c := buildOfflineCorpus(t, map[manifest.Split]scene{manifest.SplitTuning: sc, manifest.SplitHeldOut: sc})
	if out, err := c.run(t, productionStages); err != nil {
		t.Fatalf("generate: %v\n%s", err, out)
	}
	out, err := c.run(t, productionStages, "--check")
	if err != nil || !strings.Contains(out, "report is up to date") {
		t.Fatalf("check on freshly generated report: %v\n%s", err, out)
	}
	orig, _ := os.ReadFile(c.reportPath)
	tampered := append([]byte(nil), orig...)
	tampered[len(tampered)-10] ^= 0x01
	if err := os.WriteFile(c.reportPath, tampered, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := c.run(t, productionStages, "--check"); err == nil || !strings.Contains(err.Error(), "out of date") {
		t.Fatalf("expected --check to fail on a one-byte change, got %v", err)
	}
	after, _ := os.ReadFile(c.reportPath)
	if !bytes.Equal(after, tampered) {
		t.Fatal("--check replaced the report")
	}
}
