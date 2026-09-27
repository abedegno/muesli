// Package manifest defines and validates the pinned AMI corpus manifest that
// drives the reproducible AMI VAD evaluation (muesli#778). The manifest names
// every source object the evaluator needs -- headset-mix and fixed
// single-distant-microphone WAVs plus the shared participant word-annotation
// archive -- by URL, expected byte size, and lowercase SHA-256, so acquisition
// can verify every byte before it is used.
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
const SchemaVersion = 1

// Meeting classes. The initial manifest pins exactly one meeting of each
// class: ES2002a (scenario) and EN2001a (non-scenario).
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

// requiredMeetings is the hard-coded initial slice: exactly these two
// meeting IDs, each with its required class. This is deliberately not
// data-driven -- the spec pins ES2002a/EN2001a explicitly for this slice.
var requiredMeetings = map[string]string{
	"ES2002a": ClassScenario,
	"EN2001a": ClassNonScenario,
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
	HeadsetMix      AudioObject        `json:"headset_mix"`
	FixedDistantMix AudioObject        `json:"fixed_distant_mix"`
	Annotations     []AnnotationObject `json:"annotations"`
}

// Manifest is the top-level pinned corpus manifest.
type Manifest struct {
	SchemaVersion     int           `json:"schema_version"`
	AnnotationArchive ArchiveObject `json:"annotation_archive"`
	Meetings          []Meeting     `json:"meetings"`
}

// Recording is one flattened meeting/microphone-condition pair: the unit the
// rest of the evaluator (acquire, prepare, evaluate, score) operates on.
type Recording struct {
	ID          string
	MeetingID   string
	Class       string
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
// evaluator relies on: schema version, exactly the required meetings and
// classes, HTTPS URLs, positive bounded sizes, lowercase 64-character
// SHA-256 values, sane channel declarations, same-meeting annotation
// pairing, and path-safe identifiers.
func (m *Manifest) Validate() error {
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf("manifest: unsupported schema_version %d (want %d)", m.SchemaVersion, SchemaVersion)
	}
	if err := validateArchive(m.AnnotationArchive); err != nil {
		return fmt.Errorf("manifest: annotation_archive: %w", err)
	}

	if len(m.Meetings) != len(requiredMeetings) {
		return fmt.Errorf("manifest: expected exactly %d meetings, got %d", len(requiredMeetings), len(m.Meetings))
	}
	seen := make(map[string]bool, len(m.Meetings))
	for _, meeting := range m.Meetings {
		if seen[meeting.ID] {
			return fmt.Errorf("manifest: duplicate meeting id %q", meeting.ID)
		}
		seen[meeting.ID] = true

		wantClass, ok := requiredMeetings[meeting.ID]
		if !ok {
			return fmt.Errorf("manifest: unknown meeting id %q", meeting.ID)
		}
		if meeting.Class != wantClass {
			return fmt.Errorf("manifest: meeting %q must have class %q, got %q", meeting.ID, wantClass, meeting.Class)
		}
		if err := validateSafeID(meeting.ID); err != nil {
			return fmt.Errorf("manifest: meeting %q: %w", meeting.ID, err)
		}

		if err := validateAudioObject(meeting.HeadsetMix); err != nil {
			return fmt.Errorf("manifest: meeting %q headset_mix: %w", meeting.ID, err)
		}
		if err := validateAudioObject(meeting.FixedDistantMix); err != nil {
			return fmt.Errorf("manifest: meeting %q fixed_distant_mix: %w", meeting.ID, err)
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
		}
	}
	if len(m.Meetings) != len(seen) {
		return fmt.Errorf("manifest: meeting id bookkeeping mismatch")
	}
	for id := range requiredMeetings {
		if !seen[id] {
			return fmt.Errorf("manifest: missing required meeting %q", id)
		}
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

// Recordings flattens the manifest into its four (meeting, microphone
// condition) recordings, stably ordered by meeting ID then microphone
// condition.
func (m *Manifest) Recordings() []Recording {
	var out []Recording
	for _, meeting := range m.sortedMeetings() {
		out = append(out,
			Recording{
				ID:          meeting.ID + "-" + MicHeadset,
				MeetingID:   meeting.ID,
				Class:       meeting.Class,
				Mic:         MicHeadset,
				Audio:       meeting.HeadsetMix,
				Annotations: meeting.Annotations,
			},
			Recording{
				ID:          meeting.ID + "-" + MicFixedDistant,
				MeetingID:   meeting.ID,
				Class:       meeting.Class,
				Mic:         MicFixedDistant,
				Audio:       meeting.FixedDistantMix,
				Annotations: meeting.Annotations,
			},
		)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].MeetingID != out[j].MeetingID {
			return out[i].MeetingID < out[j].MeetingID
		}
		return out[i].Mic < out[j].Mic
	})
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
