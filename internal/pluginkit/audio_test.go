package pluginkit

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"testing"
)

func tinyWAVDataURL(samples int) string {
	if samples < 1 {
		samples = 1
	}
	data := make([]byte, samples*2)
	const sampleRate = 16000
	const channels = 1
	const bitsPerSample = 16
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

func TestDecodePCM(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available on PATH")
	}
	pcm, err := DecodePCM(context.Background(), tinyWAVDataURL(160))
	if err != nil {
		t.Fatalf("decode pcm: %v", err)
	}
	if len(pcm) == 0 {
		t.Fatal("expected non-empty pcm")
	}
	for i, sample := range pcm {
		if math.IsNaN(float64(sample)) {
			t.Fatalf("sample %d is NaN", i)
		}
	}
}

func TestDecodePCMSupportsDataURL(t *testing.T) {
	u, err := url.Parse(tinyWAVDataURL(1))
	if err != nil || u.Scheme != "data" {
		t.Fatalf("bad fixture url: %v %v", u, err)
	}
}

func TestFetchAudioBytesRejectsOversizedResponse(t *testing.T) {
	const limit = 1024
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("a"), limit+1))
	}))
	t.Cleanup(srv.Close)

	if _, err := fetchAudioBytesWithLimit(context.Background(), srv.URL, limit); err == nil {
		t.Fatal("expected oversized response to fail")
	}
}

func TestDecodeDataURLRejectsOversizedBase64Payload(t *testing.T) {
	const limit = 1024
	raw := bytes.Repeat([]byte("a"), limit+1)
	audioURL := "data:audio/wav;base64," + base64.StdEncoding.EncodeToString(raw)

	if _, err := decodeDataURLWithLimit(audioURL, limit); err == nil {
		t.Fatal("expected oversized data URL to fail")
	}
}

func TestFfmpegBin(t *testing.T) {
	t.Run("env override", func(t *testing.T) {
		t.Setenv("MUESLI_FFMPEG_BIN", "  /opt/muesli/bin/ffmpeg  ")
		if got := ffmpegBin(); got != "/opt/muesli/bin/ffmpeg" {
			t.Fatalf("ffmpegBin() = %q, want %q", got, "/opt/muesli/bin/ffmpeg")
		}
	})

	t.Run("default", func(t *testing.T) {
		restore := clearEnv(t, "MUESLI_FFMPEG_BIN")
		defer restore()

		if got := ffmpegBin(); got != "ffmpeg" {
			t.Fatalf("ffmpegBin() = %q, want %q", got, "ffmpeg")
		}
	})
}

func clearEnv(t *testing.T, key string) func() {
	t.Helper()

	old, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	return func() {
		if had {
			t.Setenv(key, old)
			return
		}
		_ = os.Unsetenv(key)
	}
}
