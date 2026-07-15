package embedded

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	DefaultOllamaAgentName        = "Embedded ollama agent"
	DefaultOllamaAgentVersion     = "0.1.0"
	DefaultOllamaAgentModel       = "llama3.2:3b"
	DefaultOllamaAgentTemperature = 0.2
)

type AgentHandle struct {
	EndpointURL string
	Token       string
	ConfigJSON  string

	cmd      *exec.Cmd
	done     chan error
	stopOnce sync.Once
	stopErr  error
}

func (h *AgentHandle) Stop(ctx context.Context) error {
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
			select {
			case h.stopErr = <-h.done:
			case <-time.After(2 * time.Second):
				h.stopErr = ctx.Err()
			}
		}
	})
	return h.stopErr
}

func StartOllamaAgent(ctx context.Context, ollamaURL string) (*AgentHandle, error) {
	ollamaURL = normalizeOllamaBaseURL(ollamaURL)
	if ollamaURL == "" {
		return nil, errors.New("empty ollama base url")
	}

	port, err := FreeLoopbackPort()
	if err != nil {
		return nil, fmt.Errorf("find free loopback port: %w", err)
	}

	token, err := generateToken()
	if err != nil {
		return nil, fmt.Errorf("generate agent token: %w", err)
	}

	binaryPath, err := locateOllamaAgentBinary()
	if err != nil {
		return nil, err
	}

	addr := net.JoinHostPort("127.0.0.1", fmt.Sprint(port))
	cmd := buildOllamaAgentCmd(binaryPath, addr, token, ollamaURL, DefaultOllamaAgentModel, DefaultOllamaAgentTemperature)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ollama agent: %w", err)
	}

	handle := &AgentHandle{
		EndpointURL: "http://" + addr,
		Token:       token,
		ConfigJSON:  agentConfigJSON(ollamaURL, DefaultOllamaAgentModel, DefaultOllamaAgentTemperature),
		cmd:         cmd,
		done:        make(chan error, 1),
	}
	go func() {
		handle.done <- cmd.Wait()
	}()

	if err := waitForAgentReady(ctx, handle.EndpointURL); err != nil {
		if stopErr := stopAgentStartupCleanup(handle); stopErr != nil {
			return nil, fmt.Errorf("wait for ollama agent: %w (cleanup stop failed: %v)", err, stopErr)
		}
		return nil, fmt.Errorf("wait for ollama agent: %w", err)
	}

	return handle, nil
}

func buildOllamaAgentCmd(binaryPath, addr, token, ollamaURL, model string, temperature float64) *exec.Cmd {
	cmd := exec.Command(binaryPath,
		"--addr", addr,
		"--token", token,
		"--ollama-url", ollamaURL,
		"--model", model,
		"--temperature", fmt.Sprintf("%g", temperature),
		"--name", DefaultOllamaAgentName,
		"--version", DefaultOllamaAgentVersion,
	)
	cmd.Env = append(os.Environ(),
		"MUESLI_OLLAMA_AGENT_ADDR="+addr,
		"MUESLI_OLLAMA_AGENT_TOKEN="+token,
		"MUESLI_OLLAMA_URL="+ollamaURL,
		"MUESLI_OLLAMA_AGENT_MODEL="+model,
		"MUESLI_OLLAMA_AGENT_TEMPERATURE="+fmt.Sprintf("%g", temperature),
		"MUESLI_OLLAMA_AGENT_NAME="+DefaultOllamaAgentName,
		"MUESLI_OLLAMA_AGENT_VERSION="+DefaultOllamaAgentVersion,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

func agentConfigJSON(ollamaURL, model string, temperature float64) string {
	cfg := struct {
		Model       string  `json:"model"`
		OllamaURL   string  `json:"ollama_url"`
		Temperature float64 `json:"temperature"`
	}{
		Model:       model,
		OllamaURL:   normalizeOllamaBaseURL(ollamaURL),
		Temperature: temperature,
	}
	b, _ := json.Marshal(cfg)
	return string(b)
}

// stopAgentStartupCleanup stops a child process that failed its readiness
// check during StartOllamaAgent, bounded by startupCleanupStopTimeout so an
// unresponsive (e.g. SIGINT-ignoring) child can never hang server startup
// forever; Stop falls back to Kill once that bound elapses. Mirrors
// stopStartupCleanup in whisper.go for WhisperHandle.
func stopAgentStartupCleanup(h *AgentHandle) error {
	stopCtx, cancel := context.WithTimeout(context.Background(), startupCleanupStopTimeout)
	defer cancel()
	return h.Stop(stopCtx)
}

func locateOllamaAgentBinary() (string, error) {
	if override := strings.TrimSpace(os.Getenv("MUESLI_OLLAMA_AGENT_BIN")); override != "" {
		if _, err := os.Stat(override); err == nil {
			return override, nil
		}
		return "", fmt.Errorf("MUESLI_OLLAMA_AGENT_BIN %q does not exist", override)
	}

	name := "ollama-agent"
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

	return "", errors.New("could not locate ollama-agent binary; build bin/ollama-agent or set MUESLI_OLLAMA_AGENT_BIN")
}

func waitForAgentReady(ctx context.Context, baseURL string) error {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadlineCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		req, err := http.NewRequestWithContext(deadlineCtx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/health", nil)
		if err == nil {
			resp, err := client.Do(req)
			if err == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					return nil
				}
			}
		}

		select {
		case <-deadlineCtx.Done():
			return deadlineCtx.Err()
		case <-ticker.C:
		}
	}
}

func generateToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
