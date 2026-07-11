"""Completeness tests for the conformance suite.

Each test exercises a specific gap addressed by the new check functions:
  - check_auth_on_work_endpoint  ("auth.work_endpoint")
  - check_empty_transcript       ("generate.empty_transcript")
  - check_malformed_payload      ("payload.malformed_rejected")

Both PASS (conformant) and FAIL (non-conformant) cases are covered.
Apps are in-process FastAPI instances driven via starlette.testclient.TestClient,
so no real network is required.
"""

import httpx
from fastapi import Depends, FastAPI, Header, HTTPException
from starlette.testclient import TestClient

from muesli_plugin_conformance.runner import run_conformance


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

TOKEN = "secret"


def _client_for(app: FastAPI) -> httpx.Client:
    return TestClient(app, base_url="http://plugin")


def _auth_dep(token: str = TOKEN):
    """Return a FastAPI dependency that enforces Bearer + API-version auth."""

    def _dep(
        authorization: str = Header(default=""),
        x_muesli_plugin_api: str = Header(default=""),
    ):
        if x_muesli_plugin_api != "1":
            raise HTTPException(status_code=400, detail="missing API version header")
        if authorization != f"Bearer {token}":
            raise HTTPException(status_code=401, detail="unauthorized")

    return _dep


def _good_transcriber(token: str = TOKEN) -> FastAPI:
    """Fully-conformant transcriber: auth on /info + /transcribe, rejects {} with 422."""
    from fastapi import Body

    app = FastAPI()
    dep = _auth_dep(token)

    @app.get("/info", dependencies=[Depends(dep)])
    def info():
        return {
            "name": "good-transcriber",
            "version": "1.0",
            "plugin_api": 1,
            "kind": "transcriber",
            "config_schema": {"type": "object", "properties": {}},
        }

    @app.get("/health")
    def health():
        return {"status": "ok"}

    @app.post("/transcribe", dependencies=[Depends(dep)])
    def transcribe(payload: dict = Body(...)):
        # Validate required fields — FastAPI will 422 on missing body keys when
        # using a Pydantic model; we do it manually here for simplicity.
        if "audio_url" not in payload:
            raise HTTPException(status_code=422, detail="audio_url required")
        return {
            "segments": [{"start_ms": 0, "end_ms": 10, "text": "hi", "source": "mixed"}],
            "language": "en",
            "model": "tiny",
            "duration_ms": 10,
        }

    return app


def _good_agent(token: str = TOKEN) -> FastAPI:
    """Fully-conformant agent: auth on /info + /generate, rejects {} with 422, accepts transcript:[]."""
    from fastapi import Body

    app = FastAPI()
    dep = _auth_dep(token)

    @app.get("/info", dependencies=[Depends(dep)])
    def info():
        return {
            "name": "good-agent",
            "version": "1.0",
            "plugin_api": 1,
            "kind": "agent",
            "config_schema": {"type": "object", "properties": {}},
        }

    @app.get("/health")
    def health():
        return {"status": "ok"}

    @app.post("/generate", dependencies=[Depends(dep)])
    def generate(payload: dict = Body(...)):
        if "transcript" not in payload:
            raise HTTPException(status_code=422, detail="transcript required")
        return {
            "summary": {
                "sections": [{"heading": "Overview", "content_markdown": "Ships Friday.", "refs": []}]
            },
            "model": "llama3",
        }

    return app


def _passed(report, name: str) -> bool:
    return any(r.name == name and r.passed for r in report.results)


def _failed(report, name: str) -> bool:
    return any(r.name == name and not r.passed for r in report.results)


# ---------------------------------------------------------------------------
# Gap 1: auth.work_endpoint
# ---------------------------------------------------------------------------


def test_transcriber_work_endpoint_auth_enforced():
    """Transcriber that auth-gates /info but leaves /transcribe open must FAIL auth.work_endpoint."""
    app = FastAPI()

    def info_dep(
        authorization: str = Header(default=""),
        x_muesli_plugin_api: str = Header(default=""),
    ):
        if x_muesli_plugin_api != "1":
            raise HTTPException(status_code=400)
        if authorization != f"Bearer {TOKEN}":
            raise HTTPException(status_code=401)

    @app.get("/info", dependencies=[Depends(info_dep)])
    def info():
        return {
            "name": "t", "version": "1", "plugin_api": 1,
            "kind": "transcriber",
            "config_schema": {"type": "object", "properties": {}},
        }

    @app.get("/health")
    def health():
        return {"status": "ok"}

    # /transcribe has NO auth check — this is the gap we're testing.
    @app.post("/transcribe")
    def transcribe(payload: dict):
        return {
            "segments": [{"start_ms": 0, "end_ms": 10, "text": "hi", "source": "mixed"}],
            "language": "en",
            "model": "tiny",
            "duration_ms": 10,
        }

    with _client_for(app) as c:
        report = run_conformance(c, kind="transcriber", token=TOKEN)

    assert not report.ok
    assert _failed(report, "auth.work_endpoint"), report.summary()


def test_agent_work_endpoint_auth_enforced():
    """Agent that auth-gates /info but leaves /generate open must FAIL auth.work_endpoint."""
    app = FastAPI()

    def info_dep(
        authorization: str = Header(default=""),
        x_muesli_plugin_api: str = Header(default=""),
    ):
        if x_muesli_plugin_api != "1":
            raise HTTPException(status_code=400)
        if authorization != f"Bearer {TOKEN}":
            raise HTTPException(status_code=401)

    @app.get("/info", dependencies=[Depends(info_dep)])
    def info():
        return {
            "name": "a", "version": "1", "plugin_api": 1,
            "kind": "agent",
            "config_schema": {},
        }

    @app.get("/health")
    def health():
        return {"status": "ok"}

    # /generate has NO auth check — this is the gap.
    @app.post("/generate")
    def generate(payload: dict):
        return {
            "summary": {"sections": [{"heading": "O", "content_markdown": "X.", "refs": []}]},
            "model": "llama3",
        }

    with _client_for(app) as c:
        report = run_conformance(c, kind="agent", token=TOKEN)

    assert not report.ok
    assert _failed(report, "auth.work_endpoint"), report.summary()


# ---------------------------------------------------------------------------
# Full suite pass for both kinds
# ---------------------------------------------------------------------------


def test_transcriber_passes_all_new_checks():
    """A fully-conformant transcriber passes all checks including new ones."""
    app = _good_transcriber()
    with _client_for(app) as c:
        report = run_conformance(c, kind="transcriber", token=TOKEN)
    assert report.ok, report.summary()
    assert _passed(report, "auth.work_endpoint")
    assert _passed(report, "payload.malformed_rejected")


def test_agent_passes_all_new_checks():
    """A fully-conformant agent passes all checks including new ones."""
    app = _good_agent()
    with _client_for(app) as c:
        report = run_conformance(c, kind="agent", token=TOKEN)
    assert report.ok, report.summary()
    assert _passed(report, "auth.work_endpoint")
    assert _passed(report, "payload.malformed_rejected")
    assert _passed(report, "generate.empty_transcript")


# ---------------------------------------------------------------------------
# Gap 2: generate.empty_transcript
# ---------------------------------------------------------------------------


def test_agent_rejects_empty_transcript_fails():
    """Agent that returns 400 for transcript:[] must FAIL generate.empty_transcript."""
    app = FastAPI()
    dep = _auth_dep()

    @app.get("/info", dependencies=[Depends(dep)])
    def info():
        return {
            "name": "a", "version": "1", "plugin_api": 1,
            "kind": "agent",
            "config_schema": {},
        }

    @app.get("/health")
    def health():
        return {"status": "ok"}

    @app.post("/generate", dependencies=[Depends(dep)])
    def generate(payload: dict):
        if not payload.get("transcript"):
            # Plugin rejects empty transcript — non-conformant.
            raise HTTPException(status_code=400, detail="transcript cannot be empty")
        return {
            "summary": {"sections": [{"heading": "O", "content_markdown": "X.", "refs": []}]},
            "model": "llama3",
        }

    with _client_for(app) as c:
        report = run_conformance(c, kind="agent", token=TOKEN)

    assert not report.ok
    assert _failed(report, "generate.empty_transcript"), report.summary()


# ---------------------------------------------------------------------------
# Gap 3: payload.malformed_rejected
# ---------------------------------------------------------------------------


def test_transcriber_accepts_malformed_payload_fails():
    """Transcriber that returns 200 for {} body must FAIL payload.malformed_rejected."""
    app = FastAPI()
    dep = _auth_dep()

    @app.get("/info", dependencies=[Depends(dep)])
    def info():
        return {
            "name": "t", "version": "1", "plugin_api": 1,
            "kind": "transcriber",
            "config_schema": {"type": "object", "properties": {}},
        }

    @app.get("/health")
    def health():
        return {"status": "ok"}

    @app.post("/transcribe", dependencies=[Depends(dep)])
    def transcribe(payload: dict):
        # Silently accepts any body — non-conformant.
        return {
            "segments": [],
            "language": "en",
            "model": "tiny",
            "duration_ms": 0,
        }

    with _client_for(app) as c:
        report = run_conformance(c, kind="transcriber", token=TOKEN)

    assert not report.ok
    assert _failed(report, "payload.malformed_rejected"), report.summary()


def test_agent_accepts_malformed_payload_fails():
    """Agent that returns 200 for {} body must FAIL payload.malformed_rejected."""
    app = FastAPI()
    dep = _auth_dep()

    @app.get("/info", dependencies=[Depends(dep)])
    def info():
        return {
            "name": "a", "version": "1", "plugin_api": 1,
            "kind": "agent",
            "config_schema": {},
        }

    @app.get("/health")
    def health():
        return {"status": "ok"}

    @app.post("/generate", dependencies=[Depends(dep)])
    def generate(payload: dict):
        # Silently accepts any body — non-conformant.
        return {
            "summary": {"sections": [{"heading": "O", "content_markdown": "X.", "refs": []}]},
            "model": "llama3",
        }

    with _client_for(app) as c:
        report = run_conformance(c, kind="agent", token=TOKEN)

    assert not report.ok
    assert _failed(report, "payload.malformed_rejected"), report.summary()


# ---------------------------------------------------------------------------
# Existing-check coverage (roundtrip + info schema failures)
# ---------------------------------------------------------------------------


def test_transcriber_500_on_canonical_fails():
    """Transcriber that crashes (500) on a valid canonical payload must FAIL roundtrip.status."""
    app = FastAPI()
    dep = _auth_dep()

    @app.get("/info", dependencies=[Depends(dep)])
    def info():
        return {
            "name": "t", "version": "1", "plugin_api": 1,
            "kind": "transcriber",
            "config_schema": {"type": "object", "properties": {}},
        }

    @app.get("/health")
    def health():
        return {"status": "ok"}

    @app.post("/transcribe", dependencies=[Depends(dep)])
    def transcribe(payload: dict):
        raise HTTPException(status_code=500, detail="internal server error")

    with _client_for(app) as c:
        report = run_conformance(c, kind="transcriber", token=TOKEN)

    assert not report.ok
    assert _failed(report, "roundtrip.status"), report.summary()


def test_generate_missing_model_fails():
    """Agent /generate response missing 'model' must FAIL roundtrip.schema."""
    app = FastAPI()
    dep = _auth_dep()

    @app.get("/info", dependencies=[Depends(dep)])
    def info():
        return {
            "name": "a", "version": "1", "plugin_api": 1,
            "kind": "agent",
            "config_schema": {},
        }

    @app.get("/health")
    def health():
        return {"status": "ok"}

    @app.post("/generate", dependencies=[Depends(dep)])
    def generate(payload: dict):
        if "transcript" not in payload:
            raise HTTPException(status_code=422, detail="transcript required")
        # Missing 'model' field.
        return {
            "summary": {"sections": [{"heading": "O", "content_markdown": "X.", "refs": []}]},
        }

    with _client_for(app) as c:
        report = run_conformance(c, kind="agent", token=TOKEN)

    assert not report.ok
    assert _failed(report, "roundtrip.schema"), report.summary()


def test_transcribe_missing_model_fails():
    """Transcriber /transcribe response missing 'model' must FAIL roundtrip.schema."""
    app = FastAPI()
    dep = _auth_dep()

    @app.get("/info", dependencies=[Depends(dep)])
    def info():
        return {
            "name": "t", "version": "1", "plugin_api": 1,
            "kind": "transcriber",
            "config_schema": {"type": "object", "properties": {}},
        }

    @app.get("/health")
    def health():
        return {"status": "ok"}

    @app.post("/transcribe", dependencies=[Depends(dep)])
    def transcribe(payload: dict):
        if "audio_url" not in payload:
            raise HTTPException(status_code=422, detail="audio_url required")
        # Missing 'model' field.
        return {
            "segments": [{"start_ms": 0, "end_ms": 10, "text": "hi", "source": "mixed"}],
            "language": "en",
            "duration_ms": 10,
        }

    with _client_for(app) as c:
        report = run_conformance(c, kind="transcriber", token=TOKEN)

    assert not report.ok
    assert _failed(report, "roundtrip.schema"), report.summary()


def test_transcribe_segment_missing_source_fails():
    """Transcriber /transcribe response with a segment missing 'source' must FAIL roundtrip.schema."""
    app = FastAPI()
    dep = _auth_dep()

    @app.get("/info", dependencies=[Depends(dep)])
    def info():
        return {
            "name": "t", "version": "1", "plugin_api": 1,
            "kind": "transcriber",
            "config_schema": {"type": "object", "properties": {}},
        }

    @app.get("/health")
    def health():
        return {"status": "ok"}

    @app.post("/transcribe", dependencies=[Depends(dep)])
    def transcribe(payload: dict):
        if "audio_url" not in payload:
            raise HTTPException(status_code=422, detail="audio_url required")
        # Segment is missing 'source'.
        return {
            "segments": [{"start_ms": 0, "end_ms": 10, "text": "hi"}],
            "language": "en",
            "model": "tiny",
            "duration_ms": 10,
        }

    with _client_for(app) as c:
        report = run_conformance(c, kind="transcriber", token=TOKEN)

    assert not report.ok
    assert _failed(report, "roundtrip.schema"), report.summary()


def test_info_missing_config_schema_fails():
    """Plugin /info response missing 'config_schema' must FAIL the info schema check."""
    app = FastAPI()
    dep = _auth_dep()

    @app.get("/info", dependencies=[Depends(dep)])
    def info():
        # Missing config_schema.
        return {"name": "t", "version": "1", "plugin_api": 1, "kind": "transcriber"}

    @app.get("/health")
    def health():
        return {"status": "ok"}

    @app.post("/transcribe", dependencies=[Depends(dep)])
    def transcribe(payload: dict):
        if "audio_url" not in payload:
            raise HTTPException(status_code=422, detail="audio_url required")
        return {
            "segments": [],
            "language": "en",
            "model": "tiny",
            "duration_ms": 0,
        }

    with _client_for(app) as c:
        report = run_conformance(c, kind="transcriber", token=TOKEN)

    assert not report.ok
    assert _failed(report, "info.schema"), report.summary()


def test_info_wrong_plugin_api_version_fails():
    """Plugin /info returning plugin_api:2 must FAIL the info schema check."""
    app = FastAPI()
    dep = _auth_dep()

    @app.get("/info", dependencies=[Depends(dep)])
    def info():
        return {
            "name": "t", "version": "1",
            "plugin_api": 2,  # wrong version
            "kind": "transcriber",
            "config_schema": {"type": "object"},
        }

    @app.get("/health")
    def health():
        return {"status": "ok"}

    @app.post("/transcribe", dependencies=[Depends(dep)])
    def transcribe(payload: dict):
        if "audio_url" not in payload:
            raise HTTPException(status_code=422, detail="audio_url required")
        return {
            "segments": [],
            "language": "en",
            "model": "tiny",
            "duration_ms": 0,
        }

    with _client_for(app) as c:
        report = run_conformance(c, kind="transcriber", token=TOKEN)

    assert not report.ok
    assert _failed(report, "info.schema"), report.summary()
