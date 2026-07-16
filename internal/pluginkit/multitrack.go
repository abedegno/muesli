package pluginkit

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/abedegno/muesli/internal/model"
)

func deinterleave(interleaved []float32, channels int) [][]float32 {
	out := make([][]float32, channels)
	n := len(interleaved) / channels
	for c := 0; c < channels; c++ {
		out[c] = make([]float32, n)
		for i := 0; i < n; i++ {
			out[c][i] = interleaved[i*channels+c]
		}
	}
	return out
}

const multitrackSilenceDBFS = -60.0

func isNearSilent(pcm []float32) bool {
	var peak float64
	for _, s := range pcm {
		if a := math.Abs(float64(s)); a > peak {
			peak = a
		}
	}
	if peak <= 0 {
		return true
	}
	return 20*math.Log10(peak) <= multitrackSilenceDBFS
}

func channelSpeaker(rawIndex int) string {
	switch rawIndex {
	case 0:
		return model.SpeakerYou
	case 1:
		return model.SpeakerThem
	default:
		return fmt.Sprintf("Speaker %d", rawIndex+1)
	}
}

func channelSource(rawIndex int) string {
	switch rawIndex {
	case 0:
		return "mic"
	case 1:
		return "system"
	default:
		return fmt.Sprintf("channel %d", rawIndex)
	}
}

func orchestrateChannels(ctx context.Context, channels [][]float32, eng Transcriber, req TranscribeRequest) (TranscribeResult, error) {
	var all []model.Segment
	var language, modelName string
	var durationMS int
	for i, pcm := range channels {
		if isNearSilent(pcm) {
			continue
		}
		res, err := eng.Transcribe(ctx, pcm, req)
		if err != nil {
			return TranscribeResult{}, err
		}
		for _, seg := range res.Segments {
			seg.Speaker = channelSpeaker(i)
			seg.Source = channelSource(i)
			all = append(all, seg)
		}
		if res.Language != "" {
			language = res.Language
		}
		if res.Model != "" {
			modelName = res.Model
		}
		if res.DurationMS > durationMS {
			durationMS = res.DurationMS
		}
	}
	sort.SliceStable(all, func(a, b int) bool { return all[a].StartMS < all[b].StartMS })
	return TranscribeResult{Segments: all, Language: language, Model: modelName, DurationMS: durationMS}, nil
}

// audioLayoutRe captures the channel-layout token from an ffmpeg "Audio:" line,
// e.g. "Audio: opus, 48000 Hz, stereo, fltp" -> "stereo".
var audioLayoutRe = regexp.MustCompile(`Audio:.*?\d+ Hz,\s*([^,\r\n]+)`)
var dotLayoutRe = regexp.MustCompile(`(\d+)\.(\d+)`)
var nChannelsRe = regexp.MustCompile(`(\d+) channels`)

// probeChannelCount reports the number of audio channels in the source. ffprobe
// is NOT bundled in the desktop app (only ffmpeg is), so we probe with ffmpeg:
// `-i pipe:0` with no output exits non-zero but still writes the stream info to
// stderr, from which we read the channel layout. Returns 1 on any failure so the
// caller falls back to a single-pass mono decode.
func probeChannelCount(ctx context.Context, audioURL string) (int, error) {
	raw, err := fetchAudioBytes(ctx, audioURL)
	if err != nil {
		return 1, err
	}
	cmd := exec.CommandContext(ctx, ffmpegBin(), "-hide_banner", "-i", "pipe:0")
	cmd.Stdin = bytes.NewReader(raw)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	_ = cmd.Run() // expected non-zero: no output file specified
	return channelsFromFFmpegStderr(stderr.String()), nil
}

// channelsFromFFmpegStderr parses the channel count from ffmpeg's stderr banner.
func channelsFromFFmpegStderr(stderr string) int {
	m := audioLayoutRe.FindStringSubmatch(stderr)
	if m == nil {
		return 1
	}
	layout := strings.TrimSpace(m[1])
	switch {
	case strings.HasPrefix(layout, "mono"):
		return 1
	case strings.HasPrefix(layout, "stereo"):
		return 2
	case strings.HasPrefix(layout, "quad"):
		return 4
	}
	if dm := dotLayoutRe.FindStringSubmatch(layout); dm != nil {
		a, _ := strconv.Atoi(dm[1])
		b, _ := strconv.Atoi(dm[2])
		if a+b > 0 {
			return a + b
		}
	}
	if nm := nChannelsRe.FindStringSubmatch(layout); nm != nil {
		if n, _ := strconv.Atoi(nm[1]); n > 0 {
			return n
		}
	}
	return 1
}

func runMultitrack(ctx context.Context, audioURL string, eng Transcriber, req TranscribeRequest) (TranscribeResult, error) {
	n, err := probeChannelCount(ctx, audioURL)
	if err != nil || n <= 1 {
		pcm, derr := DecodePCM(ctx, audioURL)
		if derr != nil {
			return TranscribeResult{}, derr
		}
		return eng.Transcribe(ctx, pcm, req)
	}
	channels, derr := DecodePCMChannels(ctx, audioURL, n)
	if derr != nil {
		return TranscribeResult{}, derr
	}
	return orchestrateChannels(ctx, channels, eng, req)
}

func decodeFFmpegF32LE(out []byte) ([]float32, error) {
	if len(out)%4 != 0 {
		return nil, fmt.Errorf("ffmpeg returned truncated float32 pcm")
	}
	pcm := make([]float32, len(out)/4)
	for i := range pcm {
		pcm[i] = math.Float32frombits(binary.LittleEndian.Uint32(out[i*4 : (i+1)*4]))
	}
	return pcm, nil
}
