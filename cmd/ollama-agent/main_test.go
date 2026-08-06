package main

import (
	"flag"
	"os"
	"reflect"
	"testing"

	"github.com/abedegno/muesli/cmd/ollama-agent/internal/agent"
	"github.com/abedegno/muesli/internal/pluginkit"
)

var configEnvKeys = []string{
	"MUESLI_OLLAMA_AGENT_ADDR",
	"MUESLI_OLLAMA_AGENT_TOKEN",
	"MUESLI_OLLAMA_URL",
	"MUESLI_OLLAMA_AGENT_MODEL",
	"MUESLI_OLLAMA_AGENT_TEMPERATURE",
	"MUESLI_OLLAMA_AGENT_NAME",
	"MUESLI_OLLAMA_AGENT_VERSION",
}

func TestParseFloat(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		fallback float64
		want     float64
	}{
		{name: "valid float", value: "1.25", fallback: 9, want: 1.25},
		{name: "empty string", value: "", fallback: 9, want: 9},
		{name: "non-numeric junk", value: "not-a-number", fallback: 9, want: 9},
		{name: "surrounding whitespace", value: " 1.25 ", fallback: 9, want: 9},
		{name: "negative value", value: "-2.5", fallback: 9, want: -2.5},
		{name: "zero value", value: "0", fallback: 9, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseFloat(tt.value, tt.fallback); got != tt.want {
				t.Fatalf("parseFloat(%q, %v) = %v, want %v", tt.value, tt.fallback, got, tt.want)
			}
		})
	}
}

func TestEnvOrDefault(t *testing.T) {
	const key = "MUESLI_X"

	t.Run("set to normal value", func(t *testing.T) {
		t.Setenv(key, "configured")
		if got := envOrDefault(key, "fallback"); got != "configured" {
			t.Fatalf("envOrDefault(%q, %q) = %q, want %q", key, "fallback", got, "configured")
		}
	})

	t.Run("completely unset", func(t *testing.T) {
		unsetenv(t, key)
		if got := envOrDefault(key, "fallback"); got != "fallback" {
			t.Fatalf("envOrDefault(%q, %q) = %q, want %q", key, "fallback", got, "fallback")
		}
	})

	t.Run("set to empty string uses fallback", func(t *testing.T) {
		t.Setenv(key, "")
		if got := envOrDefault(key, "fallback"); got != "fallback" {
			t.Fatalf("envOrDefault(%q, %q) = %q, want %q", key, "fallback", got, "fallback")
		}
	})
}

func TestLoadConfigDefaults(t *testing.T) {
	prepareLoadConfig(t)

	gotPlugin, gotAgent := loadConfig()
	wantPlugin := pluginkit.Config{
		Name:         agent.DefaultName,
		Version:      agent.DefaultVersion,
		Kind:         "agent",
		Token:        "",
		Addr:         "127.0.0.1:0",
		ModelDir:     "",
		ConfigSchema: agent.ConfigSchema,
	}
	wantAgent := agent.Config{
		OllamaURL:   agent.DefaultURL,
		Model:       agent.DefaultModel,
		Temperature: 0.2,
	}

	if !reflect.DeepEqual(gotPlugin, wantPlugin) {
		t.Fatalf("loadConfig() plugin config = %#v, want %#v", gotPlugin, wantPlugin)
	}
	if !reflect.DeepEqual(gotAgent, wantAgent) {
		t.Fatalf("loadConfig() agent config = %#v, want %#v", gotAgent, wantAgent)
	}
}

func TestLoadConfigEnvironmentOverrides(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		check func(t *testing.T, pluginCfg pluginkit.Config, agentCfg agent.Config)
	}{
		{name: "address", key: "MUESLI_OLLAMA_AGENT_ADDR", value: "127.0.0.1:9876", check: func(t *testing.T, cfg pluginkit.Config, _ agent.Config) {
			if cfg.Addr != "127.0.0.1:9876" {
				t.Fatalf("Addr = %q, want %q", cfg.Addr, "127.0.0.1:9876")
			}
		}},
		{name: "token", key: "MUESLI_OLLAMA_AGENT_TOKEN", value: "secret-token", check: func(t *testing.T, cfg pluginkit.Config, _ agent.Config) {
			if cfg.Token != "secret-token" {
				t.Fatalf("Token = %q, want %q", cfg.Token, "secret-token")
			}
		}},
		{name: "Ollama URL", key: "MUESLI_OLLAMA_URL", value: "http://ollama.example:11434", check: func(t *testing.T, _ pluginkit.Config, cfg agent.Config) {
			if cfg.OllamaURL != "http://ollama.example:11434" {
				t.Fatalf("OllamaURL = %q, want %q", cfg.OllamaURL, "http://ollama.example:11434")
			}
		}},
		{name: "model", key: "MUESLI_OLLAMA_AGENT_MODEL", value: "test-model:latest", check: func(t *testing.T, _ pluginkit.Config, cfg agent.Config) {
			if cfg.Model != "test-model:latest" {
				t.Fatalf("Model = %q, want %q", cfg.Model, "test-model:latest")
			}
		}},
		{name: "temperature", key: "MUESLI_OLLAMA_AGENT_TEMPERATURE", value: "0.75", check: func(t *testing.T, _ pluginkit.Config, cfg agent.Config) {
			if cfg.Temperature != 0.75 {
				t.Fatalf("Temperature = %v, want %v", cfg.Temperature, 0.75)
			}
		}},
		{name: "name", key: "MUESLI_OLLAMA_AGENT_NAME", value: "custom-agent", check: func(t *testing.T, cfg pluginkit.Config, _ agent.Config) {
			if cfg.Name != "custom-agent" {
				t.Fatalf("Name = %q, want %q", cfg.Name, "custom-agent")
			}
		}},
		{name: "version", key: "MUESLI_OLLAMA_AGENT_VERSION", value: "9.8.7", check: func(t *testing.T, cfg pluginkit.Config, _ agent.Config) {
			if cfg.Version != "9.8.7" {
				t.Fatalf("Version = %q, want %q", cfg.Version, "9.8.7")
			}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prepareLoadConfig(t)
			t.Setenv(tt.key, tt.value)

			pluginCfg, agentCfg := loadConfig()
			tt.check(t, pluginCfg, agentCfg)
		})
	}
}

func prepareLoadConfig(t *testing.T) {
	t.Helper()
	for _, key := range configEnvKeys {
		unsetenv(t, key)
	}

	originalFlagSet := flag.CommandLine
	originalArgs := os.Args
	flag.CommandLine = flag.NewFlagSet(originalArgs[0], flag.ContinueOnError)
	os.Args = []string{originalArgs[0]}
	t.Cleanup(func() {
		flag.CommandLine = originalFlagSet
		os.Args = originalArgs
	})
}

func unsetenv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
}
