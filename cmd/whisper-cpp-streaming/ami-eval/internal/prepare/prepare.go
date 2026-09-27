// Package prepare turns one acquired AMI recording into the canonical
// prepared form the evaluator consumes: mono 16kHz PCM audio plus merged
// reference speech intervals and provenance, written atomically into the
// cache and reused across runs whenever their schema and source hashes
// still match (muesli#778).
package prepare

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/acquire"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/annotation"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/audio"
)

// SchemaVersion is this package's interpretation version: bumped whenever
// audio selection or provenance shape changes in a way that must
// invalidate previously prepared data even under an unchanged manifest
// digest.
const SchemaVersion = 1

// SampleRate is the canonical evaluation sample rate. Inputs must already
// be at this rate -- audio.Decode enforces it, no resampling is performed.
const SampleRate = 16000

// Provenance records exactly what produced audio.wav and reference.json,
// so a later run can decide whether to trust and reuse them.
type Provenance struct {
	SchemaVersion          int               `json:"schema_version"`
	RecordingID            string            `json:"recording_id"`
	MeetingID              string            `json:"meeting_id"`
	Class                  string            `json:"class"`
	Mic                    string            `json:"mic"`
	AudioSourceSHA256      string            `json:"audio_source_sha256"`
	AnnotationSourceSHA256 map[string]string `json:"annotation_source_sha256"`
	ChannelPolicy          string            `json:"channel_policy"`
	SelectChannel          *int              `json:"select_channel,omitempty"`
	SampleRate             int               `json:"sample_rate"`
	FrameCount             int64             `json:"frame_count"`
}

// Prepared is one recording's canonical prepared data, either freshly
// computed or reused from a verified cache entry.
type Prepared struct {
	Dir        string
	Audio      []float32
	Reference  annotation.Reference
	Provenance Provenance
}

// Prepare produces (or reuses) the canonical prepared audio, reference, and
// provenance for one acquired recording under manifestDigest.
func Prepare(cache acquire.Cache, manifestDigest string, acq acquire.AcquiredRecording) (Prepared, error) {
	dir := cache.PreparedDir(manifestDigest, acq.Recording.ID)

	wantProvenance := buildProvenance(acq)
	if reused, ok, err := tryReuse(dir, wantProvenance); err != nil {
		return Prepared{}, err
	} else if ok {
		return reused, nil
	}

	audioFile, err := os.Open(acq.AudioPath)
	if err != nil {
		return Prepared{}, fmt.Errorf("prepare: open audio: %w", err)
	}
	decoded, err := audio.Decode(audioFile, SampleRate)
	audioFile.Close()
	if err != nil {
		return Prepared{}, fmt.Errorf("prepare: decode audio for %s: %w", acq.Recording.ID, err)
	}
	selectChannel := 0
	if acq.Recording.Audio.SelectChannel != nil {
		selectChannel = *acq.Recording.Audio.SelectChannel
	}
	samples, err := audio.Select(decoded, acq.Recording.Audio.ChannelPolicy, selectChannel)
	if err != nil {
		return Prepared{}, fmt.Errorf("prepare: select channel for %s: %w", acq.Recording.ID, err)
	}

	participantIDs := make([]string, 0, len(acq.Recording.Annotations))
	for _, ann := range acq.Recording.Annotations {
		participantIDs = append(participantIDs, ann.ParticipantID)
	}
	sort.Strings(participantIDs)

	var participants []annotation.ParticipantIntervals
	sourceSHA := make(map[string]string, len(acq.Recording.Annotations))
	for _, ann := range acq.Recording.Annotations {
		sourceSHA[ann.ParticipantID] = ann.SHA256
	}
	for _, id := range participantIDs {
		path, ok := acq.AnnotationPaths[id]
		if !ok {
			return Prepared{}, fmt.Errorf("prepare: recording %s: no acquired annotation for participant %s", acq.Recording.ID, id)
		}
		f, err := os.Open(path)
		if err != nil {
			return Prepared{}, fmt.Errorf("prepare: open annotation for participant %s: %w", id, err)
		}
		raw, _, err := annotation.ParseParticipant(f)
		f.Close()
		if err != nil {
			return Prepared{}, fmt.Errorf("prepare: parse annotation for participant %s: %w", id, err)
		}
		participants = append(participants, annotation.ParticipantIntervals{ParticipantID: id, Raw: raw})
	}

	ref, err := annotation.BuildReference(participants, sourceSHA, int64(len(samples)), SampleRate)
	if err != nil {
		return Prepared{}, fmt.Errorf("prepare: build reference for %s: %w", acq.Recording.ID, err)
	}

	wantProvenance.FrameCount = int64(len(samples))
	prepared := Prepared{Dir: dir, Audio: samples, Reference: ref, Provenance: wantProvenance}
	if err := promote(dir, prepared); err != nil {
		return Prepared{}, err
	}
	prepared.Dir = dir
	return prepared, nil
}

func buildProvenance(acq acquire.AcquiredRecording) Provenance {
	sourceSHA := make(map[string]string, len(acq.Recording.Annotations))
	for _, ann := range acq.Recording.Annotations {
		sourceSHA[ann.ParticipantID] = ann.SHA256
	}
	var selectChannel *int
	if acq.Recording.Audio.SelectChannel != nil {
		v := *acq.Recording.Audio.SelectChannel
		selectChannel = &v
	}
	return Provenance{
		SchemaVersion:          SchemaVersion,
		RecordingID:            acq.Recording.ID,
		MeetingID:              acq.Recording.MeetingID,
		Class:                  acq.Recording.Class,
		Mic:                    acq.Recording.Mic,
		AudioSourceSHA256:      acq.Recording.Audio.SHA256,
		AnnotationSourceSHA256: sourceSHA,
		ChannelPolicy:          acq.Recording.Audio.ChannelPolicy,
		SelectChannel:          selectChannel,
		SampleRate:             SampleRate,
	}
}

// tryReuse loads an existing prepared directory and reuses it only if its
// provenance's schema version and every source hash exactly match what
// this run would produce.
func tryReuse(dir string, want Provenance) (Prepared, bool, error) {
	provPath := filepath.Join(dir, "provenance.json")
	provBytes, err := os.ReadFile(provPath)
	if err != nil {
		return Prepared{}, false, nil // nothing to reuse
	}
	var got Provenance
	if err := json.Unmarshal(provBytes, &got); err != nil {
		return Prepared{}, false, nil // corrupt/foreign; recompute
	}
	if got.SchemaVersion != want.SchemaVersion ||
		got.AudioSourceSHA256 != want.AudioSourceSHA256 ||
		got.ChannelPolicy != want.ChannelPolicy ||
		!equalIntPtr(got.SelectChannel, want.SelectChannel) ||
		got.SampleRate != want.SampleRate ||
		!equalStringMaps(got.AnnotationSourceSHA256, want.AnnotationSourceSHA256) {
		return Prepared{}, false, nil
	}

	refPath := filepath.Join(dir, "reference.json")
	refBytes, err := os.ReadFile(refPath)
	if err != nil {
		return Prepared{}, false, nil
	}
	var ref annotation.Reference
	if err := json.Unmarshal(refBytes, &ref); err != nil || ref.SchemaVersion != annotation.SchemaVersion {
		return Prepared{}, false, nil
	}

	audioPath := filepath.Join(dir, "audio.wav")
	f, err := os.Open(audioPath)
	if err != nil {
		return Prepared{}, false, nil
	}
	decoded, err := audio.Decode(f, SampleRate)
	f.Close()
	if err != nil {
		return Prepared{}, false, nil
	}
	samples, err := audio.Select(decoded, audio.ChannelPolicyExplicit, 0)
	if err != nil {
		return Prepared{}, false, nil
	}
	return Prepared{Dir: dir, Audio: samples, Reference: ref, Provenance: got}, true, nil
}

func equalIntPtr(a, b *int) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	return a == nil || *a == *b
}

func equalStringMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// promote writes audio.wav, reference.json, and provenance.json into a
// sibling temporary directory, then swaps it into place only once every
// file has been written successfully -- a failure at any point during
// computation or writing leaves prior good prepared data untouched.
func promote(dir string, p Prepared) error {
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("prepare: create parent dir: %w", err)
	}
	tmpDir, err := os.MkdirTemp(parent, filepath.Base(dir)+".tmp-*")
	if err != nil {
		return fmt.Errorf("prepare: create temp dir: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			os.RemoveAll(tmpDir)
		}
	}()

	audioFile, err := os.Create(filepath.Join(tmpDir, "audio.wav"))
	if err != nil {
		return fmt.Errorf("prepare: create audio.wav: %w", err)
	}
	if err := audio.Encode(audioFile, p.Audio, SampleRate); err != nil {
		audioFile.Close()
		return fmt.Errorf("prepare: encode audio.wav: %w", err)
	}
	if err := audioFile.Sync(); err != nil {
		audioFile.Close()
		return fmt.Errorf("prepare: sync audio.wav: %w", err)
	}
	if err := audioFile.Close(); err != nil {
		return fmt.Errorf("prepare: close audio.wav: %w", err)
	}

	if err := writeJSON(filepath.Join(tmpDir, "reference.json"), p.Reference); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(tmpDir, "provenance.json"), p.Provenance); err != nil {
		return err
	}

	oldAside := ""
	if _, err := os.Stat(dir); err == nil {
		oldAside = dir + fmt.Sprintf(".old-%d", time.Now().UnixNano())
		if err := os.Rename(dir, oldAside); err != nil {
			return fmt.Errorf("prepare: move aside previous prepared dir: %w", err)
		}
	}
	if err := os.Rename(tmpDir, dir); err != nil {
		// Best-effort restore of the previous good directory.
		if oldAside != "" {
			os.Rename(oldAside, dir)
		}
		return fmt.Errorf("prepare: promote prepared dir: %w", err)
	}
	cleanup = false
	if oldAside != "" {
		os.RemoveAll(oldAside)
	}
	return nil
}

func writeJSON(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("prepare: create %s: %w", filepath.Base(path), err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		f.Close()
		return fmt.Errorf("prepare: encode %s: %w", filepath.Base(path), err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("prepare: sync %s: %w", filepath.Base(path), err)
	}
	return f.Close()
}
