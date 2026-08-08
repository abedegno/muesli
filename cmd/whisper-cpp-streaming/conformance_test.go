package main

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/testsupport"
)

func TestWhisperCppStreamingConformance(t *testing.T) {
	if os.Getenv("MUESLI_PLUGINKIT_CONFORMANCE") != "1" {
		t.Skip("pluginkit conformance disabled")
	}
	testsupport.RequireDependency(t, "python3", commandExists("python3"), "python3 not available")
	testsupport.RequireDependency(t, "muesli_plugin_conformance", exec.Command("python3", "-c", "import muesli_plugin_conformance").Run() == nil, "muesli_plugin_conformance not importable")

	binPath := filepath.Join(t.TempDir(), "whisper-cpp-streaming")
	build := exec.Command(goTool(), "build", "-o", binPath, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	cmd := exec.Command(binPath, "--token", "tok", "--addr", "127.0.0.1:0")
	cmd.Env = append(os.Environ(), "MUESLI_WHISPER_LIVE_MODEL=tiny.en")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()
	lineCh := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); lineCh <- strings.TrimSpace(line) }()
	var baseURL string
	select {
	case baseURL = <-lineCh:
	case <-time.After(20 * time.Second):
		t.Fatalf("startup timeout: %s", stderr.String())
	}
	check := exec.Command("python3", "-m", "muesli_plugin_conformance", baseURL, "--kind", "streaming-transcriber", "--token", "tok")
	if out, err := check.CombinedOutput(); err != nil {
		t.Fatalf("conformance: %v\n%s\n%s", err, out, stderr.String())
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("shutdown: %v: %s", err, stderr.String())
	}
	cmd.Process = nil
}

func commandExists(name string) bool { _, err := exec.LookPath(name); return err == nil }

func goTool() string {
	if root := runtime.GOROOT(); root != "" {
		path := filepath.Join(root, "bin", "go")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return "go"
}
