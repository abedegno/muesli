"""The reference ollama agent plugin self-certifies: run the conformance suite
against the *actual* ollama-agent app and assert it is CONFORMANT. Ollama itself
is mocked with respx so no real LLM is needed."""
import json

from fastapi import Depends, FastAPI, Header, HTTPException
import pytest
import respx
from httpx import Response
from starlette.testclient import TestClient

from muesli_plugin_conformance.runner import run_conformance

OLLAMA_URL = "http://localhost:11434"


def _model_json():
    return json.dumps({"content_markdown": "Ships Friday.", "refs": [0]})


def _agent_app_with_refs(refs):
    app = FastAPI()

    def auth(authorization: str = Header(default=""), x_muesli_plugin_api: str = Header(default="")):
        if x_muesli_plugin_api != "1":
            raise HTTPException(status_code=400)
        if authorization != "Bearer t":
            raise HTTPException(status_code=401)

    @app.get("/info", dependencies=[Depends(auth)])
    def info():
        return {"name": "x", "version": "1", "plugin_api": 1, "kind": "agent", "config_schema": {}}

    # health is intentionally UNAUTHENTICATED (matches the contract / readiness probes)
    @app.get("/health")
    def health():
        return {"status": "ok"}

    @app.post("/generate", dependencies=[Depends(auth)])
    def generate(payload: dict):
        # Reject malformed (missing transcript) with 422 — conformant plugin must validate.
        if "transcript" not in payload:
            raise HTTPException(status_code=422, detail="transcript required")
        return {
            "summary": {
                "sections": [
                    {"heading": "Overview", "content_markdown": "Ships Friday.", "refs": refs}
                ]
            },
            "model": "llama3.2",
        }

    return app


@pytest.fixture
def agent_client():
    from ollama_app.config import Settings  # ollama-agent package
    from ollama_app.main import create_app

    app = create_app(
        Settings(auth_token="t", default_ollama_url=OLLAMA_URL, default_model="llama3.2")
    )
    # The plan's `httpx.Client(ASGITransport(...))` pattern is broken for in-process
    # ASGI testing; starlette's TestClient (an httpx.Client subclass) is the
    # supported equivalent and works unchanged with run_conformance. It uses its own
    # ASGI transport, so respx (which patches httpx's network transports) only
    # intercepts the plugin's *outbound* call to Ollama, not the test client itself.
    with TestClient(app, base_url="http://plugin") as c:
        yield c


@respx.mock
def test_ollama_plugin_is_conformant(agent_client):
    respx.post(f"{OLLAMA_URL}/api/generate").mock(
        return_value=Response(200, json={"response": _model_json(), "model": "llama3.2"})
    )
    report = run_conformance(agent_client, kind="agent", token="t")
    assert report.ok, report.summary()


@pytest.mark.parametrize(
    ("refs", "expected_ok"),
    [
        (["123", "456"], False),
        ([[1, 2]], False),
        ([1, 2, 3], True),
    ],
)
def test_agent_refs_invariant(refs, expected_ok):
    app = _agent_app_with_refs(refs)
    with TestClient(app, base_url="http://plugin") as client:
        report = run_conformance(client, kind="agent", token="t")
    assert report.ok is expected_ok, report.summary()
    if not expected_ok:
        assert any(not result.passed for result in report.results if result.name.startswith("roundtrip"))
