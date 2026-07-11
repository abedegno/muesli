import os

import pytest

pytestmark = pytest.mark.skipif(
    os.environ.get("RUN_WHISPER_INTEGRATION") != "1",
    reason="set RUN_WHISPER_INTEGRATION=1 to run the real faster-whisper model",
)


def test_real_model_transcribes_fixture(client, auth_headers, served_audio_url):
    # No fake_model fixture: this exercises the real WhisperModel download + decode.
    body = {"audio_url": served_audio_url, "config": {"model": "tiny"}}
    r = client.post("/transcribe", json=body, headers=auth_headers)
    assert r.status_code == 200
    out = r.json()
    assert out["model"] == "tiny"
    assert isinstance(out["segments"], list)
