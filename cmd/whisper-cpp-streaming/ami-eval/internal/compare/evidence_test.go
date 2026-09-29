package compare

import (
	"math"
	"strings"
	"testing"

	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/evaluate"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/manifest"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/score"
)

func TestBuildEvidenceFromRealMatrices(t *testing.T) {
	tm, hm, _ := fixtureMatrices(t)
	man := fixtureManifest()
	te, err := BuildTuningEvidence(man, tm)
	if err != nil {
		t.Fatalf("tuning evidence: %v", err)
	}
	he, err := BuildHeldOutEvidence(man, hm)
	if err != nil {
		t.Fatalf("held-out evidence: %v", err)
	}
	ts, _ := te.Summary()
	hs, _ := he.Summary()
	if len(ts.Recordings) != 4 || len(hs.Recordings) != 4 {
		t.Fatalf("expected 4 recordings per split, got %d/%d", len(ts.Recordings), len(hs.Recordings))
	}
	if strings.Join(ts.MeetingIDs, ",") != "EN2001a,ES2002a" || strings.Join(hs.MeetingIDs, ",") != "EN2002a,ES2004a" {
		t.Fatalf("split membership: %v / %v", ts.MeetingIDs, hs.MeetingIDs)
	}
	wantDetectors := 1 + len(evaluate.ThresholdGrid()) + 1
	if len(ts.Detectors) != wantDetectors {
		t.Fatalf("expected %d detectors, got %d", wantDetectors, len(ts.Detectors))
	}
	if ts.Detectors[0].DetectorID != evaluate.DetectorHistoricalFixed || ts.Detectors[len(ts.Detectors)-1].DetectorID != evaluate.DetectorAdaptive {
		t.Fatalf("detector order: %s ... %s", ts.Detectors[0].DetectorID, ts.Detectors[len(ts.Detectors)-1].DetectorID)
	}
}

func TestOverallIsRecordingMacroAverageNotDurationWeighted(t *testing.T) {
	tm, _, _ := fixtureMatrices(t)
	te, err := BuildTuningEvidence(fixtureManifest(), tm)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := te.Summary()
	checked := 0
	for _, d := range s.Detectors {
		var f1Sum, uerSum, ffSum float64
		var pooled score.FrameCounts
		ff := 0
		for i, r := range s.Recordings {
			m := d.PerRecording[i]
			if !m.Frame.F1.Valid || !m.Utterance.UtteranceErrorRate.Valid {
				continue
			}
			f1Sum += m.Frame.F1.Value
			uerSum += m.Utterance.UtteranceErrorRate.Value
			pooled.TP += m.Frame.Counts.TP
			pooled.FP += m.Frame.Counts.FP
			pooled.FN += m.Frame.Counts.FN
			pooled.TN += m.Frame.Counts.TN
			if r.Mic == manifest.MicFixedDistant {
				ffSum += m.Frame.FalsePositiveRate.Value
				ff++
			}
		}
		if d.Threshold == nil {
			continue
		}
		checked++
		if d.Overall.Frame.F1.Value != f1Sum/4 {
			t.Fatalf("%s overall F1 %v != mean of recordings %v", d.DetectorID, d.Overall.Frame.F1.Value, f1Sum/4)
		}
		if d.Overall.Utterance.UtteranceErrorRate.Value != uerSum/4 {
			t.Fatalf("%s overall UER %v != mean of recordings %v", d.DetectorID, d.Overall.Utterance.UtteranceErrorRate.Value, uerSum/4)
		}
		if ff != 2 || d.FarFieldFPR.Value != ffSum/2 {
			t.Fatalf("%s far-field FPR %v != mean of %d Array1-01 FPRs %v", d.DetectorID, d.FarFieldFPR.Value, ff, ffSum/2)
		}
		pooledF1 := score.DeriveFrameMetrics(pooled).F1.Value
		if d.DetectorID == evaluate.FixedDetectorID(0.01) && math.Abs(pooledF1-d.Overall.Frame.F1.Value) < 1e-12 {
			t.Fatal("fixture cannot distinguish macro from duration-weighted pooling")
		}
	}
	if checked != 1+len(evaluate.ThresholdGrid()) {
		t.Fatalf("checked %d fixed detectors", checked)
	}
}

func TestSubstitutedHeldOutKeepsQuarterWeightsAndNACells(t *testing.T) {
	_, _, sm := fixtureMatrices(t)
	he, err := BuildHeldOutEvidence(substitutedManifest(), sm)
	if err != nil {
		t.Fatalf("substituted held-out evidence: %v", err)
	}
	s, _ := he.Summary()
	if strings.Join(s.MeetingIDs, ",") != "ES2004a,IS1009a" {
		t.Fatalf("held-out meetings %v", s.MeetingIDs)
	}
	d, _ := s.Detector(evaluate.DetectorHistoricalFixed)
	var sum float64
	for _, m := range d.PerRecording {
		sum += m.Frame.F1.Value
	}
	if d.Overall.Frame.F1.Value != sum/4 {
		t.Fatalf("substituted overall F1 %v is not the equal quarter-weight mean %v", d.Overall.Frame.F1.Value, sum/4)
	}
	for _, c := range d.Cells {
		if c.Class == manifest.ClassNonScenario {
			if c.RecordingCount != 0 || c.Metrics.Frame.F1.Valid {
				t.Fatalf("absent non-scenario cell must be n/a, got %+v", c)
			}
		} else if c.RecordingCount != 2 {
			t.Fatalf("scenario cell %s has %d recordings", c.Mic, c.RecordingCount)
		}
	}
}

func TestEvidenceRejectsMixedAndMislabelledMatrices(t *testing.T) {
	tm, hm, _ := fixtureMatrices(t)
	man := fixtureManifest()
	if _, err := BuildTuningEvidence(man, hm); err == nil {
		t.Fatal("tuning evidence accepted a held-out matrix")
	}
	if _, err := BuildHeldOutEvidence(man, tm); err == nil {
		t.Fatal("held-out evidence accepted a tuning matrix")
	}
	// A tuning-labelled matrix that smuggles in held-out entries.
	mixed := cloneMatrix(tm)
	for _, e := range hm.Entries[:3] {
		e.Split = string(manifest.SplitTuning)
		mixed.Entries = append(mixed.Entries, e)
	}
	if _, err := BuildTuningEvidence(man, mixed); err == nil || !strings.Contains(err.Error(), "unexpected recording") {
		t.Fatalf("expected mixed matrix rejection, got %v", err)
	}
	var zero TuningEvidence
	if _, err := zero.Summary(); err == nil {
		t.Fatal("zero-value tuning evidence must be rejected")
	}
	var zeroH HeldOutEvidence
	if _, err := zeroH.Summary(); err == nil {
		t.Fatal("zero-value held-out evidence must be rejected")
	}
}

func TestEvidenceRejectsCoverageAndMetadataMutations(t *testing.T) {
	tm, _, _ := fixtureMatrices(t)
	man := fixtureManifest()
	rec := "ES2002a-fixed_distant"
	grid05 := evaluate.FixedDetectorID(0.005)
	grid01 := evaluate.FixedDetectorID(0.01)

	mutations := map[string]func(m *evaluate.Matrix){
		"remove recording": func(m *evaluate.Matrix) {
			var keep []evaluate.MatrixEntry
			for _, e := range m.Entries {
				if e.RecordingID != rec {
					keep = append(keep, e)
				}
			}
			m.Entries = keep
		},
		"remove grid detector": func(m *evaluate.Matrix) {
			i := entryIndex(t, *m, rec, grid05)
			m.Entries = append(m.Entries[:i], m.Entries[i+1:]...)
		},
		"remove baseline": func(m *evaluate.Matrix) {
			i := entryIndex(t, *m, rec, evaluate.DetectorHistoricalFixed)
			m.Entries = append(m.Entries[:i], m.Entries[i+1:]...)
		},
		"remove adaptive": func(m *evaluate.Matrix) {
			i := entryIndex(t, *m, rec, evaluate.DetectorAdaptive)
			m.Entries = append(m.Entries[:i], m.Entries[i+1:]...)
		},
		"duplicate entry": func(m *evaluate.Matrix) {
			m.Entries = append(m.Entries, m.Entries[entryIndex(t, *m, rec, grid05)])
		},
		"unexpected recording": func(m *evaluate.Matrix) {
			e := m.Entries[entryIndex(t, *m, rec, grid05)]
			e.RecordingID = "XX0000a-headset"
			m.Entries = append(m.Entries, e)
		},
		"unexpected detector": func(m *evaluate.Matrix) {
			e := m.Entries[entryIndex(t, *m, rec, grid05)]
			e.DetectorID = "fixed_0.0055"
			m.Entries = append(m.Entries, e)
		},
		"alter split": func(m *evaluate.Matrix) {
			m.Entries[entryIndex(t, *m, rec, grid05)].Split = string(manifest.SplitHeldOut)
		},
		"alter meeting": func(m *evaluate.Matrix) { m.Entries[entryIndex(t, *m, rec, grid05)].MeetingID = "EN2001a" },
		"alter mic":     func(m *evaluate.Matrix) { m.Entries[entryIndex(t, *m, rec, grid05)].Mic = manifest.MicHeadset },
		"alter class":   func(m *evaluate.Matrix) { m.Entries[entryIndex(t, *m, rec, grid05)].Class = manifest.ClassNonScenario },
		"alter threshold": func(m *evaluate.Matrix) {
			v := 0.006
			m.Entries[entryIndex(t, *m, rec, grid05)].Threshold = &v
		},
		"alter alias": func(m *evaluate.Matrix) {
			m.Entries[entryIndex(t, *m, rec, grid01)].MeasurementID = grid01
		},
		"alter matrix split": func(m *evaluate.Matrix) { m.Split = string(manifest.SplitHeldOut) },
		"alter baseline":     func(m *evaluate.Matrix) { m.HistoricalBaselineThreshold = 0.02 },
		"alter version":      func(m *evaluate.Matrix) { m.EvaluationVersion = "v1" },
		"alter grid":         func(m *evaluate.Matrix) { m.ThresholdGrid = m.ThresholdGrid[1:] },
		"claim whisper":      func(m *evaluate.Matrix) { m.WhisperAvailable = true },
	}
	for name, mutate := range mutations {
		m := cloneMatrix(tm)
		mutate(&m)
		if _, err := BuildTuningEvidence(man, m); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
	if _, err := BuildTuningEvidence(man, cloneMatrix(tm)); err != nil {
		t.Fatalf("unmutated matrix rejected: %v", err)
	}
}

func TestEvidenceRejectsInvalidDecisionMetrics(t *testing.T) {
	tm, _, _ := fixtureMatrices(t)
	man := fixtureManifest()
	rec := "EN2001a-fixed_distant"
	headset := "EN2001a-headset"
	grid := evaluate.FixedDetectorID(0.002)
	cases := map[string]func(m *evaluate.Matrix){
		"F1 invalid":   func(m *evaluate.Matrix) { m.Entries[entryIndex(t, *m, rec, grid)].FrameMetrics.F1.Valid = false },
		"F1 NaN":       func(m *evaluate.Matrix) { m.Entries[entryIndex(t, *m, rec, grid)].FrameMetrics.F1.Value = math.NaN() },
		"F1 Inf":       func(m *evaluate.Matrix) { m.Entries[entryIndex(t, *m, rec, grid)].FrameMetrics.F1.Value = math.Inf(1) },
		"F1 above one": func(m *evaluate.Matrix) { m.Entries[entryIndex(t, *m, rec, grid)].FrameMetrics.F1.Value = 1.5 },
		"UER invalid": func(m *evaluate.Matrix) {
			m.Entries[entryIndex(t, *m, headset, grid)].UtteranceMetrics.UtteranceErrorRate.Valid = false
		},
		"UER negative": func(m *evaluate.Matrix) {
			m.Entries[entryIndex(t, *m, headset, grid)].UtteranceMetrics.UtteranceErrorRate.Value = -0.1
		},
		"UER NaN": func(m *evaluate.Matrix) {
			m.Entries[entryIndex(t, *m, headset, grid)].UtteranceMetrics.UtteranceErrorRate.Value = math.NaN()
		},
		"far-field FPR invalid": func(m *evaluate.Matrix) {
			m.Entries[entryIndex(t, *m, rec, grid)].FrameMetrics.FalsePositiveRate.Valid = false
		},
		"baseline FPR Inf": func(m *evaluate.Matrix) {
			m.Entries[entryIndex(t, *m, rec, evaluate.DetectorHistoricalFixed)].FrameMetrics.FalsePositiveRate.Value = math.Inf(1)
		},
	}
	for name, mutate := range cases {
		m := cloneMatrix(tm)
		mutate(&m)
		_, err := BuildTuningEvidence(man, m)
		if err == nil {
			t.Errorf("%s: expected rejection", name)
			continue
		}
		if !strings.Contains(err.Error(), "tuning") || !strings.Contains(err.Error(), "detector") {
			t.Errorf("%s: error lacks split/detector context: %v", name, err)
		}
	}

	// Allowed: UER above one, an undefined headset FPR (not a decision
	// metric), undefined boundary timing, and undefined adaptive metrics.
	allowed := cloneMatrix(tm)
	allowed.Entries[entryIndex(t, allowed, headset, grid)].UtteranceMetrics.UtteranceErrorRate.Value = 2.5
	allowed.Entries[entryIndex(t, allowed, headset, grid)].FrameMetrics.FalsePositiveRate.Valid = false
	allowed.Entries[entryIndex(t, allowed, rec, grid)].UtteranceMetrics.StartErrorMedian = score.Ratio{}
	allowed.Entries[entryIndex(t, allowed, rec, evaluate.DetectorAdaptive)].FrameMetrics.F1 = score.Ratio{}
	te, err := BuildTuningEvidence(man, allowed)
	if err != nil {
		t.Fatalf("expected UER>1, undefined timing, and undefined descriptive metrics to be allowed: %v", err)
	}
	s, _ := te.Summary()
	a, _ := s.Detector(evaluate.DetectorAdaptive)
	if a.Overall.Frame.F1.Valid {
		t.Fatal("an undefined constituent must make the descriptive aggregate n/a, not be averaged away")
	}
	g, _ := s.Detector(grid)
	if g.Overall.Utterance.StartErrorMedian.Valid {
		t.Fatal("undefined timing constituent must yield n/a aggregate timing")
	}
}
