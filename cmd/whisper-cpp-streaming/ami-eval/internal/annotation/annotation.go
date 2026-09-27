// Package annotation parses AMI participant word-annotation XML into the
// merged reference speech intervals used to score detector output
// (muesli#778). It accepts finite, non-negative timed <w> elements whose end
// is not before its start; markup, non-lexical, untimed, and zero-duration
// (punctuation) elements are ignored and counted rather than rejected, so
// schema drift stays visible without breaking harmless metadata tolerance. A
// genuinely reversed span (end before start) remains a fatal parse error.
package annotation

import (
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
)

// SchemaVersion is the reference.json schema this package emits.
const SchemaVersion = 1

// MergeGapMillis is the maximum silence gap, in milliseconds, folded into
// the same reference utterance rather than starting a new one.
const MergeGapMillis = 300

// EndOverrunFatalMillis bounds how far a raw annotation end may exceed the
// audio's sample extent before it is treated as a fatal, materially
// inconsistent annotation rather than a clampable rounding/trim artifact.
const EndOverrunFatalMillis = 1000

// RawInterval is one accepted, still-unclamped timed word span in seconds.
type RawInterval struct {
	StartSeconds float64
	EndSeconds   float64
}

// Counters records, by reason, every non-fatal thing the parser and
// clamping step tolerated -- so drift stays visible even though it does not
// stop the run.
type Counters struct {
	IgnoredMarkup       int `json:"ignored_markup"`
	IgnoredUnknown      int `json:"ignored_unknown"`
	IgnoredUntimed      int `json:"ignored_untimed"`
	IgnoredZeroDuration int `json:"ignored_zero_duration"`
	ClampedStart        int `json:"clamped_start"`
	ClampedEnd          int `json:"clamped_end"`
	ClampedEmpty        int `json:"clamped_empty"`
}

// Interval is a half-open [Start, End) span of 16kHz sample indices.
type Interval struct {
	Start int64 `json:"start_sample"`
	End   int64 `json:"end_sample"`
}

// ParseParticipant streams one participant's word-annotation XML and
// returns every accepted timed word as a RawInterval in seconds, plus
// counts of everything ignored. A malformed timed word -- non-finite,
// negative, or with end not strictly after start -- is a fatal parse error;
// markup (vocalsound/disfmarker/gap), unrecognized elements, and <w>
// elements missing one or both time attributes are tolerated and counted
// instead.
func ParseParticipant(r io.Reader) ([]RawInterval, Counters, error) {
	dec := xml.NewDecoder(r)
	// Real AMI word XML declares ISO-8859-1. We never decode character data
	// (word text), only ASCII attribute values, so treating the declared
	// charset as an identity byte passthrough is safe and avoids pulling in
	// an external charset-conversion dependency.
	dec.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		return input, nil
	}
	var (
		out  []RawInterval
		cnts Counters
	)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, Counters{}, fmt.Errorf("annotation: xml token: %w", err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "root":
			// The nite:root container; nothing to score.
		case "w":
			startAttr, hasStart := findAttr(start.Attr, "starttime")
			endAttr, hasEnd := findAttr(start.Attr, "endtime")
			if !hasStart || !hasEnd {
				cnts.IgnoredUntimed++
				continue
			}
			startSec, errS := parseFloat(startAttr)
			endSec, errE := parseFloat(endAttr)
			if errS != nil || errE != nil || !isFinite(startSec) || !isFinite(endSec) {
				return nil, Counters{}, fmt.Errorf("annotation: word has non-finite or unparsable time (starttime=%q endtime=%q)", startAttr, endAttr)
			}
			if startSec < 0 || endSec < 0 {
				return nil, Counters{}, fmt.Errorf("annotation: word has negative time (starttime=%v endtime=%v)", startSec, endSec)
			}
			if endSec < startSec {
				return nil, Counters{}, fmt.Errorf("annotation: word end (%v) is before start (%v)", endSec, startSec)
			}
			if endSec == startSec {
				// Real AMI data marks punctuation as a zero-duration <w>
				// sharing the adjacent word's timestamp. It is well-formed,
				// not corrupt -- unlike a genuinely reversed span -- so it
				// is tolerated and counted rather than treated as fatal; it
				// contributes no speech duration either way.
				cnts.IgnoredZeroDuration++
				continue
			}
			out = append(out, RawInterval{StartSeconds: startSec, EndSeconds: endSec})
		case "vocalsound", "disfmarker", "gap":
			cnts.IgnoredMarkup++
		default:
			cnts.IgnoredUnknown++
		}
	}
	return out, cnts, nil
}

func findAttr(attrs []xml.Attr, local string) (string, bool) {
	for _, a := range attrs {
		if a.Name.Local == local {
			return a.Value, true
		}
	}
	return "", false
}

func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}

func isFinite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

// secondsToSamples converts a non-negative second offset to a 16kHz-style
// sample index by rounding to nearest, halves away from zero:
// floor(seconds*sampleRate + 0.5).
func secondsToSamples(seconds float64, sampleRate int) int64 {
	return int64(math.Floor(seconds*float64(sampleRate) + 0.5))
}

// Reference is the stable, serializable result of merging one meeting's
// participant annotations into reference speech intervals.
type Reference struct {
	SchemaVersion   int               `json:"schema_version"`
	SourceSHA256    map[string]string `json:"source_sha256"`
	Counters        Counters          `json:"counters"`
	DurationSamples int64             `json:"duration_samples"`
	MergeGapSamples int64             `json:"merge_gap_samples"`
	SampleRate      int               `json:"sample_rate"`
	Intervals       []Interval        `json:"intervals"`
}

// participantIntervals is one participant's accepted raw intervals, kept
// paired with a stable participant ID purely so BuildReference's output
// does not depend on map iteration order.
type ParticipantIntervals struct {
	ParticipantID string
	Raw           []RawInterval
}

// BuildReference converts every participant's accepted raw intervals to
// sample indices, clamps them to the decoded audio's extent, unions all
// speakers, sorts, and merges overlaps or gaps of at most MergeGapMillis.
// An end more than EndOverrunFatalMillis beyond the audio extent is a fatal,
// materially inconsistent annotation; smaller overruns are clamped and
// counted, and an interval that collapses to empty after clamping is
// dropped and counted as clamped_empty.
func BuildReference(participants []ParticipantIntervals, sourceSHA256 map[string]string, audioSamples int64, sampleRate int) (Reference, error) {
	fatalOverrun := int64(sampleRate) * EndOverrunFatalMillis / 1000
	mergeGap := int64(sampleRate) * MergeGapMillis / 1000

	var cnts Counters
	var flat []Interval
	for _, p := range participants {
		for _, raw := range p.Raw {
			start := secondsToSamples(raw.StartSeconds, sampleRate)
			end := secondsToSamples(raw.EndSeconds, sampleRate)

			if end > audioSamples {
				if end-audioSamples > fatalOverrun {
					return Reference{}, fmt.Errorf("annotation: participant %s word end %d samples exceeds audio extent %d samples by more than %dms", p.ParticipantID, end, audioSamples, EndOverrunFatalMillis)
				}
				end = audioSamples
				cnts.ClampedEnd++
			}
			if start > audioSamples {
				start = audioSamples
				cnts.ClampedStart++
			}
			if start < 0 {
				start = 0
				cnts.ClampedStart++
			}
			if start >= end {
				cnts.ClampedEmpty++
				continue
			}
			flat = append(flat, Interval{Start: start, End: end})
		}
	}

	sort.Slice(flat, func(i, j int) bool {
		if flat[i].Start != flat[j].Start {
			return flat[i].Start < flat[j].Start
		}
		return flat[i].End < flat[j].End
	})

	merged := mergeIntervals(flat, mergeGap)

	return Reference{
		SchemaVersion:   SchemaVersion,
		SourceSHA256:    sourceSHA256,
		Counters:        cnts,
		DurationSamples: audioSamples,
		MergeGapSamples: mergeGap,
		SampleRate:      sampleRate,
		Intervals:       merged,
	}, nil
}

// mergeIntervals merges half-open intervals that overlap or are separated
// by a gap of at most maxGap samples. Intervals must already be sorted by
// start.
func mergeIntervals(sorted []Interval, maxGap int64) []Interval {
	var out []Interval
	for _, iv := range sorted {
		if len(out) == 0 {
			out = append(out, iv)
			continue
		}
		last := &out[len(out)-1]
		if iv.Start <= last.End+maxGap {
			if iv.End > last.End {
				last.End = iv.End
			}
			continue
		}
		out = append(out, iv)
	}
	return out
}
