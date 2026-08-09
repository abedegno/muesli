package embedded

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	DefaultWhisperName             = "Embedded whisper.cpp transcriber"
	DefaultWhisperVersion          = "0.1.0"
	DefaultWhisperStreamingName    = "Embedded whisper.cpp streaming transcriber"
	DefaultWhisperStreamingVersion = "0.1.0"

	// startupCleanupStopTimeout bounds the Stop() call used to clean up a
	// child process that failed its readiness check during startup. It must
	// never be context.Background(): that never cancels, so a child that
	// ignores SIGINT would hang the cleanup call forever, leaking the
	// subprocess and blocking server startup indefinitely.
	startupCleanupStopTimeout = 5 * time.Second
)

// WhisperHandle represents a running whisper-cpp-transcriber child process
// spawned in --embedded mode. It mirrors AgentHandle's shape.
type WhisperHandle struct {
	EndpointURL string
	Token       string
	ConfigJSON  string

	cmd      *exec.Cmd
	done     chan error
	stopOnce sync.Once
	stopErr  error
}

func (h *WhisperHandle) Stop(ctx context.Context) error {
	if h == nil {
		return nil
	}
	h.stopOnce.Do(func() {
		if h.cmd != nil && h.cmd.Process != nil {
			_ = h.cmd.Process.Signal(os.Interrupt)
		}
		select {
		case h.stopErr = <-h.done:
		case <-ctx.Done():
			if h.cmd != nil && h.cmd.Process != nil {
				_ = h.cmd.Process.Kill()
			}
			killCtx, cancelKill := context.WithTimeout(ctx, killWait)
			defer cancelKill()
			select {
			case h.stopErr = <-h.done:
			case <-killCtx.Done():
				h.stopErr = ctx.Err()
			}
		}
	})
	return h.stopErr
}

// StartWhisperTranscriber launches the bundled whisper-cpp-transcriber binary
// as the desktop-default transcriber plugin for --embedded mode. Unlike
// StartOllamaAgent, no "is it installed" detection gate is needed: whisper.cpp
// is always spawned in embedded mode since it ships as the bundled desktop
// default.
func StartWhisperTranscriber(ctx context.Context) (*WhisperHandle, error) {
	port, err := FreeLoopbackPort()
	if err != nil {
		return nil, fmt.Errorf("find free loopback port: %w", err)
	}

	token, err := generateToken()
	if err != nil {
		return nil, fmt.Errorf("generate whisper transcriber token: %w", err)
	}

	binaryPath, err := locateWhisperCppTranscriberBinary()
	if err != nil {
		return nil, err
	}

	addr := net.JoinHostPort("127.0.0.1", fmt.Sprint(port))
	cmd := buildWhisperCppTranscriberCmd(binaryPath, addr, token)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start whisper-cpp-transcriber: %w", err)
	}

	handle := &WhisperHandle{
		EndpointURL: "http://" + addr,
		Token:       token,
		ConfigJSON:  whisperConfigJSON(),
		cmd:         cmd,
		done:        make(chan error, 1),
	}
	go func() {
		handle.done <- cmd.Wait()
	}()

	if err := waitForAgentReady(ctx, handle.EndpointURL); err != nil {
		if stopErr := stopStartupCleanup(handle); stopErr != nil {
			return nil, fmt.Errorf("wait for whisper-cpp-transcriber: %w (cleanup stop failed: %v)", err, stopErr)
		}
		return nil, fmt.Errorf("wait for whisper-cpp-transcriber: %w", err)
	}

	return handle, nil
}

// stopStartupCleanup stops a child process that failed its readiness check
// during StartWhisperTranscriber, bounded by startupCleanupStopTimeout so an
// unresponsive (e.g. SIGINT-ignoring) child can never hang server startup
// forever; Stop falls back to Kill once that bound elapses.
func stopStartupCleanup(h *WhisperHandle) error {
	stopCtx, cancel := context.WithTimeout(context.Background(), startupCleanupStopTimeout)
	defer cancel()
	return h.Stop(stopCtx)
}

func buildWhisperCppTranscriberCmd(binaryPath, addr, token string) *exec.Cmd {
	cmd := exec.Command(binaryPath,
		"--addr", addr,
		"--token", token,
		"--name", DefaultWhisperName,
		"--version", DefaultWhisperVersion,
	)
	cmd.Env = append(os.Environ(),
		"MUESLI_WHISPER_ADDR="+addr,
		"MUESLI_WHISPER_TOKEN="+token,
		"MUESLI_WHISPER_NAME="+DefaultWhisperName,
		"MUESLI_WHISPER_VERSION="+DefaultWhisperVersion,
	)
	for _, key := range []string{
		"MUESLI_WHISPER_MODEL_DIR",
		"MUESLI_WHISPER_MODEL_URL",
		"MUESLI_WHISPER_MODEL",
	} {
		if value, ok := os.LookupEnv(key); ok {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

func whisperConfigJSON() string {
	return `{"multitrack":true}`
}

func locateWhisperCppTranscriberBinary() (string, error) {
	if override := strings.TrimSpace(os.Getenv("MUESLI_WHISPER_CPP_TRANSCRIBER_BIN")); override != "" {
		if _, err := os.Stat(override); err == nil {
			return override, nil
		}
		return "", fmt.Errorf("MUESLI_WHISPER_CPP_TRANSCRIBER_BIN %q does not exist", override)
	}

	name := "whisper-cpp-transcriber"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	candidates := []string{}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(exeDir, name),
			filepath.Join(exeDir, "..", name),
			filepath.Join(exeDir, "..", "bin", name),
		)
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "bin", name),
			filepath.Join(wd, name),
		)
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", errors.New("could not locate whisper-cpp-transcriber binary; build bin/whisper-cpp-transcriber or set MUESLI_WHISPER_CPP_TRANSCRIBER_BIN")
}
