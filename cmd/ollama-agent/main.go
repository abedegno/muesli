package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/abedegno/muesli/cmd/ollama-agent/internal/agent"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := loadConfig()
	if err := run(ctx, cfg); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("ollama agent", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg agent.Config) error {
	if strings.TrimSpace(cfg.Token) == "" {
		return fmt.Errorf("MUESLI_OLLAMA_AGENT_TOKEN is required")
	}
	if strings.TrimSpace(cfg.Addr) == "" {
		return fmt.Errorf("listen address is required")
	}

	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: agent.New(cfg),
	}

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}
	defer ln.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err == nil || errorsIsServerClosed(err) {
			return nil
		}
		return err
	}
}

func loadConfig() agent.Config {
	// The agent accepts its bearer token from either MUESLI_OLLAMA_AGENT_TOKEN
	// or --token; the embedded launcher sets both so the process is easy to run
	// standalone and deterministic when spawned as a child.
	defaultAddr := envOrDefault("MUESLI_OLLAMA_AGENT_ADDR", "127.0.0.1:0")
	defaultToken := envOrDefault("MUESLI_OLLAMA_AGENT_TOKEN", "")
	defaultOllamaURL := envOrDefault("MUESLI_OLLAMA_URL", "http://127.0.0.1:11434")
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

	return agent.Config{
		Addr:        *addr,
		Token:       *token,
		OllamaURL:   *ollamaURL,
		Model:       *model,
		Temperature: *temperature,
		Name:        *name,
		Version:     *version,
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

func errorsIsServerClosed(err error) bool {
	return err == http.ErrServerClosed
}
