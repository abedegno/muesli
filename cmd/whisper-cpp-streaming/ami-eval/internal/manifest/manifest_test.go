package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validManifest() Manifest {
	sel0 := 0
	return Manifest{
		SchemaVersion: SchemaVersion,
		AnnotationArchive: ArchiveObject{
			URL:       "https://example.test/ami/ami_public_manual_1.6.2.zip",
			SizeBytes: 22887865,
			SHA256:    strings.Repeat("a", 64),
		},
		Meetings: []Meeting{
			{
				ID:    "EN2001a",
				Class: ClassNonScenario,
				HeadsetMix: AudioObject{
					URL: "https://example.test/EN2001a.Mix-Headset.wav", SizeBytes: 168007726,
					SHA256: strings.Repeat("b", 64), Channels: 1, ChannelPolicy: ChannelPolicyExplicit, SelectChannel: &sel0,
				},
				FixedDistantMix: AudioObject{
					URL: "https://example.test/EN2001a.Array1-01.wav", SizeBytes: 168008068,
					SHA256: strings.Repeat("c", 64), Channels: 1, ChannelPolicy: ChannelPolicyExplicit, SelectChannel: &sel0,
				},
				Annotations: []AnnotationObject{
					{ParticipantID: "A", Member: "words/EN2001a.A.words.xml", SizeBytes: 87802, SHA256: strings.Repeat("d", 64)},
					{ParticipantID: "B", Member: "words/EN2001a.B.words.xml", SizeBytes: 167929, SHA256: strings.Repeat("e", 64)},
				},
			},
			{
				ID:    "ES2002a",
				Class: ClassScenario,
				HeadsetMix: AudioObject{
					URL: "https://example.test/ES2002a.Mix-Headset.wav", SizeBytes: 40724524,
					SHA256: strings.Repeat("1", 64), Channels: 1, ChannelPolicy: ChannelPolicyExplicit, SelectChannel: &sel0,
				},
				FixedDistantMix: AudioObject{
					URL: "https://example.test/ES2002a.Array1-01.wav", SizeBytes: 40727594,
					SHA256: strings.Repeat("2", 64), Channels: 1, ChannelPolicy: ChannelPolicyExplicit, SelectChannel: &sel0,
				},
				Annotations: []AnnotationObject{
					{ParticipantID: "A", Member: "words/ES2002a.A.words.xml", SizeBytes: 25103, SHA256: strings.Repeat("3", 64)},
					{ParticipantID: "B", Member: "words/ES2002a.B.words.xml", SizeBytes: 130553, SHA256: strings.Repeat("4", 64)},
				},
			},
		},
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
	raw := `{"schema_version":1,"bogus_field":true,"annotation_archive":{},"meetings":[]}`
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
	b.Meetings[0], b.Meetings[1] = b.Meetings[1], b.Meetings[0]
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
	m.Meetings = m.Meetings[:1]
	if err := m.Validate(); err == nil {
		t.Fatal("expected missing-meeting rejection")
	}
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
	m.Meetings[1].Annotations[0].Member = "words/EN2001a.A.words.xml"
	if err := m.Validate(); err == nil {
		t.Fatal("expected cross-meeting annotation member rejection")
	}
}

func TestValidateAnnotationTraversal(t *testing.T) {
	cases := []string{"../evil.xml", "words/../../evil.xml", "/etc/passwd", "words/ES2002a.A.words.xml/../x"}
	for _, member := range cases {
		m := validManifest()
		m.Meetings[1].Annotations[0].Member = member
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
	m := validManifest() // EN2001a listed before ES2002a in source order
	recs := m.Recordings()
	if len(recs) != 4 {
		t.Fatalf("expected 4 recordings, got %d", len(recs))
	}
	wantIDs := []string{"EN2001a-fixed_distant", "EN2001a-headset", "ES2002a-fixed_distant", "ES2002a-headset"}
	for i, id := range wantIDs {
		if recs[i].ID != id {
			t.Fatalf("recording[%d] = %q, want %q (order: %v)", i, recs[i].ID, id, recs)
		}
	}
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
	if len(m.Recordings()) != 4 {
		t.Fatalf("expected 4 recordings in committed manifest")
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
