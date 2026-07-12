package config

import (
	"fmt"
	"strconv"
	"strings"
)

// ConfigEntry is one row of the ADM06 effective-configuration view: a
// human-readable display name, the backing MUESLI_* env var, its
// stringified effective value (as loaded into the Config struct), and
// whether that value came from the environment or a built-in default.
type ConfigEntry struct {
	Name   string `json:"name"`   // human-readable display label, e.g. "Trash retention (days)"
	EnvVar string `json:"envVar"` // the MUESLI_* env var name, e.g. "MUESLI_TRASH_RETENTION_DAYS"
	Value  string `json:"value"`  // stringified value taken from the cfg struct field (redacted if secret-shaped)
	Source string `json:"source"` // "env" if that env var is currently set, else "default"
}

// sensitiveNameSubstrings lists substrings (checked case-insensitively
// against both the display name and the env var) that mark a value as
// secret. Matching entries never expose their real value: only
// "(set)" / "(unset)" is returned. Implemented as a substring match (not a
// hardcoded field list) so it stays correct as new fields are added.
var sensitiveNameSubstrings = []string{"token", "key", "password", "secret"}

// isSensitive reports whether name or envVar should be redacted based on a
// case-insensitive substring match against either.
func isSensitive(name, envVar string) bool {
	lower := strings.ToLower(name + " " + envVar)
	for _, s := range sensitiveNameSubstrings {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

// redact returns the display value for a config entry: "(set)"/"(unset)"
// for sensitive entries (based only on whether raw is non-empty), or raw
// itself otherwise.
func redact(name, envVar, raw string) string {
	if !isSensitive(name, envVar) {
		return raw
	}
	if raw == "" {
		return "(unset)"
	}
	return "(set)"
}

// source returns "env" when lookup reports the env var is actually PRESENT
// in the environment (mirrors os.LookupEnv's (value, ok) shape), else
// "default". Presence, not non-emptiness, is what matters: an operator who
// explicitly sets e.g. MUESLI_BACKUP_DIR="" has still set it, and that must
// report source="env", not "default" (a plain non-empty check would
// misclassify any field whose legitimate/default value is empty, e.g.
// MUESLI_WEBHOOK_URL, MUESLI_ALLOWED_ORIGINS, MUESLI_BACKUP_DIR).
func source(lookup func(string) (string, bool), envVar string) string {
	if _, ok := lookup(envVar); ok {
		return "env"
	}
	return "default"
}

// entry builds one ConfigEntry, applying redaction to raw before display.
func entry(lookup func(string) (string, bool), name, envVar, raw string) ConfigEntry {
	return ConfigEntry{
		Name:   name,
		EnvVar: envVar,
		Value:  redact(name, envVar, raw),
		Source: source(lookup, envVar),
	}
}

// Effective builds the effective-configuration view for the admin UI: one
// row per MUESLI_*-backed field on cfg, redacting anything that looks like
// a secret. It reads values from the already-loaded cfg struct (never by
// dumping raw env), and only uses lookup to check whether a given env var
// is currently PRESENT in the environment (for Source) — mirroring how
// config.Load itself is seamed for tests, but with an (value, ok) shape
// like os.LookupEnv so presence and non-emptiness are never conflated.
// Scope is MUESLI_* fields only (e.g. DATABASE_URL is intentionally
// excluded).
//
// ADM07/future extensibility: to add a new field, append one more entry(...)
// call below — the function is otherwise unchanged.
func Effective(cfg Config, lookup func(string) (string, bool)) []ConfigEntry {
	return []ConfigEntry{
		entry(lookup, "Server address", "MUESLI_ADDR", cfg.Addr),
		entry(lookup, "Master key", "MUESLI_MASTER_KEY", cfg.MasterKey),
		entry(lookup, "Storage directory", "MUESLI_STORAGE_DIR", cfg.StorageDir),
		entry(lookup, "Storage signing key", "MUESLI_STORAGE_SIGNING_KEY", cfg.StorageSigningKey),
		entry(lookup, "Public URL", "MUESLI_PUBLIC_URL", cfg.PublicURL),
		entry(lookup, "Internal URL", "MUESLI_INTERNAL_URL", cfg.InternalURL),
		entry(lookup, "Audio retention", "MUESLI_AUDIO_RETENTION", cfg.AudioRetention),
		entry(lookup, "Trash retention (days)", "MUESLI_TRASH_RETENTION_DAYS", strconv.Itoa(cfg.TrashRetentionDays)),
		entry(lookup, "Login rate limit (rps)", "MUESLI_RATE_LOGIN_RPS", formatFloat(cfg.RateLoginRPS)),
		entry(lookup, "Login rate limit burst", "MUESLI_RATE_LOGIN_BURST", strconv.Itoa(cfg.RateLoginBurst)),
		entry(lookup, "Upload rate limit (rps)", "MUESLI_RATE_UPLOAD_RPS", formatFloat(cfg.RateUploadRPS)),
		entry(lookup, "Upload rate limit burst", "MUESLI_RATE_UPLOAD_BURST", strconv.Itoa(cfg.RateUploadBurst)),
		entry(lookup, "Embeddings URL", "MUESLI_EMBEDDINGS_URL", cfg.EmbeddingsURL),
		entry(lookup, "Embeddings model", "MUESLI_EMBEDDINGS_MODEL", cfg.EmbeddingsModel),
		entry(lookup, "Embeddings dimension", "MUESLI_EMBEDDINGS_DIM", strconv.Itoa(cfg.EmbeddingsDim)),
		entry(lookup, "Embeddings min score", "MUESLI_EMBEDDINGS_MIN_SCORE", formatFloat(cfg.EmbeddingsMinScore)),
		entry(lookup, "Embeddings doc prefix", "MUESLI_EMBEDDINGS_DOC_PREFIX", cfg.EmbeddingsDocPrefix),
		entry(lookup, "Embeddings query prefix", "MUESLI_EMBEDDINGS_QUERY_PREFIX", cfg.EmbeddingsQueryPrefix),
		entry(lookup, "Embed backfill batch size", "MUESLI_EMBED_BACKFILL_BATCH_SIZE", strconv.Itoa(cfg.EmbedBackfillBatchSize)),
		entry(lookup, "Allowed origins", "MUESLI_ALLOWED_ORIGINS", strings.Join(cfg.AllowedOrigins, ",")),
		entry(lookup, "Upload allowed content types", "MUESLI_UPLOAD_ALLOWED_CONTENT_TYPES", strings.Join(cfg.UploadAllowedContentTypes, ",")),
		entry(lookup, "Webhook URL", "MUESLI_WEBHOOK_URL", cfg.WebhookURL),
		entry(lookup, "Webhook allowlist", "MUESLI_WEBHOOK_ALLOWLIST", strings.Join(cfg.WebhookAllowList, ",")),
		entry(lookup, "Default transcriber URL", "MUESLI_DEFAULT_TRANSCRIBER_URL", cfg.DefaultTranscriberURL),
		entry(lookup, "Default transcriber token", "MUESLI_DEFAULT_TRANSCRIBER_TOKEN", cfg.DefaultTranscriberToken),
		entry(lookup, "Default transcriber config", "MUESLI_DEFAULT_TRANSCRIBER_CONFIG", cfg.DefaultTranscriberConfig),
		entry(lookup, "Transcribe language", "MUESLI_TRANSCRIBE_LANGUAGE", cfg.TranscribeLanguage),
		entry(lookup, "Default streaming transcriber URL", "MUESLI_DEFAULT_STREAMING_TRANSCRIBER_URL", cfg.DefaultStreamingTranscriberURL),
		entry(lookup, "Default streaming transcriber token", "MUESLI_DEFAULT_STREAMING_TRANSCRIBER_TOKEN", cfg.DefaultStreamingTranscriberToken),
		entry(lookup, "Default streaming transcriber config", "MUESLI_DEFAULT_STREAMING_TRANSCRIBER_CONFIG", cfg.DefaultStreamingTranscriberConfig),
		entry(lookup, "Default agent URL", "MUESLI_DEFAULT_AGENT_URL", cfg.DefaultAgentURL),
		entry(lookup, "Default agent token", "MUESLI_DEFAULT_AGENT_TOKEN", cfg.DefaultAgentToken),
		entry(lookup, "Default agent config", "MUESLI_DEFAULT_AGENT_CONFIG", cfg.DefaultAgentConfig),
		entry(lookup, "Google OAuth client ID", "MUESLI_GOOGLE_OAUTH_CLIENT_ID", cfg.GoogleOAuthClientID),
		entry(lookup, "Google OAuth client secret", "MUESLI_GOOGLE_OAUTH_CLIENT_SECRET", cfg.GoogleOAuthClientSecret),
		entry(lookup, "Google OAuth redirect URL", "MUESLI_GOOGLE_OAUTH_REDIRECT_URL", cfg.GoogleOAuthRedirectURL),
		entry(lookup, "Microsoft OAuth client ID", "MUESLI_MICROSOFT_OAUTH_CLIENT_ID", cfg.MicrosoftOAuthClientID),
		entry(lookup, "Microsoft OAuth client secret", "MUESLI_MICROSOFT_OAUTH_CLIENT_SECRET", cfg.MicrosoftOAuthClientSecret),
		entry(lookup, "Microsoft OAuth redirect URL", "MUESLI_MICROSOFT_OAUTH_REDIRECT_URL", cfg.MicrosoftOAuthRedirectURL),
		entry(lookup, "Production mode", "MUESLI_PRODUCTION", strconv.FormatBool(cfg.Production)),
		entry(lookup, "Diarization enabled", "MUESLI_DIARIZATION", strconv.FormatBool(cfg.Diarization)),
		entry(lookup, "Backup directory", "MUESLI_BACKUP_DIR", cfg.BackupDir),
		entry(lookup, "Backup schedule interval", "MUESLI_BACKUP_SCHEDULE_INTERVAL", cfg.BackupScheduleInterval.String()),
		entry(lookup, "Backup retention count", "MUESLI_BACKUP_RETENTION_COUNT", strconv.Itoa(cfg.BackupRetentionCount)),
		// ADM07/future: append one more entry(...) line here for a new field.
	}
}

// formatFloat renders a float64 config value without trailing zeros beyond
// what's needed, matching typical env-var input style (e.g. "0.6", "5").
func formatFloat(f float64) string {
	return fmt.Sprintf("%g", f)
}
