import httpx
from fastapi import Depends, FastAPI, Header, HTTPException
from starlette.testclient import TestClient

from muesli_plugin_conformance.runner import run_conformance


def _client_for(app: FastAPI) -> httpx.Client:
    # The plan's original `httpx.Client(ASGITransport(...))` pattern is broken for
    # in-process ASGI testing; starlette's TestClient (an httpx.Client subclass)
    # is the supported equivalent and works unchanged with run_conformance.
    return TestClient(app, base_url="http://plugin")


def _good_transcriber_app(token="t"):
    app = FastAPI()

    def auth(authorization: str = Header(default=""), x_muesli_plugin_api: str = Header(default="")):
        if x_muesli_plugin_api != "1":
            raise HTTPException(status_code=400)
        if authorization != f"Bearer {token}":
            raise HTTPException(status_code=401)

    @app.get("/info", dependencies=[Depends(auth)])
    def info():
        return {"name": "x", "version": "1", "plugin_api": 1, "kind": "transcriber",
                "config_schema": {"type": "object", "properties": {}}}

    # health is intentionally UNAUTHENTICATED (matches the contract / readiness probes)
    @app.get("/health")
    def health():
        return {"status": "ok"}

    @app.post("/transcribe", dependencies=[Depends(auth)])
    def transcribe(payload: dict):
        # Reject malformed (missing audio_url) with 422 — conformant plugin must validate.
        if "audio_url" not in payload:
            raise HTTPException(status_code=422, detail="audio_url required")
        return {"segments": [{"start_ms": 0, "end_ms": 10, "text": "hi", "source": "mixed"}],
                "language": "en", "model": "tiny", "duration_ms": 10}

    return app


def test_good_plugin_passes():
    app = _good_transcriber_app()
    with _client_for(app) as c:
        report = run_conformance(c, kind="transcriber", token="t")
    assert report.ok, report.summary()


def test_plugin_without_auth_fails():
    # An app that ignores the token entirely must FAIL the auth-enforcement check.
    app = FastAPI()

    @app.get("/info")
    def info():
        return {"name": "x", "version": "1", "plugin_api": 1, "kind": "transcriber",
                "config_schema": {"type": "object", "properties": {}}}

    @app.get("/health")
    def health():
        return {"status": "ok"}

    @app.post("/transcribe")
    def transcribe(payload: dict):
        return {"segments": [], "language": "en", "model": "tiny", "duration_ms": 0}

    with _client_for(app) as c:
        report = run_conformance(c, kind="transcriber", token="t")
    assert not report.ok
    assert any("auth" in r.name and not r.passed for r in report.results)


def test_plugin_with_bad_response_shape_fails():
    # A plugin whose /transcribe returns an invalid shape (missing 'language')
    # must FAIL the roundtrip schema check.
    bad = _good_transcriber_app()
    for route in list(bad.router.routes):
        if getattr(route, "path", None) == "/transcribe":
            bad.router.routes.remove(route)

    @bad.post("/transcribe", dependencies=[Depends(lambda authorization=Header(default=""), x_muesli_plugin_api=Header(default=""): None)])
    def bad_transcribe(payload: dict):
        return {"segments": [], "model": "tiny", "duration_ms": 0}  # no 'language'

    with _client_for(bad) as c:
        report = run_conformance(c, kind="transcriber", token="t")
    assert not report.ok
    assert any("roundtrip" in r.name and not r.passed for r in report.results)
