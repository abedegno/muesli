package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/internal/live"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/pluginkit"
	"github.com/abedegno/muesli/internal/whispercpp/engine"
)

const (
	defaultName      = "muesli-whisper-cpp-streaming"
	defaultVersion   = "0.1.0"
	defaultTinyModel = "tiny.en"
	defaultTinyURL   = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-tiny.en.bin"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pluginCfg, engineCfg := loadConfig()
	if err := run(ctx, pluginCfg, live.New(engineCfg)); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("whisper cpp streaming", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg pluginkit.Config, eng pluginkit.StreamingTranscriber) error {
	if strings.TrimSpace(cfg.Token) == "" {
		return errors.New("MUESLI_WHISPER_LIVE_TOKEN is required")
	}
	if strings.TrimSpace(cfg.Addr) == "" {
		return errors.New("listen address is required")
	}
	srv, ln, err := pluginkit.NewServer(cfg, pluginkit.StreamingTranscriberHandler(cfg, eng))
	if err != nil {
		return err
	}
	fmt.Printf("http://%s\n", ln.Addr())
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func loadConfig() (pluginkit.Config, engine.Config) {
	batchModel := envOrDefault("MUESLI_WHISPER_MODEL", engine.DefaultModel)
	selectedModel, adaptive := selectLiveModel(batchModel, os.LookupEnv)
	batchDir := envOrDefault("MUESLI_WHISPER_MODEL_DIR", filepath.Join(os.TempDir(), "muesli-whisper-cpp-transcriber"))
	modelDir := envOrDefault("MUESLI_WHISPER_LIVE_MODEL_DIR", batchDir)
	modelURL := liveModelURL(selectedModel, adaptive)
	if explicit, ok := os.LookupEnv("MUESLI_WHISPER_LIVE_MODEL"); ok && strings.TrimSpace(explicit) != "" {
		// An explicit model may be operator-bundled. Never pair it with an
		// inferred URL for a different model.
		modelURL = ""
	}

	addr := flag.String("addr", envOrDefault("MUESLI_WHISPER_LIVE_ADDR", "127.0.0.1:0"), "listen address")
	token := flag.String("token", envOrDefault("MUESLI_WHISPER_LIVE_TOKEN", ""), "bearer token required by the muesli server")
	name := flag.String("name", envOrDefault("MUESLI_WHISPER_LIVE_NAME", defaultName), "plugin name")
	version := flag.String("version", envOrDefault("MUESLI_WHISPER_LIVE_VERSION", defaultVersion), "plugin version")
	modelDirFlag := flag.String("model-dir", modelDir, "directory for cached model downloads")
	modelURLFlag := flag.String("model-url", envOrDefault("MUESLI_WHISPER_LIVE_MODEL_URL", modelURL), "model download URL")
	modelFlag := flag.String("model", selectedModel, "live model name")
	language := flag.String("language", envOrDefault("MUESLI_WHISPER_LIVE_LANGUAGE", envOrDefault("MUESLI_WHISPER_LANGUAGE", engine.DefaultLanguage)), "language code")
	flag.Parse()

	return pluginkit.Config{Name: *name, Version: *version, Kind: model.PluginStreamingTranscriber, Token: *token, Addr: *addr, ModelDir: *modelDirFlag, ConfigSchema: live.ConfigSchema(engine.ConfigSchema)}, engine.Config{ModelDir: *modelDirFlag, ModelURL: *modelURLFlag, Model: *modelFlag, Language: *language}
}

func selectLiveModel(batch string, lookup func(string) (string, bool)) (string, bool) {
	if explicit, ok := lookup("MUESLI_WHISPER_LIVE_MODEL"); ok && strings.TrimSpace(explicit) != "" {
		return strings.TrimSpace(explicit), false
	}
	if fastModel(batch) {
		return batch, true
	}
	return defaultTinyModel, false
}

func fastModel(name string) bool {
	name = strings.ToLower(strings.TrimSpace(filepath.Base(name)))
	name = strings.TrimSuffix(name, ".bin")
	name = strings.TrimPrefix(name, "ggml-")
	return name == "tiny" || name == "tiny.en" || name == "base" || name == "base.en"
}

func liveModelURL(selected string, reusedBatch bool) string {
	if reusedBatch {
		return envOrDefault("MUESLI_WHISPER_MODEL_URL", "")
	}
	if fastModel(selected) {
		name := strings.TrimSuffix(strings.TrimPrefix(strings.ToLower(filepath.Base(selected)), "ggml-"), ".bin")
		return "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-" + name + ".bin"
	}
	return defaultTinyURL
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
