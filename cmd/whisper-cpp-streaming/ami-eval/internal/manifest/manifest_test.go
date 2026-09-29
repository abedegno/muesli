package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hexSeed derives a distinct lowercase 64-hex pin from a label, so every
// synthetic object in validManifest has a unique hash.
func hexSeed(label string) string {
	sum := sha256.Sum256([]byte(label))
	return hex.EncodeToString(sum[:])
}

func testMeeting(id, class string, split Split, participants ...string) Meeting {
	sel0 := 0
	if len(participants) == 0 {
		participants = []string{"A", "B"}
	}
	m := Meeting{
		ID:    id,
		Class: class,
		Split: split,
		HeadsetMix: AudioObject{
			URL: "https://example.test/" + id + "/audio/" + id + ".Mix-Headset.wav", SizeBytes: 1000,
			SHA256: hexSeed(id + "|headset"), Channels: 1, ChannelPolicy: ChannelPolicyExplicit, SelectChannel: &sel0,
		},
		FixedDistantMix: AudioObject{
			URL: "https://example.test/" + id + "/audio/" + id + ".Array1-01.wav", SizeBytes: 1001,
			SHA256: hexSeed(id + "|fixed"), Channels: 1, ChannelPolicy: ChannelPolicyExplicit, SelectChannel: &sel0,
		},
	}
	for _, p := range participants {
		m.Annotations = append(m.Annotations, AnnotationObject{
			ParticipantID: p, Member: "words/" + id + "." + p + ".words.xml", SizeBytes: 100, SHA256: hexSeed(id + "|" + p),
		})
	}
	return m
}

func validManifest() Manifest {
	return Manifest{
		SchemaVersion: SchemaVersion,
		AnnotationArchive: ArchiveObject{
			URL:       "https://example.test/ami/ami_public_manual_1.6.2.zip",
			SizeBytes: 22887865,
			SHA256:    strings.Repeat("a", 64),
		},
		// Deliberately not in sorted order.
		Meetings: []Meeting{
			testMeeting("EN2002a", ClassNonScenario, SplitHeldOut),
			testMeeting("EN2001a", ClassNonScenario, SplitTuning),
			testMeeting("ES2004a", ClassScenario, SplitHeldOut, "A", "B", "C"),
			testMeeting("ES2002a", ClassScenario, SplitTuning),
		},
	}
}

// meetingIndex returns the index of id in m.Meetings, or fails the test.
func meetingIndex(t *testing.T, m *Manifest, id string) int {
	t.Helper()
	for i := range m.Meetings {
		if m.Meetings[i].ID == id {
			return i
		}
	}
	t.Fatalf("meeting %s not in manifest", id)
	return -1
}

// replaceHeldOut swaps requested held-out meeting `replaced` for IS1009a
// with the given reason.
func replaceHeldOut(t *testing.T, m *Manifest, replaced, reason string) {
	t.Helper()
	i := meetingIndex(t, m, replaced)
	m.Meetings[i] = testMeeting(HeldOutSubstituteMeetingID, ClassScenario, SplitHeldOut)
	m.HeldOutReplacement = &HeldOutReplacement{
		ReplacedMeetingID:       replaced,
		ReplacementMeetingID:    HeldOutSubstituteMeetingID,
		AnnotationFailureReason: reason,
	}
}

func wantInvalid(t *testing.T, m Manifest, contains string) {
	t.Helper()
	err := m.Validate()
	if err == nil {
		t.Fatalf("expected validation failure containing %q", contains)
	}
	if !strings.Contains(err.Error(), contains) {
		t.Fatalf("expected error containing %q, got: %v", contains, err)
	}
}

func mustJSON(t *testing.T, m Manifest) []byte {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestValidManifestPasses(t *testing.T) {
	m := validManifest()
	if err := m.Validate(); err != nil {
		t.Fatalf("expected valid manifest, got error: %v", err)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	raw := `{"schema_version":2,"bogus_field":true,"annotation_archive":{},"meetings":[]}`
	if _, err := Load([]byte(raw)); err == nil {
		t.Fatal("expected schema rejection for unknown field")
	}
}

func TestLoadRejectsMalformedJSON(t *testing.T) {
	if _, err := Load([]byte("{not json")); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestDigestStableAcrossOrdering(t *testing.T) {
	a := validManifest()
	b := validManifest()
	// Reverse meeting order and annotation order in b; digest must not change.
	for i, j := 0, len(b.Meetings)-1; i < j; i, j = i+1, j-1 {
		b.Meetings[i], b.Meetings[j] = b.Meetings[j], b.Meetings[i]
	}
	b.Meetings[0].Annotations[0], b.Meetings[0].Annotations[1] = b.Meetings[0].Annotations[1], b.Meetings[0].Annotations[0]

	da, err := a.Digest()
	if err != nil {
		t.Fatalf("digest a: %v", err)
	}
	db, err := b.Digest()
	if err != nil {
		t.Fatalf("digest b: %v", err)
	}
	if da != db {
		t.Fatalf("digest should be stable across ordering: %s != %s", da, db)
	}
	if len(da) != 64 {
		t.Fatalf("digest should be 64 hex chars, got %d", len(da))
	}
}

func TestDigestChangesWithContent(t *testing.T) {
	a := validManifest()
	b := validManifest()
	b.Meetings[0].HeadsetMix.SHA256 = strings.Repeat("f", 64)
	da, _ := a.Digest()
	db, _ := b.Digest()
	if da == db {
		t.Fatal("digest should change when a real hash mutates")
	}
}

func TestValidateDuplicateMeetingID(t *testing.T) {
	m := validManifest()
	m.Meetings[1].ID = m.Meetings[0].ID
	m.Meetings[1].Class = m.Meetings[0].Class
	if err := m.Validate(); err == nil {
		t.Fatal("expected duplicate meeting id rejection")
	}
}

func TestValidateMissingRequiredMeeting(t *testing.T) {
	m := validManifest()
	m.Meetings = m.Meetings[:3]
	wantInvalid(t, m, "expected exactly 4 meetings")
}

func TestValidateWrongClassForKnownMeeting(t *testing.T) {
	m := validManifest()
	for i := range m.Meetings {
		if m.Meetings[i].ID == "ES2002a" {
			m.Meetings[i].Class = ClassNonScenario
		}
	}
	if err := m.Validate(); err == nil {
		t.Fatal("expected class mismatch rejection")
	}
}

func TestValidateUnknownMeetingID(t *testing.T) {
	m := validManifest()
	m.Meetings[0].ID = "XX9999z"
	if err := m.Validate(); err == nil {
		t.Fatal("expected unknown meeting id rejection")
	}
}

func TestValidateNonHTTPSURL(t *testing.T) {
	m := validManifest()
	m.Meetings[0].HeadsetMix.URL = "http://example.test/insecure.wav"
	if err := m.Validate(); err == nil {
		t.Fatal("expected non-https rejection")
	}
}

func TestValidateInvalidSizes(t *testing.T) {
	for _, size := range []int64{0, -1, MaxObjectSizeBytes + 1} {
		m := validManifest()
		m.Meetings[0].HeadsetMix.SizeBytes = size
		if err := m.Validate(); err == nil {
			t.Fatalf("expected size rejection for %d", size)
		}
	}
}

func TestValidateInvalidSHA(t *testing.T) {
	cases := []string{
		strings.Repeat("A", 64), // uppercase
		strings.Repeat("a", 63), // too short
		strings.Repeat("g", 64), // non-hex
		"",
	}
	for _, sha := range cases {
		m := validManifest()
		m.Meetings[0].HeadsetMix.SHA256 = sha
		if err := m.Validate(); err == nil {
			t.Fatalf("expected sha256 rejection for %q", sha)
		}
	}
}

func TestValidateChannelPolicy(t *testing.T) {
	m := validManifest()
	m.Meetings[0].HeadsetMix.ChannelPolicy = "bogus"
	if err := m.Validate(); err == nil {
		t.Fatal("expected unknown channel_policy rejection")
	}

	m2 := validManifest()
	m2.Meetings[0].HeadsetMix.ChannelPolicy = ChannelPolicyExplicit
	m2.Meetings[0].HeadsetMix.SelectChannel = nil
	if err := m2.Validate(); err == nil {
		t.Fatal("expected explicit policy without select_channel to be rejected")
	}

	m3 := validManifest()
	avg := 0
	m3.Meetings[0].HeadsetMix.ChannelPolicy = ChannelPolicyAverage
	m3.Meetings[0].HeadsetMix.SelectChannel = &avg
	if err := m3.Validate(); err == nil {
		t.Fatal("expected average policy with select_channel to be rejected")
	}

	m4 := validManifest()
	oob := 5
	m4.Meetings[0].HeadsetMix.SelectChannel = &oob
	if err := m4.Validate(); err == nil {
		t.Fatal("expected out-of-range select_channel to be rejected")
	}
}

func TestValidateParticipantDuplication(t *testing.T) {
	m := validManifest()
	m.Meetings[0].Annotations[1].ParticipantID = m.Meetings[0].Annotations[0].ParticipantID
	if err := m.Validate(); err == nil {
		t.Fatal("expected duplicate participant rejection")
	}
}

func TestValidateCrossMeetingAnnotation(t *testing.T) {
	m := validManifest()
	// Attach the "wrong" meeting's file name under this meeting's list.
	other := "EN2001a"
	for _, meeting := range m.Meetings {
		if meeting.Class == ClassScenario {
			other = "EN2001a"
			_ = other
		}
	}
	m.Meetings[meetingIndex(t, &m, "ES2002a")].Annotations[0].Member = "words/EN2001a.A.words.xml"
	if err := m.Validate(); err == nil {
		t.Fatal("expected cross-meeting annotation member rejection")
	}
}

func TestValidateAnnotationTraversal(t *testing.T) {
	cases := []string{"../evil.xml", "words/../../evil.xml", "/etc/passwd", "words/ES2002a.A.words.xml/../x"}
	for _, member := range cases {
		m := validManifest()
		m.Meetings[meetingIndex(t, &m, "ES2002a")].Annotations[0].Member = member
		if err := m.Validate(); err == nil {
			t.Fatalf("expected traversal rejection for member %q", member)
		}
	}
}

func TestValidateParticipantIDTraversal(t *testing.T) {
	cases := []string{"../A", "A/B", "..", ""}
	for _, id := range cases {
		m := validManifest()
		m.Meetings[0].Annotations[0].ParticipantID = id
		if err := m.Validate(); err == nil {
			t.Fatalf("expected participant id rejection for %q", id)
		}
	}
}

func TestRecordingsStableOrdering(t *testing.T) {
	m := validManifest() // meetings listed out of order in source
	recs := m.Recordings()
	if len(recs) != 8 {
		t.Fatalf("expected 8 recordings, got %d", len(recs))
	}
	want := []struct {
		id    string
		split Split
	}{
		{"EN2001a-fixed_distant", SplitTuning}, {"EN2001a-headset", SplitTuning},
		{"ES2002a-fixed_distant", SplitTuning}, {"ES2002a-headset", SplitTuning},
		{"EN2002a-fixed_distant", SplitHeldOut}, {"EN2002a-headset", SplitHeldOut},
		{"ES2004a-fixed_distant", SplitHeldOut}, {"ES2004a-headset", SplitHeldOut},
	}
	for i, w := range want {
		if recs[i].ID != w.id || recs[i].Split != w.split {
			t.Fatalf("recording[%d] = %s/%s, want %s/%s", i, recs[i].ID, recs[i].Split, w.id, w.split)
		}
	}
	// Reordering meetings must not change flattened order.
	m2 := validManifest()
	m2.Meetings[0], m2.Meetings[3] = m2.Meetings[3], m2.Meetings[0]
	recs2 := m2.Recordings()
	for i := range recs {
		if recs[i].ID != recs2[i].ID {
			t.Fatalf("flattened order depends on manifest order at %d: %s vs %s", i, recs[i].ID, recs2[i].ID)
		}
	}
	if got := m.RecordingsForSplit(SplitTuning); len(got) != 4 || got[0].MeetingID != "EN2001a" {
		t.Fatalf("tuning recordings: %+v", got)
	}
	if got := m.MeetingIDs(SplitHeldOut); strings.Join(got, ",") != "EN2002a,ES2004a" {
		t.Fatalf("held-out meeting ids: %v", got)
	}
}

func TestValidateExactSplitMembership(t *testing.T) {
	m := validManifest()
	if err := m.Validate(); err != nil {
		t.Fatalf("expected exact membership to validate: %v", err)
	}
}

func TestValidateRejectsUnknownOrEmptySplit(t *testing.T) {
	for _, split := range []Split{"", "validation", "Tuning", "heldout"} {
		m := validManifest()
		m.Meetings[meetingIndex(t, &m, "ES2002a")].Split = split
		wantInvalid(t, m, "unknown split")
	}
}

func TestValidateRejectsSwappedSplitMembership(t *testing.T) {
	// A tuning meeting labelled held_out and vice versa: same four meetings,
	// but a held-out meeting in tuning would leak holdout evidence.
	m := validManifest()
	m.Meetings[meetingIndex(t, &m, "ES2002a")].Split = SplitHeldOut
	m.Meetings[meetingIndex(t, &m, "ES2004a")].Split = SplitTuning
	wantInvalid(t, m, "must be in split")

	m2 := validManifest()
	m2.Meetings[meetingIndex(t, &m2, "EN2001a")].Split = SplitHeldOut
	wantInvalid(t, m2, `meeting "EN2001a" must be in split "tuning"`)
}

func TestValidateRejectsWrongMeetingCount(t *testing.T) {
	m := validManifest()
	m.Meetings = append(m.Meetings, testMeeting(HeldOutSubstituteMeetingID, ClassScenario, SplitHeldOut))
	wantInvalid(t, m, "expected exactly 4 meetings")

	m2 := validManifest()
	m2.Meetings = m2.Meetings[:2]
	wantInvalid(t, m2, "expected exactly 4 meetings")
}

func TestValidateRejectsDuplicatesWithinAndAcrossSplits(t *testing.T) {
	// Same meeting twice in held_out (replacing EN2002a).
	m := validManifest()
	m.Meetings[meetingIndex(t, &m, "EN2002a")] = testMeeting("ES2004a", ClassScenario, SplitHeldOut)
	wantInvalid(t, m, "duplicate meeting id")

	// A tuning meeting repeated in held_out (across splits).
	m2 := validManifest()
	m2.Meetings[meetingIndex(t, &m2, "EN2002a")] = testMeeting("EN2001a", ClassNonScenario, SplitHeldOut)
	wantInvalid(t, m2, "duplicate meeting id")
}

func TestValidateRejectsDuplicateRecordings(t *testing.T) {
	m := validManifest()
	a := meetingIndex(t, &m, "ES2004a")
	b := meetingIndex(t, &m, "ES2002a")
	m.Meetings[a].HeadsetMix.SHA256 = m.Meetings[b].HeadsetMix.SHA256
	wantInvalid(t, m, "duplicate recording")

	m2 := validManifest()
	i := meetingIndex(t, &m2, "ES2004a")
	m2.Meetings[i].FixedDistantMix.SHA256 = m2.Meetings[i].HeadsetMix.SHA256
	wantInvalid(t, m2, "duplicate recording")
}

func TestValidateRejectsMissingOrWrongMicrophone(t *testing.T) {
	// Missing fixed-distant object.
	m := validManifest()
	m.Meetings[meetingIndex(t, &m, "EN2002a")].FixedDistantMix = AudioObject{}
	wantInvalid(t, m, "fixed_distant_mix")

	// Headset slot pointing at the Array1-01 WAV.
	m2 := validManifest()
	i := meetingIndex(t, &m2, "EN2002a")
	m2.Meetings[i].HeadsetMix.URL = "https://example.test/EN2002a/audio/EN2002a.Array1-01.wav"
	wantInvalid(t, m2, "must name \"EN2002a.Mix-Headset.wav\"")
}

func TestValidateRejectsCrossMeetingAudioAndAnnotation(t *testing.T) {
	m := validManifest()
	m.Meetings[meetingIndex(t, &m, "ES2004a")].FixedDistantMix.URL = "https://example.test/ES2002a/audio/ES2002a.Array1-01.wav"
	wantInvalid(t, m, "must name \"ES2004a.Array1-01.wav\"")

	m2 := validManifest()
	m2.Meetings[meetingIndex(t, &m2, "EN2002a")].Annotations[0].Member = "words/EN2001a.A.words.xml"
	wantInvalid(t, m2, "does not belong to meeting")
}

func TestValidateRejectsDuplicateParticipantOrMember(t *testing.T) {
	m := validManifest()
	i := meetingIndex(t, &m, "ES2004a")
	m.Meetings[i].Annotations[1].ParticipantID = m.Meetings[i].Annotations[0].ParticipantID
	wantInvalid(t, m, "duplicate participant")

	m2 := validManifest()
	j := meetingIndex(t, &m2, "ES2004a")
	m2.Meetings[j].Annotations[1].Member = m2.Meetings[j].Annotations[0].Member
	wantInvalid(t, m2, "duplicate annotation member")
}

func TestValidateRejectsIncompatibleClass(t *testing.T) {
	m := validManifest()
	m.Meetings[meetingIndex(t, &m, "EN2002a")].Class = ClassScenario
	wantInvalid(t, m, "actual class")
}

func TestValidateAcceptsPermittedHeldOutReplacements(t *testing.T) {
	for _, replaced := range []string{"ES2004a", "EN2002a"} {
		m := validManifest()
		replaceHeldOut(t, &m, replaced, "participant B word XML has a reversed timed word")
		if err := m.Validate(); err != nil {
			t.Fatalf("replacing %s with IS1009a should validate: %v", replaced, err)
		}
		held := m.MeetingIDs(SplitHeldOut)
		if len(held) != 2 || !contains(held, HeldOutSubstituteMeetingID) || contains(held, replaced) {
			t.Fatalf("replacing %s: held-out = %v", replaced, held)
		}
		// The substitute keeps its actual (scenario) class even when it
		// replaces the non-scenario meeting.
		for _, r := range m.RecordingsForSplit(SplitHeldOut) {
			if r.MeetingID == HeldOutSubstituteMeetingID && r.Class != ClassScenario {
				t.Fatalf("IS1009a relabelled to %s", r.Class)
			}
		}
	}
}

func TestValidateRejectsInvalidReplacementMetadata(t *testing.T) {
	// Absent / blank reason.
	for _, reason := range []string{"", "   \t"} {
		m := validManifest()
		replaceHeldOut(t, &m, "ES2004a", reason)
		wantInvalid(t, m, "annotation_failure_reason")
	}
	// Replacing a tuning meeting.
	m := validManifest()
	replaceHeldOut(t, &m, "ES2004a", "reason")
	m.HeldOutReplacement.ReplacedMeetingID = "ES2002a"
	wantInvalid(t, m, "not a requested held-out meeting")

	// Metadata without IS1009a in the meetings (replaced ID retained).
	m2 := validManifest()
	m2.HeldOutReplacement = &HeldOutReplacement{ReplacedMeetingID: "ES2004a", ReplacementMeetingID: HeldOutSubstituteMeetingID, AnnotationFailureReason: "reason"}
	wantInvalid(t, m2, "ES2004a")

	// IS1009a without metadata.
	m3 := validManifest()
	replaceHeldOut(t, &m3, "ES2004a", "reason")
	m3.HeldOutReplacement = nil
	wantInvalid(t, m3, "IS1009a")

	// Retaining the replaced ID alongside IS1009a (five meetings).
	m4 := validManifest()
	replaceHeldOut(t, &m4, "ES2004a", "reason")
	m4.Meetings = append(m4.Meetings, testMeeting("ES2004a", ClassScenario, SplitHeldOut))
	wantInvalid(t, m4, "expected exactly 4 meetings")

	// Retaining the replaced ID in place of the other held-out meeting.
	m5 := validManifest()
	replaceHeldOut(t, &m5, "ES2004a", "reason")
	m5.Meetings[meetingIndex(t, &m5, "EN2002a")] = testMeeting("ES2004a", ClassScenario, SplitHeldOut)
	wantInvalid(t, m5, "ES2004a")

	// A replacement other than IS1009a.
	m6 := validManifest()
	replaceHeldOut(t, &m6, "ES2004a", "reason")
	m6.HeldOutReplacement.ReplacementMeetingID = "ES2005a"
	wantInvalid(t, m6, "replacement meeting must be")

	// Replacing both held-out meetings: the single permitted substitute
	// cannot supply two distinct held-out meetings.
	m7 := validManifest()
	replaceHeldOut(t, &m7, "ES2004a", "reason")
	m7.Meetings[meetingIndex(t, &m7, "EN2002a")] = testMeeting(HeldOutSubstituteMeetingID, ClassScenario, SplitHeldOut)
	wantInvalid(t, m7, "duplicate meeting id")
}

func TestDigestIncludesSplitAndReplacement(t *testing.T) {
	base := validManifest()
	d0, _ := base.Digest()

	// The split is part of the canonical bytes: mutate it in the canonical
	// encoding directly (Validate would reject a swapped manifest).
	swapped := validManifest()
	swapped.Meetings[meetingIndex(t, &swapped, "ES2002a")].Split = SplitHeldOut
	d1, _ := swapped.Digest()
	if d0 == d1 {
		t.Fatal("digest must change when a meeting's split changes")
	}

	canon, err := base.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(canon), `"split":"held_out"`) || !strings.Contains(string(canon), `"split":"tuning"`) {
		t.Fatalf("canonical bytes omit split: %s", canon)
	}

	r1 := validManifest()
	replaceHeldOut(t, &r1, "ES2004a", "reason one")
	r2 := validManifest()
	replaceHeldOut(t, &r2, "ES2004a", "reason two")
	d2, _ := r1.Digest()
	d3, _ := r2.Digest()
	if d2 == d3 {
		t.Fatal("digest must change when the replacement reason changes")
	}
	if !strings.Contains(mustCanon(t, r1), "reason one") {
		t.Fatal("canonical bytes omit replacement metadata")
	}
}

func mustCanon(t *testing.T, m Manifest) string {
	t.Helper()
	b, err := m.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func TestCellsCoverAllFour(t *testing.T) {
	m := validManifest()
	cells := m.Cells()
	if len(cells) != 4 {
		t.Fatalf("expected 4 cells, got %d", len(cells))
	}
	seen := map[Cell]bool{}
	for _, c := range cells {
		seen[c] = true
	}
	for _, class := range []string{ClassScenario, ClassNonScenario} {
		for _, mic := range []string{MicHeadset, MicFixedDistant} {
			if !seen[Cell{Class: class, Mic: mic}] {
				t.Fatalf("missing cell %+v", Cell{Class: class, Mic: mic})
			}
		}
	}
}

func TestLoadPinnedManifestFile(t *testing.T) {
	// Locates and loads the real committed manifest.json two directories up.
	path := filepath.Join("..", "..", "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest.json: %v", err)
	}
	m, err := Load(data)
	if err != nil {
		t.Fatalf("load committed manifest.json: %v", err)
	}
	if len(m.Recordings()) != 8 {
		t.Fatalf("expected 8 recordings in committed manifest")
	}
	if got := strings.Join(m.MeetingIDs(SplitTuning), ","); got != "EN2001a,ES2002a" {
		t.Fatalf("committed tuning meetings = %s", got)
	}
	if got := len(m.MeetingIDs(SplitHeldOut)); got != 2 {
		t.Fatalf("committed held-out meetings = %d", got)
	}
	digest, err := m.Digest()
	if err != nil || len(digest) != 64 {
		t.Fatalf("digest of committed manifest: %q, err=%v", digest, err)
	}
}

func TestValidateRejectsMissingAnnotations(t *testing.T) {
	m := validManifest()
	m.Meetings[0].Annotations = nil
	if err := m.Validate(); err == nil {
		t.Fatal("expected rejection of meeting with no annotations")
	}
}
