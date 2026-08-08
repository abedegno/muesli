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
)

// StartWhisperCppStreaming launches the bundled whisper-cpp-streaming binary
// as the desktop-default streaming transcriber plugin for --embedded mode.
func StartWhisperCppStreaming(ctx context.Context) (*WhisperHandle, error) {
	port, err := FreeLoopbackPort()
	if err != nil {
		return nil, fmt.Errorf("find free loopback port: %w", err)
	}

	token, err := generateToken()
	if err != nil {
		return nil, fmt.Errorf("generate whisper streaming token: %w", err)
	}

	binaryPath, err := locateWhisperCppStreamingBinary()
	if err != nil {
		return nil, err
	}

	addr := net.JoinHostPort("127.0.0.1", fmt.Sprint(port))
	cmd := buildWhisperCppStreamingCmd(binaryPath, addr, token)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start whisper-cpp-streaming: %w", err)
	}

	handle := &WhisperHandle{
		EndpointURL: "http://" + addr,
		Token:       token,
		ConfigJSON:  `{}`,
		cmd:         cmd,
		done:        make(chan error, 1),
	}
	go func() {
		handle.done <- cmd.Wait()
	}()

	if err := waitForAgentReady(ctx, handle.EndpointURL); err != nil {
		if stopErr := stopStartupCleanup(handle); stopErr != nil {
			return nil, fmt.Errorf("wait for whisper-cpp-streaming: %w (cleanup stop failed: %v)", err, stopErr)
		}
		return nil, fmt.Errorf("wait for whisper-cpp-streaming: %w", err)
	}

	return handle, nil
}

func buildWhisperCppStreamingCmd(binaryPath, addr, token string) *exec.Cmd {
	cmd := exec.Command(binaryPath,
		"--addr", addr,
		"--token", token,
		"--name", DefaultWhisperStreamingName,
		"--version", DefaultWhisperStreamingVersion,
	)
	cmd.Env = append(os.Environ(),
		"MUESLI_WHISPER_LIVE_ADDR="+addr,
		"MUESLI_WHISPER_LIVE_TOKEN="+token,
		"MUESLI_WHISPER_LIVE_NAME="+DefaultWhisperStreamingName,
		"MUESLI_WHISPER_LIVE_VERSION="+DefaultWhisperStreamingVersion,
	)
	for _, key := range []string{
		"MUESLI_WHISPER_LIVE_MODEL_DIR",
		"MUESLI_WHISPER_LIVE_MODEL_URL",
		"MUESLI_WHISPER_LIVE_MODEL",
		"MUESLI_WHISPER_LIVE_LANGUAGE",
	} {
		if value, ok := os.LookupEnv(key); ok {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

func locateWhisperCppStreamingBinary() (string, error) {
	if override := strings.TrimSpace(os.Getenv("MUESLI_WHISPER_CPP_STREAMING_BIN")); override != "" {
		if _, err := os.Stat(override); err == nil {
			return override, nil
		}
		return "", fmt.Errorf("MUESLI_WHISPER_CPP_STREAMING_BIN %q does not exist", override)
	}

	name := "whisper-cpp-streaming"
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

	return "", errors.New("could not locate whisper-cpp-streaming binary; build bin/whisper-cpp-streaming or set MUESLI_WHISPER_CPP_STREAMING_BIN")
}
