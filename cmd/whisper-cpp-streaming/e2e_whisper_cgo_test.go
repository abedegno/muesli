//go:build whisper_cgo

package main

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/internal/live"
	"github.com/abedegno/muesli/internal/pluginkit"
	"github.com/abedegno/muesli/internal/whispercpp/engine"
)

func TestRealAudioProducesPartialAndFinal(t *testing.T) {
	eng := live.New(engine.Config{ModelDir: filepath.Join(t.TempDir(), "models"), ModelURL: defaultTinyURL, Model: "tiny.en", Language: "en"})
	req := pluginkit.StreamingStartRequest{Type: "start", SampleRate: 16_000, Channels: 1}
	session := waitReady(t, func() (pluginkit.StreamingEngineSession, error) { return eng.StartStream(context.Background(), req) })
	partials, err := session.WriteAudio(context.Background(), testWAVPCM(t))
	if err != nil {
		t.Fatal(err)
	}
	finals, err := session.WriteAudio(context.Background(), make([]float32, 16_000))
	if err != nil {
		t.Fatal(err)
	}
	partialCount, finalCount, finalText := 0, 0, ""
	for _, event := range append(partials, finals...) {
		if event.Final {
			finalCount++
			finalText = event.Text
		} else {
			partialCount++
		}
	}
	if partialCount < 1 || finalCount != 1 || finalText == "" {
		t.Fatalf("partials=%d finals=%d finalText=%q", partialCount, finalCount, finalText)
	}
}

func waitReady(t *testing.T, start func() (pluginkit.StreamingEngineSession, error)) pluginkit.StreamingEngineSession {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for {
		session, err := start()
		if err == nil {
			return session
		}
		if time.Now().After(deadline) {
			t.Fatalf("engine did not become ready: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
}

func testWAVPCM(t *testing.T) []float32 {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "internal", "whispercpp", "engine", "testdata", "jfk.wav"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		t.Fatal("invalid WAV fixture")
	}
	var payload []byte
	for offset := 12; offset+8 <= len(data); {
		size := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		start, end := offset+8, offset+8+size
		if end > len(data) {
			t.Fatal("truncated WAV fixture")
		}
		if string(data[offset:offset+4]) == "data" {
			payload = data[start:end]
			break
		}
		offset = end + size%2
	}
	if len(payload) == 0 || len(payload)%2 != 0 {
		t.Fatal("WAV fixture has no aligned PCM data")
	}
	pcm := make([]float32, len(payload)/2)
	for i := range pcm {
		pcm[i] = float32(int16(binary.LittleEndian.Uint16(payload[i*2:]))) / 32768
	}
	return pcm
}
