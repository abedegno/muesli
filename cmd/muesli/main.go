package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/backup"
	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/db"
	"github.com/abedegno/muesli/internal/embed"
	"github.com/abedegno/muesli/internal/embedded"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/storage"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/webhook"
	"github.com/abedegno/muesli/internal/worker"
)

func main() {
	// The desktop app owns the read ends of stdout and stderr. If it is killed,
	// writes during parent-death shutdown must fail with EPIPE rather than letting
	// SIGPIPE terminate the process before deferred child cleanup completes.
	signal.Ignore(syscall.SIGPIPE)

	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Parent-death cancels exactly what SIGTERM cancels, so there is one shutdown
	// path and no second timeout to keep in sync. Gated on MUESLI_PARENT_PID, which
	// only the desktop app sets — a standalone `muesli --embedded` gets no watch.
	ctx, cancelForParentDeath := context.WithCancel(sigCtx)
	defer cancelForParentDeath()
	if raw := strings.TrimSpace(os.Getenv("MUESLI_PARENT_PID")); raw != "" {
		if parentPID, convErr := strconv.Atoi(raw); convErr == nil && parentPID > 0 {
			go watchParentDeath(ctx, parentPID, time.Second, os.Getppid, func() {
				slog.Warn("parent process exited; shutting down", "parent_pid", parentPID)
				cancelForParentDeath()
			})
		}
	}

	cfg, err := config.FromEnv()
	if err != nil {
		slog.Error("config", "error", err)
		os.Exit(1)
	}
	var logLevel slog.LevelVar
	level, err := config.ParseLogLevel(cfg.LogLevel)
	if err != nil {
		level = slog.LevelInfo
	}
	logLevel.Set(level)
	handlerOpts := slog.HandlerOptions{Level: &logLevel}
	var handler slog.Handler
	switch cfg.LogFormat {
	case "json":
		handler = slog.NewJSONHandler(os.Stderr, &handlerOpts)
	default:
		handler = slog.NewTextHandler(os.Stderr, &handlerOpts)
	}
	slog.SetDefault(slog.New(handler))

	if len(os.Args) > 1 && os.Args[1] == "doctor" {
		os.Exit(runDoctor(ctx, cfg, os.Args[2:], os.Stdout))
	}

	// Warn loudly (or refuse to start) when known dev-only default secrets are
	// detected. The doctor subcommand runs before this so it can report on those
	// secrets instead of being blocked by startup policy.
	if warnings := config.DevSecretWarnings(cfg); len(warnings) > 0 {
		if cfg.Production {
			log.Fatalf("FATAL: running with dev-only default secrets in production mode: %s — set real secrets or unset MUESLI_PRODUCTION", strings.Join(warnings, ", "))
		}
		for _, w := range warnings {
			log.Printf("WARNING ⚠️  DEV SECRET IN USE: %s is set to a known dev-only default — replace before deploying", w)
		}
	}

	// `muesli reembed` — clear the current model's embeddings and enqueue a fresh
	// embed job for every ready note, then exit (don't start the server). Use after
	// changing MUESLI_EMBEDDINGS_MODEL or the task prefixes to force a full re-embed.
	if len(os.Args) > 1 && os.Args[1] == "reembed" {
		if err := runReembed(ctx, cfg, os.Args[2:], os.Stdout); err != nil {
			slog.Error("reembed", "error", err)
			os.Exit(1)
		}
		return
	}
	embeddedMode := isEmbeddedMode(cfg, os.Args)
	if embeddedMode && cfg.MasterKey == "" {
		// Embedded/desktop mode has no operator to set MUESLI_MASTER_KEY; manage
		// one automatically (generate + persist in the app data dir) so the key
		// requirement below is satisfied on first and subsequent launches.
		key, err := embedded.EnsureMasterKey()
		if err != nil {
			slog.Error("embedded master key", "error", err)
			os.Exit(1)
		}
		cfg.MasterKey = key
	}
	if embeddedMode && os.Getenv("MUESLI_STORAGE_DIR") == "" {
		storageDir, err := embedded.StorageDir()
		if err != nil {
			slog.Error("resolve embedded storage dir", "error", err)
			os.Exit(1)
		}
		cfg.StorageDir = storageDir
	}
	if err := config.RequireMasterKey(cfg); err != nil {
		slog.Error("config", "error", err)
		os.Exit(1)
	}
	if cfg.Production {
		if err := config.RequireProductionSecrets(cfg); err != nil {
			slog.Error("config", "error", err)
			os.Exit(1)
		}
	}
	cfg.Embedded = embeddedMode
	if err := run(ctx, cfg); err != nil {
		slog.Error("startup", "error", err)
		os.Exit(1)
	}
}

// run owns the embedded child-process lifecycle so deferred cleanup always
// executes before any fatal error escapes to main and can call os.Exit.
func run(ctx context.Context, cfg config.Config) (err error) {
	if cfg.Embedded {
		// #651: isolate embedded processes so the desktop supervisor can sweep
		// resource-leaking children after a server SIGKILL; the next launch's
		// Postgres reaper already protects correctness if that sweep is missed.
		if err := configureEmbeddedProcessGroup(); err != nil {
			return fmt.Errorf("configure embedded process group: %w", err)
		}
	}
	cr, err := crypto.New(cfg.MasterKey)
	if err != nil {
		slog.Error("master key", "error", err, "hint", "set MUESLI_MASTER_KEY to a base64 32-byte key")
		return fmt.Errorf("master key: %w", err)
	}
	var embeddedAgent *embedded.AgentHandle
	var embeddedWhisper *embedded.WhisperHandle
	var embeddedWhisperStreaming *embedded.WhisperHandle
	var completeEmbeddedStartup func()
	var startupReporter *embedded.Reporter
	ollamaURL := embedded.OllamaBaseURL()
	embeddedMode := cfg.Embedded
	if embeddedMode {
		startupReporter = embedded.NewReporter()
		databaseURL, embeddedStop, err := embedded.Start(ctx, startupReporter)
		if err != nil {
			slog.Error("embedded startup", "error", err)
			return fmt.Errorf("embedded startup: %w", err)
		}
		cfg.DatabaseURL = databaseURL
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if embeddedAgent != nil {
				if err := embeddedAgent.Stop(shutdownCtx); err != nil {
					slog.Error("embedded agent shutdown", "error", err)
				}
			}
			if embeddedWhisper != nil {
				if err := embeddedWhisper.Stop(shutdownCtx); err != nil {
					slog.Error("embedded whisper transcriber shutdown", "error", err)
				}
			}
			if embeddedWhisperStreaming != nil {
				if err := embeddedWhisperStreaming.Stop(shutdownCtx); err != nil {
					slog.Error("embedded whisper streaming transcriber shutdown", "error", err)
				}
			}
			if err := embeddedStop(shutdownCtx); err != nil {
				slog.Error("embedded shutdown", "error", err)
			}
		}()
		completeEmbeddedStartup, err = runEmbeddedStartupPhases(ctx, &cfg, startupReporter, ollamaURL, embeddedStartupHooks{
			migrate: db.Migrate,
			detect: func(ctx context.Context, url string) bool {
				return embedded.DetectOllamaWithRetry(ctx, url, embedded.DefaultOllamaDetectAttempts, embedded.DefaultOllamaDetectInterval)
			},
			configure: embedded.ConfigureEmbeddedOllama,
			pullEmbedding: func(pullCtx context.Context, url, model string, onProgress embedded.PullProgressFunc) error {
				if err := embedded.PullEmbeddingModel(pullCtx, url, model, onProgress); err != nil {
					slog.Warn("ollama pull embedding model", "error", err, "model", model, "url", url)
					return err
				}
				return nil
			},
			pullModel: func(pullCtx context.Context, url, model string, onProgress embedded.PullProgressFunc) error {
				if err := embedded.PullModel(pullCtx, url, model, onProgress); err != nil {
					slog.Warn("ollama pull agent model", "error", err, "model", model, "url", url)
					return err
				}
				return nil
			},
		})
		if err != nil {
			slog.Error("embedded startup", "error", err)
			return fmt.Errorf("embedded startup: %w", err)
		}
	}
	if !embeddedMode {
		if err := db.Migrate(cfg.DatabaseURL); err != nil {
			slog.Error("migrate", "error", err)
			return fmt.Errorf("migrate: %w", err)
		}
	}
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("db connect", "error", err)
		return fmt.Errorf("db connect: %w", err)
	}
	defer pool.Close()

	st := store.New(pool)
	if err := st.SeedBuiltInTemplates(ctx); err != nil {
		slog.Error("seed templates", "error", err)
		return fmt.Errorf("seed templates: %w", err)
	}

	// Auto-register default plugins from config so a fresh deployment works
	// without manual admin steps. Each kind is registered only when both its
	// URL and token are set; re-running is idempotent.
	if cfg.DefaultTranscriberURL != "" && cfg.DefaultTranscriberToken != "" {
		if err := st.EnsureDefaultPlugin(ctx, cr, model.PluginTranscriber, "Default transcriber",
			cfg.DefaultTranscriberURL, cfg.DefaultTranscriberToken, cfg.DefaultTranscriberConfig); err != nil {
			slog.Error("register default transcriber", "error", err)
			return fmt.Errorf("register default transcriber: %w", err)
		}
		slog.Info("registered default transcriber plugin", "url", cfg.DefaultTranscriberURL)
	}
	if cfg.DefaultStreamingTranscriberURL != "" && cfg.DefaultStreamingTranscriberToken != "" {
		if err := st.EnsureDefaultPlugin(ctx, cr, model.PluginStreamingTranscriber, "Default streaming transcriber",
			cfg.DefaultStreamingTranscriberURL, cfg.DefaultStreamingTranscriberToken, cfg.DefaultStreamingTranscriberConfig); err != nil {
			slog.Error("register default streaming transcriber", "error", err)
			return fmt.Errorf("register default streaming transcriber: %w", err)
		}
		slog.Info("registered default streaming transcriber plugin", "url", cfg.DefaultStreamingTranscriberURL)
	}
	if cfg.DefaultAgentURL != "" && cfg.DefaultAgentToken != "" {
		if err := st.EnsureDefaultPlugin(ctx, cr, model.PluginAgent, "Default agent",
			cfg.DefaultAgentURL, cfg.DefaultAgentToken, cfg.DefaultAgentConfig); err != nil {
			slog.Error("register default agent", "error", err)
			return fmt.Errorf("register default agent: %w", err)
		}
		slog.Info("registered default agent plugin", "url", cfg.DefaultAgentURL)
	}
	// Derive a stable signing key for presigned upload URLs. Prefer the
	// dedicated config value; otherwise fall back to deriving one from the
	// master key. If neither is set, refuse to start so signatures can't
	// silently change between restarts/replicas.
	signingKey := storageSigningKey(cfg)
	if len(signingKey) == 0 {
		slog.Error("no storage signing key", "hint", "set MUESLI_STORAGE_SIGNING_KEY (or MUESLI_MASTER_KEY)")
		return fmt.Errorf("no storage signing key")
	}
	prov, err := storage.NewLocal(cfg.StorageDir, cfg.PublicURL, cfg.InternalURL, signingKey)
	if err != nil {
		slog.Error("storage", "error", err)
		return fmt.Errorf("storage: %w", err)
	}
	// HRD01: MUESLI_UPLOAD_ALLOWED_CONTENT_TYPES overrides the audio
	// Content-Type allowlist enforced on the upload PUT path; empty leaves the
	// storage.DefaultAllowedContentTypes default in place.
	if len(cfg.UploadAllowedContentTypes) > 0 {
		prov.SetAllowedContentTypes(cfg.UploadAllowedContentTypes)
	}

	emb := embed.New(cfg)
	proc := worker.NewProcessor(st, cr, prov, cfg, emb)
	wpool := worker.NewPool(st, proc, 4)
	go wpool.Run(ctx)
	go worker.RunTrashPurger(ctx, st, prov, time.Duration(cfg.TrashRetentionDays)*24*time.Hour)
	go worker.RunDigestScheduler(ctx, st, time.Hour)
	go worker.StartCalendarScheduler(ctx, st, cr, cfg.GoogleOAuthClientID, cfg.GoogleOAuthClientSecret, cfg.MicrosoftOAuthClientID, cfg.MicrosoftOAuthClientSecret)
	defer wpool.Stop()

	// BAK01: scheduled Postgres backups, only started when both a backup dir
	// and a schedule interval are configured; the admin "backup now" endpoint
	// (POST /api/admin/backup) works independently of this scheduler.
	if cfg.BackupDir != "" && cfg.BackupScheduleInterval > 0 {
		go worker.RunBackupScheduler(ctx, backup.PgDumpRunner{}, cfg.DatabaseURL, cfg.BackupDir, cfg.BackupRetentionCount, cfg.BackupScheduleInterval)
		slog.Info("scheduled backups enabled", "dir", cfg.BackupDir, "interval", cfg.BackupScheduleInterval, "retention_count", cfg.BackupRetentionCount)
	}

	whWorker := webhook.NewDeliveryWorker(st, 5*time.Second)
	go func() {
		if err := whWorker.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("webhook delivery worker error: %v", err)
		}
	}()

	// When embeddings are enabled, backfill once on startup for ready notes that
	// don't have a vector yet. Best-effort and logged; failures don't block boot.
	if emb != nil {
		go func() {
			if _, err := worker.EnqueueBackfillEmbeds(ctx, st, cfg.EmbeddingsModel, emb.Dim(), cfg.EmbedBackfillBatchSize); err != nil {
				slog.Warn("embed backfill", "error", err)
			}
		}()
	}

	if cfg.EmbeddedOllamaDetected {
		var err error
		embeddedAgent, err = embedded.StartOllamaAgent(ctx, ollamaURL)
		if err != nil {
			slog.Error("start embedded ollama agent", "error", err)
			return fmt.Errorf("start embedded ollama agent: %w", err)
		}
		// Embedded Ollama takes precedence over any hosted default agent so the
		// desktop-first path stays local when Ollama is available.
		if err := st.EnsureDefaultPlugin(ctx, cr, model.PluginAgent, embedded.DefaultOllamaAgentName,
			embeddedAgent.EndpointURL, embeddedAgent.Token, embeddedAgent.ConfigJSON); err != nil {
			slog.Error("register embedded ollama agent", "error", err)
			return fmt.Errorf("register embedded ollama agent: %w", err)
		}
		slog.Info("registered embedded ollama agent plugin", "url", embeddedAgent.EndpointURL)
		applyEmbeddedAgentDefault(&cfg, embeddedAgent.EndpointURL)
	}

	if embeddedMode {
		var err error
		embeddedWhisper, err = embedded.StartWhisperTranscriber(ctx)
		if err != nil {
			slog.Error("start embedded whisper transcriber", "error", err)
			return fmt.Errorf("start embedded whisper transcriber: %w", err)
		}
		// The bundled whisper.cpp binary is the desktop default transcriber in
		// --embedded mode, mirroring how the embedded Ollama agent above is the
		// desktop default local agent: it takes precedence over any hosted
		// default configured via MUESLI_DEFAULT_TRANSCRIBER_URL/_TOKEN. Swap in
		// a different transcriber (e.g. the Python plugins/whisper-transcriber
		// reference) via the admin UI/API after boot.
		if err := st.EnsureDefaultPlugin(ctx, cr, model.PluginTranscriber, embedded.DefaultWhisperName,
			embeddedWhisper.EndpointURL, embeddedWhisper.Token, embeddedWhisper.ConfigJSON); err != nil {
			slog.Error("register embedded whisper transcriber", "error", err)
			return fmt.Errorf("register embedded whisper transcriber: %w", err)
		}
		slog.Info("registered embedded whisper transcriber plugin", "url", embeddedWhisper.EndpointURL)
		applyEmbeddedTranscriberDefault(&cfg, embeddedWhisper.EndpointURL)

		embeddedWhisperStreaming, err = embedded.StartWhisperCppStreaming(ctx)
		if err != nil {
			slog.Error("start embedded whisper streaming transcriber", "error", err)
			return fmt.Errorf("start embedded whisper streaming transcriber: %w", err)
		}
		// The bundled whisper.cpp streaming binary is the desktop default in
		// --embedded mode and takes precedence over any hosted default configured
		// via MUESLI_DEFAULT_STREAMING_TRANSCRIBER_URL/_TOKEN. Swap in a different
		// streaming transcriber via Settings or the admin UI/API after boot.
		if err := st.EnsureDefaultPlugin(ctx, cr, model.PluginStreamingTranscriber, embedded.DefaultWhisperStreamingName,
			embeddedWhisperStreaming.EndpointURL, embeddedWhisperStreaming.Token, embeddedWhisperStreaming.ConfigJSON); err != nil {
			slog.Error("register embedded whisper streaming transcriber", "error", err)
			return fmt.Errorf("register embedded whisper streaming transcriber: %w", err)
		}
		slog.Info("registered embedded whisper streaming transcriber plugin", "url", embeddedWhisperStreaming.EndpointURL)
	}

	srv := api.NewServer(api.Deps{Store: st, Storage: prov, Crypto: cr, Worker: wpool, Config: cfg, Embedder: emb, BackupRunner: backup.PgDumpRunner{}, EmbeddedProgress: startupReporter})
	slog.Info("muesli listening", "addr", cfg.Addr)
	fmt.Print(readyBanner(cfg.PublicURL))
	if completeEmbeddedStartup != nil {
		go completeEmbeddedStartup()
	}
	if err := srv.Run(ctx, cfg.Addr); err != nil {
		slog.Error("server error", "error", err)
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

// storageSigningKey derives a stable HMAC key for presigned upload URLs.
// Preference order: the dedicated MUESLI_STORAGE_SIGNING_KEY, then a key
// derived from MUESLI_MASTER_KEY. Returns nil if neither is set.
func storageSigningKey(cfg config.Config) []byte {
	if k := strings.TrimSpace(cfg.StorageSigningKey); k != "" {
		// Hash to a fixed 32-byte key so any reasonable input is accepted
		// and the same string always yields the same key.
		sum := sha256.Sum256([]byte("muesli-storage-signing:" + k))
		return sum[:]
	}
	if mk := strings.TrimSpace(cfg.MasterKey); mk != "" {
		sum := sha256.Sum256([]byte("muesli-storage-signing:" + mk))
		return sum[:]
	}
	return nil
}

func isEmbeddedMode(cfg config.Config, args []string) bool {
	if cfg.Embedded {
		return true
	}
	for _, arg := range args {
		if arg == "--embedded" {
			return true
		}
	}
	return false
}
