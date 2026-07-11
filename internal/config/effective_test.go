package config

import (
	"strconv"
	"strings"
	"testing"
)

const fakeSecret = "fake-super-secret-value-xyz"

// noEnv is a lookup func reporting every env var as absent (mirrors
// os.LookupEnv's behavior for an unset var).
func noEnv(string) (string, bool) { return "", false }

// mapLookup builds a lookup func (os.LookupEnv shape) from a map, so tests
// can distinguish "explicitly set to X" (including X == "") from "absent
// entirely" — the distinction plain non-emptiness checks conflate.
func mapLookup(set map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := set[name]
		return v, ok
	}
}

// entriesString flattens all entries' Name/EnvVar/Value/Source into one blob
// so we can assert a secret never leaks into any field, anywhere.
func entriesString(entries []ConfigEntry) string {
	var sb strings.Builder
	for _, e := range entries {
		sb.WriteString(e.Name)
		sb.WriteString("|")
		sb.WriteString(e.EnvVar)
		sb.WriteString("|")
		sb.WriteString(e.Value)
		sb.WriteString("|")
		sb.WriteString(e.Source)
		sb.WriteString("\n")
	}
	return sb.String()
}

func TestEffectiveRedactsSecrets(t *testing.T) {
	cfg := Config{
		Addr:                       ":9999",
		MasterKey:                  fakeSecret,
		StorageSigningKey:          fakeSecret,
		DefaultTranscriberToken:    fakeSecret,
		DefaultAgentToken:          fakeSecret,
		GoogleOAuthClientSecret:    fakeSecret,
		MicrosoftOAuthClientSecret: fakeSecret,
	}

	entries := Effective(cfg, noEnv)
	blob := entriesString(entries)
	if strings.Contains(blob, fakeSecret) {
		t.Fatalf("fake secret leaked into effective config output: %s", blob)
	}

	wantSet := map[string]bool{
		"MUESLI_MASTER_KEY":                    true,
		"MUESLI_STORAGE_SIGNING_KEY":           true,
		"MUESLI_DEFAULT_TRANSCRIBER_TOKEN":     true,
		"MUESLI_DEFAULT_AGENT_TOKEN":           true,
		"MUESLI_GOOGLE_OAUTH_CLIENT_SECRET":    true,
		"MUESLI_MICROSOFT_OAUTH_CLIENT_SECRET": true,
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if wantSet[e.EnvVar] {
			seen[e.EnvVar] = true
			if e.Value != "(set)" {
				t.Errorf("%s: value = %q, want \"(set)\"", e.EnvVar, e.Value)
			}
		}
	}
	for name := range wantSet {
		if !seen[name] {
			t.Errorf("expected entry %s not found in Effective() output", name)
		}
	}
}

func TestEffectiveRedactsUnsetSecrets(t *testing.T) {
	cfg := Config{} // all secrets empty

	entries := Effective(cfg, noEnv)
	for _, e := range entries {
		if e.EnvVar == "MUESLI_MASTER_KEY" {
			if e.Value != "(unset)" {
				t.Errorf("MUESLI_MASTER_KEY value = %q, want \"(unset)\"", e.Value)
			}
		}
	}
}

func TestEffectiveNonSecretValuePassesThrough(t *testing.T) {
	cfg := Config{Addr: ":1234"}

	entries := Effective(cfg, noEnv)
	found := false
	for _, e := range entries {
		if e.EnvVar == "MUESLI_ADDR" {
			found = true
			if e.Value != ":1234" {
				t.Errorf("MUESLI_ADDR value = %q, want %q", e.Value, ":1234")
			}
			if e.Name == "" {
				t.Error("expected a non-empty display Name for MUESLI_ADDR")
			}
		}
	}
	if !found {
		t.Fatal("MUESLI_ADDR entry not found")
	}
}

func TestEffectiveGoogleOAuthRedirectURLPassesThrough(t *testing.T) {
	cfg := Config{GoogleOAuthRedirectURL: "https://example.test/api/calendar/oauth/google/callback"}

	entries := Effective(cfg, noEnv)
	for _, e := range entries {
		if e.EnvVar == "MUESLI_GOOGLE_OAUTH_REDIRECT_URL" {
			if e.Value != cfg.GoogleOAuthRedirectURL {
				t.Fatalf("redirect url value = %q, want %q", e.Value, cfg.GoogleOAuthRedirectURL)
			}
			return
		}
	}
	t.Fatal("MUESLI_GOOGLE_OAUTH_REDIRECT_URL entry not found")
}

func TestEffectiveMicrosoftOAuthRedirectURLPassesThrough(t *testing.T) {
	cfg := Config{MicrosoftOAuthRedirectURL: "https://example.test/api/calendar/oauth/microsoft/callback"}

	entries := Effective(cfg, noEnv)
	for _, e := range entries {
		if e.EnvVar == "MUESLI_MICROSOFT_OAUTH_REDIRECT_URL" {
			if e.Value != cfg.MicrosoftOAuthRedirectURL {
				t.Fatalf("redirect url value = %q, want %q", e.Value, cfg.MicrosoftOAuthRedirectURL)
			}
			return
		}
	}
	t.Fatal("MUESLI_MICROSOFT_OAUTH_REDIRECT_URL entry not found")
}

func TestEffectiveSourceReflectsEnvPresence(t *testing.T) {
	cfg := Config{Addr: ":8080", TrashRetentionDays: 30}
	lookup := mapLookup(map[string]string{"MUESLI_ADDR": ":8080"})

	entries := Effective(cfg, lookup)
	var addrSource, trashSource string
	for _, e := range entries {
		switch e.EnvVar {
		case "MUESLI_ADDR":
			addrSource = e.Source
		case "MUESLI_TRASH_RETENTION_DAYS":
			trashSource = e.Source
		}
	}
	if addrSource != "env" {
		t.Errorf("MUESLI_ADDR source = %q, want \"env\"", addrSource)
	}
	if trashSource != "default" {
		t.Errorf("MUESLI_TRASH_RETENTION_DAYS source = %q, want \"default\"", trashSource)
	}
}

// TestEffectiveSourcePresenceNotEmptiness is the regression guard for the
// code-review blocking finding: an env var that is EXPLICITLY set
// to "" (e.g. an operator deliberately clearing MUESLI_BACKUP_DIR, or
// MUESLI_WEBHOOK_URL/MUESLI_ALLOWED_ORIGINS whose legitimate value is
// empty) must still report source="env", never "default" — provenance is
// about presence in the environment, not non-emptiness of the resulting
// value.
func TestEffectiveSourcePresenceNotEmptiness(t *testing.T) {
	cfg := Config{BackupDir: "", WebhookURL: "", AllowedOrigins: nil}
	lookup := mapLookup(map[string]string{
		"MUESLI_BACKUP_DIR":      "", // explicitly set to empty
		"MUESLI_WEBHOOK_URL":     "", // explicitly set to empty
		"MUESLI_ALLOWED_ORIGINS": "", // explicitly set to empty
	})

	entries := Effective(cfg, lookup)
	want := map[string]string{
		"MUESLI_BACKUP_DIR":      "env",
		"MUESLI_WEBHOOK_URL":     "env",
		"MUESLI_ALLOWED_ORIGINS": "env",
	}
	got := map[string]string{}
	for _, e := range entries {
		if _, ok := want[e.EnvVar]; ok {
			got[e.EnvVar] = e.Source
		}
	}
	for envVar, wantSource := range want {
		if got[envVar] != wantSource {
			t.Errorf("%s source = %q, want %q (explicitly set to \"\" must still be source=env)", envVar, got[envVar], wantSource)
		}
	}

	// And the inverse: a truly absent env var (not in the lookup map at
	// all) with an empty struct value must still report "default".
	absentEntries := Effective(Config{}, noEnv)
	for _, e := range absentEntries {
		if e.EnvVar == "MUESLI_BACKUP_DIR" && e.Source != "default" {
			t.Errorf("MUESLI_BACKUP_DIR source = %q, want \"default\" when truly absent", e.Source)
		}
	}
}

// TestEffectiveIncludesTrashRetentionDays is a regression guard for the
// ADM07 coordination note: MUESLI_TRASH_RETENTION_DAYS must appear like any
// other field, with no special-casing.
func TestEffectiveIncludesTrashRetentionDays(t *testing.T) {
	cfg := Config{TrashRetentionDays: 45}
	entries := Effective(cfg, noEnv)
	for _, e := range entries {
		if e.EnvVar == "MUESLI_TRASH_RETENTION_DAYS" {
			if e.Value != "45" {
				t.Errorf("MUESLI_TRASH_RETENTION_DAYS value = %q, want \"45\"", e.Value)
			}
			return
		}
	}
	t.Fatal("MUESLI_TRASH_RETENTION_DAYS entry not found in Effective() output")
}

func TestEffectiveEntryCountSane(t *testing.T) {
	entries := Effective(Config{}, noEnv)
	if len(entries) < 30 {
		t.Fatalf("expected at least 30 entries, got %d (%s)", len(entries), strconv.Itoa(len(entries)))
	}
}

// TestEffectiveExcludesNonMuesliVars documents the scope boundary: only
// MUESLI_* env vars are included (e.g. DATABASE_URL is out of scope).
func TestEffectiveExcludesNonMuesliVars(t *testing.T) {
	cfg := Config{DatabaseURL: "postgres://example/should-not-appear"}
	entries := Effective(cfg, noEnv)
	blob := entriesString(entries)
	if strings.Contains(blob, "should-not-appear") {
		t.Fatalf("DatabaseURL leaked into Effective() output, which must be MUESLI_*-scoped only: %s", blob)
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.EnvVar, "MUESLI_") {
			t.Errorf("entry %+v has a non-MUESLI_ env var, out of scope", e)
		}
	}
}
