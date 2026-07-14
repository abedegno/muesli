//go:build whisper_cgo

package engine

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/pluginkit"
)

// tinyModelURL is the standard ggml tiny English model published alongside
// whisper.cpp, the same one whisper.cpp's own download-ggml-model.sh script
// fetches.
const tinyModelURL = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-tiny.en.bin"

// TestWhisperCGOEngineTranscribesFixture exercises the real whisper.cpp cgo
// engine end-to-end: it downloads a tiny GGML model, decodes a short checked
// in speech fixture, and runs it through Engine.Transcribe.
//
// It is env-guarded so that it skips cleanly (rather than failing) whenever
// the model can't be fetched -- e.g. no network access -- keeping it
// runner-safe. It only builds at all under `-tags whisper_cgo`, so it never
// affects the default pure-Go build or CI.
func TestWhisperCGOEngineTranscribesFixture(t *testing.T) {
	if _, err := http.Head(tinyModelURL); err != nil {
		t.Skipf("skipping whisper_cgo integration test: model host unreachable: %v", err)
	}

	pcm, err := decodeWAVMono16kFloat32("testdata/jfk.wav")
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if len(pcm) == 0 {
		t.Fatal("fixture decoded to zero samples")
	}

	// Reuse a stable cache dir across test runs so the ~75MB model is only
	// downloaded once, rather than on every `go test` invocation.
	modelDir := filepath.Join(os.TempDir(), "muesli-whisper-cgo-test-models")

	eng := New(Config{
		ModelDir: modelDir,
		ModelURL: tinyModelURL,
		Model:    "ggml-tiny.en",
		Language: "auto",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	got, err := eng.Transcribe(ctx, pcm, pluginkit.TranscribeRequest{Config: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}

	if len(got.Segments) == 0 {
		t.Fatal("expected at least one segment")
	}

	var text strings.Builder
	prevStart, prevEnd := -1, -1
	for i, seg := range got.Segments {
		if seg.StartMS < 0 || seg.EndMS < seg.StartMS {
			t.Fatalf("segment %d has invalid range: %+v", i, seg)
		}
		if seg.StartMS < prevStart || seg.EndMS < prevEnd {
			t.Fatalf("segment timestamps are not monotonic at %d: prev=(%d,%d) cur=(%d,%d)", i, prevStart, prevEnd, seg.StartMS, seg.EndMS)
		}
		prevStart, prevEnd = seg.StartMS, seg.EndMS
		text.WriteString(seg.Text)
		text.WriteString(" ")
	}

	if strings.TrimSpace(text.String()) == "" {
		t.Fatal("expected non-empty transcribed text")
	}

	if got.Language != "en" {
		t.Fatalf("language = %q, want %q (jfk.wav fixture is English speech)", got.Language, "en")
	}

	if got.DurationMS <= 0 {
		t.Fatalf("duration_ms = %d, want > 0", got.DurationMS)
	}
}

// decodeWAVMono16kFloat32 reads a canonical PCM WAV file and returns its
// samples as mono 16kHz float32 PCM, matching the shape pluginkit hands
// engines in production (after ffmpeg decoding). It walks RIFF chunks
// explicitly rather than assuming a fixed header size, since some WAV files
// (including the checked-in fixture) carry extra chunks (e.g. LIST) before
// the data chunk.
func decodeWAVMono16kFloat32(path string) ([]float32, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) < 12 || string(raw[0:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		return nil, errors.New("not a RIFF/WAVE file")
	}

	type fmtChunk struct {
		audioFormat   uint16
		numChannels   uint16
		sampleRate    uint32
		bitsPerSample uint16
	}
	var fc fmtChunk
	var data []byte

	pos := 12
	for pos+8 <= len(raw) {
		id := string(raw[pos : pos+4])
		size := binary.LittleEndian.Uint32(raw[pos+4 : pos+8])
		body := raw[pos+8:]
		if uint32(len(body)) < size {
			return nil, fmt.Errorf("truncated %q chunk", id)
		}
		body = body[:size]

		switch id {
		case "fmt ":
			if len(body) < 16 {
				return nil, errors.New("fmt chunk too small")
			}
			fc.audioFormat = binary.LittleEndian.Uint16(body[0:2])
			fc.numChannels = binary.LittleEndian.Uint16(body[2:4])
			fc.sampleRate = binary.LittleEndian.Uint32(body[4:8])
			fc.bitsPerSample = binary.LittleEndian.Uint16(body[14:16])
		case "data":
			data = body
		}

		// Chunks are word-aligned: skip a padding byte for odd sizes.
		pos += 8 + int(size) + int(size%2)
	}

	if data == nil {
		return nil, errors.New("no data chunk found")
	}
	if fc.audioFormat != 1 {
		return nil, fmt.Errorf("unsupported wav audio format %d, want PCM (1)", fc.audioFormat)
	}
	if fc.numChannels != 1 {
		return nil, fmt.Errorf("unsupported wav channel count %d, want 1 (mono)", fc.numChannels)
	}
	if fc.sampleRate != 16000 {
		return nil, fmt.Errorf("unsupported wav sample rate %d, want 16000", fc.sampleRate)
	}
	if fc.bitsPerSample != 16 {
		return nil, fmt.Errorf("unsupported wav bit depth %d, want 16", fc.bitsPerSample)
	}

	n := len(data) / 2
	pcm := make([]float32, n)
	for i := 0; i < n; i++ {
		s := int16(binary.LittleEndian.Uint16(data[i*2 : i*2+2]))
		pcm[i] = float32(s) / 32768.0
	}
	return pcm, nil
}
