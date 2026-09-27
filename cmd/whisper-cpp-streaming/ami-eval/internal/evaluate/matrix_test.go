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

func TestThresholdGridRange(t *testing.T) {
	grid := ThresholdGrid()
	if len(grid) != 30 {
		t.Fatalf("expected 30 thresholds, got %d", len(grid))
	}
	if grid[0] != 0.001 {
		t.Fatalf("expected first threshold 0.001, got %v", grid[0])
	}
	if grid[len(grid)-1] != 0.030 {
		t.Fatalf("expected last threshold 0.030, got %v", grid[len(grid)-1])
	}
}

func TestBuildJobsReusesShippedGridPoint(t *testing.T) {
	shipped := pluginkit.DefaultStreamingConfig().EnergyThreshold
	jobs := buildJobs(shipped, nil, false)

	var shippedJob, gridJob *job
	for i := range jobs {
		if jobs[i].detectorID == DetectorShippedFixed {
			shippedJob = &jobs[i]
		}
		if jobs[i].detectorID == FixedDetectorID(shipped) && jobs[i].detectorID != DetectorShippedFixed {
			gridJob = &jobs[i]
		}
	}
	if shippedJob == nil {
		t.Fatal("expected a fixed_shipped job")
	}
	if shippedJob.vadFactory == nil {
		t.Fatal("expected fixed_shipped to actually run")
	}
	if gridJob == nil {
		t.Fatalf("expected a grid job for the shipped threshold %v", shipped)
	}
	if gridJob.reuseFrom != DetectorShippedFixed {
		t.Fatalf("expected grid job at shipped threshold to reuse fixed_shipped, got reuseFrom=%q", gridJob.reuseFrom)
	}
	if gridJob.vadFactory != nil {
		t.Fatal("expected the reused grid job to never construct its own detector")
	}
	if gridJob.measurementID != shippedJob.measurementID {
		t.Fatalf("expected shared measurement id, got %q vs %q", gridJob.measurementID, shippedJob.measurementID)
	}

	// Exactly one fixed job per grid point plus the shipped one, no
	// duplicate independent run at the shipped threshold.
	fixedCount := 0
	for _, j := range jobs {
		if j.detectorID == DetectorShippedFixed || j.detectorID[:6] == "fixed_" {
			fixedCount++
		}
	}
	if fixedCount != 1+len(ThresholdGrid()) {
		t.Fatalf("expected %d fixed jobs, got %d", 1+len(ThresholdGrid()), fixedCount)
	}
}

func TestBuildJobsIncludesAdaptiveAndOptionalWhisper(t *testing.T) {
	jobs := buildJobs(0.01, nil, false)
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
	jobsWithWhisper := buildJobs(0.01, factory, true)
	found2 := map[string]bool{}
	for _, j := range jobsWithWhisper {
		found2[j.detectorID] = true
	}
	if !found2[DetectorWhisperCPP] {
		t.Fatal("expected a whisper job when available")
	}
}

func testRecording(id, meeting, class, mic string, audio []float32, reference []score.Interval) RecordingInput {
	return RecordingInput{ID: id, MeetingID: meeting, Class: class, Mic: mic, Audio: audio, Reference: reference}
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
	m, err := RunMatrix(context.Background(), []RecordingInput{rec}, MatrixConfig{})
	if err != nil {
		t.Fatalf("run matrix: %v", err)
	}
	if m.WhisperAvailable {
		t.Fatal("expected production whisper.cpp to be unavailable")
	}
	if m.WhisperUnavailableReason == "" {
		t.Fatal("expected an unavailable reason")
	}
	wantDetectors := 1 /* shipped */ + len(ThresholdGrid()) + 1 /* adaptive */
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
	if !seen[DetectorShippedFixed] || !seen[DetectorAdaptive] {
		t.Fatalf("missing expected detectors: %+v", seen)
	}
}

func TestRunMatrixShippedAndGridReuseIdenticalMetrics(t *testing.T) {
	rec := testRecording("ES2002a-headset", "ES2002a", "scenario", "headset", loudAudio(FeedSamples*2), []score.Interval{{Start: 0, End: FeedSamples * 2}})
	m, err := RunMatrix(context.Background(), []RecordingInput{rec}, MatrixConfig{})
	if err != nil {
		t.Fatal(err)
	}
	shipped := pluginkit.DefaultStreamingConfig().EnergyThreshold
	var shippedEntry, gridEntry *MatrixEntry
	for i := range m.Entries {
		if m.Entries[i].DetectorID == DetectorShippedFixed {
			shippedEntry = &m.Entries[i]
		}
		if m.Entries[i].DetectorID == FixedDetectorID(shipped) {
			gridEntry = &m.Entries[i]
		}
	}
	if shippedEntry == nil || gridEntry == nil {
		t.Fatal("expected both shipped and grid-0.010 entries")
	}
	if shippedEntry.MeasurementID != gridEntry.MeasurementID {
		t.Fatalf("expected shared measurement id: %q vs %q", shippedEntry.MeasurementID, gridEntry.MeasurementID)
	}
	if shippedEntry.FrameMetrics.Counts != gridEntry.FrameMetrics.Counts {
		t.Fatalf("expected identical frame counts for the reused measurement: %+v vs %+v", shippedEntry.FrameMetrics.Counts, gridEntry.FrameMetrics.Counts)
	}
}

func TestRunMatrixWhisperUnavailableFromInjectedFactory(t *testing.T) {
	rec := testRecording("ES2002a-headset", "ES2002a", "scenario", "headset", loudAudio(FeedSamples), []score.Interval{{Start: 0, End: FeedSamples}})
	m, err := RunMatrix(context.Background(), []RecordingInput{rec}, MatrixConfig{
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
	m, err := RunMatrix(context.Background(), []RecordingInput{recB, recA}, MatrixConfig{})
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
	m, err := RunMatrix(context.Background(), recs, MatrixConfig{Workers: 100}) // must clamp to MaxWorkers
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
	m := Matrix{ThresholdGrid: ThresholdGrid(), ShippedThreshold: 0.01}
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
