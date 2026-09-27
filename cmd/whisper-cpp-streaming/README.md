# whisper-cpp streaming transcriber

This binary provides muesli's live WebSocket transcription protocol using the
same whisper.cpp engine as the bundled batch transcriber. Live partials are
disposable UI feedback: the batch `JobTranscribe` pipeline still creates the
saved transcript after recording, and this plugin never writes saved text.

By default, `tiny`, `base`, and their `.en` variants reuse the batch model from
`MUESLI_WHISPER_MODEL`. Heavier batch models use a lazily downloaded `tiny.en`
model for live work. `MUESLI_WHISPER_LIVE_MODEL` always wins. The remaining
settings mirror the batch plugin under the `MUESLI_WHISPER_LIVE_` prefix:
`ADDR`, `TOKEN`, `NAME`, `VERSION`, `MODEL_DIR`, `MODEL_URL`, and `LANGUAGE`.

This is CPU-bound, single-model inference intended as the local/desktop
default. It is **not recommended for hosted scale**. Deployments serving
concurrent meetings should use a GPU-backed service, the Python plugin, or a
cloud transcriber.

## AMI VAD detector evaluation (`ami-eval`)

`cmd/whisper-cpp-streaming/ami-eval` is a separate, offline-by-default
evaluation command (issue #778) that reproduces a detector comparison for the
voice-activity detectors this streaming path uses. It does not affect the
streaming server above; it only reads it. Run it via `make evaluate-ami-vad`.
See [docs/ami-vad-evaluation.md](../../docs/ami-vad-evaluation.md) for full
usage, and `cmd/whisper-cpp-streaming/ami-eval/manifest.json` for the pinned
corpus objects it acquires.
