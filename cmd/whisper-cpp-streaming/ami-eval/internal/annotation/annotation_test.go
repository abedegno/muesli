package annotation

import (
	"os"
	"strings"
	"testing"
)

func TestParseParticipantMinimizedFixture(t *testing.T) {
	f, err := os.Open("testdata/ami.words.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	raw, cnts, err := ParseParticipant(f)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// words0,3,5,7 have positive-duration timing; words1 is zero-duration
	// punctuation; words9 is untimed.
	if len(raw) != 4 {
		t.Fatalf("expected 4 timed words, got %d: %+v", len(raw), raw)
	}
	if cnts.IgnoredMarkup != 3 { // vocalsound, disfmarker, gap
		t.Fatalf("expected 3 markup elements ignored, got %d", cnts.IgnoredMarkup)
	}
	if cnts.IgnoredUnknown != 1 { // <pause>
		t.Fatalf("expected 1 unknown element ignored, got %d", cnts.IgnoredUnknown)
	}
	if cnts.IgnoredUntimed != 1 { // words9
		t.Fatalf("expected 1 untimed word ignored, got %d", cnts.IgnoredUntimed)
	}
	if cnts.IgnoredZeroDuration != 1 { // words1, punctuation
		t.Fatalf("expected 1 zero-duration word ignored, got %d", cnts.IgnoredZeroDuration)
	}
}

func TestParseParticipantMalformedFixtureFails(t *testing.T) {
	f, err := os.Open("testdata/malformed.words.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, _, err := ParseParticipant(f); err == nil {
		t.Fatal("expected fatal error for reversed timing")
	}
}

func TestParseParticipantNonFiniteFails(t *testing.T) {
	xmlDoc := `<nite:root xmlns:nite="http://nite.sourceforge.net/">
		<w starttime="NaN" endtime="1.0">bad</w>
	</nite:root>`
	if _, _, err := ParseParticipant(strings.NewReader(xmlDoc)); err == nil {
		t.Fatal("expected non-finite rejection")
	}
}

func TestParseParticipantNegativeFails(t *testing.T) {
	xmlDoc := `<nite:root xmlns:nite="http://nite.sourceforge.net/">
		<w starttime="-1.0" endtime="1.0">bad</w>
	</nite:root>`
	if _, _, err := ParseParticipant(strings.NewReader(xmlDoc)); err == nil {
		t.Fatal("expected negative-time rejection")
	}
}

func TestParseParticipantReversedFails(t *testing.T) {
	xmlDoc := `<nite:root xmlns:nite="http://nite.sourceforge.net/">
		<w starttime="2.0" endtime="1.0">bad</w>
	</nite:root>`
	if _, _, err := ParseParticipant(strings.NewReader(xmlDoc)); err == nil {
		t.Fatal("expected reversed-time rejection")
	}
}

func TestParseParticipantZeroLengthTolerated(t *testing.T) {
	// end == start is real-shape AMI punctuation (shares the adjacent
	// word's timestamp): tolerated and counted, not fatal.
	xmlDoc := `<nite:root xmlns:nite="http://nite.sourceforge.net/">
		<w starttime="2.0" endtime="2.0" punc="true">,</w>
	</nite:root>`
	raw, cnts, err := ParseParticipant(strings.NewReader(xmlDoc))
	if err != nil {
		t.Fatalf("expected zero-length word tolerated, got error: %v", err)
	}
	if len(raw) != 0 {
		t.Fatalf("expected zero-length word excluded from timed words, got %+v", raw)
	}
	if cnts.IgnoredZeroDuration != 1 {
		t.Fatalf("expected 1 zero-duration counted, got %d", cnts.IgnoredZeroDuration)
	}
}

func TestParseParticipantUnparsableNumberFails(t *testing.T) {
	xmlDoc := `<nite:root xmlns:nite="http://nite.sourceforge.net/">
		<w starttime="not-a-number" endtime="1.0">bad</w>
	</nite:root>`
	if _, _, err := ParseParticipant(strings.NewReader(xmlDoc)); err == nil {
		t.Fatal("expected unparsable-number rejection")
	}
}

const sampleRate = 16000

func iv(startSamples, endSamples int64) Interval {
	return Interval{Start: startSamples, End: endSamples}
}

func TestBuildReferenceHalfOpenAndSpeakerUnion(t *testing.T) {
	participants := []ParticipantIntervals{
		{ParticipantID: "A", Raw: []RawInterval{{StartSeconds: 1.0, EndSeconds: 2.0}}},
		{ParticipantID: "B", Raw: []RawInterval{{StartSeconds: 5.0, EndSeconds: 6.0}}},
	}
	ref, err := BuildReference(participants, map[string]string{"A": "x", "B": "y"}, 10*sampleRate, sampleRate)
	if err != nil {
		t.Fatalf("build reference: %v", err)
	}
	want := []Interval{iv(16000, 32000), iv(80000, 96000)}
	if len(ref.Intervals) != len(want) {
		t.Fatalf("got %+v want %+v", ref.Intervals, want)
	}
	for i := range want {
		if ref.Intervals[i] != want[i] {
			t.Fatalf("interval %d: got %+v want %+v", i, ref.Intervals[i], want[i])
		}
	}
}

func TestBuildReferenceOverlapMerges(t *testing.T) {
	participants := []ParticipantIntervals{
		{ParticipantID: "A", Raw: []RawInterval{{StartSeconds: 1.0, EndSeconds: 3.0}}},
		{ParticipantID: "B", Raw: []RawInterval{{StartSeconds: 2.0, EndSeconds: 4.0}}},
	}
	ref, err := BuildReference(participants, nil, 10*sampleRate, sampleRate)
	if err != nil {
		t.Fatal(err)
	}
	if len(ref.Intervals) != 1 {
		t.Fatalf("expected overlap to merge into 1 interval, got %+v", ref.Intervals)
	}
	if ref.Intervals[0] != iv(16000, 64000) {
		t.Fatalf("got %+v", ref.Intervals[0])
	}
}

func TestBuildReferenceExactly300msGapMerges(t *testing.T) {
	// Second interval starts exactly 300ms after the first ends.
	participants := []ParticipantIntervals{
		{ParticipantID: "A", Raw: []RawInterval{
			{StartSeconds: 1.0, EndSeconds: 2.0},
			{StartSeconds: 2.3, EndSeconds: 2.5},
		}},
	}
	ref, err := BuildReference(participants, nil, 10*sampleRate, sampleRate)
	if err != nil {
		t.Fatal(err)
	}
	if len(ref.Intervals) != 1 {
		t.Fatalf("expected exactly-300ms gap to merge, got %+v", ref.Intervals)
	}
}

func TestBuildReferenceJustOver300msGapDoesNotMerge(t *testing.T) {
	// Second interval starts one sample beyond the 300ms tolerance.
	overGap := 2.0 + 0.300 + 1.0/float64(sampleRate)
	participants := []ParticipantIntervals{
		{ParticipantID: "A", Raw: []RawInterval{
			{StartSeconds: 1.0, EndSeconds: 2.0},
			{StartSeconds: overGap, EndSeconds: overGap + 0.2},
		}},
	}
	ref, err := BuildReference(participants, nil, 10*sampleRate, sampleRate)
	if err != nil {
		t.Fatal(err)
	}
	if len(ref.Intervals) != 2 {
		t.Fatalf("expected just-over-300ms gap to stay separate, got %+v", ref.Intervals)
	}
}

func TestBuildReferenceRoundingHalvesAwayFromZero(t *testing.T) {
	// Use a small power-of-two rate so the exact-.5-sample boundary is
	// exactly representable in float64, avoiding incidental precision noise
	// in this test: at rate 2, 0.75s is exactly 1.5 samples, which must
	// round away from zero (up) to 2, and 0.25s is exactly 0.5 samples,
	// which must round up to 1.
	const rate = 2
	participants := []ParticipantIntervals{
		{ParticipantID: "A", Raw: []RawInterval{{StartSeconds: 0.25, EndSeconds: 0.75}}},
	}
	ref, err := BuildReference(participants, nil, 100, rate)
	if err != nil {
		t.Fatal(err)
	}
	if len(ref.Intervals) != 1 {
		t.Fatalf("expected 1 interval, got %+v", ref.Intervals)
	}
	if ref.Intervals[0].Start != 1 {
		t.Fatalf("got start=%d, want 1 (0.5 samples rounds away from zero)", ref.Intervals[0].Start)
	}
	if ref.Intervals[0].End != 2 {
		t.Fatalf("got end=%d, want 2 (1.5 samples rounds away from zero)", ref.Intervals[0].End)
	}
}

func TestBuildReferenceClampsToAudioExtent(t *testing.T) {
	audioSamples := int64(5 * sampleRate)
	participants := []ParticipantIntervals{
		// Ends 0.5s beyond a 5s audio extent -- within the 1s fatal threshold, so clamped.
		{ParticipantID: "A", Raw: []RawInterval{{StartSeconds: 4.5, EndSeconds: 5.5}}},
	}
	ref, err := BuildReference(participants, nil, audioSamples, sampleRate)
	if err != nil {
		t.Fatalf("expected clamp, not fatal: %v", err)
	}
	if ref.Counters.ClampedEnd != 1 {
		t.Fatalf("expected 1 clamped end, got %d", ref.Counters.ClampedEnd)
	}
	if ref.Intervals[0].End != audioSamples {
		t.Fatalf("expected end clamped to audio extent, got %d", ref.Intervals[0].End)
	}
}

func TestBuildReferenceFatalOnLargeOverrun(t *testing.T) {
	audioSamples := int64(5 * sampleRate)
	participants := []ParticipantIntervals{
		// Ends 2s beyond the audio extent -- beyond the 1s fatal threshold.
		{ParticipantID: "A", Raw: []RawInterval{{StartSeconds: 6.0, EndSeconds: 7.0}}},
	}
	if _, err := BuildReference(participants, nil, audioSamples, sampleRate); err == nil {
		t.Fatal("expected fatal error for annotation extent far beyond audio")
	}
}

func TestBuildReferenceClampedEmptyCollapsesAndIsExcluded(t *testing.T) {
	audioSamples := int64(2 * sampleRate)
	participants := []ParticipantIntervals{
		// Entirely beyond the audio extent by less than 1s: both start and
		// end clamp to audioSamples, collapsing to an empty interval.
		{ParticipantID: "A", Raw: []RawInterval{{StartSeconds: 2.1, EndSeconds: 2.4}}},
	}
	ref, err := BuildReference(participants, nil, audioSamples, sampleRate)
	if err != nil {
		t.Fatalf("expected clamp, not fatal: %v", err)
	}
	if ref.Counters.ClampedEmpty != 1 {
		t.Fatalf("expected 1 clamped_empty, got %d", ref.Counters.ClampedEmpty)
	}
	if len(ref.Intervals) != 0 {
		t.Fatalf("expected collapsed interval excluded from output, got %+v", ref.Intervals)
	}
}

func TestBuildReferenceMergeGapSamplesRecorded(t *testing.T) {
	ref, err := BuildReference(nil, nil, sampleRate, sampleRate)
	if err != nil {
		t.Fatal(err)
	}
	if ref.MergeGapSamples != 4800 {
		t.Fatalf("expected merge gap of 4800 samples (300ms @ 16kHz), got %d", ref.MergeGapSamples)
	}
	if ref.SchemaVersion != SchemaVersion {
		t.Fatalf("expected schema version %d, got %d", SchemaVersion, ref.SchemaVersion)
	}
}
