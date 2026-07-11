# Muesli Whisper transcriber plugin

Local transcription via [faster-whisper](https://github.com/SYSTRAN/faster-whisper).
Implements the Muesli transcriber contract (`GET /info`, `GET /health`, `POST /transcribe`).

## Run

    docker build -t muesli/whisper-transcriber .
    docker run -p 8000:8000 -e MUESLI_PLUGIN_TOKEN=changeme muesli/whisper-transcriber

The Muesli server sends `Authorization: Bearer $MUESLI_PLUGIN_TOKEN` and
`X-Muesli-Plugin-API: 1` on every request.

## Config (`config` field on /transcribe, schema in /info)

| field         | meaning                                                    |
| ------------- | ---------------------------------------------------------- |
| model         | faster-whisper size: tiny/base/small/medium/large-v3       |
| language_hint | ISO-639-1 code, empty = autodetect                         |
| beam_size     | decoding beam (default 5)                                  |
| multitrack    | opt-in channel-per-speaker mode, default false (see below) |

## GPU

Build on a CUDA base image (e.g. `nvidia/cuda:12-cudnn-runtime`), install the
CUDA build of ctranslate2, and run with `WHISPER_DEVICE=cuda
WHISPER_COMPUTE_TYPE=float16`. Everything else is identical.

## Scale-to-zero

The service is stateless (no DB, no disk state beyond a temp file per request,
deleted immediately). Run a single uvicorn worker so the model stays cached
in-process; deploy on Cloud Run / KEDA with min-instances 0. Cold start = process
start + first model load; keep the model small (`base`) or bake weights into the
image to cut cold-start latency.

## Source tagging (v1 limitation) / multitrack mode

v1 returns every segment with `source: "mixed"` — we do not separate mic vs
system audio server-side. When the desktop client uploads separate tracks
(backlog item), this plugin can tag segments per track. Until then `mixed` is
the honest answer.

For recording hardware that already isolates speakers per channel (multi-mic
interfaces, conference systems, podcast decks), set `config.multitrack: true`
on `/transcribe`. This is opt-in and orthogonal to the separate
speaker-diarization feature (which infers speakers from a single mixed
channel via voice modelling) — multitrack instead relies on deterministic
channel isolation already present in the source audio, when available:

1. The uploaded audio is probed with `ffprobe` for its channel count.
2. If it has more than one channel, each channel is checked with ffmpeg's
   `volumedetect` filter and any near-silent channel (mean/max volume at or
   below ~-60 dBFS — e.g. an unused mic input) is skipped.
3. Each remaining channel is split out to its own mono track (ffmpeg `pan`
   filter) and transcribed independently through the same faster-whisper
   model used for the single-pass path (one model load, reused per channel).
4. All channels' segments are merged into one list, interleaved by
   `start_ms`, with `Segment.speaker` set to a deterministic per-channel label
   ("Speaker 1", "Speaker 2", ...) assigned in channel order among the
   non-silent channels. `source` remains `"mixed"`, consistent with today's
   convention.

When `multitrack` is disabled, the input is mono, or `ffprobe` reports a
single channel, behavior is unchanged: the existing single-pass transcription
runs exactly as before. This mode requires `ffmpeg`/`ffprobe` to be on `PATH`
(the reference Dockerfile installs the `ffmpeg` package for this).
