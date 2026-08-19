package live

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/abedegno/muesli/internal/pluginkit"
)

// Voice-activity detection modes selectable per session.
const (
	// VADFixed compares frame energy against a fixed threshold. It is the
	// default because it is predictable, and because the adaptive detector has
	// a measured limitation at very low speech occupancy (see muesli#565).
	VADFixed = "fixed"
	// VADAdaptive estimates the threshold from the audio itself. Experimental.
	VADAdaptive = "adaptive"
)

// streamingProperties are the settings this plugin honours per session, on top
// of whatever the shared engine schema publishes. They live here rather than in
// the engine schema because that schema is also published by the *batch*
// whisper plugin, where a voice-activity setting means nothing.
const streamingProperties = `{
	"vad": {
		"type": "string",
		"enum": ["fixed", "adaptive"],
		"default": "fixed",
		"title": "Voice activity detection",
		"description": "How speech is separated from silence. \"fixed\" compares against the threshold below. \"adaptive\" estimates the threshold from the audio, which handles rooms whose background noise is louder than the fixed threshold, but is experimental."
	},
	"vad_threshold": {
		"type": "number",
		"minimum": 0,
		"maximum": 1,
		"default": 0.01,
		"title": "Fixed VAD threshold",
		"description": "RMS energy above which audio counts as speech in fixed mode, and during warm-up in adaptive mode. Raise it in a noisy room: a threshold below the room's own noise floor never detects silence, so nothing is ever transcribed."
	}
}`

// ConfigSchema extends the engine's published schema with the streaming-only
// settings above. It extends rather than replaces so that the engine's own
// properties keep whatever defaults the active build publishes.
func ConfigSchema(engineSchema json.RawMessage) json.RawMessage {
	var schema map[string]any
	if err := json.Unmarshal(engineSchema, &schema); err != nil {
		return engineSchema
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return engineSchema
	}
	var extra map[string]any
	if err := json.Unmarshal([]byte(streamingProperties), &extra); err != nil {
		return engineSchema
	}
	for name, definition := range extra {
		properties[name] = definition
	}
	merged, err := json.Marshal(schema)
	if err != nil {
		return engineSchema
	}
	return merged
}

// sessionConfig is the subset of a session's stored plugin config that the
// streaming path honours.
type sessionConfig struct {
	vad       string
	threshold float64
}

// parseSessionConfig validates the per-session config supplied at stream start.
// A JSON Schema in the admin UI is not runtime validation: the value arrives
// here as a stored snapshot that may predate the schema, so it is checked
// again. Unknown *properties* are tolerated for forward compatibility; unknown
// or out-of-range *values* are rejected rather than silently defaulted, so a
// misconfigured plugin fails visibly at stream start instead of transcribing
// with settings nobody chose.
func parseSessionConfig(raw json.RawMessage) (sessionConfig, error) {
	cfg := sessionConfig{vad: VADFixed, threshold: pluginkit.DefaultStreamingConfig().EnergyThreshold}

	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return cfg, nil
	}

	var fields struct {
		VAD       *string  `json:"vad"`
		Threshold *float64 `json:"vad_threshold"`
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return sessionConfig{}, fmt.Errorf("streaming config is not a JSON object: %w", err)
	}

	if fields.VAD != nil {
		switch mode := strings.TrimSpace(*fields.VAD); mode {
		case "":
			// Absent in practice: an empty string keeps the default rather than
			// failing a config that simply never set the field.
		case VADFixed, VADAdaptive:
			cfg.vad = mode
		default:
			return sessionConfig{}, fmt.Errorf("unknown vad mode %q: want %q or %q", mode, VADFixed, VADAdaptive)
		}
	}

	if fields.Threshold != nil {
		t := *fields.Threshold
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return sessionConfig{}, errors.New("vad_threshold must be a finite number")
		}
		if t < 0 || t > 1 {
			return sessionConfig{}, fmt.Errorf("vad_threshold %v out of range [0,1]", t)
		}
		cfg.threshold = t
	}

	return cfg, nil
}

// newVAD builds the detector for one session. It returns nil for fixed mode,
// which lets StreamingSession construct its own energy detector from the
// threshold. A detector is always constructed per session: the adaptive one
// carries per-session state and must never be shared between streams.
func newVAD(cfg sessionConfig, streaming pluginkit.StreamingConfig) (pluginkit.VAD, error) {
	if cfg.vad != VADAdaptive {
		return nil, nil
	}
	adaptive := pluginkit.DefaultAdaptiveVADConfig(streaming.SampleRate, streaming.VADFrame)
	adaptive.Fallback = cfg.threshold
	return pluginkit.NewAdaptiveEnergyVAD(adaptive)
}
