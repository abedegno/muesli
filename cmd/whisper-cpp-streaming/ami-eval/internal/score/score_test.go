package score

import (
	"math"
	"testing"
)

const sr = 16000 // 16kHz; 20ms frame = 320 samples

func frame(startSample int64, validSamples int64, predicted bool) Frame {
	return Frame{Start: startSample, End: startSample + 320, ValidSamples: validSamples, Predicted: predicted}
}

// --- frame scoring -------------------------------------------------------

func TestScoreFramesHalfOpenIntersection(t *testing.T) {
	refs := []Interval{{Start: 320, End: 640}} // exactly frame 1
	frames := []Frame{
		frame(0, 320, false),   // frame 0: [0,320) does not intersect [320,640)
		frame(320, 320, true),  // frame 1: [320,640) exactly the reference
		frame(640, 320, false), // frame 2: [640,960) does not intersect
	}
	counts := ScoreFrames(frames, refs)
	if counts.TP != 320 {
		t.Fatalf("expected TP=320, got %+v", counts)
	}
	if counts.TN != 640 {
		t.Fatalf("expected TN=640 (frames 0 and 2 correctly negative), got %+v", counts)
	}
}

func TestScoreFramesWeightedTPFPTNFN(t *testing.T) {
	refs := []Interval{{Start: 0, End: 320}}
	frames := []Frame{
		frame(0, 320, true),    // TP
		frame(320, 320, true),  // FP (predicted speech, no reference)
		frame(640, 320, false), // TN
	}
	// Add an FN: reference says speech, predicted false.
	refs = append(refs, Interval{Start: 960, End: 1280})
	frames = append(frames, frame(960, 320, false))

	counts := ScoreFrames(frames, refs)
	if counts.TP != 320 || counts.FP != 320 || counts.TN != 320 || counts.FN != 320 {
		t.Fatalf("unexpected counts: %+v", counts)
	}
}

func TestScoreFramesPartialFinalFrameWeighting(t *testing.T) {
	// A final frame padded from 200 valid samples to a full 320-sample
	// frame: only the 200 valid samples should be scored.
	refs := []Interval{{Start: 0, End: 320}} // reference covers the whole nominal span
	frames := []Frame{frame(0, 200, true)}
	counts := ScoreFrames(frames, refs)
	if counts.TP != 200 {
		t.Fatalf("expected TP weighted by 200 valid samples, got %+v", counts)
	}
	if counts.TP+counts.FP+counts.TN+counts.FN != 200 {
		t.Fatalf("expected total weight 200 (padding excluded), got %+v", counts)
	}
}

func TestScoreFramesZeroWeightPaddingAndFlushExcluded(t *testing.T) {
	frames := []Frame{
		{Start: 0, End: 320, ValidSamples: 0, Predicted: true},    // all padding
		{Start: 320, End: 640, ValidSamples: 0, Predicted: false}, // all flush
	}
	counts := ScoreFrames(frames, nil)
	if counts.TP != 0 || counts.FP != 0 || counts.TN != 0 || counts.FN != 0 {
		t.Fatalf("expected all-zero counts for zero-weight frames, got %+v", counts)
	}
}

func TestDeriveFrameMetricsZeroDenominators(t *testing.T) {
	m := DeriveFrameMetrics(FrameCounts{})
	for name, r := range map[string]Ratio{
		"precision": m.Precision, "recall": m.Recall, "f1": m.F1,
		"fpr": m.FalsePositiveRate, "fnr": m.FalseNegativeRate,
	} {
		if r.Valid {
			t.Fatalf("%s expected n/a for all-zero counts, got %+v", name, r)
		}
	}
}

func TestDeriveFrameMetricsComputed(t *testing.T) {
	c := FrameCounts{TP: 80, FP: 20, TN: 70, FN: 30}
	m := DeriveFrameMetrics(c)
	if !m.Precision.Valid || math.Abs(m.Precision.Value-0.8) > 1e-9 {
		t.Fatalf("precision: %+v", m.Precision)
	}
	if !m.Recall.Valid || math.Abs(m.Recall.Value-(80.0/110.0)) > 1e-9 {
		t.Fatalf("recall: %+v", m.Recall)
	}
	wantF1 := 2 * 0.8 * (80.0 / 110.0) / (0.8 + 80.0/110.0)
	if !m.F1.Valid || math.Abs(m.F1.Value-wantF1) > 1e-9 {
		t.Fatalf("f1: %+v want %v", m.F1, wantF1)
	}
	if !m.FalsePositiveRate.Valid || math.Abs(m.FalsePositiveRate.Value-(20.0/90.0)) > 1e-9 {
		t.Fatalf("fpr: %+v", m.FalsePositiveRate)
	}
	if !m.FalseNegativeRate.Valid || math.Abs(m.FalseNegativeRate.Value-(30.0/110.0)) > 1e-9 {
		t.Fatalf("fnr: %+v", m.FalseNegativeRate)
	}
}

func TestScoreFramesDegenerateValidSamplesIgnored(t *testing.T) {
	// A frame with a nonsensical negative ValidSamples must contribute no
	// weight rather than panicking or corrupting counts.
	frames := []Frame{{Start: 100, End: 420, ValidSamples: -1, Predicted: true}}
	counts := ScoreFrames(frames, nil)
	if counts.TP+counts.FP+counts.TN+counts.FN != 0 {
		t.Fatalf("expected zero weight for degenerate frame, got %+v", counts)
	}
}

// --- utterance scoring -----------------------------------------------------

func TestScoreUtterancesOneToOneBoundaryErrors(t *testing.T) {
	refs := []Interval{{Start: 1000, End: 5000}}
	preds := []Interval{{Start: 1100, End: 4900}}
	m := ScoreUtterances(preds, refs, sr)
	if m.Counts.OneToOnePairs != 1 {
		t.Fatalf("expected 1 one-to-one pair, got %+v", m.Counts)
	}
	wantStart := 100.0 / sr
	wantEnd := 100.0 / sr
	if !m.StartErrorMedian.Valid || math.Abs(m.StartErrorMedian.Value-wantStart) > 1e-12 {
		t.Fatalf("start error: %+v want %v", m.StartErrorMedian, wantStart)
	}
	if !m.EndErrorMedian.Valid || math.Abs(m.EndErrorMedian.Value-wantEnd) > 1e-12 {
		t.Fatalf("end error: %+v want %v", m.EndErrorMedian, wantEnd)
	}
}

func TestScoreUtterancesMiss(t *testing.T) {
	refs := []Interval{{Start: 0, End: 100}}
	m := ScoreUtterances(nil, refs, sr)
	if m.Counts.Misses != 1 {
		t.Fatalf("expected 1 miss, got %+v", m.Counts)
	}
	if !m.MissRate.Valid || m.MissRate.Value != 1.0 {
		t.Fatalf("miss rate: %+v", m.MissRate)
	}
}

func TestScoreUtterancesSpurious(t *testing.T) {
	preds := []Interval{{Start: 0, End: 100}}
	m := ScoreUtterances(preds, nil, sr)
	if m.Counts.Spurious != 1 {
		t.Fatalf("expected 1 spurious, got %+v", m.Counts)
	}
	if !m.SpuriousRate.Valid || m.SpuriousRate.Value != 1.0 {
		t.Fatalf("spurious rate: %+v", m.SpuriousRate)
	}
}

func TestScoreUtterancesSplit(t *testing.T) {
	// One reference covered by two non-overlapping predictions.
	refs := []Interval{{Start: 0, End: 1000}}
	preds := []Interval{{Start: 0, End: 400}, {Start: 500, End: 1000}}
	m := ScoreUtterances(preds, refs, sr)
	if m.Counts.SplitExtras != 1 {
		t.Fatalf("expected 1 split extra, got %+v", m.Counts)
	}
	if m.Counts.Misses != 0 || m.Counts.Spurious != 0 {
		t.Fatalf("unexpected misses/spurious: %+v", m.Counts)
	}
}

func TestScoreUtterancesMerge(t *testing.T) {
	// One prediction spanning two separate references.
	refs := []Interval{{Start: 0, End: 400}, {Start: 500, End: 1000}}
	preds := []Interval{{Start: 0, End: 1000}}
	m := ScoreUtterances(preds, refs, sr)
	if m.Counts.MergeExtras != 1 {
		t.Fatalf("expected 1 merge extra, got %+v", m.Counts)
	}
}

func TestScoreUtterancesManyToMany(t *testing.T) {
	// 2 references and 2 predictions, all four pairs overlapping.
	refs := []Interval{{Start: 0, End: 600}, {Start: 400, End: 1000}}
	preds := []Interval{{Start: 0, End: 700}, {Start: 300, End: 1000}}
	m := ScoreUtterances(preds, refs, sr)
	// Each reference has 2 overlapping predictions -> 1 split extra each = 2.
	// Each prediction has 2 overlapping references -> 1 merge extra each = 2.
	if m.Counts.SplitExtras != 2 {
		t.Fatalf("expected 2 split extras, got %+v", m.Counts)
	}
	if m.Counts.MergeExtras != 2 {
		t.Fatalf("expected 2 merge extras, got %+v", m.Counts)
	}
	if m.Counts.OneToOnePairs != 0 {
		t.Fatalf("expected no one-to-one pairs in many-to-many, got %+v", m.Counts)
	}
}

func TestScoreUtterancesClipping(t *testing.T) {
	// Predictions are assumed already clipped by the caller; verify overlap
	// classification still behaves sanely at the exact audio boundary edge
	// (half-open, so touching endpoints do not count as overlap).
	refs := []Interval{{Start: 0, End: 100}}
	preds := []Interval{{Start: 100, End: 200}} // touches but does not overlap
	m := ScoreUtterances(preds, refs, sr)
	if m.Counts.Misses != 1 || m.Counts.Spurious != 1 {
		t.Fatalf("expected touching intervals to not overlap: %+v", m.Counts)
	}
}

func TestScoreUtterancesAllDenominatorsZero(t *testing.T) {
	m := ScoreUtterances(nil, nil, sr)
	for name, r := range map[string]Ratio{
		"miss": m.MissRate, "split": m.SplitExtraRate, "merge": m.MergeExtraRate,
		"spurious": m.SpuriousRate, "error": m.UtteranceErrorRate,
	} {
		if r.Valid {
			t.Fatalf("%s expected n/a with no references/predictions, got %+v", name, r)
		}
	}
	if m.StartErrorMedian.Valid || m.EndErrorP95.Valid {
		t.Fatal("expected boundary error stats n/a with no one-to-one pairs")
	}
}

func TestScoreUtterancesErrorRateFormula(t *testing.T) {
	// 3 references: 1 miss, 1 one-to-one, 1 split into 2 predictions (1 extra).
	// 1 additional spurious prediction.
	refs := []Interval{
		{Start: 0, End: 100},   // miss
		{Start: 200, End: 300}, // one-to-one
		{Start: 400, End: 900}, // split by two predictions
	}
	preds := []Interval{
		{Start: 200, End: 300},   // matches ref 1 one-to-one
		{Start: 400, End: 600},   // half of split ref 2
		{Start: 650, End: 900},   // other half of split ref 2
		{Start: 1000, End: 1100}, // spurious
	}
	m := ScoreUtterances(preds, refs, sr)
	if m.Counts.Misses != 1 || m.Counts.SplitExtras != 1 || m.Counts.Spurious != 1 || m.Counts.MergeExtras != 0 {
		t.Fatalf("unexpected counts: %+v", m.Counts)
	}
	// error rate = (1+1+0+1)/3 references = 1.0
	if !m.UtteranceErrorRate.Valid || math.Abs(m.UtteranceErrorRate.Value-1.0) > 1e-9 {
		t.Fatalf("utterance error rate: %+v", m.UtteranceErrorRate)
	}
}

// --- median / p95 ----------------------------------------------------------

func TestMedianOdd(t *testing.T) {
	if got := Median([]float64{3, 1, 2}); got != 2 {
		t.Fatalf("got %v want 2", got)
	}
}

func TestMedianEven(t *testing.T) {
	if got := Median([]float64{1, 2, 3, 4}); got != 2.5 {
		t.Fatalf("got %v want 2.5", got)
	}
}

func TestP95NearestRank(t *testing.T) {
	// 20 values 1..20: ceil(0.95*20)=19th smallest value = 19.
	values := make([]float64, 20)
	for i := range values {
		values[i] = float64(i + 1)
	}
	if got := P95NearestRank(values); got != 19 {
		t.Fatalf("got %v want 19", got)
	}
}

func TestP95NearestRankSmallN(t *testing.T) {
	// n=1: ceil(0.95*1)=1 -> the only value.
	if got := P95NearestRank([]float64{42}); got != 42 {
		t.Fatalf("got %v want 42", got)
	}
	// n=3: ceil(0.95*3)=ceil(2.85)=3 -> the largest.
	if got := P95NearestRank([]float64{1, 2, 3}); got != 3 {
		t.Fatalf("got %v want 3", got)
	}
}
