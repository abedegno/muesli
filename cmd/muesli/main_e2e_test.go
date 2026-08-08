package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEmbeddedMainE2E(t *testing.T) {
	if os.Getenv("MUESLI_EMBEDDED_IT") == "" || os.Getenv("MUESLI_EMBEDDED_PGVECTOR_DIR") == "" {
		t.Skip("set MUESLI_EMBEDDED_IT and MUESLI_EMBEDDED_PGVECTOR_DIR to run embedded main e2e test")
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error: %v", err)
	}

	bin := filepath.Join(t.TempDir(), "muesli")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = cwd
	buildOut, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed: %v\n%s", err, buildOut)
	}

	// The embedded server always spawns whisper-cpp-transcriber as the
	// desktop-default transcriber plugin now, so build it too (untagged,
	// no cgo needed) and point the embedded locate step at it via the same
	// override mechanism MUESLI_OLLAMA_AGENT_BIN uses.
	whisperBin := filepath.Join(t.TempDir(), "whisper-cpp-transcriber")
	buildWhisper := exec.Command("go", "build", "-o", whisperBin, "github.com/abedegno/muesli/cmd/whisper-cpp-transcriber")
	buildWhisper.Dir = cwd
	buildWhisperOut, err := buildWhisper.CombinedOutput()
	if err != nil {
		t.Fatalf("go build whisper-cpp-transcriber failed: %v\n%s", err, buildWhisperOut)
	}

	// The embedded server also always spawns whisper-cpp-streaming as the
	// desktop-default streaming transcriber plugin. Build its untagged stub and
	// point the embedded locate step at it just like the batch transcriber.
	whisperStreamingBin := filepath.Join(t.TempDir(), "whisper-cpp-streaming")
	buildWhisperStreaming := exec.Command("go", "build", "-o", whisperStreamingBin, "github.com/abedegno/muesli/cmd/whisper-cpp-streaming")
	buildWhisperStreaming.Dir = cwd
	buildWhisperStreamingOut, err := buildWhisperStreaming.CombinedOutput()
	if err != nil {
		t.Fatalf("go build whisper-cpp-streaming failed: %v\n%s", err, buildWhisperStreamingOut)
	}

	addr := freeLoopbackAddress(t)
	root := t.TempDir()
	appDataDir := filepath.Join(root, "appdata")
	storageDir := filepath.Join(root, "storage")
	dbURL := "postgres://placeholder?sslmode=disable"
	masterKey := mustBase64Key(t)

	env := mergedEnv(os.Environ(), map[string]string{
		"DATABASE_URL":                       dbURL,
		"MUESLI_ADDR":                        addr,
		"MUESLI_APPDATA":                     appDataDir,
		"MUESLI_MODE":                        "",
		"MUESLI_MASTER_KEY":                  masterKey,
		"MUESLI_PUBLIC_URL":                  "http://" + addr,
		"MUESLI_STORAGE_DIR":                 storageDir,
		"MUESLI_EMBEDDED_PGVECTOR_DIR":       os.Getenv("MUESLI_EMBEDDED_PGVECTOR_DIR"),
		"MUESLI_WHISPER_CPP_TRANSCRIBER_BIN": whisperBin,
		"MUESLI_WHISPER_CPP_STREAMING_BIN":   whisperStreamingBin,
	})

	cmd := exec.Command(bin, "--embedded")
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start muesli: %v", err)
	}

	client := &http.Client{Timeout: 500 * time.Millisecond}
	healthzURL := "http://" + addr + "/healthz"
	if err := waitForHTTP(t, client, healthzURL, 45*time.Second); err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("wait for /healthz: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}

	if err := postSetup(t, client, "http://"+addr+"/api/setup"); err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("POST /api/setup: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("signal interrupt: %v", err)
	}

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	select {
	case err := <-waitErr:
		if err != nil {
			t.Fatalf("muesli exited with error: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
		}
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("muesli did not exit after interrupt\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}

	pidFile := filepath.Join(appDataDir, "postgres", "data", "postmaster.pid")
	if _, err := os.Stat(pidFile); err == nil {
		t.Fatalf("postmaster pid file still exists after shutdown: %s", pidFile)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat postmaster pid file: %v", err)
	}

	if err := waitForPortClose(addr, 10*time.Second); err != nil {
		t.Fatalf("postgres port still open after shutdown: %v", err)
	}
}

func freeLoopbackAddress(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error: %v", err)
	}
	defer ln.Close()

	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address is %T, want *net.TCPAddr", ln.Addr())
	}
	return fmt.Sprintf("127.0.0.1:%d", tcpAddr.Port)
}

func mustBase64Key(t *testing.T) string {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand.Read() error: %v", err)
	}
	return base64.StdEncoding.EncodeToString(key)
}

func mergedEnv(base []string, overrides map[string]string) []string {
	env := make(map[string]string, len(base)+len(overrides))
	for _, kv := range base {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		env[k] = v
	}
	for k, v := range overrides {
		env[k] = v
	}

	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

func waitForHTTP(t *testing.T, client *http.Client, url string, deadline time.Duration) error {
	t.Helper()
	expire := time.Now().Add(deadline)
	var lastErr error
	for time.Now().Before(expire) {
		resp, err := client.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			return nil
		}
		if resp != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("unexpected status %s", resp.Status)
		}
		time.Sleep(200 * time.Millisecond)
	}
	return lastErr
}

func postSetup(t *testing.T, client *http.Client, url string) error {
	t.Helper()
	body, err := json.Marshal(map[string]string{
		"email":    "embedded@example.com",
		"password": "password123",
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("status %s", resp.Status)
	}
	return nil
}

func waitForPortClose(addr string, deadline time.Duration) error {
	expire := time.Now().Add(deadline)
	for time.Now().Before(expire) {
		conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
		if err != nil {
			return nil
		}
		_ = conn.Close()
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("still accepting connections on %s", addr)
}
