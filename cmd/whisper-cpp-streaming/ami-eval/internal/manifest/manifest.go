// Package manifest defines and validates the pinned AMI corpus manifest that
// drives the reproducible AMI VAD evaluation (muesli#778, muesli#782). The
// manifest names every source object the evaluator needs -- headset-mix and
// fixed single-distant-microphone WAVs plus the shared participant
// word-annotation archive -- by URL, expected byte size, and lowercase
// SHA-256, so acquisition can verify every byte before it is used.
//
// Schema 2 assigns every meeting to exactly one evaluation split: the tuning
// meetings select a fixed energy threshold, and the separate held-out
// meetings validate that frozen selection. The split is part of the
// canonical bytes and therefore of the manifest digest.
package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"
)

// SchemaVersion is the manifest schema this package understands. Bumping it
// is a breaking change to the manifest shape.
const SchemaVersion = 2

// Meeting classes. Each split pins one meeting of each class unless a
// recorded held-out replacement preserves the substitute's actual class.
const (
	ClassScenario    = "scenario"
	ClassNonScenario = "non_scenario"
)

// Microphone conditions. Every meeting supplies exactly one recording for
// each, giving one recording per meeting-class/microphone-condition cell.
const (
	MicHeadset      = "headset"
	MicFixedDistant = "fixed_distant"
)

// Channel policies for an AudioObject.
const (
	ChannelPolicyExplicit = "explicit"
	ChannelPolicyAverage  = "average"
)

// Split is a meeting-level evaluation partition.
type Split string

// The two evaluation splits. Tuning meetings are the only evidence the
// threshold selector may see; held-out meetings only validate the frozen
// selection.
const (
	SplitTuning  Split = "tuning"
	SplitHeldOut Split = "held_out"
)

// Splits returns both splits in their fixed processing order.
func Splits() []Split { return []Split{SplitTuning, SplitHeldOut} }

// Valid reports whether s is one of the two known splits.
func (s Split) Valid() bool { return s == SplitTuning || s == SplitHeldOut }

// knownMeetingClasses is every meeting ID this manifest may name, with its
// actual AMI meeting class. It is deliberately not data-driven: the spec
// pins these identities explicitly (muesli#782).
var knownMeetingClasses = map[string]string{
	"ES2002a": ClassScenario,
	"EN2001a": ClassNonScenario,
	"ES2004a": ClassScenario,
	"EN2002a": ClassNonScenario,
	"IS1009a": ClassScenario,
}

// tuningMeetings is the exact tuning split membership.
var tuningMeetings = []string{"EN2001a", "ES2002a"}

// requestedHeldOutMeetings is the held-out membership absent a recorded
// replacement.
var requestedHeldOutMeetings = []string{"EN2002a", "ES2004a"}

// HeldOutSubstituteMeetingID is the single meeting permitted to replace one
// requested held-out meeting, and only when that meeting's annotations are
// unusable under the existing preparation rules.
const HeldOutSubstituteMeetingID = "IS1009a"

// Required microphone audio basename suffixes. A meeting's audio URLs must
// name that meeting's own headset-mix and Array1-01 WAVs.
const (
	HeadsetAudioSuffix      = ".Mix-Headset.wav"
	FixedDistantAudioSuffix = ".Array1-01.wav"
)

// HeldOutReplacement records the single permitted held-out substitution:
// which requested held-out meeting was replaced by IS1009a and the concrete
// annotation failure that justified it. It is decided before any detector
// metric is inspected.
type HeldOutReplacement struct {
	ReplacedMeetingID       string `json:"replaced_meeting_id"`
	ReplacementMeetingID    string `json:"replacement_meeting_id"`
	AnnotationFailureReason string `json:"annotation_failure_reason"`
}

// MaxObjectSizeBytes bounds any single pinned object's declared size. It is
// a generous ceiling (10 GiB) meant to catch manifest corruption or a
// obviously-wrong pin, not to constrain real AMI object sizes.
const MaxObjectSizeBytes = 10 << 30

// AudioObject pins one downloadable WAV source and how to interpret its
// channels.
type AudioObject struct {
	URL           string `json:"url"`
	SizeBytes     int64  `json:"size_bytes"`
	SHA256        string `json:"sha256"`
	Channels      int    `json:"channels"`
	ChannelPolicy string `json:"channel_policy"`
	SelectChannel *int   `json:"select_channel,omitempty"`
}

// ArchiveObject pins one downloadable archive that contains further
// content-addressed members (used for the shared AMI annotation archive).
type ArchiveObject struct {
	URL       string `json:"url"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
}

// AnnotationObject pins one participant's word-annotation XML document as a
// member of the manifest's AnnotationArchive, identified by its own
// (post-extraction) size and SHA-256.
type AnnotationObject struct {
	ParticipantID string `json:"participant_id"`
	Member        string `json:"member"`
	SizeBytes     int64  `json:"size_bytes"`
	SHA256        string `json:"sha256"`
}

// Meeting pins one AMI meeting's headset mix, fixed single distant
// microphone, and participant annotations.
type Meeting struct {
	ID              string             `json:"id"`
	Class           string             `json:"class"`
	Split           Split              `json:"split"`
	HeadsetMix      AudioObject        `json:"headset_mix"`
	FixedDistantMix AudioObject        `json:"fixed_distant_mix"`
	Annotations     []AnnotationObject `json:"annotations"`
}

// Manifest is the top-level pinned corpus manifest.
type Manifest struct {
	SchemaVersion      int                 `json:"schema_version"`
	AnnotationArchive  ArchiveObject       `json:"annotation_archive"`
	HeldOutReplacement *HeldOutReplacement `json:"held_out_replacement,omitempty"`
	Meetings           []Meeting           `json:"meetings"`
}

// Recording is one flattened meeting/microphone-condition pair: the unit the
// rest of the evaluator (acquire, prepare, evaluate, score) operates on.
type Recording struct {
	ID          string
	MeetingID   string
	Class       string
	Split       Split
	Mic         string
	Audio       AudioObject
	Annotations []AnnotationObject
}

// Cell is one meeting-class/microphone-condition combination.
type Cell struct {
	Class string
	Mic   string
}

// Load reads, strictly parses, and validates a manifest file.
func Load(data []byte) (*Manifest, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("manifest: parse: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// Validate checks every structural and content invariant the rest of the
// evaluator relies on: schema version, exact split membership (the fixed
// tuning pair plus the requested or one recorded-replacement held-out
// pair), each meeting's actual class, HTTPS URLs naming the meeting's own
// microphone WAVs, positive bounded sizes, lowercase 64-character SHA-256
// values, sane channel declarations, same-meeting annotation pairing, no
// duplicate recordings or members, and path-safe identifiers.
func (m *Manifest) Validate() error {
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf("manifest: unsupported schema_version %d (want %d)", m.SchemaVersion, SchemaVersion)
	}
	if err := validateArchive(m.AnnotationArchive); err != nil {
		return fmt.Errorf("manifest: annotation_archive: %w", err)
	}

	wantHeldOut, err := m.expectedHeldOut()
	if err != nil {
		return err
	}
	want := map[string]Split{}
	for _, id := range tuningMeetings {
		want[id] = SplitTuning
	}
	for _, id := range wantHeldOut {
		want[id] = SplitHeldOut
	}

	if len(m.Meetings) != len(want) {
		return fmt.Errorf("manifest: expected exactly %d meetings (2 tuning, 2 held_out), got %d", len(want), len(m.Meetings))
	}
	// Duplicate meeting IDs are reported first, whether within one split or
	// across both, so the diagnostic names the real defect.
	ids := make(map[string]Split, len(m.Meetings))
	for _, meeting := range m.Meetings {
		if prev, dup := ids[meeting.ID]; dup {
			return fmt.Errorf("manifest: duplicate meeting id %q (splits %q and %q)", meeting.ID, prev, meeting.Split)
		}
		ids[meeting.ID] = meeting.Split
	}
	seen := make(map[string]bool, len(m.Meetings))
	audioURLs := map[string]string{}
	audioHashes := map[string]string{}
	members := map[string]string{}
	for _, meeting := range m.Meetings {
		if err := validateSafeID(meeting.ID); err != nil {
			return fmt.Errorf("manifest: meeting %q: %w", meeting.ID, err)
		}
		if seen[meeting.ID] {
			return fmt.Errorf("manifest: duplicate meeting id %q", meeting.ID)
		}
		seen[meeting.ID] = true

		if !meeting.Split.Valid() {
			return fmt.Errorf("manifest: meeting %q has unknown split %q (want %q or %q)", meeting.ID, meeting.Split, SplitTuning, SplitHeldOut)
		}
		wantSplit, ok := want[meeting.ID]
		if !ok {
			if _, known := knownMeetingClasses[meeting.ID]; known {
				return fmt.Errorf("manifest: meeting %q is not a member of any split under this manifest's replacement metadata", meeting.ID)
			}
			return fmt.Errorf("manifest: unknown meeting id %q", meeting.ID)
		}
		if meeting.Split != wantSplit {
			return fmt.Errorf("manifest: meeting %q must be in split %q, got %q", meeting.ID, wantSplit, meeting.Split)
		}
		if wantClass := knownMeetingClasses[meeting.ID]; meeting.Class != wantClass {
			return fmt.Errorf("manifest: meeting %q must have its actual class %q, got %q", meeting.ID, wantClass, meeting.Class)
		}

		for _, mic := range []struct {
			name   string
			obj    AudioObject
			suffix string
		}{
			{"headset_mix", meeting.HeadsetMix, HeadsetAudioSuffix},
			{"fixed_distant_mix", meeting.FixedDistantMix, FixedDistantAudioSuffix},
		} {
			if err := validateAudioObject(mic.obj); err != nil {
				return fmt.Errorf("manifest: meeting %q %s: %w", meeting.ID, mic.name, err)
			}
			if err := validateAudioIdentity(mic.obj.URL, meeting.ID, mic.suffix); err != nil {
				return fmt.Errorf("manifest: meeting %q %s: %w", meeting.ID, mic.name, err)
			}
			where := meeting.ID + " " + mic.name
			if prev, dup := audioURLs[mic.obj.URL]; dup {
				return fmt.Errorf("manifest: duplicate recording: %s audio url is also %s", where, prev)
			}
			audioURLs[mic.obj.URL] = where
			if prev, dup := audioHashes[mic.obj.SHA256]; dup {
				return fmt.Errorf("manifest: duplicate recording: %s audio sha256 is also %s", where, prev)
			}
			audioHashes[mic.obj.SHA256] = where
		}

		if len(meeting.Annotations) == 0 {
			return fmt.Errorf("manifest: meeting %q has no annotations", meeting.ID)
		}
		participants := make(map[string]bool, len(meeting.Annotations))
		for _, ann := range meeting.Annotations {
			if err := validateSafeID(ann.ParticipantID); err != nil {
				return fmt.Errorf("manifest: meeting %q annotation participant %q: %w", meeting.ID, ann.ParticipantID, err)
			}
			if participants[ann.ParticipantID] {
				return fmt.Errorf("manifest: meeting %q duplicate participant %q", meeting.ID, ann.ParticipantID)
			}
			participants[ann.ParticipantID] = true

			if err := validateAnnotationObject(ann, meeting.ID); err != nil {
				return fmt.Errorf("manifest: meeting %q annotation %q: %w", meeting.ID, ann.ParticipantID, err)
			}
			where := meeting.ID + " participant " + ann.ParticipantID
			if prev, dup := members[ann.Member]; dup {
				return fmt.Errorf("manifest: duplicate annotation member %q (%s and %s)", ann.Member, prev, where)
			}
			members[ann.Member] = where
		}
	}
	for id, split := range want {
		if !seen[id] {
			return fmt.Errorf("manifest: missing required %s meeting %q", split, id)
		}
	}
	return nil
}

// expectedHeldOut returns the held-out membership implied by the optional
// replacement metadata, validating that metadata.
func (m *Manifest) expectedHeldOut() ([]string, error) {
	r := m.HeldOutReplacement
	if r == nil {
		return append([]string(nil), requestedHeldOutMeetings...), nil
	}
	if r.ReplacementMeetingID != HeldOutSubstituteMeetingID {
		return nil, fmt.Errorf("manifest: held_out_replacement: replacement meeting must be %q, got %q", HeldOutSubstituteMeetingID, r.ReplacementMeetingID)
	}
	if strings.TrimSpace(r.AnnotationFailureReason) == "" {
		return nil, fmt.Errorf("manifest: held_out_replacement: a concrete annotation_failure_reason is required")
	}
	var out []string
	replaced := false
	for _, id := range requestedHeldOutMeetings {
		if id == r.ReplacedMeetingID {
			replaced = true
			out = append(out, HeldOutSubstituteMeetingID)
			continue
		}
		out = append(out, id)
	}
	if !replaced {
		return nil, fmt.Errorf("manifest: held_out_replacement: replaced meeting %q is not a requested held-out meeting (want one of %v)", r.ReplacedMeetingID, requestedHeldOutMeetings)
	}
	return out, nil
}

// validateAudioIdentity requires the audio URL's basename to be exactly
// <meetingID><suffix>, so one meeting can never borrow another meeting's
// (or the other microphone's) WAV.
func validateAudioIdentity(raw, meetingID, suffix string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid url %q: %w", raw, err)
	}
	if base := path.Base(u.Path); base != meetingID+suffix {
		return fmt.Errorf("audio url %q must name %q", raw, meetingID+suffix)
	}
	return nil
}

func validateArchive(a ArchiveObject) error {
	if err := validateHTTPSURL(a.URL); err != nil {
		return err
	}
	if err := validateSize(a.SizeBytes); err != nil {
		return err
	}
	return validateSHA256(a.SHA256)
}

func validateAudioObject(a AudioObject) error {
	if err := validateHTTPSURL(a.URL); err != nil {
		return err
	}
	if err := validateSize(a.SizeBytes); err != nil {
		return err
	}
	if err := validateSHA256(a.SHA256); err != nil {
		return err
	}
	if a.Channels < 1 {
		return fmt.Errorf("channels must be at least 1, got %d", a.Channels)
	}
	switch a.ChannelPolicy {
	case ChannelPolicyExplicit:
		if a.SelectChannel == nil {
			return fmt.Errorf("channel_policy %q requires select_channel", ChannelPolicyExplicit)
		}
		if *a.SelectChannel < 0 || *a.SelectChannel >= a.Channels {
			return fmt.Errorf("select_channel %d out of range [0,%d)", *a.SelectChannel, a.Channels)
		}
	case ChannelPolicyAverage:
		if a.SelectChannel != nil {
			return fmt.Errorf("channel_policy %q must not set select_channel", ChannelPolicyAverage)
		}
	default:
		return fmt.Errorf("unknown channel_policy %q", a.ChannelPolicy)
	}
	return nil
}

func validateAnnotationObject(a AnnotationObject, meetingID string) error {
	if err := validateSize(a.SizeBytes); err != nil {
		return err
	}
	if err := validateSHA256(a.SHA256); err != nil {
		return err
	}
	if a.Member == "" {
		return fmt.Errorf("member path must not be empty")
	}
	cleaned := path.Clean(a.Member)
	if cleaned != a.Member || strings.HasPrefix(cleaned, "/") || strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, "..") {
		return fmt.Errorf("member path %q is not a clean, contained relative path", a.Member)
	}
	// Same-meeting audio/annotation pairing: the member's filename must be
	// prefixed with this meeting's ID, so a manifest cannot accidentally
	// attach one meeting's annotation to another meeting's cell.
	base := path.Base(cleaned)
	if !strings.HasPrefix(base, meetingID+".") {
		return fmt.Errorf("member %q does not belong to meeting %q", a.Member, meetingID)
	}
	return nil
}

func validateHTTPSURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid url %q: %w", raw, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("url %q must use https", raw)
	}
	if u.Host == "" {
		return fmt.Errorf("url %q must have a host", raw)
	}
	return nil
}

func validateSize(size int64) error {
	if size <= 0 {
		return fmt.Errorf("size_bytes must be positive, got %d", size)
	}
	if size > MaxObjectSizeBytes {
		return fmt.Errorf("size_bytes %d exceeds manifest ceiling %d", size, MaxObjectSizeBytes)
	}
	return nil
}

func validateSHA256(s string) error {
	if len(s) != 64 {
		return fmt.Errorf("sha256 %q must be 64 hex characters", s)
	}
	for _, r := range s {
		isLowerHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
		if !isLowerHex {
			return fmt.Errorf("sha256 %q must be lowercase hex", s)
		}
	}
	return nil
}

func validateSafeID(id string) error {
	if id == "" {
		return fmt.Errorf("id must not be empty")
	}
	if strings.ContainsAny(id, "/\\") || strings.Contains(id, "..") {
		return fmt.Errorf("id %q must not contain path separators or '..'", id)
	}
	first := id[0]
	isAlnum := (first >= 'A' && first <= 'Z') || (first >= 'a' && first <= 'z') || (first >= '0' && first <= '9')
	if !isAlnum {
		return fmt.Errorf("id %q must start with a letter or digit", id)
	}
	for _, r := range id {
		ok := (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.'
		if !ok {
			return fmt.Errorf("id %q contains an unsafe character %q", id, r)
		}
	}
	return nil
}

// sortedMeetings returns a copy of m.Meetings sorted by ID, with each
// meeting's annotations sorted by participant ID. This is the ordering used
// both for CanonicalBytes and for Recordings, so cache keys and report rows
// never depend on manifest file ordering.
func (m *Manifest) sortedMeetings() []Meeting {
	out := make([]Meeting, len(m.Meetings))
	copy(out, m.Meetings)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	for i := range out {
		anns := make([]AnnotationObject, len(out[i].Annotations))
		copy(anns, out[i].Annotations)
		sort.Slice(anns, func(a, b int) bool { return anns[a].ParticipantID < anns[b].ParticipantID })
		out[i].Annotations = anns
	}
	return out
}

// Recordings flattens the manifest into its eight (meeting, microphone
// condition) recordings, stably ordered by split (tuning first), then
// meeting ID, then microphone condition.
func (m *Manifest) Recordings() []Recording {
	var out []Recording
	for _, meeting := range m.sortedMeetings() {
		out = append(out,
			Recording{
				ID:          meeting.ID + "-" + MicHeadset,
				MeetingID:   meeting.ID,
				Class:       meeting.Class,
				Split:       meeting.Split,
				Mic:         MicHeadset,
				Audio:       meeting.HeadsetMix,
				Annotations: meeting.Annotations,
			},
			Recording{
				ID:          meeting.ID + "-" + MicFixedDistant,
				MeetingID:   meeting.ID,
				Class:       meeting.Class,
				Split:       meeting.Split,
				Mic:         MicFixedDistant,
				Audio:       meeting.FixedDistantMix,
				Annotations: meeting.Annotations,
			},
		)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Split != out[j].Split {
			return splitRank(out[i].Split) < splitRank(out[j].Split)
		}
		if out[i].MeetingID != out[j].MeetingID {
			return out[i].MeetingID < out[j].MeetingID
		}
		return out[i].Mic < out[j].Mic
	})
	return out
}

func splitRank(s Split) int {
	if s == SplitTuning {
		return 0
	}
	return 1
}

// RecordingsForSplit returns the flattened recordings of one split, in
// Recordings order.
func (m *Manifest) RecordingsForSplit(s Split) []Recording {
	var out []Recording
	for _, r := range m.Recordings() {
		if r.Split == s {
			out = append(out, r)
		}
	}
	return out
}

// MeetingIDs returns one split's meeting IDs, sorted.
func (m *Manifest) MeetingIDs(s Split) []string {
	var out []string
	for _, meeting := range m.sortedMeetings() {
		if meeting.Split == s {
			out = append(out, meeting.ID)
		}
	}
	return out
}

// Cells returns the four meeting-class/microphone-condition cells in stable
// order.
func (m *Manifest) Cells() []Cell {
	classes := []string{ClassScenario, ClassNonScenario}
	mics := []string{MicHeadset, MicFixedDistant}
	var cells []Cell
	for _, c := range classes {
		for _, mic := range mics {
			cells = append(cells, Cell{Class: c, Mic: mic})
		}
	}
	return cells
}

// CanonicalBytes returns a stable, deterministic JSON encoding of the
// manifest (sorted meetings and annotations), suitable for hashing into a
// Digest that does not depend on incidental file ordering.
func (m *Manifest) CanonicalBytes() ([]byte, error) {
	canon := Manifest{
		SchemaVersion:     m.SchemaVersion,
		AnnotationArchive: m.AnnotationArchive,
		Meetings:          m.sortedMeetings(),
	}
	if m.HeldOutReplacement != nil {
		r := *m.HeldOutReplacement
		canon.HeldOutReplacement = &r
	}
	return json.Marshal(canon)
}

// Digest returns the lowercase hex SHA-256 of CanonicalBytes. It is the
// manifest-digest component of every cache path and report.
func (m *Manifest) Digest() (string, error) {
	b, err := m.CanonicalBytes()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
