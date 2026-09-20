package execution

import "errors"

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
