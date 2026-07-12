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
	"strings"
	"syscall"
	"time"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/backup"
	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/db"
	"github.com/abedegno/muesli/internal/embed"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/storage"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/webhook"
	"github.com/abedegno/muesli/internal/worker"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.FromEnv()
	if err != nil {
		slog.Error("config", "error", err)
		os.Exit(1)
	}

	// Warn loudly (or refuse to start) when known dev-only default secrets are
	// detected. This runs before any subcommand check so all code paths are covered.
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
	if err := config.RequireMasterKey(cfg); err != nil {
		slog.Error("config", "error", err)
		os.Exit(1)
	}
	cr, err := crypto.New(cfg.MasterKey)
	if err != nil {
		slog.Error("master key", "error", err, "hint", "set MUESLI_MASTER_KEY to a base64 32-byte key")
		os.Exit(1)
	}
	if err := db.Migrate(cfg.DatabaseURL); err != nil {
		slog.Error("migrate", "error", err)
		os.Exit(1)
	}
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("db connect", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	st := store.New(pool)
	if err := st.SeedBuiltInTemplates(ctx); err != nil {
		slog.Error("seed templates", "error", err)
		os.Exit(1)
	}

	// Auto-register default plugins from config so a fresh deployment works
	// without manual admin steps. Each kind is registered only when both its
	// URL and token are set; re-running is idempotent.
	if cfg.DefaultTranscriberURL != "" && cfg.DefaultTranscriberToken != "" {
		if err := st.EnsureDefaultPlugin(ctx, cr, model.PluginTranscriber, "Default transcriber",
			cfg.DefaultTranscriberURL, cfg.DefaultTranscriberToken, cfg.DefaultTranscriberConfig); err != nil {
			slog.Error("register default transcriber", "error", err)
			os.Exit(1)
		}
		slog.Info("registered default transcriber plugin", "url", cfg.DefaultTranscriberURL)
	}
	if cfg.DefaultStreamingTranscriberURL != "" && cfg.DefaultStreamingTranscriberToken != "" {
		if err := st.EnsureDefaultPlugin(ctx, cr, model.PluginStreamingTranscriber, "Default streaming transcriber",
			cfg.DefaultStreamingTranscriberURL, cfg.DefaultStreamingTranscriberToken, cfg.DefaultStreamingTranscriberConfig); err != nil {
			slog.Error("register default streaming transcriber", "error", err)
			os.Exit(1)
		}
		slog.Info("registered default streaming transcriber plugin", "url", cfg.DefaultStreamingTranscriberURL)
	}
	if cfg.DefaultAgentURL != "" && cfg.DefaultAgentToken != "" {
		if err := st.EnsureDefaultPlugin(ctx, cr, model.PluginAgent, "Default agent",
			cfg.DefaultAgentURL, cfg.DefaultAgentToken, cfg.DefaultAgentConfig); err != nil {
			slog.Error("register default agent", "error", err)
			os.Exit(1)
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
		os.Exit(1)
	}
	prov, err := storage.NewLocal(cfg.StorageDir, cfg.PublicURL, cfg.InternalURL, signingKey)
	if err != nil {
		slog.Error("storage", "error", err)
		os.Exit(1)
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

	srv := api.NewServer(api.Deps{Store: st, Storage: prov, Crypto: cr, Worker: wpool, Config: cfg, Embedder: emb, BackupRunner: backup.PgDumpRunner{}})
	slog.Info("muesli listening", "addr", cfg.Addr)
	fmt.Print(readyBanner(cfg.PublicURL))
	if err := srv.Run(ctx, cfg.Addr); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
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
