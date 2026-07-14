package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/abedegno/muesli/cmd/ollama-agent/internal/agent"
	"github.com/abedegno/muesli/internal/pluginkit"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	plugCfg, engCfg := loadConfig()
	if err := run(ctx, plugCfg, agent.New(engCfg)); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("ollama agent", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg pluginkit.Config, eng pluginkit.Agent) error {
	if strings.TrimSpace(cfg.Token) == "" {
		return fmt.Errorf("MUESLI_OLLAMA_AGENT_TOKEN is required")
	}
	if strings.TrimSpace(cfg.Addr) == "" {
		return fmt.Errorf("listen address is required")
	}
	return pluginkit.ServeAgent(ctx, cfg, eng)
}

func loadConfig() (pluginkit.Config, agent.Config) {
	defaultAddr := envOrDefault("MUESLI_OLLAMA_AGENT_ADDR", "127.0.0.1:0")
	defaultToken := envOrDefault("MUESLI_OLLAMA_AGENT_TOKEN", "")
	defaultOllamaURL := envOrDefault("MUESLI_OLLAMA_URL", agent.DefaultURL)
	defaultModel := envOrDefault("MUESLI_OLLAMA_AGENT_MODEL", agent.DefaultModel)
	defaultTemperature := parseFloat(envOrDefault("MUESLI_OLLAMA_AGENT_TEMPERATURE", "0.2"), 0.2)
	defaultName := envOrDefault("MUESLI_OLLAMA_AGENT_NAME", agent.DefaultName)
	defaultVersion := envOrDefault("MUESLI_OLLAMA_AGENT_VERSION", agent.DefaultVersion)

	addr := flag.String("addr", defaultAddr, "listen address")
	token := flag.String("token", defaultToken, "bearer token required by the muesli server")
	ollamaURL := flag.String("ollama-url", defaultOllamaURL, "Ollama base URL")
	model := flag.String("model", defaultModel, "Ollama model name")
	temperature := flag.Float64("temperature", defaultTemperature, "sampling temperature")
	name := flag.String("name", defaultName, "plugin name")
	version := flag.String("version", defaultVersion, "plugin version")
	flag.Parse()

	return pluginkit.Config{
			Name:         *name,
			Version:      *version,
			Kind:         "agent",
			Token:        *token,
			Addr:         *addr,
			ModelDir:     "",
			ConfigSchema: agent.ConfigSchema,
		}, agent.Config{
			OllamaURL:   *ollamaURL,
			Model:       *model,
			Temperature: *temperature,
		}
}

func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func parseFloat(v string, fallback float64) float64 {
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return f
}
