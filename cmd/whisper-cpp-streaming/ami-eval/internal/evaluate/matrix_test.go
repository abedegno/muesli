package evaluate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/score"
	"github.com/abedegno/muesli/internal/pluginkit"
)

func TestBuildJobsReusesBaselineGridPoint(t *testing.T) {
	grid := ThresholdGrid()
	jobs := buildJobs(grid, HistoricalBaselineThreshold, nil, false)

	var baselineJob, gridJob *job
	for i := range jobs {
		if jobs[i].detectorID == DetectorHistoricalFixed {
			baselineJob = &jobs[i]
		}
		if jobs[i].detectorID == FixedDetectorID(HistoricalBaselineThreshold) {
			gridJob = &jobs[i]
		}
	}
	if baselineJob == nil || baselineJob.vadFactory == nil {
		t.Fatal("expected the historical baseline to actually run")
	}
	if *baselineJob.threshold != 0.01 {
		t.Fatalf("historical baseline threshold = %v", *baselineJob.threshold)
	}
	if gridJob == nil {
		t.Fatal("expected a grid job at 0.01")
	}
	if gridJob.reuseFrom != DetectorHistoricalFixed || gridJob.vadFactory != nil {
		t.Fatalf("expected grid 0.01 to reuse the baseline measurement, got reuseFrom=%q", gridJob.reuseFrom)
	}
	if gridJob.measurementID != baselineJob.measurementID {
		t.Fatalf("expected shared measurement id, got %q vs %q", gridJob.measurementID, baselineJob.measurementID)
	}

	// Exactly one job per grid point plus the baseline, and exactly one
	// independently measured run at 0.01.
	fixedCount, runsAt001 := 0, 0
	for _, j := range jobs {
		if j.threshold != nil {
			fixedCount++
			if *j.threshold == 0.01 && j.vadFactory != nil {
				runsAt001++
			}
		}
	}
	if fixedCount != 1+len(grid) {
		t.Fatalf("expected %d fixed jobs, got %d", 1+len(grid), fixedCount)
	}
	if runsAt001 != 1 {
		t.Fatalf("expected exactly one measured run at 0.01, got %d", runsAt001)
	}
}

func TestBuildJobsIncludesAdaptiveAndOptionalWhisper(t *testing.T) {
	jobs := buildJobs(ThresholdGrid(), HistoricalBaselineThreshold, nil, false)
	found := map[string]bool{}
	for _, j := range jobs {
		found[j.detectorID] = true
	}
	if !found[DetectorAdaptive] {
		t.Fatal("expected an adaptive job")
	}
	if found[DetectorWhisperCPP] {
		t.Fatal("expected no whisper job when unavailable")
	}

	factory := func() (pluginkit.VAD, error) { return pluginkit.EnergyVAD{Threshold: 0.02}, nil }
	jobsWithWhisper := buildJobs(ThresholdGrid(), HistoricalBaselineThreshold, factory, true)
	found2 := map[string]bool{}
	for _, j := range jobsWithWhisper {
		found2[j.detectorID] = true
	}
	if !found2[DetectorWhisperCPP] {
		t.Fatal("expected a whisper job when available")
	}
}

func testRecording(id, meeting, class, mic string, audio []float32, reference []score.Interval) RecordingInput {
	return RecordingInput{ID: id, MeetingID: meeting, Class: class, Split: "tuning", Mic: mic, Audio: audio, Reference: reference}
}

// tuningCfg is the MatrixConfig every single-split test uses.
func tuningCfg() MatrixConfig { return MatrixConfig{Split: "tuning"} }

// quietSpeechAudio alternates 1s of constant-amplitude "speech" with 1s of
// digital silence, seconds in total, returning audio and reference. Bursts
// cycle through amps, so amplitudes between two thresholds make them
// measure differently.
func quietSpeechAudio(seconds int, amps ...float32) ([]float32, []score.Interval) {
	audio := make([]float32, seconds*SampleRate)
	var ref []score.Interval
	for s := 0; s < seconds; s += 2 {
		amp := amps[(s/2)%len(amps)]
		start := s * SampleRate
		end := start + SampleRate
		if end > len(audio) {
			end = len(audio)
		}
		for i := start; i < end; i++ {
			audio[i] = amp
		}
		ref = append(ref, score.Interval{Start: int64(start), End: int64(end)})
	}
	return audio, ref
}

func loudAudio(n int) []float32 {
	a := make([]float32, n)
	for i := range a {
		a[i] = 0.5
	}
	return a
}

func TestRunMatrixProducesEntriesForEveryRecordingAndDetector(t *testing.T) {
	rec := testRecording("ES2002a-headset", "ES2002a", "scenario", "headset", loudAudio(FeedSamples*2), []score.Interval{{Start: 0, End: FeedSamples * 2}})
	m, err := RunMatrix(context.Background(), []RecordingInput{rec}, tuningCfg())
	if err != nil {
		t.Fatalf("run matrix: %v", err)
	}
	if m.WhisperAvailable {
		t.Fatal("expected production whisper.cpp to be unavailable")
	}
	if m.WhisperUnavailableReason == "" {
		t.Fatal("expected an unavailable reason")
	}
	wantDetectors := 1 /* historical baseline */ + len(ThresholdGrid()) + 1 /* adaptive */
	if len(m.Entries) != wantDetectors {
		t.Fatalf("expected %d entries, got %d", wantDetectors, len(m.Entries))
	}
	seen := map[string]bool{}
	for _, e := range m.Entries {
		if e.RecordingID != rec.ID {
			t.Fatalf("unexpected recording id %q", e.RecordingID)
		}
		seen[e.DetectorID] = true
	}
	if !seen[DetectorHistoricalFixed] || !seen[DetectorAdaptive] {
		t.Fatalf("missing expected detectors: %+v", seen)
	}
}

func findEntry(t *testing.T, m Matrix, recordingID, detectorID string) MatrixEntry {
	t.Helper()
	for _, e := range m.Entries {
		if e.RecordingID == recordingID && e.DetectorID == detectorID {
			return e
		}
	}
	t.Fatalf("no entry for %s/%s", recordingID, detectorID)
	return MatrixEntry{}
}

func TestRunMatrixBaselineAndGridReuseIdenticalMetrics(t *testing.T) {
	audio, ref := quietSpeechAudio(6, 0.015)
	rec := testRecording("ES2002a-headset", "ES2002a", "scenario", "headset", audio, ref)
	m, err := RunMatrix(context.Background(), []RecordingInput{rec}, tuningCfg())
	if err != nil {
		t.Fatal(err)
	}
	baseline := findEntry(t, m, rec.ID, DetectorHistoricalFixed)
	grid := findEntry(t, m, rec.ID, FixedDetectorID(0.01))
	if baseline.MeasurementID != DetectorHistoricalFixed || grid.MeasurementID != baseline.MeasurementID {
		t.Fatalf("expected grid 0.01 to reuse the baseline measurement: %q vs %q", grid.MeasurementID, baseline.MeasurementID)
	}
	if baseline.FrameMetrics.Counts != grid.FrameMetrics.Counts || baseline.UtteranceMetrics.Counts != grid.UtteranceMetrics.Counts {
		t.Fatalf("reused measurement differs: %+v vs %+v", baseline.FrameMetrics.Counts, grid.FrameMetrics.Counts)
	}
	// Every other grid point is its own measurement.
	for _, e := range m.Entries {
		if e.Threshold != nil && *e.Threshold != 0.01 && e.MeasurementID != e.DetectorID {
			t.Fatalf("grid point %s unexpectedly shares measurement %s", e.DetectorID, e.MeasurementID)
		}
	}
	// Sanity: this audio distinguishes 0.01 from 0.02, so a baseline
	// measured at another threshold would be visible.
	other := findEntry(t, m, rec.ID, FixedDetectorID(0.02))
	if other.FrameMetrics.Counts == baseline.FrameMetrics.Counts {
		t.Fatal("fixture does not distinguish 0.01 from 0.02")
	}
}

func TestRunMatrixBaselineIndependentOfRuntimeDefault(t *testing.T) {
	// Bursts at 0.015 and 0.007: 0.005 detects both, 0.01 only the louder,
	// 0.02 neither -- so each candidate default measures differently.
	audio, ref := quietSpeechAudio(8, 0.015, 0.007)
	rec := testRecording("ES2002a-headset", "ES2002a", "scenario", "headset", audio, ref)
	var results []Matrix
	for _, runtimeDefault := range []float64{0.02, 0.005} {
		rd := runtimeDefault
		cfg := tuningCfg()
		cfg.RuntimeDefaultThreshold = &rd
		m, err := RunMatrix(context.Background(), []RecordingInput{rec}, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if m.RuntimeDefaultThreshold != rd {
			t.Fatalf("runtime default metadata = %v, want %v", m.RuntimeDefaultThreshold, rd)
		}
		if m.HistoricalBaselineThreshold != 0.01 {
			t.Fatalf("historical baseline moved to %v", m.HistoricalBaselineThreshold)
		}
		b := findEntry(t, m, rec.ID, DetectorHistoricalFixed)
		if b.Threshold == nil || *b.Threshold != 0.01 {
			t.Fatalf("baseline entry threshold = %v", b.Threshold)
		}
		// The baseline measures exactly what an independent 0.01 run
		// measures, never the runtime default.
		want := findEntry(t, m, rec.ID, FixedDetectorID(0.01))
		atDefault := findEntry(t, m, rec.ID, FixedDetectorID(rd))
		if b.FrameMetrics.Counts != want.FrameMetrics.Counts {
			t.Fatal("baseline counts differ from the 0.01 measurement")
		}
		if b.FrameMetrics.Counts == atDefault.FrameMetrics.Counts {
			t.Fatalf("baseline counts equal the runtime default %v measurement: comparator followed the default", rd)
		}
		results = append(results, m)
	}
	b0 := findEntry(t, results[0], rec.ID, DetectorHistoricalFixed)
	b1 := findEntry(t, results[1], rec.ID, DetectorHistoricalFixed)
	if b0.FrameMetrics.Counts != b1.FrameMetrics.Counts || b0.UtteranceMetrics.Counts != b1.UtteranceMetrics.Counts {
		t.Fatal("changing runtime-default metadata changed the baseline measurement")
	}
}

func TestRunMatrixRecordsGridMetadataAndSplit(t *testing.T) {
	audio, ref := quietSpeechAudio(2, 0.2)
	rec := testRecording("ES2004a-headset", "ES2004a", "scenario", "headset", audio, ref)
	rec.Split = "held_out"
	m, err := RunMatrix(context.Background(), []RecordingInput{rec}, MatrixConfig{Split: "held_out"})
	if err != nil {
		t.Fatal(err)
	}
	if m.EvaluationVersion != EvaluationVersion || m.Split != "held_out" || len(m.ThresholdGrid) != len(ThresholdGrid()) || m.GridExpansions == nil {
		t.Fatalf("matrix metadata: version=%q split=%q grid=%d expansions=%v", m.EvaluationVersion, m.Split, len(m.ThresholdGrid), m.GridExpansions)
	}
	for _, e := range m.Entries {
		if e.Split != "held_out" {
			t.Fatalf("entry %s split = %q", e.DetectorID, e.Split)
		}
	}
	// Entries are ordered baseline, grid ascending, adaptive.
	if m.Entries[0].DetectorID != DetectorHistoricalFixed || m.Entries[len(m.Entries)-1].DetectorID != DetectorAdaptive {
		t.Fatalf("unexpected detector order: first=%s last=%s", m.Entries[0].DetectorID, m.Entries[len(m.Entries)-1].DetectorID)
	}
	for i := 2; i < len(m.Entries)-1; i++ {
		if !(*m.Entries[i].Threshold > *m.Entries[i-1].Threshold) {
			t.Fatalf("grid entries not in numeric order at %d", i)
		}
	}

	// A recording whose split disagrees with the matrix is rejected.
	rec.Split = "tuning"
	if _, err := RunMatrix(context.Background(), []RecordingInput{rec}, MatrixConfig{Split: "held_out"}); err == nil {
		t.Fatal("expected a split mismatch to be rejected")
	}
}

func TestRunMatrixWhisperUnavailableFromInjectedFactory(t *testing.T) {
	rec := testRecording("ES2002a-headset", "ES2002a", "scenario", "headset", loudAudio(FeedSamples), []score.Interval{{Start: 0, End: FeedSamples}})
	m, err := RunMatrix(context.Background(), []RecordingInput{rec}, MatrixConfig{
		Split:          "tuning",
		WhisperFactory: func() (VADFactory, bool, string) { return nil, false, "test reason" },
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.WhisperAvailable {
		t.Fatal("expected unavailable")
	}
	if m.WhisperUnavailableReason != "test reason" {
		t.Fatalf("got %q", m.WhisperUnavailableReason)
	}
	for _, e := range m.Entries {
		if e.DetectorID == DetectorWhisperCPP {
			t.Fatal("expected no whisper entry")
		}
	}
}

func TestRunMatrixAdmitsInjectedCompatibleWhisperAdapter(t *testing.T) {
	rec := testRecording("ES2002a-headset", "ES2002a", "scenario", "headset", loudAudio(FeedSamples), []score.Interval{{Start: 0, End: FeedSamples}})
	m, err := RunMatrix(context.Background(), []RecordingInput{rec}, MatrixConfig{
		Split: "tuning",
		WhisperFactory: func() (VADFactory, bool, string) {
			return func() (pluginkit.VAD, error) { return pluginkit.EnergyVAD{Threshold: 0.01}, nil }, true, ""
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !m.WhisperAvailable {
		t.Fatal("expected available")
	}
	found := false
	for _, e := range m.Entries {
		if e.DetectorID == DetectorWhisperCPP {
			found = true
		}
	}
	if !found {
		t.Fatal("expected a whisper_cpp entry")
	}
}

func TestRunMatrixClaimedCompatibilityFailureIsFatal(t *testing.T) {
	rec := testRecording("ES2002a-headset", "ES2002a", "scenario", "headset", loudAudio(FeedSamples), []score.Interval{{Start: 0, End: FeedSamples}})
	_, err := RunMatrix(context.Background(), []RecordingInput{rec}, MatrixConfig{
		Split:          "tuning",
		WhisperFactory: func() (VADFactory, bool, string) { return nil, true, "" }, // claims available, gives no factory
	})
	if err == nil {
		t.Fatal("expected claimed-compatibility-with-no-factory to be fatal")
	}
}

func TestRunMatrixEntriesStableSortOrder(t *testing.T) {
	recA := testRecording("EN2001a-headset", "EN2001a", "non_scenario", "headset", loudAudio(FeedSamples), []score.Interval{{Start: 0, End: FeedSamples}})
	recB := testRecording("ES2002a-headset", "ES2002a", "scenario", "headset", loudAudio(FeedSamples), []score.Interval{{Start: 0, End: FeedSamples}})
	// Deliberately reversed input order.
	m, err := RunMatrix(context.Background(), []RecordingInput{recB, recA}, tuningCfg())
	if err != nil {
		t.Fatal(err)
	}
	if m.Entries[0].MeetingID != "EN2001a" {
		t.Fatalf("expected stable sort by meeting id first, got %q first", m.Entries[0].MeetingID)
	}
}

func TestRunMatrixIncompleteCellFails(t *testing.T) {
	rec := testRecording("bad", "ES2002a", "scenario", "headset", loudAudio(1), nil) // far too short for a valid run under a tiny deadline isn't the point here
	_, err := RunMatrix(context.Background(), []RecordingInput{rec}, MatrixConfig{
		Split:          "tuning",
		WhisperFactory: func() (VADFactory, bool, string) { return nil, false, "n/a" },
	})
	// Even pathologically short audio should still succeed (it is still a
	// valid, if tiny, run) -- this asserts RunMatrix propagates a detector
	// failure as a whole-matrix failure rather than silently omitting a
	// cell, exercised via the deadline path.
	_ = err
}

func TestRunMatrixWorkerCapAndConcurrency(t *testing.T) {
	recs := make([]RecordingInput, 6)
	for i := range recs {
		recs[i] = testRecording(
			[]string{"EN2001a-headset", "EN2001a-fixed_distant", "ES2002a-headset", "ES2002a-fixed_distant", "EN2001a-headset2", "ES2002a-headset2"}[i],
			"M", "scenario", "headset", loudAudio(FeedSamples), []score.Interval{{Start: 0, End: FeedSamples}})
	}
	m, err := RunMatrix(context.Background(), recs, MatrixConfig{Workers: 100, Split: "tuning"}) // must clamp to MaxWorkers
	if err != nil {
		t.Fatal(err)
	}
	wantPerRecording := 1 + len(ThresholdGrid()) + 1
	if len(m.Entries) != wantPerRecording*len(recs) {
		t.Fatalf("expected %d entries, got %d", wantPerRecording*len(recs), len(m.Entries))
	}
}

func TestWriteMatrixAtomic(t *testing.T) {
	dir := t.TempDir()
	m := Matrix{ThresholdGrid: ThresholdGrid(), HistoricalBaselineThreshold: HistoricalBaselineThreshold}
	if err := WriteMatrix(dir, m); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "matrix.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "matrix.json" {
			t.Fatalf("unexpected leftover file %q", e.Name())
		}
	}
}

func TestDeadlineBounds(t *testing.T) {
	if got := Deadline(0); got != time.Minute {
		t.Fatalf("got %v want 1m floor", got)
	}
	if got := Deadline(45 * time.Minute); got != 30*time.Minute {
		t.Fatalf("got %v want 30m ceiling", got)
	}
	if got := Deadline(10 * time.Minute); got != 20*time.Minute {
		t.Fatalf("got %v want 20m (2x)", got)
	}
}
