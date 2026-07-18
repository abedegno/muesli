package audiohash_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/audiohash"
)

func TestHashRaw(t *testing.T) {
	got, err := audiohash.HashRaw(strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("hash raw: %v", err)
	}
	const want = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got != want {
		t.Fatalf("hash raw = %q, want %q", got, want)
	}
}

func TestHashNormalized_Fallback(t *testing.T) {
	got, err := audiohash.HashNormalized(context.Background(), "/nonexistent/does-not-exist.mp3")
	if err != nil {
		t.Fatalf("hash normalized: %v", err)
	}
	if got != "" {
		t.Fatalf("hash normalized = %q, want empty string", got)
	}
}

func TestHashNormalized_WithFFmpeg(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}

	dir := t.TempDir()
	audioPath := filepath.Join(dir, "silence.wav")
	if err := os.WriteFile(audioPath, wavSilence(), 0o600); err != nil {
		t.Fatalf("write wav: %v", err)
	}

	got, err := audiohash.HashNormalized(context.Background(), audioPath)
	if err != nil {
		t.Fatalf("hash normalized: %v", err)
	}
	if got == "" {
		t.Fatal("expected non-empty normalized hash")
	}
}

func TestHashNormalizedUsesConfiguredBinary(t *testing.T) {
	dir := t.TempDir()
	stubPath := filepath.Join(dir, "ffmpeg-stub")
	stubOutput := []byte("stub-normalized-output")
	if err := os.WriteFile(stubPath, []byte("#!/bin/sh\nprintf '%s' 'stub-normalized-output'\n"), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	if err := os.Chmod(stubPath, 0o755); err != nil {
		t.Fatalf("chmod stub: %v", err)
	}
	t.Setenv("MUESLI_FFMPEG_BIN", stubPath)

	got, err := audiohash.HashNormalized(context.Background(), "ignored-input")
	if err != nil {
		t.Fatalf("hash normalized: %v", err)
	}
	wantSum := sha256.Sum256(stubOutput)
	want := hex.EncodeToString(wantSum[:])
	if got != want {
		t.Fatalf("hash normalized = %q, want %q", got, want)
	}
}

func TestHashNormalizedRespectsContextTimeout(t *testing.T) {
	dir := t.TempDir()
	stubPath := filepath.Join(dir, "ffmpeg-stub")
	if err := os.WriteFile(stubPath, []byte("#!/bin/sh\nexec sleep 5\n"), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	if err := os.Chmod(stubPath, 0o755); err != nil {
		t.Fatalf("chmod stub: %v", err)
	}
	t.Setenv("MUESLI_FFMPEG_BIN", stubPath)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	type result struct {
		got string
		err error
	}
	resultCh := make(chan result, 1)
	go func() {
		got, err := audiohash.HashNormalized(ctx, "ignored-input")
		resultCh <- result{got: got, err: err}
	}()

	timer := time.NewTimer(time.Second)
	defer timer.Stop()

	select {
	case res := <-resultCh:
		if res.err != nil {
			t.Fatalf("hash normalized: %v", res.err)
		}
		if res.got != "" {
			t.Fatalf("hash normalized = %q, want empty string", res.got)
		}
	case <-timer.C:
		t.Fatal("hash normalized did not return promptly after context timeout")
	}
}

func wavSilence() []byte {
	const (
		sampleRate    = 16000
		channels      = 1
		bitsPerSample = 16
		numSamples    = 16
	)
	data := make([]byte, numSamples*channels*bitsPerSample/8)

	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36+len(data)))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(channels))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate*channels*bitsPerSample/8))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(channels*bitsPerSample/8))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(bitsPerSample))
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(data)))
	buf.Write(data)
	return buf.Bytes()
}
