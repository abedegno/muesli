package embedded

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildOllamaAgentCmdAndConfigJSON(t *testing.T) {
	t.Parallel()

	cmd := buildOllamaAgentCmd("/bin/ollama-agent", "127.0.0.1:42123", "secret-token", "http://127.0.0.1:11434", DefaultOllamaAgentModel, DefaultOllamaAgentTemperature)
	if got := filepath.Base(cmd.Path); got != "ollama-agent" {
		t.Fatalf("path base = %q, want ollama-agent", got)
	}

	wantArgs := []string{
		"/bin/ollama-agent",
		"--addr", "127.0.0.1:42123",
		"--token", "secret-token",
		"--ollama-url", "http://127.0.0.1:11434",
		"--model", DefaultOllamaAgentModel,
		"--temperature", "0.2",
		"--name", DefaultOllamaAgentName,
		"--version", DefaultOllamaAgentVersion,
	}
	if len(cmd.Args) != len(wantArgs) {
		t.Fatalf("args len = %d, want %d", len(cmd.Args), len(wantArgs))
	}
	for i, want := range wantArgs {
		if cmd.Args[i] != want {
			t.Fatalf("arg[%d] = %q, want %q", i, cmd.Args[i], want)
		}
	}

	env := strings.Join(cmd.Env, "\n")
	for _, want := range []string{
		"MUESLI_OLLAMA_AGENT_ADDR=127.0.0.1:42123",
		"MUESLI_OLLAMA_AGENT_TOKEN=secret-token",
		"MUESLI_OLLAMA_URL=http://127.0.0.1:11434",
		"MUESLI_OLLAMA_AGENT_MODEL=" + DefaultOllamaAgentModel,
		"MUESLI_OLLAMA_AGENT_TEMPERATURE=0.2",
		"MUESLI_OLLAMA_AGENT_NAME=" + DefaultOllamaAgentName,
		"MUESLI_OLLAMA_AGENT_VERSION=" + DefaultOllamaAgentVersion,
	} {
		if !strings.Contains(env, want) {
			t.Fatalf("env missing %q", want)
		}
	}

	cfg := agentConfigJSON("http://127.0.0.1:11434/", DefaultOllamaAgentModel, DefaultOllamaAgentTemperature)
	var got struct {
		Model       string  `json:"model"`
		OllamaURL   string  `json:"ollama_url"`
		Temperature float64 `json:"temperature"`
	}
	if err := json.Unmarshal([]byte(cfg), &got); err != nil {
		t.Fatalf("unmarshal config json: %v", err)
	}
	if got.Model != DefaultOllamaAgentModel || got.OllamaURL != "http://127.0.0.1:11434" || got.Temperature != DefaultOllamaAgentTemperature {
		t.Fatalf("config json = %+v", got)
	}
}

func TestLocateOllamaAgentBinaryOverride(t *testing.T) {
	tmpDir := t.TempDir()
	bin := filepath.Join(tmpDir, "ollama-agent")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write bin: %v", err)
	}

	old := os.Getenv("MUESLI_OLLAMA_AGENT_BIN")
	t.Cleanup(func() {
		_ = os.Setenv("MUESLI_OLLAMA_AGENT_BIN", old)
	})
	if err := os.Setenv("MUESLI_OLLAMA_AGENT_BIN", bin); err != nil {
		t.Fatalf("set env: %v", err)
	}

	got, err := locateOllamaAgentBinary()
	if err != nil {
		t.Fatalf("locate binary: %v", err)
	}
	if got != bin {
		t.Fatalf("binary = %q, want %q", got, bin)
	}
}

// newFakeAgentHandle spawns the current test binary re-invoked as
// TestHelperProcess (the standard os/exec test double pattern), standing in
// for the ollama-agent child process without touching the network or the real
// binary. mode selects the helper's behavior on SIGINT.
func newFakeAgentHandle(t *testing.T, mode string) (*AgentHandle, *exec.Cmd, <-chan struct{}) {
	t.Helper()

	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess", "--", mode)
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start fake process: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})

	// Block until the helper has installed its signal handling and reported
	// readiness, so the SIGINT sent below can never race the child's startup.
	reader := bufio.NewReader(stdout)
	line, err := reader.ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "ready" {
		t.Fatalf("helper process did not signal ready: line=%q err=%v", line, err)
	}

	handle := &AgentHandle{
		cmd:  cmd,
		done: make(chan error, 1),
	}
	exited := make(chan struct{})
	go func() {
		// Drain any remaining stdout so Wait doesn't block on pending pipe I/O.
		_, _ = io.Copy(io.Discard, reader)
	}()
	go func() {
		handle.done <- cmd.Wait()
		close(exited)
	}()

	return handle, cmd, exited
}

func TestAgentHandleStopSignalsThenExits(t *testing.T) {
	t.Parallel()

	handle, cmd, _ := newFakeAgentHandle(t, "exit-on-interrupt")

	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}
	if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
		t.Fatal("expected process to have exited cleanly after SIGINT")
	}

	// Calling Stop again must be a no-op (sync.Once) and return the same result.
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop() error: %v", err)
	}
}

func TestStopAgentStartupCleanupDoesNotHangForever(t *testing.T) {
	t.Parallel()

	handle, _, exited := newFakeAgentHandle(t, "ignore-interrupt")

	// stopAgentStartupCleanup must bound its Stop() call: this is the regression
	// this test guards against (it previously passed context.Background(),
	// which never cancels, to the startup-cleanup Stop call, so a child
	// that ignores SIGINT would hang server startup forever).
	cleanupDone := make(chan error, 1)
	go func() {
		cleanupDone <- stopAgentStartupCleanup(handle)
	}()

	assertCtx, assertCancel := context.WithTimeout(context.Background(), startupCleanupStopTimeout+3*time.Second)
	defer assertCancel()
	select {
	case err := <-cleanupDone:
		if err == nil {
			t.Fatal("stopAgentStartupCleanup() error = nil, want non-nil after killing an unresponsive child")
		}
	case <-assertCtx.Done():
		t.Fatal("stopAgentStartupCleanup did not return within a bounded time against an unresponsive child")
	}

	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("child process did not exit after stopAgentStartupCleanup's Kill fallback")
	}
}
