"""Tests for partial transcript behaviour on chunk failures (TR02)."""
import base64
import pytest

from whisper_app.schema import TranscribeRequest
from whisper_app.transcribe import run_transcribe


# ---------------------------------------------------------------------------
# Minimal valid audio bytes (44-byte WAV header, empty data chunk).
# The fake model never decodes audio; _download just writes bytes to a temp file.
# ---------------------------------------------------------------------------
_MINIMAL_WAV = bytes([
    0x52, 0x49, 0x46, 0x46,  # "RIFF"
    0x24, 0x00, 0x00, 0x00,  # chunk size = 36
    0x57, 0x41, 0x56, 0x45,  # "WAVE"
    0x66, 0x6D, 0x74, 0x20,  # "fmt "
    0x10, 0x00, 0x00, 0x00,  # subchunk1 size = 16
    0x01, 0x00,              # PCM format
    0x01, 0x00,              # 1 channel
    0x44, 0xAC, 0x00, 0x00,  # 44100 Hz
    0x88, 0x58, 0x01, 0x00,  # byte rate
    0x02, 0x00,              # block align
    0x10, 0x00,              # bits per sample = 16
    0x64, 0x61, 0x74, 0x61,  # "data"
    0x00, 0x00, 0x00, 0x00,  # data size = 0
])
_AUDIO_URL = "data:audio/wav;base64," + base64.b64encode(_MINIMAL_WAV).decode()


class _FakeSegment:
    """Mimics a faster_whisper segment."""
    def __init__(self, start, end, text):
        self.start = start
        self.end = end
        self.text = " " + text
        self.words = None


class _FakeInfo:
    language = "en"
    duration = 3.0


def _make_req():
    return TranscribeRequest(audio_url=_AUDIO_URL, language_hint="en", config={"model": "tiny"})


# ---------------------------------------------------------------------------
# 1. Full success: all 3 segments yielded without error
# ---------------------------------------------------------------------------
def test_full_success(monkeypatch, settings):
    segs = [
        _FakeSegment(0.0, 1.0, "hello"),
        _FakeSegment(1.0, 2.0, "world"),
        _FakeSegment(2.0, 3.0, "foo"),
    ]
    info = _FakeInfo()

    class _FakeModel:
        def transcribe(self, path, language=None, beam_size=5):
            return iter(segs), info

    monkeypatch.setattr("whisper_app.transcribe.load_model", lambda m, d, c: _FakeModel())

    resp = run_transcribe(_make_req(), settings)

    assert resp.partial is False
    assert resp.partial_reason is None
    assert len(resp.segments) == 3
    assert resp.segments[0].text == "hello"
    assert resp.segments[2].text == "foo"


# ---------------------------------------------------------------------------
# 2. First-chunk failure: generator raises on first next() → run_transcribe re-raises
# ---------------------------------------------------------------------------
def test_first_chunk_failure_reraises(monkeypatch, settings):
    def _bad_gen():
        raise RuntimeError("gpu oom on first chunk")
        yield  # make it a generator

    info = _FakeInfo()

    class _FakeModel:
        def transcribe(self, path, language=None, beam_size=5):
            return _bad_gen(), info

    monkeypatch.setattr("whisper_app.transcribe.load_model", lambda m, d, c: _FakeModel())

    with pytest.raises(RuntimeError, match="gpu oom on first chunk"):
        run_transcribe(_make_req(), settings)


# ---------------------------------------------------------------------------
# 3. Partial success: 2 segments succeed, then generator raises
# ---------------------------------------------------------------------------
def test_partial_success(monkeypatch, settings):
    def _partial_gen():
        yield _FakeSegment(0.0, 1.0, "chunk one")
        yield _FakeSegment(1.0, 2.0, "chunk two")
        raise RuntimeError("gpu oom")

    info = _FakeInfo()

    class _FakeModel:
        def transcribe(self, path, language=None, beam_size=5):
            return _partial_gen(), info

    monkeypatch.setattr("whisper_app.transcribe.load_model", lambda m, d, c: _FakeModel())

    resp = run_transcribe(_make_req(), settings)

    assert resp.partial is True
    assert resp.partial_reason == "gpu oom"
    assert len(resp.segments) == 2
    assert resp.segments[0].text == "chunk one"
    assert resp.segments[1].text == "chunk two"
