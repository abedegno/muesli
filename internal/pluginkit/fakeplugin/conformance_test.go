package main

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestFakePluginConformance(t *testing.T) {
	if os.Getenv("MUESLI_PLUGINKIT_CONFORMANCE") != "1" {
		t.Skip("pluginkit conformance disabled")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	if err := exec.Command("python3", "-c", "import muesli_plugin_conformance").Run(); err != nil {
		t.Skip("muesli_plugin_conformance not importable")
	}

	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "fakeplugin")
	build := exec.Command("go", "build", "-o", binPath, ".")
	build.Dir = "."
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fakeplugin: %v\n%s", err, out)
	}

	for _, kind := range []string{"transcriber", "agent"} {
		t.Run(kind, func(t *testing.T) {
			cmd := exec.Command(binPath, "--kind", kind, "--token", "tok")
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatalf("stdout pipe: %v", err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatalf("start fakeplugin: %v", err)
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
				t.Fatalf("timeout waiting for fakeplugin startup\n%s", stderr.String())
			}
			if !strings.HasPrefix(baseURL, "http://") {
				t.Fatalf("startup line = %q", baseURL)
			}

			py := exec.Command("python3", "-m", "muesli_plugin_conformance", baseURL, "--kind", kind, "--token", "tok")
			py.Env = os.Environ()
			py.Stderr = &stderr
			var pyOut bytes.Buffer
			py.Stdout = &pyOut
			if err := py.Run(); err != nil {
				t.Fatalf("conformance failed: %v\nstdout:\n%s\nstderr:\n%s", err, pyOut.String(), stderr.String())
			}
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatalf("signal fakeplugin: %v\nstderr:\n%s", err, stderr.String())
			}
			waitDone := make(chan error, 1)
			go func() { waitDone <- cmd.Wait() }()
			select {
			case waitErr := <-waitDone:
				if waitErr != nil {
					t.Fatalf("fakeplugin wait: %v\nstderr:\n%s", waitErr, stderr.String())
				}
			case <-time.After(10 * time.Second):
				t.Fatalf("timeout waiting for fakeplugin shutdown\nstderr:\n%s", stderr.String())
			}
			stopped = true
		})
	}
}
