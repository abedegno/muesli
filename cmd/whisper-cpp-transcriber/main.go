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

	"github.com/abedegno/muesli/internal/pluginkit"
	"github.com/abedegno/muesli/internal/whispercpp/engine"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	plugCfg, engCfg := loadConfig()
	if err := run(ctx, plugCfg, engine.New(engCfg)); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("whisper cpp transcriber", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg pluginkit.Config, eng pluginkit.Transcriber) error {
	if strings.TrimSpace(cfg.Token) == "" {
		return fmt.Errorf("MUESLI_WHISPER_TOKEN is required")
	}
	if strings.TrimSpace(cfg.Addr) == "" {
		return fmt.Errorf("listen address is required")
	}

	srv, ln, err := pluginkit.NewServer(cfg, pluginkit.TranscriberHandler(cfg, eng))
	if err != nil {
		return err
	}

	fmt.Printf("http://%s\n", ln.Addr().String())

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
	defaultAddr := envOrDefault("MUESLI_WHISPER_ADDR", "127.0.0.1:0")
	defaultToken := envOrDefault("MUESLI_WHISPER_TOKEN", "")
	defaultName := envOrDefault("MUESLI_WHISPER_NAME", engine.DefaultName)
	defaultVersion := envOrDefault("MUESLI_WHISPER_VERSION", engine.DefaultVersion)
	defaultModelDir := envOrDefault("MUESLI_WHISPER_MODEL_DIR", filepath.Join(os.TempDir(), "muesli-whisper-cpp-transcriber"))
	defaultModelURL := envOrDefault("MUESLI_WHISPER_MODEL_URL", "")
	defaultModel := envOrDefault("MUESLI_WHISPER_MODEL", engine.DefaultModel)
	defaultLanguage := envOrDefault("MUESLI_WHISPER_LANGUAGE", engine.DefaultLanguage)

	addr := flag.String("addr", defaultAddr, "listen address")
	token := flag.String("token", defaultToken, "bearer token required by the muesli server")
	name := flag.String("name", defaultName, "plugin name")
	version := flag.String("version", defaultVersion, "plugin version")
	modelDir := flag.String("model-dir", defaultModelDir, "directory for cached model downloads")
	modelURL := flag.String("model-url", defaultModelURL, "model download URL")
	model := flag.String("model", defaultModel, "reported model name")
	language := flag.String("language", defaultLanguage, "reported language code")
	flag.Parse()

	return pluginkit.Config{
			Name:         *name,
			Version:      *version,
			Kind:         "transcriber",
			Token:        *token,
			Addr:         *addr,
			ModelDir:     *modelDir,
			ConfigSchema: engine.ConfigSchema,
		}, engine.Config{
			ModelDir: *modelDir,
			ModelURL: *modelURL,
			Model:    *model,
			Language: *language,
		}
}

func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
