package config

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr               string
	DatabaseURL        string
	MasterKey          string // base64, 32 bytes; required only when plugin secrets are stored (later plan)
	StorageDir         string
	StorageSigningKey  string // signing key for presigned upload URLs; must be stable across restarts/replicas
	PublicURL          string
	InternalURL        string // base URL plugins use to fetch from the server (in-network name); defaults to PublicURL
	AudioRetention     string // "keep" (default) or "discard"
	TrashRetentionDays int    // MUESLI_TRASH_RETENTION_DAYS; default 30

	// Rate limiting for auth, upload, and public share endpoints.
	// Login: default 5 req/min (0.0833 rps), burst 3.
	// Upload: default 20 req/min (0.333 rps), burst 5.
	// Shared: default 10 req/min (0.1667 rps), burst 5.
	// Set to 0 to disable rate limiting on that endpoint.
	RateLoginRPS    float64 // MUESLI_RATE_LOGIN_RPS
	RateLoginBurst  int     // MUESLI_RATE_LOGIN_BURST
	RateUploadRPS   float64 // MUESLI_RATE_UPLOAD_RPS
	RateUploadBurst int     // MUESLI_RATE_UPLOAD_BURST
	RateSharedRPS   float64 // MUESLI_RATE_SHARED_RPS
	RateSharedBurst int     // MUESLI_RATE_SHARED_BURST

	// Semantic-search embeddings (optional, config-gated). When EmbeddingsURL is
	// empty, semantic embeddings are disabled and the system runs lexical-only.
	EmbeddingsURL      string  // MUESLI_EMBEDDINGS_URL; empty disables semantic embeddings
	EmbeddingsModel    string  // MUESLI_EMBEDDINGS_MODEL; default "nomic-embed-text"
	EmbeddingsDim      int     // MUESLI_EMBEDDINGS_DIM; embedding dimension (default 768); must match the model
	EmbeddingsMinScore float64 // MUESLI_EMBEDDINGS_MIN_SCORE; cosine cutoff for a semantic hit (default 0.6, tuned for prefixed nomic)
	// Task prefixes for asymmetric embedding models (nomic-embed-text expects
	// "search_document: " on stored text and "search_query: " on the query).
	// Defaults match nomic; leave the default for prefix-less models (a wrong
	// prefix only mildly affects quality).
	EmbeddingsDocPrefix    string // MUESLI_EMBEDDINGS_DOC_PREFIX; default "search_document: "
	EmbeddingsQueryPrefix  string // MUESLI_EMBEDDINGS_QUERY_PREFIX; default "search_query: "
	EmbedBackfillBatchSize int    // MUESLI_EMBED_BACKFILL_BATCH_SIZE; max notes backfilled on startup (default 500)

	AllowedOrigins []string // MUESLI_ALLOWED_ORIGINS; comma-separated; empty = no cross-origin access

	// UploadAllowedContentTypes overrides the audio Content-Type allowlist
	// enforced on the upload PUT path (internal/storage.Local.UploadHandler).
	// Comma-separated, trimmed; empty = use storage.DefaultAllowedContentTypes.
	UploadAllowedContentTypes []string // MUESLI_UPLOAD_ALLOWED_CONTENT_TYPES

	// Outbound webhook (EXT01 chain).
	WebhookURL       string   // MUESLI_WEBHOOK_URL; empty = no webhook
	WebhookAllowList []string // MUESLI_WEBHOOK_ALLOWLIST; comma-separated exact scheme+host+port entries permitted even if they resolve to private IPs (e.g. http://localhost:9000 for self-host testing)

	// Default plugins auto-registered on startup (all optional). A kind is only
	// registered when both its URL and token are set. Config fields hold a JSON
	// string and default to "{}".
	DefaultTranscriberURL             string
	DefaultTranscriberToken           string
	DefaultTranscriberConfig          string
	TranscribeLanguage                string
	DefaultStreamingTranscriberURL    string
	DefaultStreamingTranscriberToken  string
	DefaultStreamingTranscriberConfig string
	DefaultAgentURL                   string
	DefaultAgentToken                 string
	DefaultAgentConfig                string

	GoogleOAuthClientID        string
	GoogleOAuthClientSecret    string
	GoogleOAuthRedirectURL     string
	MicrosoftOAuthClientID     string
	MicrosoftOAuthClientSecret string
	MicrosoftOAuthRedirectURL  string

	Embedded   bool   // MUESLI_MODE=embedded
	AppDataDir string // embedded desktop app-data root; populated by the embedded runtime slice
	Production bool   // MUESLI_PRODUCTION; refuse to start with dev secrets

	Diarization bool // MUESLI_DIARIZATION; enable speaker diarization in transcribe requests

	// In-app Postgres backup (BAK01). BackupDir empty disables the feature
	// entirely (admin API returns 400; the scheduler is never started).
	BackupDir              string        // MUESLI_BACKUP_DIR; empty = backup feature disabled
	BackupScheduleInterval time.Duration // MUESLI_BACKUP_SCHEDULE_INTERVAL; empty/unset = scheduled backups disabled
	BackupRetentionCount   int           // MUESLI_BACKUP_RETENTION_COUNT; default 7; applied after manual AND scheduled backups
}

// Load reads config from a getter (os.Getenv in production, a map in tests).
func Load(get func(string) string) (Config, error) {
	cfg := Config{
		Addr:              def(get("MUESLI_ADDR"), ":8080"),
		DatabaseURL:       get("DATABASE_URL"),
		MasterKey:         get("MUESLI_MASTER_KEY"),
		StorageDir:        def(get("MUESLI_STORAGE_DIR"), "./data/audio"),
		StorageSigningKey: get("MUESLI_STORAGE_SIGNING_KEY"),
		PublicURL:         def(get("MUESLI_PUBLIC_URL"), "http://localhost:8080"),
	}
	// InternalURL is the base URL plugins use to reach the server by its
	// in-network name. It defaults to PublicURL so single-host deploys are
	// unchanged.
	cfg.InternalURL = def(get("MUESLI_INTERNAL_URL"), cfg.PublicURL)
	cfg.DefaultTranscriberURL = get("MUESLI_DEFAULT_TRANSCRIBER_URL")
	cfg.DefaultTranscriberToken = get("MUESLI_DEFAULT_TRANSCRIBER_TOKEN")
	cfg.DefaultTranscriberConfig = def(get("MUESLI_DEFAULT_TRANSCRIBER_CONFIG"), "{}")
	cfg.TranscribeLanguage = get("MUESLI_TRANSCRIBE_LANGUAGE")
	cfg.DefaultStreamingTranscriberURL = get("MUESLI_DEFAULT_STREAMING_TRANSCRIBER_URL")
	cfg.DefaultStreamingTranscriberToken = get("MUESLI_DEFAULT_STREAMING_TRANSCRIBER_TOKEN")
	cfg.DefaultStreamingTranscriberConfig = def(get("MUESLI_DEFAULT_STREAMING_TRANSCRIBER_CONFIG"), "{}")
	cfg.DefaultAgentURL = get("MUESLI_DEFAULT_AGENT_URL")
	cfg.DefaultAgentToken = get("MUESLI_DEFAULT_AGENT_TOKEN")
	cfg.DefaultAgentConfig = def(get("MUESLI_DEFAULT_AGENT_CONFIG"), "{}")
	cfg.GoogleOAuthClientID = get("MUESLI_GOOGLE_OAUTH_CLIENT_ID")
	cfg.GoogleOAuthClientSecret = get("MUESLI_GOOGLE_OAUTH_CLIENT_SECRET")
	cfg.GoogleOAuthRedirectURL = def(get("MUESLI_GOOGLE_OAUTH_REDIRECT_URL"), "")
	cfg.MicrosoftOAuthClientID = get("MUESLI_MICROSOFT_OAUTH_CLIENT_ID")
	cfg.MicrosoftOAuthClientSecret = get("MUESLI_MICROSOFT_OAUTH_CLIENT_SECRET")
	cfg.MicrosoftOAuthRedirectURL = def(get("MUESLI_MICROSOFT_OAUTH_REDIRECT_URL"), "")
	cfg.Embedded = get("MUESLI_MODE") == "embedded"
	cfg.Production = get("MUESLI_PRODUCTION") == "true" || get("MUESLI_PRODUCTION") == "1"
	cfg.Diarization = get("MUESLI_DIARIZATION") == "true" || get("MUESLI_DIARIZATION") == "1"
	cfg.EmbeddingsURL = get("MUESLI_EMBEDDINGS_URL")
	cfg.EmbeddingsModel = def(get("MUESLI_EMBEDDINGS_MODEL"), "nomic-embed-text")
	cfg.EmbeddingsDim = parseInt(get("MUESLI_EMBEDDINGS_DIM"), 768)
	if cfg.EmbeddingsDim <= 0 {
		slog.Warn("config: MUESLI_EMBEDDINGS_DIM must be > 0, using default", "default", 768)
		cfg.EmbeddingsDim = 768
	}
	cfg.EmbeddingsMinScore = parseFloat(get("MUESLI_EMBEDDINGS_MIN_SCORE"), 0.6)
	cfg.EmbeddingsDocPrefix = def(get("MUESLI_EMBEDDINGS_DOC_PREFIX"), "search_document: ")
	cfg.EmbeddingsQueryPrefix = def(get("MUESLI_EMBEDDINGS_QUERY_PREFIX"), "search_query: ")
	cfg.EmbedBackfillBatchSize = parseInt(get("MUESLI_EMBED_BACKFILL_BATCH_SIZE"), 500)
	if cfg.EmbedBackfillBatchSize <= 0 {
		slog.Warn("config: MUESLI_EMBED_BACKFILL_BATCH_SIZE must be > 0, using default", "default", 500)
		cfg.EmbedBackfillBatchSize = 500
	}
	cfg.AudioRetention = def(get("MUESLI_AUDIO_RETENTION"), "keep")
	cfg.TrashRetentionDays = parseIntPositive("MUESLI_TRASH_RETENTION_DAYS", get("MUESLI_TRASH_RETENTION_DAYS"), 30)
	cfg.RateLoginRPS = parseFloatDef(get("MUESLI_RATE_LOGIN_RPS"), 5.0/60.0)
	cfg.RateLoginBurst = parseIntDef(get("MUESLI_RATE_LOGIN_BURST"), 3)
	cfg.RateUploadRPS = parseFloatDef(get("MUESLI_RATE_UPLOAD_RPS"), 20.0/60.0)
	cfg.RateUploadBurst = parseIntDef(get("MUESLI_RATE_UPLOAD_BURST"), 5)
	cfg.RateSharedRPS = parseFloatDef(get("MUESLI_RATE_SHARED_RPS"), 10.0/60.0)
	cfg.RateSharedBurst = parseIntDef(get("MUESLI_RATE_SHARED_BURST"), 5)
	cfg.AllowedOrigins = parseStringSlice(get("MUESLI_ALLOWED_ORIGINS"))
	cfg.UploadAllowedContentTypes = parseStringSlice(get("MUESLI_UPLOAD_ALLOWED_CONTENT_TYPES"))
	cfg.WebhookURL = get("MUESLI_WEBHOOK_URL")
	cfg.WebhookAllowList = splitComma(get("MUESLI_WEBHOOK_ALLOWLIST"))
	cfg.BackupDir = get("MUESLI_BACKUP_DIR")
	cfg.BackupScheduleInterval = parseDuration("MUESLI_BACKUP_SCHEDULE_INTERVAL", get("MUESLI_BACKUP_SCHEDULE_INTERVAL"), 0)
	cfg.BackupRetentionCount = parseIntPositive("MUESLI_BACKUP_RETENTION_COUNT", get("MUESLI_BACKUP_RETENTION_COUNT"), 7)
	if cfg.AudioRetention != "keep" && cfg.AudioRetention != "discard" {
		return Config{}, fmt.Errorf("MUESLI_AUDIO_RETENTION must be 'keep' or 'discard', got %q", cfg.AudioRetention)
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	return cfg, nil
}

// FromEnv is the production convenience wrapper.
func FromEnv() (Config, error) { return Load(os.Getenv) }

func def(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// parseIntPositive returns the int value of v when v is a positive integer,
// or fallback when v is empty, unparseable, or ≤ 0. name is only used in the
// warning log so callers get an actionable message.
func parseIntPositive(name, v string, fallback int) int {
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		slog.Warn(name+" must be > 0", "value", v, "default", fallback)
		return fallback
	}
	return n
}

// parseFloat returns the float value of v, or fallback when empty/unparseable.
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

// parseFloatDef returns the float value of v when positive, or fallback when
// v is empty, unparseable, or non-positive.
func parseFloatDef(v string, fallback float64) float64 {
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f <= 0 {
		return fallback
	}
	return f
}

// parseIntDef returns the int value of v when positive, or fallback when
// v is empty, unparseable, or non-positive.
func parseIntDef(v string, fallback int) int {
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

// parseInt returns the int value of v, or fallback when empty/unparseable.
func parseInt(v string, fallback int) int {
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

// parseDuration returns the time.Duration value of v (parsed with
// time.ParseDuration, e.g. "24h", "30m"), or fallback when v is empty or
// unparseable. name is only used in the warning log.
func parseDuration(name, v string, fallback time.Duration) time.Duration {
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		slog.Warn(name+" is not a valid duration, using default", "value", v, "default", fallback)
		return fallback
	}
	return d
}

// splitComma splits v on commas, trims whitespace, and drops empty elements.
// An empty v returns nil. Alias for parseStringSlice; used by webhook config.
func splitComma(v string) []string { return parseStringSlice(v) }

// parseStringSlice splits v on commas, trims whitespace, and drops empty
// elements. An empty v returns nil (safe default: no entries).
func parseStringSlice(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// RequireMasterKey validates cfg.MasterKey and returns a clear, actionable
// error when the key is missing or unusable. It must be called AFTER any
// subcommand dispatch that does not need the master key (e.g. reembed), and
// BEFORE crypto.New. It is never called by Load or FromEnv.
func RequireMasterKey(cfg Config) error {
	if cfg.MasterKey == "" {
		return fmt.Errorf("MUESLI_MASTER_KEY is required; generate one with: openssl rand -base64 32")
	}
	raw, err := base64.StdEncoding.DecodeString(cfg.MasterKey)
	if err != nil {
		return fmt.Errorf("MUESLI_MASTER_KEY is not valid base64: %w", err)
	}
	if len(raw) != 32 {
		return fmt.Errorf("MUESLI_MASTER_KEY must be a base64-encoded 32-byte key (got %d bytes); generate one with: openssl rand -base64 32", len(raw))
	}
	return nil
}
