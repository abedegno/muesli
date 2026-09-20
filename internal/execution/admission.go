package execution

import (
	"encoding/json"
	"errors"
)

// AdmissionConfig is the byte-conservative context budget an agent plugin
// declares (see internal/embedded/agent.go and plugins/ollama-agent's
// /info): a raw context-window size plus reserves subtracted from it before
// any prompt is admitted. ByteFallbackTokenizer must be true -- an agent
// that has not declared this capability fails closed (ErrInvalidAdmissionConfig)
// rather than falling back to an unproven heuristic (see the accepted plan's
// ruling 3).
type AdmissionConfig struct {
	ContextTokens         int
	OutputReserveTokens   int
	ProviderFramingTokens int
	ByteFallbackTokenizer bool
}

// ErrInvalidAdmissionConfig is returned by Admit when cfg itself is
// malformed: non-positive context, negative reserves, reserves that consume
// the whole context, or a missing byte-fallback-tokenizer capability
// declaration.
var ErrInvalidAdmissionConfig = errors.New("execution: invalid admission config")

// ErrContextBudget is returned by Admit when the largest complete
// section-prompt (see SectionPrompt) exceeds the effective byte ceiling.
var ErrContextBudget = errors.New("execution: exceeds context budget")

// Admit validates cfg and then checks every one of prepared's SectionPrompts
// against the effective ceiling: ContextTokens minus OutputReserveTokens
// minus ProviderFramingTokens, counting one possible token per UTF-8 byte of
// the complete rendered section prompt (see the accepted plan's ruling 3 --
// this intentionally over-rejects multilingual text, code, punctuation, and
// identifiers rather than risk under-counting). Admits exactly at the
// ceiling; rejects one byte over it. Performs no I/O and calls the Generator
// zero times.
func Admit(prepared PreparedExecution, cfg AdmissionConfig) error {
	if cfg.ContextTokens <= 0 {
		return ErrInvalidAdmissionConfig
	}
	if cfg.OutputReserveTokens < 0 || cfg.ProviderFramingTokens < 0 {
		return ErrInvalidAdmissionConfig
	}
	if !cfg.ByteFallbackTokenizer {
		return ErrInvalidAdmissionConfig
	}
	effective := cfg.ContextTokens - cfg.OutputReserveTokens - cfg.ProviderFramingTokens
	if effective <= 0 {
		return ErrInvalidAdmissionConfig
	}
	for _, sp := range prepared.SectionPrompts {
		if len([]byte(sp.Prompt)) > effective {
			return ErrContextBudget
		}
	}
	return nil
}

// admissionConfigJSON is the JSON shape an agent plugin's config publishes
// for admission (see plugins/ollama-agent's /info and
// internal/embedded/agent.go's agentConfigJSON). Pointer fields distinguish
// "absent" from "zero" so ParseAdmissionConfig can fail closed on a missing
// field rather than silently defaulting it.
type admissionConfigJSON struct {
	ContextTokens         *int  `json:"context_tokens"`
	OutputReserveTokens   *int  `json:"output_reserve_tokens"`
	ProviderFramingTokens *int  `json:"provider_framing_tokens"`
	ByteFallbackTokenizer *bool `json:"byte_fallback_tokenizer"`
}

// ParseAdmissionConfig decodes raw (an agent plugin's config or /info JSON)
// into an AdmissionConfig, failing closed -- a non-nil error, never a
// heuristic fallback -- on malformed JSON, any missing numeric/boolean
// field, a false/absent byte_fallback_tokenizer capability, or invalid
// numeric values (see Admit's own validation, applied here too so a caller
// never has to call both to be safe).
func ParseAdmissionConfig(raw []byte) (AdmissionConfig, error) {
	if len(raw) == 0 {
		return AdmissionConfig{}, ErrInvalidAdmissionConfig
	}
	var fields admissionConfigJSON
	if err := json.Unmarshal(raw, &fields); err != nil {
		return AdmissionConfig{}, ErrInvalidAdmissionConfig
	}
	if fields.ContextTokens == nil || fields.OutputReserveTokens == nil || fields.ProviderFramingTokens == nil || fields.ByteFallbackTokenizer == nil {
		return AdmissionConfig{}, ErrInvalidAdmissionConfig
	}
	cfg := AdmissionConfig{
		ContextTokens:         *fields.ContextTokens,
		OutputReserveTokens:   *fields.OutputReserveTokens,
		ProviderFramingTokens: *fields.ProviderFramingTokens,
		ByteFallbackTokenizer: *fields.ByteFallbackTokenizer,
	}
	if cfg.ContextTokens <= 0 || cfg.OutputReserveTokens < 0 || cfg.ProviderFramingTokens < 0 || !cfg.ByteFallbackTokenizer {
		return AdmissionConfig{}, ErrInvalidAdmissionConfig
	}
	if cfg.ContextTokens-cfg.OutputReserveTokens-cfg.ProviderFramingTokens <= 0 {
		return AdmissionConfig{}, ErrInvalidAdmissionConfig
	}
	return cfg, nil
}
