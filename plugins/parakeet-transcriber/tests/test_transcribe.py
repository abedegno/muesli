import base64

from parakeet_app.schema import TranscribeRequest
from parakeet_app.transcribe import _decode_data_url, run_transcribe


def test_transcribe_returns_contract_response(client, auth_headers, fake_model, served_audio_url):
    body = {
        "audio_url": served_audio_url,
        "language_hint": "en",
        "config": {"model": "nvidia/parakeet-tdt-1.1b"},
    }
    r = client.post("/transcribe", json=body, headers=auth_headers)
    assert r.status_code == 200
    out = r.json()
    assert out["model"] == "nvidia/parakeet-tdt-1.1b"
    assert out["language"] == "en"
    assert out["duration_ms"] >= 0
    assert len(out["segments"]) == 2
    first = out["segments"][0]
    assert first == {"start_ms": 0, "end_ms": 500, "text": "hello", "source": "mixed"}
    assert fake_model.loaded_with == ("nvidia/parakeet-tdt-1.1b", "cpu")


def test_transcribe_rejects_bad_body(client, auth_headers):
    r = client.post("/transcribe", json={"config": {}}, headers=auth_headers)
    assert r.status_code == 422


def test_language_hint_forwarded_to_model(fake_model, settings):
    payload = base64.b64encode(b"RIFF\x24\x00\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00"
                              b"D\xac\x00\x00\x88X\x01\x00\x02\x00\x10\x00data\x00\x00\x00\x00").decode()
    audio_url = f"data:audio/wav;base64,{payload}"
    req = TranscribeRequest(audio_url=audio_url, language_hint="fr", config={})
    run_transcribe(req, settings)
    assert fake_model.last_language == "fr"


def test_transcribe_accepts_data_url(fake_model, settings):
    audio_url = "data:audio/wav;base64,UklGRiQAAABXQVZFZm10IBAAAAABAAEAQB8AAIA+AAACABAAZGF0YQAAAAA="
    req = TranscribeRequest(audio_url=audio_url, config={"model": "nvidia/parakeet-tdt-1.1b"})
    resp = run_transcribe(req, settings)
    assert resp.language == "en"
    assert len(resp.segments) == 2


def test_decode_data_url_round_trip():
    assert _decode_data_url("data:audio/wav;base64,YXVkaW8tYnl0ZXM=") == b"audio-bytes"
