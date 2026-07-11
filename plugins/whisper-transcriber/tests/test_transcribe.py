import pathlib

from whisper_app.schema import Segment, TranscribeRequest, TranscribeResponse


def test_request_parses_minimal():
    req = TranscribeRequest(audio_url="https://store/a.wav", config={})
    assert req.audio_url == "https://store/a.wav"
    assert req.language_hint is None
    assert req.options == {}


def test_segment_defaults_source_mixed():
    seg = Segment(start_ms=0, end_ms=500, text="hi")
    assert seg.source == "mixed"
    assert seg.speaker is None


def test_response_roundtrips():
    resp = TranscribeResponse(
        segments=[Segment(start_ms=0, end_ms=10, text="x")],
        language="en",
        model="base",
        duration_ms=10,
    )
    dumped = resp.model_dump(exclude_none=True)
    assert dumped["segments"][0]["source"] == "mixed"
    assert "speaker" not in dumped["segments"][0]
    assert dumped["language"] == "en"


def test_transcribe_returns_contract_response(client, auth_headers, fake_model, served_audio_url):
    body = {"audio_url": served_audio_url, "language_hint": "en", "config": {"model": "tiny"}}
    r = client.post("/transcribe", json=body, headers=auth_headers)
    assert r.status_code == 200
    out = r.json()
    assert out["model"] == "tiny"
    assert out["language"] == "en"
    assert out["duration_ms"] >= 0
    assert len(out["segments"]) == 2
    first = out["segments"][0]
    assert first == {"start_ms": 0, "end_ms": 500, "text": "hello", "source": "mixed"}
    assert "words" not in first, "words must be absent when word_timestamps_enabled=False"
    # fake model was asked to load the requested config model on the configured device
    assert fake_model.loaded_with == ("tiny", "cpu", "int8")


def test_transcribe_rejects_bad_body(client, auth_headers):
    r = client.post("/transcribe", json={"config": {}}, headers=auth_headers)
    assert r.status_code == 422  # missing audio_url


def test_language_hint_forwarded_to_model(fake_model, settings):
    """Assert that language_hint in the request reaches the model as the language kwarg."""
    import base64
    from whisper_app.schema import TranscribeRequest
    from whisper_app.transcribe import run_transcribe

    # Minimal 44-byte WAV (RIFF header + empty data chunk) encoded as a data URL.
    # The fake model never decodes the audio; _download just writes the bytes to a
    # temp file, so any valid bytes are sufficient here.
    MINIMAL_WAV = bytes([
        0x52, 0x49, 0x46, 0x46,  # "RIFF"
        0x24, 0x00, 0x00, 0x00,  # chunk size = 36
        0x57, 0x41, 0x56, 0x45,  # "WAVE"
        0x66, 0x6D, 0x74, 0x20,  # "fmt "
        0x10, 0x00, 0x00, 0x00,  # subchunk1 size = 16
        0x01, 0x00,              # PCM format
        0x01, 0x00,              # 1 channel
        0x44, 0xAC, 0x00, 0x00, # 44100 Hz
        0x88, 0x58, 0x01, 0x00, # byte rate
        0x02, 0x00,              # block align
        0x10, 0x00,              # bits per sample = 16
        0x64, 0x61, 0x74, 0x61,  # "data"
        0x00, 0x00, 0x00, 0x00,  # data size = 0
    ])
    audio_b64 = base64.b64encode(MINIMAL_WAV).decode()
    audio_url = f"data:audio/wav;base64,{audio_b64}"

    req = TranscribeRequest(audio_url=audio_url, language_hint="fr", config={})
    run_transcribe(req, settings)

    assert fake_model.last_language == "fr"


# ---------------------------------------------------------------------------
# Word-timestamp tests
# ---------------------------------------------------------------------------


def test_word_timestamps_off_by_default(settings):
    """word_timestamps_enabled defaults to False."""
    assert settings.word_timestamps_enabled is False


def test_word_timestamps_from_env(monkeypatch):
    monkeypatch.setenv("WHISPER_WORD_TIMESTAMPS", "1")
    from whisper_app.config import Settings
    s = Settings.from_env()
    assert s.word_timestamps_enabled is True


def test_word_timestamps_populated(settings_with_words, served_audio_url):
    """When word_timestamps_enabled=True, Segment.words is populated."""
    from whisper_app.transcribe import run_transcribe
    from whisper_app.schema import TranscribeRequest

    req = TranscribeRequest(audio_url=served_audio_url, config={"model": "tiny"})
    # settings_with_words has word_timestamps_enabled=True and load_model patched
    resp = run_transcribe(req, settings_with_words)
    assert len(resp.segments) > 0
    seg = resp.segments[0]
    assert len(seg.words) > 0
    assert seg.words[0].text != ""
    assert seg.words[0].end_ms > seg.words[0].start_ms


# ---------------------------------------------------------------------------
# compute_type per-request config tests (TR06)
# ---------------------------------------------------------------------------


def test_compute_type_from_config_reaches_model(client, auth_headers, fake_model, served_audio_url):
    """compute_type sent in the request config reaches load_model as the third argument."""
    body = {"audio_url": served_audio_url, "language_hint": "en", "config": {"compute_type": "float32"}}
    r = client.post("/transcribe", json=body, headers=auth_headers)
    assert r.status_code == 200
    assert fake_model.loaded_with[2] == "float32"


def test_compute_type_falls_back_to_settings(client, auth_headers, fake_model, served_audio_url):
    """When config omits compute_type, settings.compute_type (int8) is used."""
    body = {"audio_url": served_audio_url, "language_hint": "en", "config": {}}
    r = client.post("/transcribe", json=body, headers=auth_headers)
    assert r.status_code == 200
    assert fake_model.loaded_with[2] == "int8"


def test_compute_type_default_sentinel_uses_settings(client, auth_headers, fake_model, served_audio_url):
    """compute_type='default' is the schema sentinel meaning use settings; must not be passed literally to load_model."""
    body = {"audio_url": served_audio_url, "language_hint": "en", "config": {"compute_type": "default"}}
    r = client.post("/transcribe", json=body, headers=auth_headers)
    assert r.status_code == 200
    assert fake_model.loaded_with[2] == "int8"  # settings default in the test fixture


def test_cpu_float16_falls_back_to_int8(fake_model, monkeypatch):
    """float16 on a cpu device is silently downgraded to int8."""
    import base64
    from whisper_app.config import Settings
    from whisper_app.schema import TranscribeRequest
    from whisper_app.transcribe import run_transcribe

    MINIMAL_WAV = bytes([
        0x52, 0x49, 0x46, 0x46,
        0x24, 0x00, 0x00, 0x00,
        0x57, 0x41, 0x56, 0x45,
        0x66, 0x6D, 0x74, 0x20,
        0x10, 0x00, 0x00, 0x00,
        0x01, 0x00,
        0x01, 0x00,
        0x44, 0xAC, 0x00, 0x00,
        0x88, 0x58, 0x01, 0x00,
        0x02, 0x00,
        0x10, 0x00,
        0x64, 0x61, 0x74, 0x61,
        0x00, 0x00, 0x00, 0x00,
    ])
    audio_b64 = base64.b64encode(MINIMAL_WAV).decode()
    audio_url = f"data:audio/wav;base64,{audio_b64}"

    cpu_settings = Settings(auth_token="test", default_model="tiny", device="cpu", compute_type="int8")
    req = TranscribeRequest(audio_url=audio_url, config={"compute_type": "float16"})
    run_transcribe(req, cpu_settings)

    assert fake_model.loaded_with[2] == "int8"
