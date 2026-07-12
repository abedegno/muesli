from __future__ import annotations

import contextlib
import tempfile
import wave
from dataclasses import dataclass
from functools import lru_cache
from typing import Any


@dataclass(frozen=True)
class TranscriptionSettings:
    model: str
    device: str
    compute_type: str
    language_hint: str | None = None


def load_model(model: str, device: str, compute_type: str):
    """Lazily import and construct the faster-whisper model.

    The import stays inside the function body so tests can monkeypatch this seam
    without importing faster_whisper or downloading weights.
    """

    from faster_whisper import WhisperModel

    return WhisperModel(model, device=device, compute_type=compute_type)


@lru_cache(maxsize=8)
def _cached_model(model: str, device: str, compute_type: str):
    return load_model(model, device, compute_type)


def _write_temp_wav(pcm_bytes: bytes, sample_rate: int) -> str:
    with tempfile.NamedTemporaryFile(suffix=".wav", delete=False) as tmp:
        with wave.open(tmp, "wb") as wav_file:
            wav_file.setnchannels(1)
            wav_file.setsampwidth(2)
            wav_file.setframerate(sample_rate)
            wav_file.writeframes(pcm_bytes)
        return tmp.name


def _coerce_text(segments: Any, info: Any) -> str:
    texts: list[str] = []
    if segments is None:
        return ""
    for segment in segments:
        text = getattr(segment, "text", "")
        if text:
            texts.append(str(text).strip())
    if texts:
        return " ".join(texts).strip()
    text = getattr(info, "text", "")
    return str(text).strip() if text else ""


def transcribe_utterance(
    pcm_bytes: bytes,
    sample_rate: int,
    settings: TranscriptionSettings,
) -> str:
    """Transcribe one buffered utterance into plain text.

    This is the seam tests monkeypatch. The default implementation writes the
    PCM payload to a temp WAV, loads the model once per process, and joins all
    faster-whisper segments into one string.
    """

    path = _write_temp_wav(pcm_bytes, sample_rate)
    try:
        model = _cached_model(settings.model, settings.device, settings.compute_type)
        segments, info = model.transcribe(path, language=settings.language_hint, beam_size=5)
        return _coerce_text(segments, info)
    finally:
        with contextlib.suppress(OSError):
            import os

            os.unlink(path)
