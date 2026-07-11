"""Tests for the optional speaker-diarization stage in run_transcribe().

All three tests use a fake diarizer so pyannote.audio is never imported.
The monkeypatch replaces `whisper_app.transcribe.load_diarization_pipeline`
before the real library is touched.
"""

import base64

import pytest

from whisper_app.config import Settings
from whisper_app.transcribe import run_transcribe
from whisper_app.schema import TranscribeRequest


# ---------------------------------------------------------------------------
# Shared helpers
# ---------------------------------------------------------------------------

# Minimal valid WAV (44 bytes) encoded as a data: URL — the fake model never
# decodes the audio, but _download() must succeed.
_MINIMAL_WAV = bytes([
    0x52, 0x49, 0x46, 0x46,  # "RIFF"
    0x24, 0x00, 0x00, 0x00,  # chunk size = 36
    0x57, 0x41, 0x56, 0x45,  # "WAVE"
    0x66, 0x6D, 0x74, 0x20,  # "fmt "
    0x10, 0x00, 0x00, 0x00,  # subchunk1 size = 16
    0x01, 0x00,               # PCM format
    0x01, 0x00,               # 1 channel
    0x44, 0xAC, 0x00, 0x00,  # 44100 Hz
    0x88, 0x58, 0x01, 0x00,  # byte rate
    0x02, 0x00,               # block align
    0x10, 0x00,               # bits per sample = 16
    0x64, 0x61, 0x74, 0x61,  # "data"
    0x00, 0x00, 0x00, 0x00,  # data size = 0
])
_AUDIO_URL = f"data:audio/wav;base64,{base64.b64encode(_MINIMAL_WAV).decode()}"


class _FakeTurn:
    """Mimics a pyannote Segment (turn) object."""
    def __init__(self, start: float, end: float):
        self.start = start
        self.end = end


class _FakeDiarization:
    """Mimics a pyannote Annotation returned by pipeline(audio_path)."""

    def __init__(self, turns: list[tuple[float, float, str]]):
        # turns: [(start_s, end_s, label), ...]
        self._turns = turns

    def itertracks(self, yield_label: bool = False):
        for start, end, label in self._turns:
            yield _FakeTurn(start, end), None, label


class _FakeModelTwoSegments:
    """Stand-in for faster_whisper.WhisperModel — returns two time-aligned segments."""

    def load(self, model, device, compute_type):
        return self

    def transcribe(self, audio_path, language=None, beam_size=5):
        # Segment 0: 0–4 s  |  Segment 1: 5–10 s
        segs = [
            type("S", (), {"start": 0.0, "end": 4.0, "text": "hello"})(),
            type("S", (), {"start": 5.0, "end": 10.0, "text": "world"})(),
        ]
        info = type("Info", (), {"language": language or "en", "duration": 10.0})()
        return segs, info


# ---------------------------------------------------------------------------
# Test a — enabled + fake diarizer → distinct speakers
# ---------------------------------------------------------------------------

def test_diarization_enabled_assigns_distinct_speakers(monkeypatch):
    """When diarization is enabled and the pipeline returns two turns,
    each whisper segment should be labelled with the best-overlap speaker."""

    # Diarizer: SPEAKER_00 owns 0–5 s, SPEAKER_01 owns 5–10 s
    fake_diarization = _FakeDiarization([
        (0.0, 5.0, "SPEAKER_00"),
        (5.0, 10.0, "SPEAKER_01"),
    ])

    fake = _FakeModelTwoSegments()
    monkeypatch.setattr("whisper_app.transcribe.load_model", lambda m, d, c: fake.load(m, d, c))
    monkeypatch.setattr(
        "whisper_app.transcribe.load_diarization_pipeline",
        lambda model_name, hf_token: (lambda _path: fake_diarization),
    )

    settings = Settings(
        auth_token="tok",
        diarization_enabled=True,
        diarization_hf_token="hf-token",
        diarization_model="pyannote/speaker-diarization-3.1",
    )
    req = TranscribeRequest(audio_url=_AUDIO_URL, language_hint="en", config={})
    resp = run_transcribe(req, settings)

    assert len(resp.segments) == 2
    assert resp.segments[0].speaker == "SPEAKER_00"
    assert resp.segments[1].speaker == "SPEAKER_01"


# ---------------------------------------------------------------------------
# Test b — disabled → empty speaker
# ---------------------------------------------------------------------------

def test_diarization_disabled_leaves_speaker_empty(monkeypatch):
    """When diarization_enabled=False the speaker field must remain None even if
    a functional-looking pipeline is patched in."""

    called = {"count": 0}

    def _should_not_be_called(model_name, hf_token):
        called["count"] += 1
        raise AssertionError("load_diarization_pipeline must not be called when disabled")

    fake = _FakeModelTwoSegments()
    monkeypatch.setattr("whisper_app.transcribe.load_model", lambda m, d, c: fake.load(m, d, c))
    monkeypatch.setattr(
        "whisper_app.transcribe.load_diarization_pipeline",
        _should_not_be_called,
    )

    settings = Settings(
        auth_token="tok",
        diarization_enabled=False,
        diarization_hf_token="hf-token",  # token present but disabled
    )
    req = TranscribeRequest(audio_url=_AUDIO_URL, language_hint="en", config={})
    resp = run_transcribe(req, settings)

    assert called["count"] == 0
    for seg in resp.segments:
        assert seg.speaker is None


# ---------------------------------------------------------------------------
# Test c — diarizer raises → empty speaker + transcription success
# ---------------------------------------------------------------------------

def test_diarization_raises_graceful_empty_speaker(monkeypatch):
    """If the diarizer raises any exception, transcription must still succeed
    (HTTP 200 equivalent) and all segments have empty/None speaker."""

    def _boom(model_name, hf_token):
        raise RuntimeError("boom")

    fake = _FakeModelTwoSegments()
    monkeypatch.setattr("whisper_app.transcribe.load_model", lambda m, d, c: fake.load(m, d, c))
    monkeypatch.setattr("whisper_app.transcribe.load_diarization_pipeline", _boom)

    settings = Settings(
        auth_token="tok",
        diarization_enabled=True,
        diarization_hf_token="hf-token",
    )
    req = TranscribeRequest(audio_url=_AUDIO_URL, language_hint="en", config={})
    # Must NOT raise
    resp = run_transcribe(req, settings)

    assert len(resp.segments) == 2
    for seg in resp.segments:
        assert seg.speaker is None or seg.speaker == ""
