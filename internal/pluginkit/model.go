package pluginkit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// EnsureModel downloads a model into dir once and returns the cached path on
// later calls for the same URL.
func EnsureModel(ctx context.Context, dir, modelURL string, onProgress func(done, total int64)) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	target := cachedModelPath(dir, modelURL)
	if _, err := os.Stat(target); err == nil {
		return target, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download model: %s", resp.Status)
	}

	tmp, err := os.CreateTemp(dir, ".model-*.tmp")
	if err != nil {
		return "", err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	var written int64
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := tmp.Write(buf[:n]); err != nil {
				return "", err
			}
			written += int64(n)
			if onProgress != nil {
				onProgress(written, resp.ContentLength)
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return "", readErr
		}
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return "", err
	}
	if onProgress != nil && written == 0 {
		onProgress(0, resp.ContentLength)
	}
	return target, nil
}

func cachedModelPath(dir, modelURL string) string {
	sum := sha256.Sum256([]byte(modelURL))
	name := hex.EncodeToString(sum[:])
	if u, err := url.Parse(modelURL); err == nil {
		ext := filepath.Ext(u.Path)
		if ext != "" && len(ext) <= 16 {
			name += ext
		}
	}
	return filepath.Join(dir, strings.TrimSpace(name))
}
