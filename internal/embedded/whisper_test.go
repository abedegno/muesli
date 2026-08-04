package embedded

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildWhisperCppTranscriberCmd(t *testing.T) {
	t.Parallel()

	cmd := buildWhisperCppTranscriberCmd("/bin/whisper-cpp-transcriber", "127.0.0.1:42124", "secret-token")
	if got := filepath.Base(cmd.Path); got != "whisper-cpp-transcriber" {
		t.Fatalf("path base = %q, want whisper-cpp-transcriber", got)
	}

	wantArgs := []string{
		"/bin/whisper-cpp-transcriber",
		"--addr", "127.0.0.1:42124",
		"--token", "secret-token",
		"--name", DefaultWhisperName,
		"--version", DefaultWhisperVersion,
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
		"MUESLI_WHISPER_ADDR=127.0.0.1:42124",
		"MUESLI_WHISPER_TOKEN=secret-token",
		"MUESLI_WHISPER_NAME=" + DefaultWhisperName,
		"MUESLI_WHISPER_VERSION=" + DefaultWhisperVersion,
	} {
		if !strings.Contains(env, want) {
			t.Fatalf("env missing %q", want)
		}
	}

	if got := whisperConfigJSON(); got != `{"multitrack":true}` {
		t.Fatalf("whisperConfigJSON() = %q, want {\"multitrack\":true}", got)
	}
}

func TestBuildWhisperCppTranscriberCmdForwardsWhisperModelEnv(t *testing.T) {
	t.Run("forwarded when set", func(t *testing.T) {
		t.Setenv("MUESLI_WHISPER_MODEL_DIR", "/bundle/models")
		t.Setenv("MUESLI_WHISPER_MODEL_URL", "file:///bundle/models/model.bin")
		t.Setenv("MUESLI_WHISPER_MODEL", "model.bin")

		cmd := buildWhisperCppTranscriberCmd("/bin/whisper-cpp-transcriber", "127.0.0.1:42124", "secret-token")
		for _, want := range []string{
			"MUESLI_WHISPER_MODEL_DIR=/bundle/models",
			"MUESLI_WHISPER_MODEL_URL=file:///bundle/models/model.bin",
			"MUESLI_WHISPER_MODEL=model.bin",
		} {
			if !hasEnvEntry(cmd.Env, want) {
				t.Fatalf("env missing %q", want)
			}
		}
	})

	t.Run("omitted when unset", func(t *testing.T) {
		restore := clearWhisperModelEnv(t)
		defer restore()

		cmd := buildWhisperCppTranscriberCmd("/bin/whisper-cpp-transcriber", "127.0.0.1:42124", "secret-token")
		for _, key := range []string{
			"MUESLI_WHISPER_MODEL_DIR",
			"MUESLI_WHISPER_MODEL_URL",
			"MUESLI_WHISPER_MODEL",
		} {
			if hasEnvPrefix(cmd.Env, key+"=") {
				t.Fatalf("env unexpectedly contains %s", key)
			}
		}
	})
}

func TestLocateWhisperCppTranscriberBinaryOverride(t *testing.T) {
	tmpDir := t.TempDir()
	bin := filepath.Join(tmpDir, "whisper-cpp-transcriber")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write bin: %v", err)
	}

	old := os.Getenv("MUESLI_WHISPER_CPP_TRANSCRIBER_BIN")
	t.Cleanup(func() {
		_ = os.Setenv("MUESLI_WHISPER_CPP_TRANSCRIBER_BIN", old)
	})
	if err := os.Setenv("MUESLI_WHISPER_CPP_TRANSCRIBER_BIN", bin); err != nil {
		t.Fatalf("set env: %v", err)
	}

	got, err := locateWhisperCppTranscriberBinary()
	if err != nil {
		t.Fatalf("locate binary: %v", err)
	}
	if got != bin {
		t.Fatalf("binary = %q, want %q", got, bin)
	}
}

func TestLocateWhisperCppTranscriberBinaryOverrideMissing(t *testing.T) {
	old := os.Getenv("MUESLI_WHISPER_CPP_TRANSCRIBER_BIN")
	t.Cleanup(func() {
		_ = os.Setenv("MUESLI_WHISPER_CPP_TRANSCRIBER_BIN", old)
	})
	if err := os.Setenv("MUESLI_WHISPER_CPP_TRANSCRIBER_BIN", filepath.Join(t.TempDir(), "does-not-exist")); err != nil {
		t.Fatalf("set env: %v", err)
	}

	if _, err := locateWhisperCppTranscriberBinary(); err == nil {
		t.Fatal("locateWhisperCppTranscriberBinary() error = nil, want non-nil for missing override")
	}
}

func TestLocateWhisperCppTranscriberBinaryCandidateSearch(t *testing.T) {
	old := os.Getenv("MUESLI_WHISPER_CPP_TRANSCRIBER_BIN")
	t.Cleanup(func() {
		_ = os.Setenv("MUESLI_WHISPER_CPP_TRANSCRIBER_BIN", old)
	})
	if err := os.Unsetenv("MUESLI_WHISPER_CPP_TRANSCRIBER_BIN"); err != nil {
		t.Fatalf("unset env: %v", err)
	}

	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	bin := filepath.Join(binDir, "whisper-cpp-transcriber")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write bin: %v", err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWd)
	})

	got, err := locateWhisperCppTranscriberBinary()
	if err != nil {
		t.Fatalf("locate binary: %v", err)
	}
	if got != bin {
		t.Fatalf("binary = %q, want %q", got, bin)
	}
}

func clearWhisperModelEnv(t *testing.T) func() {
	t.Helper()

	keys := []string{
		"MUESLI_WHISPER_MODEL_DIR",
		"MUESLI_WHISPER_MODEL_URL",
		"MUESLI_WHISPER_MODEL",
	}
	restores := make([]func(), 0, len(keys))
	for _, key := range keys {
		old, had := os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
		if had {
			k, v := key, old
			restores = append(restores, func() {
				_ = os.Setenv(k, v)
			})
		}
	}
	return func() {
		for i := len(restores) - 1; i >= 0; i-- {
			restores[i]()
		}
	}
}

func hasEnvEntry(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}

func hasEnvPrefix(env []string, prefix string) bool {
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return true
		}
	}
	return false
}

// newFakeWhisperHandle spawns the current test binary re-invoked as
// TestHelperProcess (the standard os/exec test double pattern), standing in
// for the whisper-cpp-transcriber child process without touching the network
// or the real binary. mode selects the helper's behavior on SIGINT.
func newFakeWhisperHandle(t *testing.T, mode string) (*WhisperHandle, *exec.Cmd, <-chan struct{}) {
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

	handle := &WhisperHandle{
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

func TestWhisperHandleStopSignalsThenExits(t *testing.T) {
	t.Parallel()

	handle, cmd, _ := newFakeWhisperHandle(t, "exit-on-interrupt")

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

func TestWhisperHandleStopFallsBackToKillOnTimeout(t *testing.T) {
	t.Parallel()

	handle, _, exited := newFakeWhisperHandle(t, "ignore-interrupt")

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	if err := handle.Stop(ctx); err == nil {
		t.Fatal("Stop() error = nil, want non-nil after killing an unresponsive child")
	}

	// Stop() already drained handle.done internally (see the select in
	// Stop's stopOnce.Do). Wait, event-driven via close(exited) from the
	// same goroutine, to confirm the child actually exited via the Kill
	// fallback, instead of polling on a wall-clock deadline.
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer waitCancel()
	select {
	case <-exited:
	case <-waitCtx.Done():
		t.Fatal("child process did not exit after Kill fallback")
	}
}

func TestStopStartupCleanupDoesNotHangForever(t *testing.T) {
	t.Parallel()

	handle, _, exited := newFakeWhisperHandle(t, "ignore-interrupt")

	// stopStartupCleanup must bound its Stop() call: this is the regression
	// this test guards against (it previously passed context.Background(),
	// which never cancels, to the startup-cleanup Stop call, so a child
	// that ignores SIGINT would hang server startup forever).
	cleanupDone := make(chan error, 1)
	go func() {
		cleanupDone <- stopStartupCleanup(handle)
	}()

	assertCtx, assertCancel := context.WithTimeout(context.Background(), startupCleanupStopTimeout+3*time.Second)
	defer assertCancel()
	select {
	case err := <-cleanupDone:
		if err == nil {
			t.Fatal("stopStartupCleanup() error = nil, want non-nil after killing an unresponsive child")
		}
	case <-assertCtx.Done():
		t.Fatal("stopStartupCleanup did not return within a bounded time against an unresponsive child")
	}

	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("child process did not exit after stopStartupCleanup's Kill fallback")
	}
}

func TestWaitForAgentReadyTimeout(t *testing.T) {
	t.Parallel()

	// Nothing listens on this port, so /health can never succeed; the wait
	// should return a non-nil error once its deadline elapses.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := waitForAgentReady(ctx, "http://127.0.0.1:1"); err == nil {
		t.Fatal("waitForAgentReady() error = nil, want non-nil for unreachable endpoint")
	}
}

// TestHelperProcess is not a real test. It is re-executed as a subprocess by
// newFakeWhisperHandle (the standard os/exec test-double pattern; see
// https://pkg.go.dev/os/exec#Cmd for the technique's origin in Go's own
// exec/net/http tests) and does nothing when run normally via `go test`.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}
	if len(args) == 0 {
		os.Exit(2)
	}

	switch args[0] {
	case "exit-on-interrupt":
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt)
		fmt.Println("ready")
		<-ch
		os.Exit(0)
	case "ignore-interrupt":
		signal.Ignore(os.Interrupt)
		fmt.Println("ready")
		select {}
	default:
		os.Exit(2)
	}
}
