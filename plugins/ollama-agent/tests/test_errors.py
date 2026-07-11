"""Tests that transport/HTTP errors from the upstream model server map to 502."""

import httpx
import pytest

import ollama_app.main as main_mod

OLLAMA_URL = "http://ollama:11434"

_VALID_BODY = {
    "transcript": [
        {"start_ms": 0, "end_ms": 1000, "text": "We ship Friday.", "source": "mixed"}
    ],
    "notes_markdown": "- ship date?",
    "template": {
        "sections": [
            {"heading": "Overview", "instruction": "Summarise."},
        ]
    },
    "config": {"ollama_url": OLLAMA_URL, "model": "llama3.2"},
}


def test_upstream_connect_error_maps_to_502(client, auth_headers, monkeypatch):
    def boom(req, settings):
        raise httpx.ConnectError("connection refused")

    monkeypatch.setattr(main_mod, "generate", boom)
    resp = client.post("/generate", json=_VALID_BODY, headers=auth_headers)
    assert resp.status_code == 502
    assert "upstream" in resp.json()["detail"].lower()


def test_upstream_timeout_maps_to_502(client, auth_headers, monkeypatch):
    def boom(req, settings):
        raise httpx.TimeoutException("timed out")

    monkeypatch.setattr(main_mod, "generate", boom)
    resp = client.post("/generate", json=_VALID_BODY, headers=auth_headers)
    assert resp.status_code == 502


def test_upstream_http_status_error_maps_to_502(client, auth_headers, monkeypatch):
    """An upstream 5xx surfaced via raise_for_status() must also become 502."""

    def boom(req, settings):
        request = httpx.Request("POST", f"{OLLAMA_URL}/api/generate")
        response = httpx.Response(503, request=request)
        raise httpx.HTTPStatusError("503 Service Unavailable", request=request, response=response)

    monkeypatch.setattr(main_mod, "generate", boom)
    resp = client.post("/generate", json=_VALID_BODY, headers=auth_headers)
    assert resp.status_code == 502
