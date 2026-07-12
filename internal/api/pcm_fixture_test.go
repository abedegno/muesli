package api_test

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

type pcmFixture struct {
	InputSampleRate int       `json:"inputSampleRate"`
	FrameMs         int       `json:"frameMs"`
	PatternRepeats  int       `json:"patternRepeats"`
	SamplePattern   []float64 `json:"samplePattern"`
}

func loadPCMFixture(t *testing.T) pcmFixture {
	t.Helper()
	path := filepath.Join("..", "..", "src", "shared", "pcm.fixture.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pcm fixture: %v", err)
	}
	var fx pcmFixture
	if err := json.Unmarshal(data, &fx); err != nil {
		t.Fatalf("decode pcm fixture: %v", err)
	}
	return fx
}

func pcmFixtureAllFrames(t *testing.T) [][]byte {
	t.Helper()
	fx := loadPCMFixture(t)
	if fx.InputSampleRate <= 0 || fx.FrameMs <= 0 || fx.PatternRepeats <= 0 {
		t.Fatalf("invalid pcm fixture: %+v", fx)
	}
	frameSamples := int(math.Round(float64(fx.InputSampleRate*fx.FrameMs) / 1000))
	if frameSamples <= 0 {
		t.Fatalf("invalid frame size from fixture: %+v", fx)
	}
	samples := make([]float64, 0, len(fx.SamplePattern)*fx.PatternRepeats)
	for i := 0; i < fx.PatternRepeats; i++ {
		samples = append(samples, fx.SamplePattern...)
	}
	if len(samples)%frameSamples != 0 {
		t.Fatalf("fixture sample count %d does not divide frame size %d", len(samples), frameSamples)
	}

	frames := make([][]byte, 0, len(samples)/frameSamples)
	for i := 0; i < len(samples); i += frameSamples {
		frames = append(frames, encodePCMFixtureFrame(samples[i:i+frameSamples]))
	}
	return frames
}

func encodePCMFixtureFrame(samples []float64) []byte {
	out := make([]byte, len(samples)*2)
	for i, sample := range samples {
		v := clampPCMFixture(sample)
		out[i*2] = byte(v)
		out[i*2+1] = byte(v >> 8)
	}
	return out
}

func clampPCMFixture(sample float64) int16 {
	if sample < -1 {
		sample = -1
	}
	if sample > 1 {
		sample = 1
	}
	return int16(math.Round(sample * 0x7fff))
}
