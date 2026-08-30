"""Streaming conformance checks against the reference and broken plugins."""

import pytest
from fastapi import Depends, FastAPI, Header, HTTPException, WebSocket, WebSocketException
from starlette.testclient import TestClient

from muesli_plugin_conformance.runner import run_conformance


@pytest.fixture
def streaming_client(monkeypatch):
    from streaming_app.config import Settings
    from streaming_app.main import create_app

    monkeypatch.setattr("streaming_app.main.transcribe_module.transcribe_utterance", lambda *args: "")
    app = create_app(Settings(auth_token="t", default_model="tiny"))
    with TestClient(app, base_url="http://plugin") as client:
        yield client


def _base_app(stream_handler, *, enforce_stream_auth=True):
    app = FastAPI()

    @app.get("/info")
    def info(
        authorization: str = Header(default=""),
        x_muesli_plugin_api: str = Header(default=""),
    ):
        if authorization != "Bearer t":
            raise HTTPException(status_code=401)
        return {
            "name": "broken-stream",
            "version": "1",
            "plugin_api": 1,
            "kind": "streaming-transcriber",
            "config_schema": {},
        }

    @app.get("/health")
    def health():
        return {"status": "ok"}

    async def ws_auth(websocket: WebSocket):
        if not enforce_stream_auth:
            return
        if (
            websocket.headers.get("authorization") != "Bearer t"
            or websocket.headers.get("x-muesli-plugin-api") != "1"
        ):
            raise WebSocketException(code=4401)

    app.websocket("/stream", dependencies=[Depends(ws_auth)])(stream_handler)
    return app


def test_reference_streaming_plugin_is_conformant(streaming_client):
    report = run_conformance(streaming_client, kind="streaming-transcriber", token="t")
    assert report.ok, report.summary()


def test_streaming_auth_check_catches_unprotected_work_endpoint():
    async def stream(websocket: WebSocket):
        await websocket.accept()
        await websocket.receive_json()
        await websocket.send_json({"type": "ready"})
        await websocket.close()

    with TestClient(_base_app(stream, enforce_stream_auth=False), base_url="http://plugin") as client:
        report = run_conformance(client, kind="streaming-transcriber", token="t")

    result = next(r for r in report.results if r.name == "auth.streaming_work_endpoint")
    assert result.passed is False


def test_streaming_malformed_check_catches_ready_response():
    async def stream(websocket: WebSocket):
        await websocket.accept()
        first = await websocket.receive_json()
        await websocket.send_json({"type": "ready"})
        if first.get("type") == "start":
            await websocket.receive_json()
        await websocket.close()

    with TestClient(_base_app(stream), base_url="http://plugin") as client:
        report = run_conformance(client, kind="streaming-transcriber", token="t")

    result = next(r for r in report.results if r.name == "payload.streaming_malformed_rejected")
    assert result.passed is False
