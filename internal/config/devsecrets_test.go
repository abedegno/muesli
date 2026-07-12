package config

import (
	"testing"
)

func TestDevSecretWarnings(t *testing.T) {
	// helper to build a minimal valid Config for these tests
	base := func() Config {
		return Config{}
	}

	tests := []struct {
		name     string
		cfg      Config
		wantVars []string // env-var names expected in result (order matters)
	}{
		{
			name:     "all empty — no warnings",
			cfg:      base(),
			wantVars: nil,
		},
		{
			name: "real values — no warnings",
			cfg: Config{
				MasterKey:                        "cmVhbC1rZXktdGhhdC1pcy1ub3QtZGVmYXVsdA==",
				StorageSigningKey:                "real-signing-key",
				DefaultTranscriberToken:          "real-whisper-token",
				DefaultStreamingTranscriberToken: "real-streaming-token",
				DefaultAgentToken:                "real-agent-token",
			},
			wantVars: nil,
		},
		{
			name: "dev master key only",
			cfg: Config{
				MasterKey: "+kdb0f+R3nCdy80T2zDMmZm5lUxfWpzehijIE3Zvsw8=",
			},
			wantVars: []string{"MUESLI_MASTER_KEY"},
		},
		{
			name: "dev storage signing key only",
			cfg: Config{
				StorageSigningKey: "dev-storage-signing-key-change-me",
			},
			wantVars: []string{"MUESLI_STORAGE_SIGNING_KEY"},
		},
		{
			name: "dev transcriber token only",
			cfg: Config{
				DefaultTranscriberToken: "dev-whisper-token",
			},
			wantVars: []string{"MUESLI_DEFAULT_TRANSCRIBER_TOKEN"},
		},
		{
			name: "dev streaming transcriber token only",
			cfg: Config{
				DefaultStreamingTranscriberToken: "dev-streaming-token",
			},
			wantVars: []string{"MUESLI_DEFAULT_STREAMING_TRANSCRIBER_TOKEN"},
		},
		{
			name: "dev agent token only",
			cfg: Config{
				DefaultAgentToken: "dev-agent-token",
			},
			wantVars: []string{"MUESLI_DEFAULT_AGENT_TOKEN"},
		},
		{
			name: "all four dev defaults at once",
			cfg: Config{
				MasterKey:                        "+kdb0f+R3nCdy80T2zDMmZm5lUxfWpzehijIE3Zvsw8=",
				StorageSigningKey:                "dev-storage-signing-key-change-me",
				DefaultTranscriberToken:          "dev-whisper-token",
				DefaultStreamingTranscriberToken: "dev-streaming-token",
				DefaultAgentToken:                "dev-agent-token",
			},
			wantVars: []string{
				"MUESLI_MASTER_KEY",
				"MUESLI_STORAGE_SIGNING_KEY",
				"MUESLI_DEFAULT_TRANSCRIBER_TOKEN",
				"MUESLI_DEFAULT_STREAMING_TRANSCRIBER_TOKEN",
				"MUESLI_DEFAULT_AGENT_TOKEN",
			},
		},
		{
			name: "real master key, other fields empty — no warnings",
			cfg: Config{
				MasterKey: "cmVhbC1rZXktdGhhdC1pcy1ub3QtZGVmYXVsdA==",
			},
			wantVars: nil,
		},
		{
			name: "mix: real master key, dev signing key, empty tokens",
			cfg: Config{
				MasterKey:         "cmVhbC1rZXktdGhhdC1pcy1ub3QtZGVmYXVsdA==",
				StorageSigningKey: "dev-storage-signing-key-change-me",
			},
			wantVars: []string{"MUESLI_STORAGE_SIGNING_KEY"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DevSecretWarnings(tc.cfg)

			if len(got) != len(tc.wantVars) {
				t.Fatalf("DevSecretWarnings() = %v (len %d), want %v (len %d)",
					got, len(got), tc.wantVars, len(tc.wantVars))
			}
			for i, v := range tc.wantVars {
				if got[i] != v {
					t.Errorf("DevSecretWarnings()[%d] = %q, want %q", i, got[i], v)
				}
			}
		})
	}
}

// TestProductionFlagDetected verifies that the Production field and
// DevSecretWarnings together can be used to decide whether to refuse startup.
// (We don't test log.Fatalf itself — instead we confirm the logic a caller
// would apply.)
func TestProductionFlagDetected(t *testing.T) {
	cfgProdWithDevSecrets := Config{
		Production: true,
		MasterKey:  "+kdb0f+R3nCdy80T2zDMmZm5lUxfWpzehijIE3Zvsw8=",
	}
	warnings := DevSecretWarnings(cfgProdWithDevSecrets)
	if len(warnings) == 0 {
		t.Fatal("expected at least one warning for dev master key in production config")
	}
	if !cfgProdWithDevSecrets.Production {
		t.Fatal("Production field should be true")
	}

	cfgProdNoDevSecrets := Config{
		Production: true,
		MasterKey:  "cmVhbC1rZXktdGhhdC1pcy1ub3QtZGVmYXVsdA==",
	}
	warnings = DevSecretWarnings(cfgProdNoDevSecrets)
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings for real secrets in production, got %v", warnings)
	}
}

// TestLoadProductionFlag verifies the Production flag is parsed from env.
func TestLoadProductionFlag(t *testing.T) {
	tests := []struct {
		envVal string
		want   bool
	}{
		{"true", true},
		{"1", true},
		{"false", false},
		{"0", false},
		{"", false},
		{"yes", false},
	}
	for _, tc := range tests {
		t.Run("MUESLI_PRODUCTION="+tc.envVal, func(t *testing.T) {
			cfg, err := Load(func(k string) string {
				switch k {
				case "MUESLI_PRODUCTION":
					return tc.envVal
				case "DATABASE_URL":
					return "postgres://localhost/test"
				default:
					return ""
				}
			})
			if err != nil {
				t.Fatalf("Load() error: %v", err)
			}
			if cfg.Production != tc.want {
				t.Errorf("Production = %v, want %v (env %q)", cfg.Production, tc.want, tc.envVal)
			}
		})
	}
}
