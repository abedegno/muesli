package live

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/pluginkit"
	"github.com/abedegno/muesli/internal/whispercpp/engine"
)

func TestParseSessionConfigAcceptsValidValues(t *testing.T) {
	defaultThreshold := pluginkit.DefaultStreamingConfig().EnergyThreshold
	for _, tc := range []struct {
		name      string
		raw       string
		wantVAD   string
		wantLevel float64
	}{
		{"absent", "", VADFixed, defaultThreshold},
		{"json null", "null", VADFixed, defaultThreshold},
		{"empty object", "{}", VADFixed, defaultThreshold},
		{"explicit fixed", `{"vad":"fixed"}`, VADFixed, defaultThreshold},
		{"adaptive", `{"vad":"adaptive"}`, VADAdaptive, defaultThreshold},
		{"empty mode keeps default", `{"vad":""}`, VADFixed, defaultThreshold},
		{"threshold only", `{"vad_threshold":0.03}`, VADFixed, 0.03},
		{"both", `{"vad":"adaptive","vad_threshold":0.05}`, VADAdaptive, 0.05},
		{"zero threshold", `{"vad_threshold":0}`, VADFixed, 0},
		{"unrelated properties tolerated", `{"model":"tiny.en","language":"en","multitrack":true}`, VADFixed, defaultThreshold},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSessionConfig(json.RawMessage(tc.raw))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.vad != tc.wantVAD || got.threshold != tc.wantLevel {
				t.Errorf("got {%s %v}, want {%s %v}", got.vad, got.threshold, tc.wantVAD, tc.wantLevel)
			}
		})
	}
}

func TestParseSessionConfigRejectsInvalidValues(t *testing.T) {
	for _, tc := range []struct {
		name, raw, wantMessage string
	}{
		{"unknown mode", `{"vad":"silero"}`, "unknown vad mode"},
		{"negative threshold", `{"vad_threshold":-0.1}`, "out of range"},
		{"threshold above one", `{"vad_threshold":1.5}`, "out of range"},
		{"threshold not a number", `{"vad_threshold":"loud"}`, "not a JSON object"},
		{"mode not a string", `{"vad":7}`, "not a JSON object"},
		{"array instead of object", `[]`, "not a JSON object"},
		{"string instead of object", `"fixed"`, "not a JSON object"},
		{"malformed json", `{"vad":`, "not a JSON object"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseSessionConfig(json.RawMessage(tc.raw))
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.wantMessage) {
				t.Errorf("error %q does not mention %q", err, tc.wantMessage)
			}
		})
	}
}

func TestConfigSchemaExtendsTheEngineSchema(t *testing.T) {
	extended := ConfigSchema(engine.ConfigSchema)

	var got struct {
		Properties           map[string]json.RawMessage `json:"properties"`
		AdditionalProperties *bool                      `json:"additionalProperties"`
	}
	if err := json.Unmarshal(extended, &got); err != nil {
		t.Fatalf("extended schema is not valid JSON: %v", err)
	}

	// The engine's own properties must survive, or the streaming plugin's
	// existing settings would disappear from the admin UI.
	var base struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(engine.ConfigSchema, &base); err != nil {
		t.Fatalf("engine schema is not valid JSON: %v", err)
	}
	for name, definition := range base.Properties {
		kept, ok := got.Properties[name]
		if !ok {
			t.Errorf("engine property %q was dropped", name)
			continue
		}
		// Compared semantically: re-marshalling reorders object keys, so equal
		// schemas need not be byte-identical.
		var keptValue, originalValue any
		if err := json.Unmarshal(kept, &keptValue); err != nil {
			t.Errorf("property %q is not valid JSON after extension: %v", name, err)
			continue
		}
		if err := json.Unmarshal(definition, &originalValue); err != nil {
			t.Fatalf("engine property %q is not valid JSON: %v", name, err)
		}
		if !reflect.DeepEqual(keptValue, originalValue) {
			t.Errorf("engine property %q was altered:\n got %v\nwant %v", name, keptValue, originalValue)
		}
	}

	for _, name := range []string{"vad", "vad_threshold"} {
		if _, ok := got.Properties[name]; !ok {
			t.Errorf("streaming property %q missing from the published schema", name)
		}
	}
	if got.AdditionalProperties == nil || *got.AdditionalProperties {
		t.Error("additionalProperties must stay false so the new settings validate")
	}
}

func TestConfigSchemaFallsBackWhenTheBaseIsUnusable(t *testing.T) {
	for _, base := range []string{``, `not json`, `[]`, `{"no":"properties"}`} {
		if got := ConfigSchema(json.RawMessage(base)); string(got) != base {
			t.Errorf("base %q: expected it returned unchanged, got %q", base, got)
		}
	}
}

func TestNewVADIsConstructedPerSession(t *testing.T) {
	streaming := pluginkit.DefaultStreamingConfig()

	fixed, err := newVAD(sessionConfig{vad: VADFixed, threshold: 0.02}, streaming)
	if err != nil {
		t.Fatal(err)
	}
	if fixed != nil {
		t.Error("fixed mode should defer to the session's own energy detector")
	}

	first, err := newVAD(sessionConfig{vad: VADAdaptive, threshold: 0.02}, streaming)
	if err != nil {
		t.Fatal(err)
	}
	second, err := newVAD(sessionConfig{vad: VADAdaptive, threshold: 0.02}, streaming)
	if err != nil {
		t.Fatal(err)
	}
	if first == nil || second == nil {
		t.Fatal("adaptive mode must supply a detector")
	}
	// Sharing one adaptive detector between streams would mix their audio into
	// a single estimate and race, since sessions are serialized independently.
	if first == second {
		t.Error("adaptive detectors must not be shared between sessions")
	}

	adaptive, ok := first.(*pluginkit.AdaptiveEnergyVAD)
	if !ok {
		t.Fatalf("expected an adaptive detector, got %T", first)
	}
	if adaptive.Threshold() != 0.02 {
		t.Errorf("configured threshold should seed warm-up, got %v", adaptive.Threshold())
	}
}
