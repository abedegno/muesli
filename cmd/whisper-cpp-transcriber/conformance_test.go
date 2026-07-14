package main

import (
	"bufio"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestWhisperCppTranscriberConformance(t *testing.T) {
	if os.Getenv("MUESLI_PLUGINKIT_CONFORMANCE") != "1" {
		t.Skip("pluginkit conformance disabled")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	if err := exec.Command("python3", "-c", "import muesli_plugin_conformance").Run(); err != nil {
		t.Skip("muesli_plugin_conformance not importable")
	}

	var modelHits atomic.Int32
	modelSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modelHits.Add(1)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("stub-model"))
	}))
	defer modelSrv.Close()

	ffmpegDir := t.TempDir()
	ffmpegPath := filepath.Join(ffmpegDir, "ffmpeg")
	ffmpegScript := `#!/usr/bin/env python3
import struct
import sys

_ = sys.stdin.buffer.read()
sys.stdout.buffer.write(struct.pack("<f", 0.0) * 16000)
`
	if err := os.WriteFile(ffmpegPath, []byte(ffmpegScript), 0o755); err != nil {
		t.Fatalf("write ffmpeg stub: %v", err)
	}

	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "whisper-cpp-transcriber")
	build := exec.Command(goTool(), "build", "-o", binPath, ".")
	build.Dir = "."
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build whisper-cpp-transcriber: %v\n%s", err, out)
	}

	cmd := exec.Command(binPath, "--token", "tok", "--addr", "127.0.0.1:0")
	cmd.Env = replaceEnvPath(os.Environ(), ffmpegDir)
	cmd.Env = append(cmd.Env,
		"MUESLI_WHISPER_MODEL_DIR="+t.TempDir(),
		"MUESLI_WHISPER_MODEL_URL="+modelSrv.URL+"/model.bin",
		"MUESLI_WHISPER_MODEL=stub-transcriber",
		"MUESLI_WHISPER_LANGUAGE=en",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start whisper-cpp-transcriber: %v", err)
	}
	stopped := false
	defer func() {
		if stopped || cmd.Process == nil {
			return
		}
		_ = cmd.Process.Signal(syscall.SIGTERM)
		waitDone := make(chan error, 1)
		go func() { waitDone <- cmd.Wait() }()
		select {
		case <-waitDone:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
		}
	}()

	reader := bufio.NewReader(stdout)
	lineCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			errCh <- readErr
			return
		}
		lineCh <- strings.TrimSpace(line)
	}()

	var baseURL string
	select {
	case line := <-lineCh:
		baseURL = strings.TrimSpace(line)
	case err := <-errCh:
		t.Fatalf("read startup line: %v\n%s", err, stderr.String())
	case <-time.After(20 * time.Second):
		t.Fatalf("timeout waiting for whisper-cpp-transcriber startup\n%s", stderr.String())
	}
	if !strings.HasPrefix(baseURL, "http://") {
		t.Fatalf("startup line = %q", baseURL)
	}

	py := exec.Command("python3", "-m", "muesli_plugin_conformance", baseURL, "--kind", "transcriber", "--token", "tok")
	py.Env = os.Environ()
	py.Stderr = &stderr
	var pyOut bytes.Buffer
	py.Stdout = &pyOut
	if err := py.Run(); err != nil {
		t.Fatalf("conformance failed: %v\nstdout:\n%s\nstderr:\n%s", err, pyOut.String(), stderr.String())
	}
	if modelHits.Load() == 0 {
		t.Fatalf("expected model download to be exercised\nstderr:\n%s", stderr.String())
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal whisper-cpp-transcriber: %v\nstderr:\n%s", err, stderr.String())
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	select {
	case waitErr := <-waitDone:
		if waitErr != nil {
			t.Fatalf("whisper-cpp-transcriber wait: %v\nstderr:\n%s", waitErr, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("timeout waiting for whisper-cpp-transcriber shutdown\nstderr:\n%s", stderr.String())
	}
	stopped = true
}

func replaceEnvPath(env []string, dir string) []string {
	out := make([]string, 0, len(env)+1)
	replaced := false
	prefix := "PATH="
	sep := string(os.PathListSeparator)
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			out = append(out, prefix+dir+sep+strings.TrimPrefix(kv, prefix))
			replaced = true
			continue
		}
		out = append(out, kv)
	}
	if !replaced {
		out = append(out, prefix+dir+sep+os.Getenv("PATH"))
	}
	return out
}

func goTool() string {
	if root := runtime.GOROOT(); root != "" {
		if path := filepath.Join(root, "bin", "go"); fileExists(path) {
			return path
		}
	}
	return "go"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
