"""Tests for the model-download status tracker (TR03).

Covers:
- GET /status shape and default state
- State mutations reflected in /status
- Endpoint is unauthenticated
- load_model() sets state to 'ready' after WhisperModel returns
"""
import pytest

from whisper_app import download_state as _ds


# ---------------------------------------------------------------------------
# Per-test state reset so tests are order-independent.
# ---------------------------------------------------------------------------

@pytest.fixture(autouse=True)
def reset_state():
    """Reset the module-level singleton before every test in this module."""
    _ds.get_state().update(status="idle", model="", percent=0)
    yield
    _ds.get_state().update(status="idle", model="", percent=0)


# ---------------------------------------------------------------------------
# /status endpoint tests
# ---------------------------------------------------------------------------

def test_status_idle(client):
    r = client.get("/status")
    assert r.status_code == 200
    body = r.json()
    assert body["status"] == "idle"
    assert body["model"] == ""
    assert body["percent"] == 0


def test_status_downloading(client):
    _ds.get_state().update(status="downloading", model="base", percent=42)
    r = client.get("/status")
    assert r.status_code == 200
    body = r.json()
    assert body["status"] == "downloading"
    assert body["model"] == "base"
    assert body["percent"] == 42


def test_status_ready(client):
    _ds.get_state().update(status="ready", model="base", percent=100)
    r = client.get("/status")
    assert r.status_code == 200
    body = r.json()
    assert body["status"] == "ready"
    assert body["model"] == "base"
    assert body["percent"] == 100


def test_status_no_auth_required(client):
    """GET /status must return 200 with NO Authorization header."""
    r = client.get("/status")
    assert r.status_code == 200


def test_load_model_sets_state(monkeypatch):
    """load_model() must mark state as 'ready' after WhisperModel returns."""
    from unittest.mock import MagicMock

    mock_model = MagicMock()
    monkeypatch.setattr(
        "faster_whisper.WhisperModel",
        lambda model, device, compute_type: mock_model,
    )

    from whisper_app import transcribe

    # Ensure state starts clean.
    _ds.get_state().update(status="idle", model="", percent=0)

    result = transcribe.load_model("base", "cpu", "int8")

    state = _ds.get_state()
    assert state.status == "ready"
    assert state.model == "base"
    assert result is mock_model
