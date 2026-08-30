import pytest
from fastapi import Depends, FastAPI, Header, HTTPException
from starlette.testclient import TestClient

from muesli_plugin_conformance import checks
from muesli_plugin_conformance.checks import Report
from muesli_plugin_conformance.runner import run_conformance


def _streaming_app(token: str = "t") -> FastAPI:
    app = FastAPI()

    def auth(
        authorization: str = Header(default=""),
        x_muesli_plugin_api: str = Header(default=""),
    ) -> None:
        if x_muesli_plugin_api != "1":
            raise HTTPException(status_code=400)
        if authorization != f"Bearer {token}":
            raise HTTPException(status_code=401)

    @app.get("/info", dependencies=[Depends(auth)])
    def info():
        return {
            "name": "streaming-test",
            "version": "1",
            "plugin_api": 1,
            "kind": "streaming-transcriber",
            "config_schema": {},
        }

    @app.get("/health")
    def health():
        return {"status": "ok"}

    return app


def test_streaming_transcriber_runs_only_transport_agnostic_checks():
    with TestClient(_streaming_app(), base_url="http://plugin") as client:
        report = run_conformance(client, kind="streaming-transcriber", token="t")

    assert report.ok is True
    assert [result.name for result in report.results] == [
        "info.shape",
        "health.unauthenticated",
        "auth.enforced",
        "work_endpoint.streaming_not_covered",
    ]
    assert "deferred" in report.results[-1].detail


def test_run_conformance_rejects_unknown_kind():
    with pytest.raises(ValueError, match="bogus"):
        run_conformance(None, kind="bogus", token="t")  # type: ignore[arg-type]


def test_roundtrip_rejects_kind_without_http_work_endpoint():
    with pytest.raises(ValueError, match="no HTTP work endpoint.*streaming-transcriber"):
        checks.check_roundtrip(
            None,  # type: ignore[arg-type]
            token="t",
            kind="streaming-transcriber",
            report=Report(),
        )
