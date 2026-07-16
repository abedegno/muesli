package pluginkit

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

// DecodePCM fetches an audio URL and decodes it to 16 kHz mono float32 PCM via
// the external ffmpeg binary.
func DecodePCM(ctx context.Context, audioURL string) ([]float32, error) {
	raw, err := fetchAudioBytes(ctx, audioURL)
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, ffmpegBin(),
		"-hide_banner",
		"-loglevel", "error",
		"-i", "pipe:0",
		"-f", "f32le",
		"-ac", "1",
		"-ar", "16000",
		"pipe:1",
	)
	cmd.Stdin = bytes.NewReader(raw)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("ffmpeg decode failed: %w: %s", err, msg)
		}
		return nil, fmt.Errorf("ffmpeg decode failed: %w", err)
	}

	return decodeFFmpegF32LE(stdout.Bytes())
}

func DecodePCMChannels(ctx context.Context, audioURL string, channels int) ([][]float32, error) {
	raw, err := fetchAudioBytes(ctx, audioURL)
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, ffmpegBin(),
		"-hide_banner",
		"-loglevel", "error",
		"-i", "pipe:0",
		"-f", "f32le",
		"-ar", "16000",
		"pipe:1",
	)
	cmd.Stdin = bytes.NewReader(raw)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("ffmpeg decode failed: %w: %s", err, msg)
		}
		return nil, fmt.Errorf("ffmpeg decode failed: %w", err)
	}

	pcm, err := decodeFFmpegF32LE(stdout.Bytes())
	if err != nil {
		return nil, err
	}
	if channels <= 0 {
		channels = 1
	}
	if len(pcm)%channels != 0 {
		return nil, errors.New("ffmpeg returned truncated multichannel pcm")
	}
	return deinterleave(pcm, channels), nil
}

func fetchAudioBytes(ctx context.Context, audioURL string) ([]byte, error) {
	switch {
	case strings.HasPrefix(audioURL, "data:"):
		return decodeDataURL(audioURL)
	case strings.HasPrefix(audioURL, "http://"), strings.HasPrefix(audioURL, "https://"):
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, audioURL, nil)
		if err != nil {
			return nil, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("download audio: %s", resp.Status)
		}
		return io.ReadAll(resp.Body)
	default:
		return nil, fmt.Errorf("unsupported audio URL scheme: %q", audioURL)
	}
}

func decodeDataURL(audioURL string) ([]byte, error) {
	payload := strings.TrimPrefix(audioURL, "data:")
	parts := strings.SplitN(payload, ",", 2)
	if len(parts) != 2 {
		return nil, errors.New("malformed data URL")
	}
	meta, data := parts[0], parts[1]
	if strings.Contains(meta, ";base64") {
		decoded, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return nil, fmt.Errorf("decode base64 data URL: %w", err)
		}
		return decoded, nil
	}
	return []byte(data), nil
}

func ffmpegBin() string {
	if v := strings.TrimSpace(os.Getenv("MUESLI_FFMPEG_BIN")); v != "" {
		return v
	}
	return "ffmpeg"
}
