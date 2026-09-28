package live

import (
	"encoding/json"
	"fmt"
	"math"
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
		{"fixed with surrounding whitespace", `{"vad":" fixed "}`, VADFixed, defaultThreshold},
		{"adaptive with surrounding whitespace/tabs", `{"vad":"\tadaptive\n"}`, VADAdaptive, defaultThreshold},
		{"threshold only", `{"vad_threshold":0.03}`, VADFixed, 0.03},
		{"both", `{"vad":"adaptive","vad_threshold":0.05}`, VADAdaptive, 0.05},
		{"zero threshold", `{"vad_threshold":0}`, VADFixed, 0},
		{"threshold at upper bound", `{"vad_threshold":1}`, VADFixed, 1},
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
		{"whitespace-only mode is not silently defaulted", `{"vad":" "}`, "unknown vad mode"},
		{"tab-only mode is not silently defaulted", `{"vad":"\t"}`, "unknown vad mode"},
		{"negative threshold", `{"vad_threshold":-0.1}`, "out of range"},
		{"threshold above one", `{"vad_threshold":1.5}`, "out of range"},
		{"threshold overflow token", `{"vad_threshold":1e10000}`, "invalid streaming config"},
		{"threshold invalid numeric token", `{"vad_threshold":1.2.3}`, "invalid streaming config"},
		{"array instead of object", `[]`, "not a JSON object"},
		{"string instead of object", `"fixed"`, "not a JSON object"},
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

// TestParseSessionConfigUnknownModeErrorNamesOriginalSuppliedValue pins that
// the "unknown vad mode" error reports the mode exactly as it was supplied,
// not its trimmed form. Reporting the trimmed form makes a whitespace-only
// value indistinguishable from an empty one in the error message ("unknown
// vad mode """), which looks like a report about an absent/default value
// rather than the whitespace that was actually sent.
func TestParseSessionConfigUnknownModeErrorNamesOriginalSuppliedValue(t *testing.T) {
	for _, tc := range []struct {
		name, raw, original string
	}{
		{"whitespace-only mode", `{"vad":" "}`, " "},
		{"tab-only mode", `{"vad":"\t"}`, "	"},
		{"padded unknown mode", `{"vad":" silero "}`, " silero "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseSessionConfig(json.RawMessage(tc.raw))
			if err == nil {
				t.Fatal("expected an error")
			}
			wantQuoted := fmt.Sprintf("%q", tc.original)
			if !strings.Contains(err.Error(), wantQuoted) {
				t.Errorf("error %q does not report the original supplied mode %s", err, wantQuoted)
			}
		})
	}
}

// TestParseSessionConfigWrongTypedFieldIsNotReportedAsNonObject pins the
// distinction between a genuinely non-object top level (an array, a bare
// string: see TestParseSessionConfigRejectsInvalidValues) and a syntactically
// valid JSON object whose vad/vad_threshold field has the wrong type. Both
// used to share the same "not a JSON object" wording, which is misleading for
// the latter: the value *was* an object, just an invalid one. Malformed JSON
// (a decode failure that isn't even a wrong-typed field) must also stay
// distinguishable via its own wrapped syntax error rather than collapsing
// into either message.
func TestParseSessionConfigWrongTypedFieldIsNotReportedAsNonObject(t *testing.T) {
	for _, tc := range []struct {
		name, raw, wantField string
	}{
		{"vad wrong type", `{"vad":7}`, "vad"},
		{"vad_threshold wrong type", `{"vad_threshold":"loud"}`, "vad_threshold"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseSessionConfig(json.RawMessage(tc.raw))
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), "invalid streaming config") {
				t.Errorf("error %q does not mention %q", err, "invalid streaming config")
			}
			if !strings.Contains(err.Error(), tc.wantField) {
				t.Errorf("error %q does not name the offending field %q", err, tc.wantField)
			}
			if strings.Contains(err.Error(), "not a JSON object") {
				t.Errorf("error %q wrongly reused the non-object message for a wrong-typed field", err)
			}
		})
	}

	// Malformed JSON is a different failure again: it must not be reported
	// as "not a JSON object" (that message is reserved for a syntactically
	// valid, non-object top level) and must carry its own distinguishing
	// syntax-error text.
	_, err := parseSessionConfig(json.RawMessage(`{"vad":`))
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "not a JSON object") {
		t.Errorf("malformed JSON error %q wrongly reused the non-object message", err)
	}
	if !strings.Contains(err.Error(), "unexpected end of JSON input") {
		t.Errorf("malformed JSON error %q lost its distinguishing syntax error", err)
	}
}

func TestValidateVADThreshold(t *testing.T) {
	defaultThreshold := pluginkit.DefaultStreamingConfig().EnergyThreshold
	for _, accepted := range []float64{0, defaultThreshold, 0.5, 1} {
		t.Run("accepted", func(t *testing.T) {
			if err := validateVADThreshold(accepted); err != nil {
				t.Errorf("validateVADThreshold(%v) = %v, want nil", accepted, err)
			}
		})
	}

	for _, rejected := range []float64{-0.0001, 1.0001, math.NaN(), math.Inf(1), math.Inf(-1)} {
		t.Run("rejected", func(t *testing.T) {
			if err := validateVADThreshold(rejected); err == nil {
				t.Errorf("validateVADThreshold(%v) = nil, want an error", rejected)
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
