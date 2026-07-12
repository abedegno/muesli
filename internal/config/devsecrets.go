package config

// devDefaults maps env-var names to their known dev-only default values
// (taken verbatim from docker-compose.yml). An empty string as the default
// value means the field has no known dev default (so it is never flagged).
var devDefaults = map[string]string{
	"MUESLI_MASTER_KEY":                          "+kdb0f+R3nCdy80T2zDMmZm5lUxfWpzehijIE3Zvsw8=",
	"MUESLI_STORAGE_SIGNING_KEY":                 "dev-storage-signing-key-change-me",
	"MUESLI_DEFAULT_TRANSCRIBER_TOKEN":           "dev-whisper-token",
	"MUESLI_DEFAULT_STREAMING_TRANSCRIBER_TOKEN": "dev-streaming-token",
	"MUESLI_DEFAULT_AGENT_TOKEN":                 "dev-agent-token",
}

// devDefaultOrder preserves a stable, predictable iteration order for the map
// above (Go map iteration is random).
var devDefaultOrder = []string{
	"MUESLI_MASTER_KEY",
	"MUESLI_STORAGE_SIGNING_KEY",
	"MUESLI_DEFAULT_TRANSCRIBER_TOKEN",
	"MUESLI_DEFAULT_STREAMING_TRANSCRIBER_TOKEN",
	"MUESLI_DEFAULT_AGENT_TOKEN",
}

// fieldValue returns the Config field value that corresponds to an env-var name.
// Returns "" for unknown names (which are never flagged).
func fieldValue(cfg Config, envVar string) string {
	switch envVar {
	case "MUESLI_MASTER_KEY":
		return cfg.MasterKey
	case "MUESLI_STORAGE_SIGNING_KEY":
		return cfg.StorageSigningKey
	case "MUESLI_DEFAULT_TRANSCRIBER_TOKEN":
		return cfg.DefaultTranscriberToken
	case "MUESLI_DEFAULT_STREAMING_TRANSCRIBER_TOKEN":
		return cfg.DefaultStreamingTranscriberToken
	case "MUESLI_DEFAULT_AGENT_TOKEN":
		return cfg.DefaultAgentToken
	default:
		return ""
	}
}

// DevSecretWarnings returns the env-var names of any Config field that is set
// to a known dev-only default value. An empty slice means all secrets look
// non-default (or are simply unset — an empty field cannot match a dev default).
func DevSecretWarnings(cfg Config) []string {
	var warnings []string
	for _, envVar := range devDefaultOrder {
		devVal := devDefaults[envVar]
		if devVal == "" {
			continue
		}
		actual := fieldValue(cfg, envVar)
		if actual != "" && actual == devVal {
			warnings = append(warnings, envVar)
		}
	}
	return warnings
}
