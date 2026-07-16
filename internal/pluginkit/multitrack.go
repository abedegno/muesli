package pluginkit

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"sort"

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
		return "You"
	case 1:
		return "Them"
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

func probeChannelCount(ctx context.Context, audioURL string) (int, error) {
	raw, err := fetchAudioBytes(ctx, audioURL)
	if err != nil {
		return 1, err
	}

	cmd := exec.CommandContext(ctx, "ffprobe",
		"-hide_banner",
		"-loglevel", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=channels",
		"-of", "json",
		"-i", "pipe:0",
	)
	cmd.Stdin = bytes.NewReader(raw)
	out, err := cmd.Output()
	if err != nil {
		return 1, nil
	}
	var probe struct {
		Streams []struct {
			Channels int `json:"channels"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &probe); err != nil {
		return 1, nil
	}
	for _, stream := range probe.Streams {
		if stream.Channels > 0 {
			return stream.Channels, nil
		}
	}
	return 1, nil
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
