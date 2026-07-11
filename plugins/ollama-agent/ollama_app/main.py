import httpx
from fastapi import Depends, FastAPI, HTTPException, status

from .auth import require_auth
from .config import Settings
from .llm import ModelOutputError, fetch_ollama_models, fetch_openai_models, generate
from .schema import GenerateRequest

PLUGIN_API = 1
NAME = "muesli-ollama-agent"
VERSION = "0.1.0"

# Default = local Ollama (privacy). BYO cloud is opt-in via base_url + api_key.
CONFIG_SCHEMA = {
    "type": "object",
    "properties": {
        "ollama_url": {
            "type": "string",
            "title": "Ollama URL",
            "description": "Base URL of the local Ollama server.",
            "default": "http://localhost:11434",
        },
        "model": {
            "type": "string",
            "title": "Model",
            "description": "Model name (Ollama model, or remote model id when base_url is set).",
            "default": "llama3.2",
        },
        "base_url": {
            "type": "string",
            "title": "OpenAI-compatible base URL (opt-in egress)",
            "description": "Set to send to a cloud, OpenAI-compatible API instead of local Ollama. Leaving this empty keeps everything local.",
        },
        "api_key": {
            "type": "string",
            "title": "API key (for base_url)",
            "description": "Secret for the cloud endpoint. Stored encrypted by the server.",
            "writeOnly": True,
            "format": "password",
        },
        "temperature": {
            "type": "number",
            "title": "Temperature",
            "minimum": 0,
            "maximum": 2,
            "default": 0.2,
        },
    },
    "additionalProperties": False,
}


def create_app(settings: Settings) -> FastAPI:
    app = FastAPI(title=NAME, version=VERSION)
    app.state.settings = settings

    @app.get("/info", dependencies=[Depends(require_auth)])
    def info() -> dict:
        s = app.state.settings
        if s.base_url:
            available_models = fetch_openai_models(s.base_url, s.api_key)
        else:
            available_models = fetch_ollama_models(s.default_ollama_url)
        config_schema = CONFIG_SCHEMA
        if available_models:
            # Build a fresh dict per-request rather than mutating the
            # module-level CONFIG_SCHEMA constant, which must stay a plain
            # no-enum shape when discovery is empty (Ollama down/unreachable,
            # or genuinely no models installed) — a free-text fallback.
            config_schema = {
                **CONFIG_SCHEMA,
                "properties": {
                    **CONFIG_SCHEMA["properties"],
                    "model": {
                        **CONFIG_SCHEMA["properties"]["model"],
                        "enum": available_models,
                    },
                },
            }
        return {
            "name": NAME,
            "version": VERSION,
            "plugin_api": PLUGIN_API,
            "kind": "agent",
            "config_schema": config_schema,
            "available_models": available_models,
        }

    # /health is intentionally UNAUTHENTICATED so scale-to-zero / k8s readiness
    # probes (which send no auth headers) can reach it. /info and /generate
    # remain gated.
    @app.get("/health")
    def health() -> dict:
        return {"status": "ok"}

    @app.post("/generate", dependencies=[Depends(require_auth)])
    def generate_route(req: GenerateRequest) -> dict:
        # Resolve effective config: fall back to settings-level env defaults when
        # per-request fields are empty. This lets operators configure LLM_BASE_URL
        # and LLM_API_KEY at container startup without requiring every caller to
        # repeat the cloud provider in each request body.
        s = app.state.settings
        if not req.config.base_url and s.base_url:
            req = req.model_copy(
                update={"config": req.config.model_copy(update={"base_url": s.base_url})}
            )
        if not req.config.api_key and s.api_key:
            req = req.model_copy(
                update={"config": req.config.model_copy(update={"api_key": s.api_key})}
            )
        try:
            resp = generate(req, s)
        except ModelOutputError as e:
            # Upstream model returned something we can't map to the contract.
            # 502 Bad Gateway: it's a model/upstream failure, not a bad request.
            raise HTTPException(
                status_code=status.HTTP_502_BAD_GATEWAY,
                detail=f"malformed model output: {e}",
            ) from e
        except httpx.HTTPError as e:
            # Connection refused / timeout / upstream 5xx — an upstream failure,
            # not the caller's fault. 502 Bad Gateway, like malformed model output.
            raise HTTPException(
                status_code=status.HTTP_502_BAD_GATEWAY,
                detail=f"agent upstream error: {e}",
            ) from e
        return resp.model_dump(exclude_none=True)

    return app


def app() -> FastAPI:  # `uvicorn ollama_app.main:app --factory`
    return create_app(Settings.from_env())
