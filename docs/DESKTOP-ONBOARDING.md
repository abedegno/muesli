# Desktop Onboarding

This guide explains what the desktop app needs on first launch, what happens
when Ollama is missing, and how to point Muesli at a non-default Ollama
instance.

## Embedded mode

In embedded mode, the desktop app runs its own Postgres, pgvector, and Whisper
services locally. See [EMBEDDED-POSTGRES-BUNDLE.md](EMBEDDED-POSTGRES-BUNDLE.md)
for the embedded database bundle and operational details.

Because those services are bundled with the app, recording and transcription do
not require any external services.

## Degraded mode

When the server reports `embedded.ollamaDetected: false` from `/readyz`, the
desktop app enters degraded mode. The probe is implemented in
[`internal/embedded/ollama.go`](../internal/embedded/ollama.go) via
`DetectOllama`, which checks `http://127.0.0.1:11434/api/version` with a
1.5s timeout.

Degraded mode only disables AI summaries and semantic search. Recording,
transcription, and manual note-taking continue to work.

## Install Ollama

The first-run `SetupWizard` AI step already shows these platform-specific
install hints, and the persistent `StartupBanner` uses the same Ollama
download flow when the app starts in degraded mode.

- macOS: `brew install ollama`
- Linux: `curl -fsSL https://ollama.com/install.sh | sh`
- Windows: download the installer from [ollama.com](https://ollama.com)

## Use a remote Ollama

To point Muesli at a non-default or remote Ollama instance, set
`MUESLI_OLLAMA_URL`.

[`internal/embedded/ollama.go`](../internal/embedded/ollama.go) exposes this
through `OllamaBaseURL()`, which falls back to `DefaultOllamaURL`
(`http://127.0.0.1:11434`) when the variable is unset or blank.

## Where this appears

- First run: `SetupWizard` AI step
- Later launches: `StartupBanner` in [`src/renderer/components/StartupScreen.tsx`](../src/renderer/components/StartupScreen.tsx)
