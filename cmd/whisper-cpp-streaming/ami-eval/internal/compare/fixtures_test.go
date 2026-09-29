package compare

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"testing"

	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/evaluate"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/manifest"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/score"
)

func seedHex(label string) string {
	sum := sha256.Sum256([]byte(label))
	return hex.EncodeToString(sum[:])
}

func fixtureMeeting(id, class string, split manifest.Split) manifest.Meeting {
	sel0 := 0
	audio := func(suffix string) manifest.AudioObject {
		return manifest.AudioObject{
			URL: "https://example.test/" + id + "/audio/" + id + suffix, SizeBytes: 1000, SHA256: seedHex(id + suffix),
			Channels: 1, ChannelPolicy: manifest.ChannelPolicyExplicit, SelectChannel: &sel0,
		}
	}
	return manifest.Meeting{
		ID: id, Class: class, Split: split,
		HeadsetMix:      audio(manifest.HeadsetAudioSuffix),
		FixedDistantMix: audio(manifest.FixedDistantAudioSuffix),
		Annotations: []manifest.AnnotationObject{
			{ParticipantID: "A", Member: "words/" + id + ".A.words.xml", SizeBytes: 10, SHA256: seedHex(id + ".A")},
		},
	}
}

// fixtureManifest is a valid schema-2 manifest with synthetic pins.
func fixtureManifest() *manifest.Manifest {
	return &manifest.Manifest{
		SchemaVersion:     manifest.SchemaVersion,
		AnnotationArchive: manifest.ArchiveObject{URL: "https://example.test/archive.zip", SizeBytes: 10, SHA256: seedHex("archive")},
		Meetings: []manifest.Meeting{
			fixtureMeeting("ES2002a", manifest.ClassScenario, manifest.SplitTuning),
			fixtureMeeting("EN2001a", manifest.ClassNonScenario, manifest.SplitTuning),
			fixtureMeeting("ES2004a", manifest.ClassScenario, manifest.SplitHeldOut),
			fixtureMeeting("EN2002a", manifest.ClassNonScenario, manifest.SplitHeldOut),
		},
	}
}

// substitutedManifest replaces EN2002a with IS1009a (a scenario meeting), so
// the held-out split has two scenario meetings and no non-scenario meeting.
func substitutedManifest() *manifest.Manifest {
	m := fixtureManifest()
	m.Meetings[3] = fixtureMeeting("IS1009a", manifest.ClassScenario, manifest.SplitHeldOut)
	m.HeldOutReplacement = &manifest.HeldOutReplacement{
		ReplacedMeetingID: "EN2002a", ReplacementMeetingID: "IS1009a",
		AnnotationFailureReason: "synthetic fixture: participant A word has end before start",
	}
	return m
}

// syntheticRecording builds deterministic audio for recording index i:
// alternating speech bursts (one loud, one quiet at an amplitude that
// varies by recording) and silence, with a recording-dependent length so
// recordings are deliberately unequal in duration.
func syntheticRecording(r manifest.Recording, i int) evaluate.RecordingInput {
	seconds := 4 + 2*i // unequal lengths: 4s, 6s, 8s, 10s
	quiet := float32(0.004 + 0.003*float64(i))
	audio := make([]float32, seconds*evaluate.SampleRate)
	var ref []score.Interval
	for s := 0; s+1 < seconds; s += 2 {
		amp := float32(0.5)
		if (s/2)%2 == 1 {
			amp = quiet
		}
		start, end := s*evaluate.SampleRate, (s+1)*evaluate.SampleRate
		for k := start; k < end; k++ {
			audio[k] = amp
		}
		ref = append(ref, score.Interval{Start: int64(start), End: int64(end)})
	}
	// Mark a short silent tail stretch as reference speech, so every
	// detector has at least one miss-able reference.
	tail := int64(seconds*evaluate.SampleRate) - int64(evaluate.SampleRate/2)
	ref = append(ref, score.Interval{Start: tail, End: tail + int64(evaluate.SampleRate/4)})
	return evaluate.RecordingInput{
		ID: r.ID, MeetingID: r.MeetingID, Class: r.Class, Split: string(r.Split), Mic: r.Mic,
		Audio: audio, Reference: ref,
	}
}

func runFixtureSplit(t testing.TB, man *manifest.Manifest, split manifest.Split) evaluate.Matrix {
	t.Helper()
	var inputs []evaluate.RecordingInput
	for i, r := range man.RecordingsForSplit(split) {
		inputs = append(inputs, syntheticRecording(r, i))
	}
	m, err := evaluate.RunMatrix(context.Background(), inputs, evaluate.MatrixConfig{Workers: evaluate.MaxWorkers, Split: string(split)})
	if err != nil {
		t.Fatalf("run %s matrix: %v", split, err)
	}
	return m
}

var (
	fixtureOnce    sync.Once
	fixtureTuning  evaluate.Matrix
	fixtureHeldOut evaluate.Matrix
	fixtureSubst   evaluate.Matrix
)

// fixtureMatrices returns real evaluator output for both splits of
// fixtureManifest and for the held-out split of substitutedManifest.
// Callers must copy before mutating (see cloneMatrix).
func fixtureMatrices(t testing.TB) (tuning, heldOut, substituted evaluate.Matrix) {
	t.Helper()
	fixtureOnce.Do(func() {
		fixtureTuning = runFixtureSplit(t, fixtureManifest(), manifest.SplitTuning)
		fixtureHeldOut = runFixtureSplit(t, fixtureManifest(), manifest.SplitHeldOut)
		fixtureSubst = runFixtureSplit(t, substitutedManifest(), manifest.SplitHeldOut)
	})
	return cloneMatrix(fixtureTuning), cloneMatrix(fixtureHeldOut), cloneMatrix(fixtureSubst)
}

func cloneMatrix(m evaluate.Matrix) evaluate.Matrix {
	out := m
	out.ThresholdGrid = append([]float64(nil), m.ThresholdGrid...)
	out.Entries = append([]evaluate.MatrixEntry(nil), m.Entries...)
	for i := range out.Entries {
		if out.Entries[i].Threshold != nil {
			v := *out.Entries[i].Threshold
			out.Entries[i].Threshold = &v
		}
	}
	return out
}

func entryIndex(t testing.TB, m evaluate.Matrix, recordingID, detectorID string) int {
	t.Helper()
	for i, e := range m.Entries {
		if e.RecordingID == recordingID && e.DetectorID == detectorID {
			return i
		}
	}
	t.Fatalf("no entry %s/%s", recordingID, detectorID)
	return -1
}
