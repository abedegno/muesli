import base64
from typing import Any, Optional

from fastapi import Depends, FastAPI, File, Form, HTTPException, UploadFile
from fastapi.responses import PlainTextResponse

from . import download_state
from .auth import require_auth
from .config import Settings
from .schema import TranscribeRequest
from .transcribe import run_transcribe

PLUGIN_API = 1
NAME = "muesli-whisper-transcriber"
VERSION = "0.1.0"

# Known OpenAI model aliases that faster-whisper cannot load.
# When a client sends one of these, fall back to the configured local model.
OPENAI_MODEL_ALIASES: frozenset[str] = frozenset({"whisper-1", "whisper-1-turbo"})

# JSON Schema the admin UI renders to collect this plugin's config.
CONFIG_SCHEMA = {
    "type": "object",
    "properties": {
        "model": {
            "type": "string",
            "title": "Whisper model",
            "description": "faster-whisper model size or path.",
            "enum": ["tiny", "base", "small", "medium", "large-v3"],
            "default": "base",
        },
        "language_hint": {
            "type": "string",
            "title": "Language hint",
            "description": "Optional ISO-639-1 code (e.g. 'en'). Empty = autodetect.",
        },
        "beam_size": {
            "type": "integer",
            "title": "Beam size",
            "minimum": 1,
            "maximum": 10,
            "default": 5,
        },
        "compute_type": {
            "type": "string",
            "title": "Compute type",
            "description": "faster-whisper precision. 'default' uses the WHISPER_COMPUTE_TYPE env var (int8 by default). float16 requires a CUDA device; falls back to int8 on CPU.",
            "enum": ["default", "int8", "float16", "float32"],
            "default": "default",
        },
        "multitrack": {
            "type": "boolean",
            "title": "Multitrack (channel-per-speaker)",
            "description": (
                "When the source audio has more than one channel, transcribe each "
                "channel independently and merge the results by start time, "
                "labelling segments with a deterministic per-channel speaker "
                "('Speaker 1', 'Speaker 2', ...). Near-silent channels (~-60 dBFS "
                "or below) are skipped. Requires ffmpeg/ffprobe on PATH. No effect "
                "when disabled or the input audio is mono."
            ),
            "default": False,
        },
    },
    "additionalProperties": False,
}


def create_app(settings: Settings) -> FastAPI:
    app = FastAPI(title=NAME, version=VERSION)
    app.state.settings = settings
    app.state.download_state = download_state.get_state()

    @app.get("/info", dependencies=[Depends(require_auth)])
    def info() -> dict:
        return {
            "name": NAME,
            "version": VERSION,
            "plugin_api": PLUGIN_API,
            "kind": "transcriber",
            "config_schema": CONFIG_SCHEMA,
        }

    # /health is intentionally UNAUTHENTICATED so scale-to-zero / k8s readiness
    # probes (which send no auth headers) can reach it. /info and /transcribe
    # remain gated.
    @app.get("/health")
    def health() -> dict:
        return {"status": "ok"}

    # /status is intentionally UNAUTHENTICATED: the Go server polls it during
    # first-run model downloads before a token exchange has been set up, and the
    # data it exposes (download percent / model name) is non-sensitive.
    @app.get("/status")
    def status() -> dict:
        return app.state.download_state.snapshot()

    @app.post("/transcribe", dependencies=[Depends(require_auth)])
    def transcribe(req: TranscribeRequest) -> dict:
        resp = run_transcribe(req, app.state.settings)
        return resp.model_dump(exclude_none=True)

    @app.post("/v1/audio/transcriptions", dependencies=[Depends(require_auth)])
    async def audio_transcriptions(
        file: UploadFile = File(...),
        model: Optional[str] = Form(default=None),
        language: Optional[str] = Form(default=None),
        response_format: str = Form(default="json"),
    ) -> Any:
        """OpenAI-compatible speech-to-text endpoint.

        Accepts multipart/form-data with an audio file and optional parameters.
        Applies a model-alias fix: OpenAI clients send ``model=whisper-1`` which
        faster-whisper cannot load; known aliases are mapped to the configured
        local model (``settings.default_model``).
        """
        if response_format not in ("json", "text", "verbose_json"):
            raise HTTPException(
                status_code=400,
                detail=(
                    f"unsupported response_format: {response_format!r}. "
                    "Supported values: json, text, verbose_json"
                ),
            )

        # Model alias fix: map known OpenAI aliases to the configured local
        # model so faster-whisper does not try (and fail) to load e.g. "whisper-1".
        resolved_model: Optional[str] = (
            None if (model is None or model in OPENAI_MODEL_ALIASES) else model
        )

        # Read audio bytes and encode as a data: URL for run_transcribe
        audio_bytes = await file.read()
        b64 = base64.b64encode(audio_bytes).decode()
        audio_url = f"data:application/octet-stream;base64,{b64}"

        config: dict[str, Any] = {}
        if resolved_model is not None:
            config["model"] = resolved_model

        req = TranscribeRequest(
            audio_url=audio_url,
            language_hint=language,
            config=config,
        )
        resp = run_transcribe(req, app.state.settings)

        text = " ".join(seg.text for seg in resp.segments)

        if response_format == "text":
            return PlainTextResponse(text)

        if response_format == "verbose_json":
            return {
                "text": text,
                "segments": [
                    {
                        "start": seg.start_ms / 1000.0,
                        "end": seg.end_ms / 1000.0,
                        "text": seg.text,
                    }
                    for seg in resp.segments
                ],
            }

        # default: json
        return {"text": text}

    return app


def app() -> FastAPI:  # uvicorn entrypoint: `uvicorn whisper_app.main:app --factory`
    return create_app(Settings.from_env())
