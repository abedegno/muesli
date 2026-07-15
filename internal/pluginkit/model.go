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

// ResolveModel returns a usable local path to a model, preferring a PRE-BUNDLED
// file. If name is set and `<name>` (or `<name>.bin`) already exists in dir,
// that path is returned directly with NO download -- this is how the packaged
// desktop app loads its bundled ggml model when no MODEL_URL is set. Otherwise
// it falls back to EnsureModel (download + cache from modelURL). It errors when
// there is neither a bundled file nor a modelURL.
func ResolveModel(ctx context.Context, dir, name, modelURL string, onProgress func(done, total int64)) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", fmt.Errorf("pluginkit: model dir is required")
	}
	if name = strings.TrimSpace(name); name != "" {
		fname := name
		if !strings.HasSuffix(fname, ".bin") {
			fname += ".bin"
		}
		bundled := filepath.Join(dir, fname)
		if fi, err := os.Stat(bundled); err == nil && !fi.IsDir() {
			return bundled, nil
		}
	}
	if strings.TrimSpace(modelURL) == "" {
		return "", fmt.Errorf("pluginkit: no bundled model file for %q in %q and no model-url to download from", name, dir)
	}
	return EnsureModel(ctx, dir, modelURL, onProgress)
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
