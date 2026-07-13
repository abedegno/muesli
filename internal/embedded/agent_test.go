package embedded

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
