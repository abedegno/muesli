from fastapi import Depends, FastAPI

from .auth import require_auth
from .config import Settings
from .schema import TranscribeRequest
from .transcribe import run_transcribe

PLUGIN_API = 1
NAME = "muesli-parakeet-transcriber"
VERSION = "0.1.0"

CONFIG_SCHEMA = {
    "type": "object",
    "properties": {
        "model": {
            "type": "string",
            "title": "Parakeet model",
            "description": "NeMo/Parakeet model name or path.",
            "default": "nvidia/parakeet-tdt-1.1b",
        },
        "device": {
            "type": "string",
            "title": "Device",
            "description": "Inference device. CUDA is the intended production setting.",
            "enum": ["cpu", "cuda"],
            "default": "cpu",
        },
    },
    "additionalProperties": False,
}


def create_app(settings: Settings) -> FastAPI:
    app = FastAPI(title=NAME, version=VERSION)
    app.state.settings = settings

    @app.get("/info", dependencies=[Depends(require_auth)])
    def info() -> dict:
        return {
            "name": NAME,
            "version": VERSION,
            "plugin_api": PLUGIN_API,
            "kind": "transcriber",
            "config_schema": CONFIG_SCHEMA,
        }

    @app.get("/health")
    def health() -> dict:
        return {"status": "ok"}

    @app.post("/transcribe", dependencies=[Depends(require_auth)])
    def transcribe(req: TranscribeRequest) -> dict:
        resp = run_transcribe(req, app.state.settings)
        return resp.model_dump(exclude_none=True)

    return app


def app() -> FastAPI:  # uvicorn entrypoint: `uvicorn parakeet_app.main:app --factory`
    return create_app(Settings.from_env())
