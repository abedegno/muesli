package pluginkit

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"os/exec"
	"testing"

	"github.com/abedegno/muesli/internal/model"
)

func stereoWAVDataURL() string {
	const (
		sampleRate    = 16000
		channels      = 2
		bitsPerSample = 16
		samples       = 160
	)
	left := int16(22000)
	right := int16(22000)
	data := make([]byte, samples*channels*2)
	for i := 0; i < samples; i++ {
		binary.LittleEndian.PutUint16(data[i*4:], uint16(left))
		binary.LittleEndian.PutUint16(data[i*4+2:], uint16(right))
	}
	byteRate := uint32(sampleRate * channels * bitsPerSample / 8)
	blockAlign := uint16(channels * bitsPerSample / 8)
	chunkSize := uint32(36 + len(data))
	buf := make([]byte, 44+len(data))
	copy(buf[0:], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:], chunkSize)
	copy(buf[8:], "WAVE")
	copy(buf[12:], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:], 16)
	binary.LittleEndian.PutUint16(buf[20:], 1)
	binary.LittleEndian.PutUint16(buf[22:], channels)
	binary.LittleEndian.PutUint32(buf[24:], sampleRate)
	binary.LittleEndian.PutUint32(buf[28:], byteRate)
	binary.LittleEndian.PutUint16(buf[32:], blockAlign)
	binary.LittleEndian.PutUint16(buf[34:], bitsPerSample)
	copy(buf[36:], "data")
	binary.LittleEndian.PutUint32(buf[40:], uint32(len(data)))
	copy(buf[44:], data)
	return "data:audio/wav;base64," + base64.StdEncoding.EncodeToString(buf)
}

func TestDeinterleaveStereo(t *testing.T) {
	got := deinterleave([]float32{1, 2, 3, 4, 5, 6}, 2)
	want := [][]float32{{1, 3, 5}, {2, 4, 6}}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if len(got[i]) != len(want[i]) {
			t.Fatalf("len(got[%d]) = %d, want %d", i, len(got[i]), len(want[i]))
		}
		for j := range want[i] {
			if got[i][j] != want[i][j] {
				t.Fatalf("got[%d][%d] = %v, want %v", i, j, got[i][j], want[i][j])
			}
		}
	}
}

func TestNearSilent(t *testing.T) {
	if !isNearSilent([]float32{0, 0, 0}) {
		t.Fatal("expected zeros to be near silent")
	}
	if isNearSilent([]float32{1}) {
		t.Fatal("expected full scale sample to be audible")
	}
}

func TestOrchestrateChannelsMergesSortsAndRoleLabels(t *testing.T) {
	eng := fakeMultitrackTranscriber(func(_ context.Context, pcm []float32, _ TranscribeRequest) (TranscribeResult, error) {
		start := int(pcm[0])
		return TranscribeResult{
			Segments:   []model.Segment{{StartMS: start, EndMS: start + 1, Text: "seg"}},
			Language:   "en",
			Model:      "fake",
			DurationMS: start + 1,
		}, nil
	})
	got, err := orchestrateChannels(context.Background(), [][]float32{{1000}, {200}}, eng, TranscribeRequest{})
	if err != nil {
		t.Fatalf("orchestrate: %v", err)
	}
	if len(got.Segments) != 2 {
		t.Fatalf("len(segments) = %d, want 2", len(got.Segments))
	}
	if got.Segments[0].StartMS != 200 || got.Segments[0].Speaker != "Them" || got.Segments[0].Source != "system" {
		t.Fatalf("segment[0] = %+v", got.Segments[0])
	}
	if got.Segments[1].StartMS != 1000 || got.Segments[1].Speaker != "You" || got.Segments[1].Source != "mic" {
		t.Fatalf("segment[1] = %+v", got.Segments[1])
	}
	if got.Language != "en" || got.Model != "fake" || got.DurationMS != 1001 {
		t.Fatalf("result = %+v", got)
	}
}

func TestOrchestrateSkipsSilentChannel(t *testing.T) {
	eng := fakeMultitrackTranscriber(func(_ context.Context, pcm []float32, _ TranscribeRequest) (TranscribeResult, error) {
		start := int(pcm[0])
		return TranscribeResult{
			Segments:   []model.Segment{{StartMS: start, EndMS: start + 1, Text: "seg"}},
			Language:   "en",
			Model:      "fake",
			DurationMS: start + 1,
		}, nil
	})
	got, err := orchestrateChannels(context.Background(), [][]float32{{1000}, {0, 0, 0}}, eng, TranscribeRequest{})
	if err != nil {
		t.Fatalf("orchestrate: %v", err)
	}
	if len(got.Segments) != 1 {
		t.Fatalf("len(segments) = %d, want 1", len(got.Segments))
	}
	if got.Segments[0].Speaker != "You" || got.Segments[0].Source != "mic" {
		t.Fatalf("segment = %+v", got.Segments[0])
	}
}

func TestRunMultitrackDecodesTwoChannels(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available on PATH")
	}
	// One segment per channel so the two non-silent channels yield exactly two
	// segments (You + Them). recordingTranscriber returns two segments per call,
	// which would (correctly) produce four here.
	eng := fakeMultitrackTranscriber(func(_ context.Context, _ []float32, _ TranscribeRequest) (TranscribeResult, error) {
		return TranscribeResult{Segments: []model.Segment{{StartMS: 0, EndMS: 100, Text: "seg"}}}, nil
	})
	res, err := runMultitrack(context.Background(), stereoWAVDataURL(), eng, TranscribeRequest{Config: []byte(`{}`)})
	if err != nil {
		t.Fatalf("run multitrack: %v", err)
	}
	if len(res.Segments) != 2 {
		t.Fatalf("len(segments) = %d, want 2", len(res.Segments))
	}
	if res.Segments[0].Speaker != "You" || res.Segments[1].Speaker != "Them" {
		t.Fatalf("segments = %+v", res.Segments)
	}
}

type fakeMultitrackTranscriber func(context.Context, []float32, TranscribeRequest) (TranscribeResult, error)

func (f fakeMultitrackTranscriber) Transcribe(ctx context.Context, pcm []float32, req TranscribeRequest) (TranscribeResult, error) {
	return f(ctx, pcm, req)
}

func TestChannelsFromFFmpegStderr(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"stereo", "  Stream #0:0(eng): Audio: opus, 48000 Hz, stereo, fltp (default)", 2},
		{"mono", "  Stream #0:0: Audio: pcm_s16le, 44100 Hz, mono, s16", 1},
		{"5.1 side", "  Stream #0:0: Audio: aac, 48000 Hz, 5.1(side), fltp", 6},
		{"n channels", "  Stream #0:0: Audio: pcm_f32le, 16000 Hz, 3 channels, flt", 3},
		{"quad", "  Stream #0:0: Audio: aac, 48000 Hz, quad, fltp", 4},
		{"no audio line", "  Stream #0:0: Video: h264, yuv420p, 1920x1080", 1},
		{"empty", "", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := channelsFromFFmpegStderr(tc.in); got != tc.want {
				t.Fatalf("channelsFromFFmpegStderr(%s) = %d, want %d", tc.name, got, tc.want)
			}
		})
	}
}
