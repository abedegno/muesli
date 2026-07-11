"""Tests for the OpenAI-compatible POST /v1/audio/transcriptions endpoint.

Key coverage:
- All three response_format values (json, text, verbose_json)
- language passthrough
- Authentication enforcement
- Bad response_format -> 400
- Model alias fix: model=whisper-1 must NOT be forwarded to faster-whisper;
  it must fall back to settings.default_model ("tiny").
"""
import io

import pytest

# Minimal fake audio payload (no real audio needed — the fake model never
# decodes it, and _download just writes it to a temp file).
DUMMY_AUDIO = b"\x00" * 32


def _post(client, auth_headers, data=None, model=None, language=None, response_format=None):
    """Helper: POST multipart to /v1/audio/transcriptions."""
    form_data = {}
    if model is not None:
        form_data["model"] = model
    if language is not None:
        form_data["language"] = language
    if response_format is not None:
        form_data["response_format"] = response_format
    if data:
        form_data.update(data)

    return client.post(
        "/v1/audio/transcriptions",
        files={"file": ("audio.wav", io.BytesIO(DUMMY_AUDIO), "audio/wav")},
        data=form_data,
        headers=auth_headers,
    )


def test_oai_json_format(client, auth_headers, fake_model):
    """Default (no response_format) returns JSON with a 'text' key."""
    resp = _post(client, auth_headers)
    assert resp.status_code == 200
    body = resp.json()
    assert "text" in body
    assert body["text"] == "hello world"


def test_oai_text_format(client, auth_headers, fake_model):
    """response_format=text returns plain text (no JSON wrapper)."""
    resp = _post(client, auth_headers, response_format="text")
    assert resp.status_code == 200
    # Plain text: the exact transcript, not a JSON-encoded string.
    assert resp.text == "hello world"


def test_oai_verbose_json_format(client, auth_headers, fake_model):
    """response_format=verbose_json returns text + segments list."""
    resp = _post(client, auth_headers, response_format="verbose_json")
    assert resp.status_code == 200
    body = resp.json()
    assert "text" in body
    assert "segments" in body
    assert len(body["segments"]) > 0
    seg = body["segments"][0]
    assert "start" in seg
    assert "end" in seg
    assert "text" in seg
    # Timestamps are in seconds (float), not raw ms ints.
    assert isinstance(seg["start"], float)
    assert isinstance(seg["end"], float)


def test_oai_language_passthrough(client, auth_headers, fake_model):
    """The language field is forwarded as language_hint to the transcriber."""
    resp = _post(client, auth_headers, language="fr")
    assert resp.status_code == 200
    assert fake_model.last_language == "fr"


def test_oai_requires_auth(client):
    """Requests without auth headers must be rejected (400 or 401)."""
    resp = client.post(
        "/v1/audio/transcriptions",
        files={"file": ("audio.wav", io.BytesIO(DUMMY_AUDIO), "audio/wav")},
    )
    assert resp.status_code in (400, 401)


def test_oai_bad_response_format(client, auth_headers, fake_model):
    """An unsupported response_format (e.g. 'srt') must return 400 or 422."""
    resp = _post(client, auth_headers, response_format="srt")
    assert resp.status_code in (400, 422)


def test_oai_whisper1_alias_uses_local_model(client, auth_headers, fake_model):
    """CRITICAL: model=whisper-1 must NOT be forwarded to faster-whisper.

    OpenAI clients always send model=whisper-1 (or similar aliases) because
    that is the API model name. faster-whisper has no such model and would
    raise an error. The endpoint must map the alias to settings.default_model
    ("tiny" in tests).
    """
    resp = _post(client, auth_headers, model="whisper-1")
    assert resp.status_code == 200

    # The fake model records what it was loaded with.
    assert fake_model.loaded_with is not None, "load_model was never called"
    loaded_model_name = fake_model.loaded_with[0]

    # Must NOT have forwarded the alias to faster-whisper.
    assert loaded_model_name != "whisper-1", (
        f"Alias 'whisper-1' was forwarded to faster-whisper as-is. "
        f"It should have been replaced by settings.default_model."
    )
    # Must have used the configured local model instead.
    assert loaded_model_name == "tiny", (
        f"Expected local model 'tiny' (settings.default_model) but got {loaded_model_name!r}"
    )
