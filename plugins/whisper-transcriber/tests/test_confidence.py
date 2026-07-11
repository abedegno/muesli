"""Tests for per-segment diarization confidence computation in _assign_speakers
and that run_transcribe WITHOUT diarization leaves confidence=None.
"""

import base64

import pytest

from whisper_app.schema import Segment, TranscribeRequest
from whisper_app.config import Settings
from whisper_app.transcribe import _assign_speakers, run_transcribe


# ---------------------------------------------------------------------------
# Minimal valid WAV data URL (identical to test_diarization.py helper)
# ---------------------------------------------------------------------------
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


# ---------------------------------------------------------------------------
# Fake diarization helpers
# ---------------------------------------------------------------------------

class _FakeTurn:
    """Mimics a pyannote Segment (turn) object."""
    def __init__(self, start: float, end: float):
        self.start = start
        self.end = end


class _FakeDiarization:
    """Mimics a pyannote Annotation returned by pipeline(audio_path)."""

    def __init__(self, turns):
        self._turns = turns

    def itertracks(self, yield_label=False):
        for start, end, label in self._turns:
            yield _FakeTurn(start, end), None, label


# ---------------------------------------------------------------------------
# Test 1: confidence = best_overlap / duration
# Segment 0-1000 ms, turn covering 0-800 ms -> confidence = 0.8
# ---------------------------------------------------------------------------

def test_confidence_partial_overlap():
    """A diarization turn covering 800 ms of a 1000 ms segment gives confidence ~0.8."""
    seg = Segment(start_ms=0, end_ms=1000, text="hello", source="mixed")
    diarization = _FakeDiarization([(0.0, 0.8, "SPEAKER_00")])

    result = _assign_speakers([seg], diarization)

    assert len(result) == 1
    assert result[0].speaker == "SPEAKER_00"
    assert result[0].confidence is not None
    assert abs(result[0].confidence - 0.8) < 1e-9


# ---------------------------------------------------------------------------
# Test 2: no overlapping turns -> confidence == 0.0
# ---------------------------------------------------------------------------

def test_confidence_no_overlap():
    """When no diarization turn overlaps the segment, confidence must be 0.0."""
    seg = Segment(start_ms=0, end_ms=1000, text="hello", source="mixed")
    diarization = _FakeDiarization([(5.0, 6.0, "SPEAKER_00")])

    result = _assign_speakers([seg], diarization)

    assert len(result) == 1
    assert result[0].speaker is None
    assert result[0].confidence == 0.0


# ---------------------------------------------------------------------------
# Test 3: full overlap is clamped to 1.0 (turn wider than segment)
# ---------------------------------------------------------------------------

def test_confidence_full_overlap_clamped():
    """When the diarization turn fully covers the segment, confidence == 1.0."""
    seg = Segment(start_ms=1000, end_ms=2000, text="hello", source="mixed")
    diarization = _FakeDiarization([(0.0, 10.0, "SPEAKER_00")])

    result = _assign_speakers([seg], diarization)

    assert len(result) == 1
    assert result[0].confidence == 1.0


# ---------------------------------------------------------------------------
# Test 4: run_transcribe WITHOUT diarization -> confidence stays None
# ---------------------------------------------------------------------------

class _FakeModelNoDiarization:
    """Minimal faster_whisper stub returning one segment."""

    def transcribe(self, audio_path, language=None, beam_size=5):
        seg = type("S", (), {"start": 0.0, "end": 1.0, "text": "hello"})()
        info = type("Info", (), {"language": "en", "duration": 1.0})()
        return [seg], info


def test_no_diarization_confidence_is_none(monkeypatch):
    """Without diarization enabled, confidence must remain None on all segments."""
    fake_model = _FakeModelNoDiarization()
    monkeypatch.setattr("whisper_app.transcribe.load_model", lambda m, d, c: fake_model)

    settings = Settings(
        auth_token="tok",
        diarization_enabled=False,
    )
    req = TranscribeRequest(audio_url=_AUDIO_URL, language_hint="en", config={})
    resp = run_transcribe(req, settings)

    assert len(resp.segments) >= 1
    for seg in resp.segments:
        assert seg.confidence is None, f"expected None confidence, got {seg.confidence}"
