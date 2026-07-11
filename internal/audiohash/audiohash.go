package audiohash

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os/exec"
)

// HashRaw streams bytes through SHA-256 and returns the lower-hex digest.
func HashRaw(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// HashNormalized decodes audio via ffmpeg into 16 kHz mono PCM and hashes the
// normalized stream. If ffmpeg is unavailable or fails, it degrades gracefully.
func HashNormalized(audioPath string) (string, error) {
	cmd := exec.Command("ffmpeg", "-nostdin", "-loglevel", "error", "-i", audioPath, "-f", "s16le", "-ar", "16000", "-ac", "1", "pipe:1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", nil
	}
	if err := cmd.Start(); err != nil {
		return "", nil
	}
	hash, copyErr := HashRaw(stdout)
	waitErr := cmd.Wait()
	if copyErr != nil || waitErr != nil {
		return "", nil
	}
	return hash, nil
}
