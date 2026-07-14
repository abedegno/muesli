package config

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestLoadDefaultsAndRequired(t *testing.T) {
	get := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}

	// Missing DATABASE_URL is an error.
	if _, err := Load(get(map[string]string{})); err == nil {
		t.Fatal("expected error when DATABASE_URL is missing")
	}

	cfg, err := Load(get(map[string]string{
		"DATABASE_URL": "postgres://localhost/muesli",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Addr != ":8080" {
		t.Fatalf("default Addr = %q, want :8080", cfg.Addr)
	}
	if cfg.PublicURL != "http://localhost:8080" {
		t.Fatalf("default PublicURL = %q", cfg.PublicURL)
	}
	// InternalURL defaults to PublicURL when unset.
	if cfg.InternalURL != cfg.PublicURL {
		t.Fatalf("default InternalURL = %q, want %q (PublicURL)", cfg.InternalURL, cfg.PublicURL)
	}
	if cfg.DatabaseURL != "postgres://localhost/muesli" {
		t.Fatalf("DatabaseURL = %q", cfg.DatabaseURL)
	}
}

func TestLoadValidationErrors(t *testing.T) {
	get := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}

	tests := []struct {
		name       string
		env        map[string]string
		wantSubstr string
	}{
		{
			name: "database url wrong scheme",
			env: map[string]string{
				"DATABASE_URL": "http://localhost/muesli",
			},
			wantSubstr: "DATABASE_URL must be a postgres:// or postgresql:// URL with a host",
		},
		{
			name: "database url missing host",
			env: map[string]string{
				"DATABASE_URL": "postgres:///muesli",
			},
			wantSubstr: "DATABASE_URL must be a postgres:// or postgresql:// URL with a host",
		},
		{
			name: "embeddings url wrong scheme",
			env: map[string]string{
				"DATABASE_URL":          "postgres://localhost/muesli",
				"MUESLI_EMBEDDINGS_URL": "ftp://ollama:11434",
			},
			wantSubstr: "MUESLI_EMBEDDINGS_URL must be an http:// or https:// URL with a host",
		},
		{
			name: "webhook url wrong scheme",
			env: map[string]string{
				"DATABASE_URL":       "postgres://localhost/muesli",
				"MUESLI_WEBHOOK_URL": "ws://hooks.example.com",
			},
			wantSubstr: "MUESLI_WEBHOOK_URL must be an http:// or https:// URL with a host",
		},
		{
			name: "default transcriber url wrong scheme",
			env: map[string]string{
				"DATABASE_URL":                   "postgres://localhost/muesli",
				"MUESLI_DEFAULT_TRANSCRIBER_URL": "ws://transcriber.example.com",
			},
			wantSubstr: "MUESLI_DEFAULT_TRANSCRIBER_URL must be an http:// or https:// URL with a host",
		},
		{
			name: "default streaming transcriber url missing host",
			env: map[string]string{
				"DATABASE_URL": "postgres://localhost/muesli",
				"MUESLI_DEFAULT_STREAMING_TRANSCRIBER_URL": "https:///stream",
			},
			wantSubstr: "MUESLI_DEFAULT_STREAMING_TRANSCRIBER_URL must be an http:// or https:// URL with a host",
		},
		{
			name: "default agent url wrong scheme",
			env: map[string]string{
				"DATABASE_URL":             "postgres://localhost/muesli",
				"MUESLI_DEFAULT_AGENT_URL": "ftp://agent.example.com",
			},
			wantSubstr: "MUESLI_DEFAULT_AGENT_URL must be an http:// or https:// URL with a host",
		},
		{
			name: "mode invalid",
			env: map[string]string{
				"DATABASE_URL": "postgres://localhost/muesli",
				"MUESLI_MODE":  "desktop",
			},
			wantSubstr: "MUESLI_MODE must be 'embedded' or 'server'",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(get(tc.env))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Fatalf("error %q does not contain %q", err, tc.wantSubstr)
			}
		})
	}
}

func TestLoadValidationValidConfig(t *testing.T) {
	get := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}

	cfg, err := Load(get(map[string]string{
		"DATABASE_URL":                             "postgres://user:pass@db.example.com:5432/muesli",
		"MUESLI_EMBEDDINGS_URL":                    "https://embeddings.example.com",
		"MUESLI_WEBHOOK_URL":                       "https://hooks.example.com",
		"MUESLI_DEFAULT_TRANSCRIBER_URL":           "http://transcriber.example.com",
		"MUESLI_DEFAULT_STREAMING_TRANSCRIBER_URL": "https://streaming.example.com",
		"MUESLI_DEFAULT_AGENT_URL":                 "https://agent.example.com",
		"MUESLI_MODE":                              "server",
	}))
	if err != nil {
		t.Fatalf("unexpected error for valid config: %v", err)
	}
	if cfg.Embedded {
		t.Fatalf("Embedded = %v, want false for MUESLI_MODE=server", cfg.Embedded)
	}
}

func TestLoadInternalURLOverrideAndFallback(t *testing.T) {
	get := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}

	// Explicit InternalURL is read and is independent of PublicURL.
	cfg, err := Load(get(map[string]string{
		"DATABASE_URL":        "postgres://localhost/muesli",
		"MUESLI_PUBLIC_URL":   "https://app.example.com",
		"MUESLI_INTERNAL_URL": "http://muesli.internal:8080",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PublicURL != "https://app.example.com" {
		t.Fatalf("PublicURL = %q", cfg.PublicURL)
	}
	if cfg.InternalURL != "http://muesli.internal:8080" {
		t.Fatalf("InternalURL = %q", cfg.InternalURL)
	}

	// Unset InternalURL falls back to the (overridden) PublicURL.
	cfg, err = Load(get(map[string]string{
		"DATABASE_URL":      "postgres://localhost/muesli",
		"MUESLI_PUBLIC_URL": "https://app.example.com",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.InternalURL != "https://app.example.com" {
		t.Fatalf("InternalURL fallback = %q, want %q", cfg.InternalURL, cfg.PublicURL)
	}
}

func TestLoadAudioRetentionDefaultAndOverride(t *testing.T) {
	get := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}

	cfg, err := Load(get(map[string]string{"DATABASE_URL": "postgres://localhost/muesli"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AudioRetention != "keep" {
		t.Fatalf("default AudioRetention = %q, want keep", cfg.AudioRetention)
	}

	cfg, err = Load(get(map[string]string{
		"DATABASE_URL":           "postgres://localhost/muesli",
		"MUESLI_AUDIO_RETENTION": "discard",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AudioRetention != "discard" {
		t.Fatalf("AudioRetention = %q, want discard", cfg.AudioRetention)
	}

	// An invalid value is rejected.
	if _, err := Load(get(map[string]string{
		"DATABASE_URL":           "postgres://localhost/muesli",
		"MUESLI_AUDIO_RETENTION": "bogus",
	})); err == nil {
		t.Fatal("expected error for invalid MUESLI_AUDIO_RETENTION")
	}
}

func TestLoadTranscribeLanguage(t *testing.T) {
	get := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}

	cfg, err := Load(get(map[string]string{"DATABASE_URL": "postgres://localhost/muesli"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TranscribeLanguage != "" {
		t.Fatalf("default TranscribeLanguage = %q, want empty", cfg.TranscribeLanguage)
	}

	cfg, err = Load(get(map[string]string{
		"DATABASE_URL":               "postgres://localhost/muesli",
		"MUESLI_TRANSCRIBE_LANGUAGE": "fr",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TranscribeLanguage != "fr" {
		t.Fatalf("TranscribeLanguage = %q, want fr", cfg.TranscribeLanguage)
	}
}

func TestLoadEmbeddingsFields(t *testing.T) {
	get := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}

	// Unset: URL is empty (disables embeddings), model defaults to nomic-embed-text, dim defaults to 768.
	cfg, err := Load(get(map[string]string{"DATABASE_URL": "postgres://localhost/muesli"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.EmbeddingsURL != "" {
		t.Fatalf("default EmbeddingsURL = %q, want empty", cfg.EmbeddingsURL)
	}
	if cfg.EmbeddingsModel != "nomic-embed-text" {
		t.Fatalf("default EmbeddingsModel = %q, want nomic-embed-text", cfg.EmbeddingsModel)
	}
	if cfg.EmbeddingsDim != 768 {
		t.Fatalf("default EmbeddingsDim = %d, want 768", cfg.EmbeddingsDim)
	}
	if cfg.EmbeddingsMinScore != 0.6 {
		t.Fatalf("default EmbeddingsMinScore = %v, want 0.6", cfg.EmbeddingsMinScore)
	}
	// Task prefixes default to nomic's (search_document / search_query).
	if cfg.EmbeddingsDocPrefix != "search_document: " {
		t.Fatalf("default EmbeddingsDocPrefix = %q, want %q", cfg.EmbeddingsDocPrefix, "search_document: ")
	}
	if cfg.EmbeddingsQueryPrefix != "search_query: " {
		t.Fatalf("default EmbeddingsQueryPrefix = %q, want %q", cfg.EmbeddingsQueryPrefix, "search_query: ")
	}

	// Set: all are read from the getter.
	cfg, err = Load(get(map[string]string{
		"DATABASE_URL":                   "postgres://localhost/muesli",
		"MUESLI_EMBEDDINGS_URL":          "http://ollama:11434",
		"MUESLI_EMBEDDINGS_MODEL":        "custom-embed",
		"MUESLI_EMBEDDINGS_DIM":          "1024",
		"MUESLI_EMBEDDINGS_MIN_SCORE":    "0.72",
		"MUESLI_EMBEDDINGS_DOC_PREFIX":   "passage: ",
		"MUESLI_EMBEDDINGS_QUERY_PREFIX": "query: ",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.EmbeddingsURL != "http://ollama:11434" {
		t.Fatalf("EmbeddingsURL = %q", cfg.EmbeddingsURL)
	}
	if cfg.EmbeddingsModel != "custom-embed" {
		t.Fatalf("EmbeddingsModel = %q", cfg.EmbeddingsModel)
	}
	if cfg.EmbeddingsDim != 1024 {
		t.Fatalf("EmbeddingsDim = %d, want 1024", cfg.EmbeddingsDim)
	}
	if cfg.EmbeddingsMinScore != 0.72 {
		t.Fatalf("EmbeddingsMinScore = %v, want 0.72", cfg.EmbeddingsMinScore)
	}
	if cfg.EmbeddingsDocPrefix != "passage: " {
		t.Fatalf("EmbeddingsDocPrefix = %q, want %q", cfg.EmbeddingsDocPrefix, "passage: ")
	}
	if cfg.EmbeddingsQueryPrefix != "query: " {
		t.Fatalf("EmbeddingsQueryPrefix = %q, want %q", cfg.EmbeddingsQueryPrefix, "query: ")
	}
}

func TestLoadEmbeddingsDim(t *testing.T) {
	get := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}

	// Explicit value is read.
	cfg, err := Load(get(map[string]string{
		"DATABASE_URL":          "postgres://localhost/muesli",
		"MUESLI_EMBEDDINGS_DIM": "1536",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.EmbeddingsDim != 1536 {
		t.Fatalf("EmbeddingsDim = %d, want 1536", cfg.EmbeddingsDim)
	}

	// Unset defaults to 768.
	cfg, err = Load(get(map[string]string{"DATABASE_URL": "postgres://localhost/muesli"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.EmbeddingsDim != 768 {
		t.Fatalf("default EmbeddingsDim = %d, want 768", cfg.EmbeddingsDim)
	}

	// Zero value falls back to 768 (with a warning logged).
	cfg, err = Load(get(map[string]string{
		"DATABASE_URL":          "postgres://localhost/muesli",
		"MUESLI_EMBEDDINGS_DIM": "0",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.EmbeddingsDim != 768 {
		t.Fatalf("EmbeddingsDim(0) = %d, want 768", cfg.EmbeddingsDim)
	}

	// Negative value falls back to 768 (with a warning logged).
	cfg, err = Load(get(map[string]string{
		"DATABASE_URL":          "postgres://localhost/muesli",
		"MUESLI_EMBEDDINGS_DIM": "-1",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.EmbeddingsDim != 768 {
		t.Fatalf("EmbeddingsDim(-1) = %d, want 768", cfg.EmbeddingsDim)
	}
}

func TestLoadEmbedBackfillBatchSize(t *testing.T) {
	get := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}

	// Explicit value is read.
	cfg, err := Load(get(map[string]string{
		"DATABASE_URL":                     "postgres://localhost/muesli",
		"MUESLI_EMBED_BACKFILL_BATCH_SIZE": "50",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.EmbedBackfillBatchSize != 50 {
		t.Fatalf("EmbedBackfillBatchSize = %d, want 50", cfg.EmbedBackfillBatchSize)
	}

	// Unset defaults to 500.
	cfg, err = Load(get(map[string]string{"DATABASE_URL": "postgres://localhost/muesli"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.EmbedBackfillBatchSize != 500 {
		t.Fatalf("default EmbedBackfillBatchSize = %d, want 500", cfg.EmbedBackfillBatchSize)
	}

	// Zero value falls back to 500 (with a warning logged).
	cfg, err = Load(get(map[string]string{
		"DATABASE_URL":                     "postgres://localhost/muesli",
		"MUESLI_EMBED_BACKFILL_BATCH_SIZE": "0",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.EmbedBackfillBatchSize != 500 {
		t.Fatalf("EmbedBackfillBatchSize(0) = %d, want 500", cfg.EmbedBackfillBatchSize)
	}

	// Negative value falls back to 500 (with a warning logged).
	cfg, err = Load(get(map[string]string{
		"DATABASE_URL":                     "postgres://localhost/muesli",
		"MUESLI_EMBED_BACKFILL_BATCH_SIZE": "-1",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.EmbedBackfillBatchSize != 500 {
		t.Fatalf("EmbedBackfillBatchSize(-1) = %d, want 500", cfg.EmbedBackfillBatchSize)
	}
}

func TestLoadDefaultPluginFields(t *testing.T) {
	get := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}

	// Unset: all empty except the *Config fields default to "{}".
	cfg, err := Load(get(map[string]string{"DATABASE_URL": "postgres://localhost/muesli"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DefaultTranscriberURL != "" || cfg.DefaultTranscriberToken != "" {
		t.Fatalf("expected empty transcriber url/token, got %q %q", cfg.DefaultTranscriberURL, cfg.DefaultTranscriberToken)
	}
	if cfg.DefaultStreamingTranscriberURL != "" || cfg.DefaultStreamingTranscriberToken != "" {
		t.Fatalf("expected empty streaming transcriber url/token, got %q %q", cfg.DefaultStreamingTranscriberURL, cfg.DefaultStreamingTranscriberToken)
	}
	if cfg.DefaultAgentURL != "" || cfg.DefaultAgentToken != "" {
		t.Fatalf("expected empty agent url/token, got %q %q", cfg.DefaultAgentURL, cfg.DefaultAgentToken)
	}
	if cfg.DefaultTranscriberConfig != "{}" {
		t.Fatalf("DefaultTranscriberConfig = %q, want {}", cfg.DefaultTranscriberConfig)
	}
	if cfg.DefaultStreamingTranscriberConfig != "{}" {
		t.Fatalf("DefaultStreamingTranscriberConfig = %q, want {}", cfg.DefaultStreamingTranscriberConfig)
	}
	if cfg.DefaultAgentConfig != "{}" {
		t.Fatalf("DefaultAgentConfig = %q, want {}", cfg.DefaultAgentConfig)
	}

	// Set: values are read from env.
	cfg, err = Load(get(map[string]string{
		"DATABASE_URL":                                "postgres://localhost/muesli",
		"MUESLI_DEFAULT_TRANSCRIBER_URL":              "http://transcriber:9000",
		"MUESLI_DEFAULT_TRANSCRIBER_TOKEN":            "t-tok",
		"MUESLI_DEFAULT_TRANSCRIBER_CONFIG":           `{"model":"whisper"}`,
		"MUESLI_DEFAULT_STREAMING_TRANSCRIBER_URL":    "http://streaming-transcriber:8000",
		"MUESLI_DEFAULT_STREAMING_TRANSCRIBER_TOKEN":  "s-tok",
		"MUESLI_DEFAULT_STREAMING_TRANSCRIBER_CONFIG": `{"model":"tiny.en"}`,
		"MUESLI_DEFAULT_AGENT_URL":                    "http://agent:9100",
		"MUESLI_DEFAULT_AGENT_TOKEN":                  "a-tok",
		"MUESLI_DEFAULT_AGENT_CONFIG":                 `{"model":"gpt"}`,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DefaultTranscriberURL != "http://transcriber:9000" || cfg.DefaultTranscriberToken != "t-tok" || cfg.DefaultTranscriberConfig != `{"model":"whisper"}` {
		t.Fatalf("transcriber fields = %q %q %q", cfg.DefaultTranscriberURL, cfg.DefaultTranscriberToken, cfg.DefaultTranscriberConfig)
	}
	if cfg.DefaultStreamingTranscriberURL != "http://streaming-transcriber:8000" || cfg.DefaultStreamingTranscriberToken != "s-tok" || cfg.DefaultStreamingTranscriberConfig != `{"model":"tiny.en"}` {
		t.Fatalf("streaming transcriber fields = %q %q %q", cfg.DefaultStreamingTranscriberURL, cfg.DefaultStreamingTranscriberToken, cfg.DefaultStreamingTranscriberConfig)
	}
	if cfg.DefaultAgentURL != "http://agent:9100" || cfg.DefaultAgentToken != "a-tok" || cfg.DefaultAgentConfig != `{"model":"gpt"}` {
		t.Fatalf("agent fields = %q %q %q", cfg.DefaultAgentURL, cfg.DefaultAgentToken, cfg.DefaultAgentConfig)
	}
}

func TestLoadGoogleOAuthFields(t *testing.T) {
	get := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}

	cfg, err := Load(get(map[string]string{"DATABASE_URL": "postgres://localhost/muesli"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.GoogleOAuthClientID != "" || cfg.GoogleOAuthClientSecret != "" {
		t.Fatalf("expected empty google oauth fields, got %q %q", cfg.GoogleOAuthClientID, cfg.GoogleOAuthClientSecret)
	}
	if cfg.GoogleOAuthRedirectURL != "" {
		t.Fatalf("expected empty google oauth redirect url, got %q", cfg.GoogleOAuthRedirectURL)
	}

	cfg, err = Load(get(map[string]string{
		"DATABASE_URL":                      "postgres://localhost/muesli",
		"MUESLI_GOOGLE_OAUTH_CLIENT_ID":     "google-client-id",
		"MUESLI_GOOGLE_OAUTH_CLIENT_SECRET": "google-client-secret",
		"MUESLI_GOOGLE_OAUTH_REDIRECT_URL":  "https://example.test/api/calendar/oauth/google/callback",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.GoogleOAuthClientID != "google-client-id" || cfg.GoogleOAuthClientSecret != "google-client-secret" {
		t.Fatalf("google oauth fields = %q %q", cfg.GoogleOAuthClientID, cfg.GoogleOAuthClientSecret)
	}
	if cfg.GoogleOAuthRedirectURL != "https://example.test/api/calendar/oauth/google/callback" {
		t.Fatalf("google oauth redirect url = %q", cfg.GoogleOAuthRedirectURL)
	}
}

func TestLoadMicrosoftOAuthFields(t *testing.T) {
	get := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}

	cfg, err := Load(get(map[string]string{"DATABASE_URL": "postgres://localhost/muesli"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.MicrosoftOAuthClientID != "" || cfg.MicrosoftOAuthClientSecret != "" {
		t.Fatalf("expected empty microsoft oauth fields, got %q %q", cfg.MicrosoftOAuthClientID, cfg.MicrosoftOAuthClientSecret)
	}
	if cfg.MicrosoftOAuthRedirectURL != "" {
		t.Fatalf("expected empty microsoft oauth redirect url, got %q", cfg.MicrosoftOAuthRedirectURL)
	}

	cfg, err = Load(get(map[string]string{
		"DATABASE_URL":                         "postgres://localhost/muesli",
		"MUESLI_MICROSOFT_OAUTH_CLIENT_ID":     "microsoft-client-id",
		"MUESLI_MICROSOFT_OAUTH_CLIENT_SECRET": "microsoft-client-secret",
		"MUESLI_MICROSOFT_OAUTH_REDIRECT_URL":  "https://example.test/api/calendar/oauth/microsoft/callback",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.MicrosoftOAuthClientID != "microsoft-client-id" || cfg.MicrosoftOAuthClientSecret != "microsoft-client-secret" {
		t.Fatalf("microsoft oauth fields = %q %q", cfg.MicrosoftOAuthClientID, cfg.MicrosoftOAuthClientSecret)
	}
	if cfg.MicrosoftOAuthRedirectURL != "https://example.test/api/calendar/oauth/microsoft/callback" {
		t.Fatalf("microsoft oauth redirect url = %q", cfg.MicrosoftOAuthRedirectURL)
	}
}

func TestLoadTrashRetentionDays(t *testing.T) {
	get := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	base := map[string]string{"DATABASE_URL": "postgres://localhost/muesli"}
	with := func(k, v string) map[string]string {
		m := map[string]string{"DATABASE_URL": "postgres://localhost/muesli"}
		for kk, vv := range base {
			m[kk] = vv
		}
		m[k] = v
		return m
	}

	// Unset → default 30.
	cfg, err := Load(get(base))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TrashRetentionDays != 30 {
		t.Fatalf("unset TrashRetentionDays = %d, want 30", cfg.TrashRetentionDays)
	}

	// Valid positive integer.
	cfg, err = Load(get(with("MUESLI_TRASH_RETENTION_DAYS", "7")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TrashRetentionDays != 7 {
		t.Fatalf("TrashRetentionDays = %d, want 7", cfg.TrashRetentionDays)
	}

	// Zero → default 30.
	cfg, err = Load(get(with("MUESLI_TRASH_RETENTION_DAYS", "0")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TrashRetentionDays != 30 {
		t.Fatalf("TrashRetentionDays (0) = %d, want 30", cfg.TrashRetentionDays)
	}

	// Negative → default 30.
	cfg, err = Load(get(with("MUESLI_TRASH_RETENTION_DAYS", "-5")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TrashRetentionDays != 30 {
		t.Fatalf("TrashRetentionDays (-5) = %d, want 30", cfg.TrashRetentionDays)
	}

	// Invalid string → default 30.
	cfg, err = Load(get(with("MUESLI_TRASH_RETENTION_DAYS", "invalid")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TrashRetentionDays != 30 {
		t.Fatalf("TrashRetentionDays (invalid) = %d, want 30", cfg.TrashRetentionDays)
	}
}

// TestRequireMasterKey covers all four validation paths in RequireMasterKey.
func TestRequireMasterKey(t *testing.T) {
	// (a) Empty key → error containing the var name and the openssl hint.
	err := RequireMasterKey(Config{MasterKey: ""})
	if err == nil {
		t.Fatal("(a) expected error for empty MasterKey, got nil")
	}
	if !strings.Contains(err.Error(), "MUESLI_MASTER_KEY") {
		t.Errorf("(a) error %q does not mention MUESLI_MASTER_KEY", err)
	}
	if !strings.Contains(err.Error(), "openssl rand -base64 32") {
		t.Errorf("(a) error %q does not contain the openssl hint", err)
	}

	// (b) Non-empty but invalid base64 → error.
	err = RequireMasterKey(Config{MasterKey: "not!!valid!!base64"})
	if err == nil {
		t.Fatal("(b) expected error for invalid base64 MasterKey, got nil")
	}

	// (c) Valid base64 but wrong decoded length (16 bytes) → error.
	shortKey := base64.StdEncoding.EncodeToString(make([]byte, 16))
	err = RequireMasterKey(Config{MasterKey: shortKey})
	if err == nil {
		t.Fatal("(c) expected error for 16-byte MasterKey, got nil")
	}

	// (d) Valid 32-byte base64 key → nil.
	validKey := base64.StdEncoding.EncodeToString(make([]byte, 32))
	if err := RequireMasterKey(Config{MasterKey: validKey}); err != nil {
		t.Fatalf("(d) unexpected error for valid 32-byte key: %v", err)
	}
}

func TestLoadDiarization(t *testing.T) {
	get := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}

	// Unset -> default false.
	cfg, err := Load(get(map[string]string{"DATABASE_URL": "postgres://localhost/muesli"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Diarization != false {
		t.Fatalf("default Diarization = %v, want false", cfg.Diarization)
	}

	// MUESLI_DIARIZATION=true -> true.
	cfg, err = Load(get(map[string]string{
		"DATABASE_URL":       "postgres://localhost/muesli",
		"MUESLI_DIARIZATION": "true",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Diarization {
		t.Fatalf("Diarization = %v, want true (MUESLI_DIARIZATION=true)", cfg.Diarization)
	}

	// MUESLI_DIARIZATION=1 -> true.
	cfg, err = Load(get(map[string]string{
		"DATABASE_URL":       "postgres://localhost/muesli",
		"MUESLI_DIARIZATION": "1",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Diarization {
		t.Fatalf("Diarization = %v, want true (MUESLI_DIARIZATION=1)", cfg.Diarization)
	}
}

func TestLoadEmbeddedMode(t *testing.T) {
	get := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}

	cfg, err := Load(get(map[string]string{"DATABASE_URL": "postgres://localhost/muesli"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Embedded {
		t.Fatalf("default Embedded = %v, want false", cfg.Embedded)
	}

	cfg, err = Load(get(map[string]string{
		"DATABASE_URL": "postgres://localhost/muesli",
		"MUESLI_MODE":  "embedded",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Embedded {
		t.Fatalf("Embedded = %v, want true (MUESLI_MODE=embedded)", cfg.Embedded)
	}
}

// TestLoadBackupConfig covers MUESLI_BACKUP_DIR, MUESLI_BACKUP_SCHEDULE_INTERVAL,
// and MUESLI_BACKUP_RETENTION_COUNT (BAK01).
func TestLoadBackupConfig(t *testing.T) {
	get := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	base := map[string]string{"DATABASE_URL": "postgres://localhost/muesli"}
	with := func(extra map[string]string) map[string]string {
		m := map[string]string{"DATABASE_URL": "postgres://localhost/muesli"}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}

	// Unset -> backups disabled, retention default 7, schedule disabled.
	cfg, err := Load(get(base))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BackupDir != "" {
		t.Errorf("default BackupDir = %q, want empty", cfg.BackupDir)
	}
	if cfg.BackupScheduleInterval != 0 {
		t.Errorf("default BackupScheduleInterval = %v, want 0", cfg.BackupScheduleInterval)
	}
	if cfg.BackupRetentionCount != 7 {
		t.Errorf("default BackupRetentionCount = %d, want 7", cfg.BackupRetentionCount)
	}

	// Set: dir + schedule interval + retention are all read from env.
	cfg, err = Load(get(with(map[string]string{
		"MUESLI_BACKUP_DIR":               "/var/backups/muesli",
		"MUESLI_BACKUP_SCHEDULE_INTERVAL": "24h",
		"MUESLI_BACKUP_RETENTION_COUNT":   "14",
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BackupDir != "/var/backups/muesli" {
		t.Errorf("BackupDir = %q, want /var/backups/muesli", cfg.BackupDir)
	}
	if cfg.BackupScheduleInterval != 24*time.Hour {
		t.Errorf("BackupScheduleInterval = %v, want 24h", cfg.BackupScheduleInterval)
	}
	if cfg.BackupRetentionCount != 14 {
		t.Errorf("BackupRetentionCount = %d, want 14", cfg.BackupRetentionCount)
	}

	// Invalid duration string -> falls back to disabled (0), not an error.
	cfg, err = Load(get(with(map[string]string{
		"MUESLI_BACKUP_SCHEDULE_INTERVAL": "not-a-duration",
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BackupScheduleInterval != 0 {
		t.Errorf("invalid BackupScheduleInterval = %v, want 0 (fallback)", cfg.BackupScheduleInterval)
	}

	// Non-positive / invalid retention count -> falls back to default 7.
	for _, v := range []string{"0", "-3", "nope"} {
		cfg, err = Load(get(with(map[string]string{"MUESLI_BACKUP_RETENTION_COUNT": v})))
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", v, err)
		}
		if cfg.BackupRetentionCount != 7 {
			t.Errorf("BackupRetentionCount(%q) = %d, want 7 (fallback)", v, cfg.BackupRetentionCount)
		}
	}
}

// TestLoadUploadAllowedContentTypes covers MUESLI_UPLOAD_ALLOWED_CONTENT_TYPES
// (HRD01): unset defaults to nil (storage applies its own built-in default),
// and a comma-separated value is split/trimmed like MUESLI_ALLOWED_ORIGINS.
func TestLoadUploadAllowedContentTypes(t *testing.T) {
	get := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}

	cfg, err := Load(get(map[string]string{"DATABASE_URL": "postgres://localhost/muesli"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.UploadAllowedContentTypes != nil {
		t.Fatalf("default UploadAllowedContentTypes = %v, want nil", cfg.UploadAllowedContentTypes)
	}

	cfg, err = Load(get(map[string]string{
		"DATABASE_URL":                        "postgres://localhost/muesli",
		"MUESLI_UPLOAD_ALLOWED_CONTENT_TYPES": " audio/wav , audio/x-custom ,,audio/flac",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"audio/wav", "audio/x-custom", "audio/flac"}
	if len(cfg.UploadAllowedContentTypes) != len(want) {
		t.Fatalf("UploadAllowedContentTypes = %v, want %v", cfg.UploadAllowedContentTypes, want)
	}
	for i, v := range want {
		if cfg.UploadAllowedContentTypes[i] != v {
			t.Fatalf("UploadAllowedContentTypes[%d] = %q, want %q", i, cfg.UploadAllowedContentTypes[i], v)
		}
	}
}
