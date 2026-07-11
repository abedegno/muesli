# Muesli Plugin Authoring Guide

This guide explains how to write a Muesli plugin — a language-agnostic
HTTP/JSON service that the Muesli server calls to transcribe audio or generate
structured summaries.

---

## Table of Contents

1. [Introduction](#1-introduction)
2. [Plugin kinds](#2-plugin-kinds)
3. [HTTP contract — all endpoints](#3-http-contract--all-endpoints)
4. [Authentication](#4-authentication)
5. [Minimal worked example](#5-minimal-worked-example)
6. [Validating your plugin — the conformance suite](#6-validating-your-plugin--the-conformance-suite)
7. [Registering your plugin](#7-registering-your-plugin)

---

## 1. Introduction

A **Muesli plugin** is an independently-deployed HTTP/JSON service. The Muesli
server calls it over HTTP using a per-plugin bearer token. You can write a
plugin in any language or runtime — Python, Node.js, Go, Rust, or anything else
that can serve HTTP — as long as it implements the contract described below.

Key facts:

- The server-side client lives in [`internal/plugin/client.go`](../internal/plugin/client.go).
- The contract is versioned. All current plugins implement **`plugin_api: 1`**.
- There are exactly two plugin **kinds**: `transcriber` and `agent`.
- The reference transcribers are `whisper-transcriber` (CPU-first) and
  `parakeet-transcriber` (GPU-oriented).

The contract is pinned: the server sends `X-Muesli-Plugin-API: 1` on every
authenticated call and your plugin should verify it (see [§4 Authentication](#4-authentication)).

---

## 2. Plugin kinds

| Kind          | Purpose                                                         | Server calls                                   |
| ------------- | --------------------------------------------------------------- | ---------------------------------------------- |
| `transcriber` | Receives audio, returns a timed transcript                      | `POST /transcribe` (transcribe pipeline stage) |
| `agent`       | Receives a transcript + template, produces a structured summary | `POST /generate` (summarize pipeline stage)    |

A single plugin must be one kind only. It declares its kind in the `/info` response.

---

## 3. HTTP contract — all endpoints

Every plugin exposes three HTTP endpoints regardless of kind.

### `GET /info` — plugin metadata (authenticated)

Returns a JSON object describing the plugin. All fields below are required
except `config_schema`.

```json
{
  "name": "my-transcriber",
  "version": "1.0.0",
  "plugin_api": 1,
  "kind": "transcriber",
  "config_schema": {}
}
```

| Field           | Type    | Notes                                                                                                                                              |
| --------------- | ------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| `name`          | string  | Human-readable plugin name                                                                                                                         |
| `version`       | string  | Semver string                                                                                                                                      |
| `plugin_api`    | integer | Must be `1`                                                                                                                                        |
| `kind`          | string  | Must be `"transcriber"` or `"agent"`                                                                                                               |
| `config_schema` | object  | Optional. A JSON Schema describing the `config` object stored on the server. Recommended so the admin UI can validate config at registration time. |

### `GET /health` — liveness check (**no auth required**)

Returns HTTP `200`. The response body is ignored.

> **Important:** This endpoint **must not** require any `Authorization` header.
> It is called by scale-to-zero infrastructure and Kubernetes readiness probes
> before any bearer token is available. If `/health` requires auth, the
> infrastructure will consider the plugin unhealthy and kill it before it
> handles real traffic.

### `POST /transcribe` — audio transcription (transcriber only; authenticated)

**Request body:**

```json
{
  "audio_url": "https://example.com/uploads/audio.wav",
  "language_hint": "en",
  "options": {},
  "config": {}
}
```

| Field           | Type   | Notes                                                                 |
| --------------- | ------ | --------------------------------------------------------------------- |
| `audio_url`     | string | Presigned URL to download the audio file. Required.                   |
| `language_hint` | string | Optional BCP-47 language hint (e.g. `"en"`, `"fr"`).                  |
| `options`       | object | Optional free-form per-request overrides (e.g. `{"temperature": 0}`). |
| `config`        | object | The plugin's stored config, decrypted at call time. May be `{}`.      |

**Response body:**

```json
{
  "segments": [
    {
      "start_ms": 0,
      "end_ms": 1500,
      "text": "Hello, world.",
      "source": "whisper",
      "speaker": "Speaker 1"
    }
  ],
  "language": "en",
  "model": "whisper-large-v3",
  "duration_ms": 60000
}
```

| Field                 | Type    | Notes                                         |
| --------------------- | ------- | --------------------------------------------- |
| `segments`            | array   | Ordered list of transcript segments.          |
| `segments[].start_ms` | integer | Segment start time in milliseconds.           |
| `segments[].end_ms`   | integer | Segment end time in milliseconds.             |
| `segments[].text`     | string  | Transcribed text for this segment.            |
| `segments[].source`   | string  | Model/engine that produced this segment.      |
| `segments[].speaker`  | string  | Optional. Speaker label (e.g. `"Speaker 1"`). |
| `language`            | string  | Detected or used language code.               |
| `model`               | string  | Model name used for transcription.            |
| `duration_ms`         | integer | Total audio duration in milliseconds.         |

### `POST /generate` — structured summary generation (agent only; authenticated)

**Request body:**

```json
{
  "transcript": [
    {
      "start_ms": 0,
      "end_ms": 1500,
      "text": "Hello.",
      "source": "whisper",
      "speaker": "A"
    }
  ],
  "notes_markdown": "## Meeting context\n…",
  "template": {
    "sections": [
      {
        "heading": "Action Items",
        "instruction": "List all action items mentioned."
      }
    ]
  },
  "options": {},
  "config": {}
}
```

| Field                             | Type   | Notes                                                                                                                    |
| --------------------------------- | ------ | ------------------------------------------------------------------------------------------------------------------------ |
| `transcript`                      | array  | Array of segment objects — same shape as the `/transcribe` response segments. Will be `[]` if no segments were produced. |
| `notes_markdown`                  | string | Free-text context the user added to the note. Always present; may be an empty string if the user added no notes.         |
| `template.sections`               | array  | Ordered list of sections to produce.                                                                                     |
| `template.sections[].heading`     | string | Section heading to use in the output.                                                                                    |
| `template.sections[].instruction` | string | Instruction the model should follow when generating this section.                                                        |
| `options`                         | object | Optional free-form per-request overrides.                                                                                |
| `config`                          | object | The plugin's stored config, decrypted at call time. May be `{}`.                                                         |

**Response body:**

```json
{
  "summary": {
    "sections": [
      {
        "heading": "Action Items",
        "content_markdown": "- Alice to send the report by Friday.",
        "refs": [0, 3]
      }
    ]
  },
  "model": "llama3.2"
}
```

| Field                                 | Type              | Notes                                                                                                         |
| ------------------------------------- | ----------------- | ------------------------------------------------------------------------------------------------------------- |
| `summary.sections`                    | array             | One entry per requested template section, **in the same order** as `template.sections` in the request.        |
| `summary.sections[].heading`          | string            | Section heading (should match the requested heading).                                                         |
| `summary.sections[].content_markdown` | string            | Generated content for this section, in Markdown.                                                              |
| `summary.sections[].refs`             | array of integers | Optional. 0-based indices into the `transcript` array sent in the request, grounding the section in evidence. |
| `model`                               | string            | Model name used for generation.                                                                               |

---

## 4. Authentication

Every endpoint **except `GET /health`** must enforce two headers:

| Header                | Required value                                                    | On failure        |
| --------------------- | ----------------------------------------------------------------- | ----------------- |
| `Authorization`       | `Bearer <token>` — the per-plugin secret configured on the server | Return HTTP `401` |
| `X-Muesli-Plugin-API` | `1` — the contract version                                        | Return HTTP `401` |

`GET /health` must return `200` **without** checking any auth headers.

**Token security:**

- The token is a shared secret between the Muesli server and your plugin. Store it in an environment variable, a secrets manager, or a vault — never hard-code it.
- The server sends the token on every authenticated call and never logs it.
- The plugin's config object (which may itself contain secrets such as API keys) is stored encrypted at rest on the server and decrypted only at call time.

---

## 5. Minimal worked example

Both examples below use [FastAPI](https://fastapi.tiangolo.com/) and are fully
conformant with `plugin_api: 1`. Each implements all three required endpoints.

### Transcriber plugin example

```python
# transcriber_plugin.py — minimal Muesli transcriber plugin
from fastapi import FastAPI, Request, HTTPException, Depends
from fastapi.responses import JSONResponse
import httpx, os

app = FastAPI()
TOKEN = os.environ["PLUGIN_TOKEN"]


def require_auth(request: Request):
    """Validate Bearer token and contract-version header (skip for /health)."""
    auth = request.headers.get("Authorization", "")
    api_version = request.headers.get("X-Muesli-Plugin-API", "")
    if auth != f"Bearer {TOKEN}" or api_version != "1":
        raise HTTPException(status_code=401, detail="Unauthorized")


@app.get("/info", dependencies=[Depends(require_auth)])
def info():
    return {
        "name": "my-transcriber",
        "version": "0.1.0",
        "plugin_api": 1,
        "kind": "transcriber",
        "config_schema": {},
    }


@app.get("/health")
def health():
    # No auth — must respond 200 without any Authorization header.
    return {"status": "ok"}


@app.post("/transcribe", dependencies=[Depends(require_auth)])
async def transcribe(body: dict):
    audio_url = body["audio_url"]
    language_hint = body.get("language_hint", "en")
    # config = body.get("config", {})   # use stored plugin config if needed

    # Download the audio and run your transcription model here.
    # async with httpx.AsyncClient() as client:
    #     audio = await client.get(audio_url)
    #     ...

    # Return the required response shape.
    return {
        "segments": [
            {
                "start_ms": 0,
                "end_ms": 3000,
                "text": "Hello, world.",
                "source": "my-model",
                "speaker": "Speaker 1",
            }
        ],
        "language": language_hint,
        "model": "my-model-v1",
        "duration_ms": 3000,
    }
```

**Run it:**

```bash
PLUGIN_TOKEN=supersecret uvicorn transcriber_plugin:app --port 8000
```

### Whisper transcriber reference plugin

The reference implementation in [`plugins/whisper-transcriber`](../plugins/whisper-transcriber)
is the canonical example of the current transcriber config surface. Its
`/info` response exposes this `config_schema`, and `/transcribe` reads the same
`config` object at request time. Any fields not listed here are not part of the
current supported surface.

#### `/transcribe` request fields

| Field           | Type   | Location  | Notes                                                                                                                                |
| --------------- | ------ | --------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| `language_hint` | string | top level | Optional ISO-639-1 hint. The request field is forwarded to faster-whisper as `language=...`, which skips autodetection when present. |

#### `/transcribe` config object fields

| Field          | Type    | Default / source                                                                           | Notes                                                                                                                                                                                                                                                                 |
| -------------- | ------- | ------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `model`        | string  | `WHISPER_MODEL` env var, default `base`                                                    | faster-whisper model size or local model path. If omitted, the plugin loads the configured default model.                                                                                                                                                             |
| `beam_size`    | integer | `5`                                                                                        | Decoding beam size forwarded to faster-whisper.                                                                                                                                                                                                                       |
| `compute_type` | string  | `default` sentinel in schema; falls back to `WHISPER_COMPUTE_TYPE` env var, default `int8` | Precision passed to faster-whisper. Schema values are `default`, `int8`, `float16`, and `float32`. `default` means "use the environment setting"; on CPU, `float16` is coerced to `int8`.                                                                             |
| `multitrack`   | boolean | `false`                                                                                    | Opt-in channel-per-speaker mode. When enabled and the input audio has more than one channel, each non-silent channel is split, transcribed independently, merged by `start_ms`, and labeled `Speaker 1`, `Speaker 2`, etc. Requires `ffmpeg` and `ffprobe` on `PATH`. |

#### Runtime env vars

| Env var                        | Type        | Default                            | Notes                                                                                                                                               |
| ------------------------------ | ----------- | ---------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| `MUESLI_PLUGIN_TOKEN`          | string      | empty                              | Required shared secret used for `Authorization: Bearer <token>`.                                                                                    |
| `WHISPER_MODEL`                | string      | `base`                             | Default model used when `config.model` is omitted.                                                                                                  |
| `WHISPER_DEVICE`               | string      | `cpu`                              | Device passed to faster-whisper (`cpu` or `cuda`).                                                                                                  |
| `WHISPER_COMPUTE_TYPE`         | string      | `int8`                             | Default compute type used when `config.compute_type` is omitted or set to `default`.                                                                |
| `WHISPER_DIARIZATION_ENABLED`  | boolean-ish | `false`                            | Enables the optional speaker-diarization stage when set to `1` or `true`.                                                                           |
| `WHISPER_DIARIZATION_HF_TOKEN` | string      | empty                              | Hugging Face token for the pyannote diarization pipeline. The diarization stage only runs when this and `WHISPER_DIARIZATION_ENABLED` are both set. |
| `WHISPER_DIARIZATION_MODEL`    | string      | `pyannote/speaker-diarization-3.1` | pyannote pipeline model name / Hugging Face repo id used for diarization.                                                                           |
| `WHISPER_WORD_TIMESTAMPS`      | boolean-ish | `false`                            | Enables word-level timestamps in `Segment.words` when faster-whisper provides them.                                                                 |

#### OpenAI-compatible transcription endpoint

The same plugin also serves `POST /v1/audio/transcriptions` with the same auth
envelope as `/transcribe`. It accepts multipart form data, not the Muesli JSON
payload, and maps the OpenAI fields onto the same transcription path.

| Field             | Type           | Notes                                                                                                                                                                                                                                     |
| ----------------- | -------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `file`            | multipart file | Required audio upload.                                                                                                                                                                                                                    |
| `model`           | form string    | Optional OpenAI model name. `whisper-1` and `whisper-1-turbo` are treated as aliases and fall back to the configured local default model instead of being forwarded literally.                                                            |
| `language`        | form string    | Optional language hint. This is forwarded to the same `language_hint` path used by `/transcribe`.                                                                                                                                         |
| `response_format` | form string    | Optional. Supported values are `json` (default), `text`, and `verbose_json`. `json` returns `{"text": "..."}`; `text` returns plain text; `verbose_json` returns `{"text": "...", "segments": [...]}` with segment timestamps in seconds. |

### Agent plugin example

```python
# agent_plugin.py — minimal Muesli agent plugin
from fastapi import FastAPI, Request, HTTPException, Depends
import os

app = FastAPI()
TOKEN = os.environ["PLUGIN_TOKEN"]


def require_auth(request: Request):
    """Validate Bearer token and contract-version header (skip for /health)."""
    auth = request.headers.get("Authorization", "")
    api_version = request.headers.get("X-Muesli-Plugin-API", "")
    if auth != f"Bearer {TOKEN}" or api_version != "1":
        raise HTTPException(status_code=401, detail="Unauthorized")


@app.get("/info", dependencies=[Depends(require_auth)])
def info():
    return {
        "name": "my-agent",
        "version": "0.1.0",
        "plugin_api": 1,
        "kind": "agent",
        "config_schema": {},
    }


@app.get("/health")
def health():
    # No auth — must respond 200 without any Authorization header.
    return {"status": "ok"}


@app.post("/generate", dependencies=[Depends(require_auth)])
async def generate(body: dict):
    transcript = body["transcript"]           # list of segment dicts; may be []
    template_sections = body["template"]["sections"]
    notes_markdown = body["notes_markdown"]   # always present; may be empty string
    # config = body.get("config", {})         # use stored plugin config if needed

    # Generate one output section per template section.
    sections = []
    for section in template_sections:
        sections.append({
            "heading": section["heading"],
            "content_markdown": f"Generated content for: {section['heading']}",
            "refs": [0] if transcript else [],  # 0-based indices into transcript
        })

    return {
        "summary": {"sections": sections},
        "model": "my-agent-model-v1",
    }
```

**Run it:**

```bash
PLUGIN_TOKEN=supersecret uvicorn agent_plugin:app --port 8001
```

---

## 6. Validating your plugin — the conformance suite

The [conformance suite](../plugins/conformance) is the **executable contract
spec** for Muesli plugins. It exercises every endpoint and asserts every
required field, status code, and auth behaviour. Run it against your plugin
before registering it with the server.

### Install

```bash
cd plugins/conformance
pip install -e .
```

### Run

Against a **transcriber** plugin at `http://localhost:8000`:

```bash
python -m muesli_plugin_conformance http://localhost:8000 \
    --kind transcriber \
    --token <your-token>
```

Against an **agent** plugin at `http://localhost:8001`:

```bash
python -m muesli_plugin_conformance http://localhost:8001 \
    --kind agent \
    --token <your-token>
```

### Exit codes

| Exit code | Meaning                                                                    |
| --------- | -------------------------------------------------------------------------- |
| `0`       | **CONFORMANT** — plugin passed all checks                                  |
| `1`       | **NON-CONFORMANT** — one or more checks failed (details printed to stdout) |

A plugin must exit with code `0` (CONFORMANT) before you register it with the
server.

---

## 7. Registering your plugin

Once your plugin is running and conformant, register it with the Muesli server
in one of two ways.

### Option A — Admin API / UI

Send a `POST /api/admin/plugins` request (or use the admin UI). Supply the
plugin URL, bearer token, kind, and any config values.

### Option B — Environment variables (default plugins)

Set these environment variables on the server **before it starts**; the server
auto-registers the plugins on boot and the operation is idempotent:

| Variable                           | Description                                     |
| ---------------------------------- | ----------------------------------------------- |
| `MUESLI_DEFAULT_TRANSCRIBER_URL`   | Base URL of the default transcriber plugin      |
| `MUESLI_DEFAULT_TRANSCRIBER_TOKEN` | Bearer token for the default transcriber plugin |
| `MUESLI_DEFAULT_AGENT_URL`         | Base URL of the default agent plugin            |
| `MUESLI_DEFAULT_AGENT_TOKEN`       | Bearer token for the default agent plugin       |

See [`cmd/muesli/main.go`](../cmd/muesli/main.go) for the boot-time
registration logic.

> **Config security reminder:** The plugin config object (which may contain
> API keys or other secrets) is stored encrypted at rest on the server. It is
> decrypted only at call time and sent directly to your plugin in the `config`
> field of each request.
