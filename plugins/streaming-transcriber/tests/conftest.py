from datetime import datetime
import math
from array import array
from typing import Iterable

import pytest
from freezegun import freeze_time
from starlette.testclient import TestClient

from streaming_app.config import Settings
from streaming_app.main import create_app

TOKEN = "test-token"
SAMPLE_RATE = 16000


@pytest.fixture
def frozen_clock():
    with freeze_time("2024-01-15 12:00:00"):
        yield datetime(2024, 1, 15, 12, 0, 0)


@pytest.fixture
def settings() -> Settings:
    return Settings(
        auth_token=TOKEN,
        default_model="tiny.en",
        device="cpu",
        compute_type="int8",
        vad_aggressiveness=2,
        silence_threshold_ms=600,
        min_speech_ms=120,
        partial_interval_ms=400,
    )


@pytest.fixture
def client(settings):
    app = create_app(settings)
    with TestClient(app, base_url="http://plugin") as c:
        yield c


@pytest.fixture
def auth_headers():
    return {"Authorization": f"Bearer {TOKEN}", "X-Muesli-Plugin-API": "1"}


def _tone_burst(duration_ms: int, amplitude: int = 16000) -> bytes:
    """Generate a speech-ish synthetic utterance for deterministic VAD tests."""

    total_samples = SAMPLE_RATE * duration_ms // 1000
    data = array("h")
    for i in range(total_samples):
        t = i / SAMPLE_RATE
        envelope = min(1.0, max(0.0, t / 0.08)) * min(1.0, max(0.0, (duration_ms / 1000.0 - t) / 0.08))
        carrier = (
            0.45 * math.sin(2 * math.pi * 180 * t)
            + 0.25 * math.sin(2 * math.pi * 320 * t)
            + 0.2 * math.sin(2 * math.pi * 820 * t)
            + 0.1 * math.sin(2 * math.pi * 1200 * t)
        )
        sample = int(amplitude * envelope * carrier)
        data.append(max(-32768, min(32767, sample)))
    return data.tobytes()


def synthetic_stream(*parts: Iterable[bytes]) -> bytes:
    out = bytearray()
    for part in parts:
        out.extend(part)
    return bytes(out)


@pytest.fixture
def utterance_pcm() -> bytes:
    silence_700 = b"\x00\x00" * (SAMPLE_RATE * 700 // 1000)
    speech_600 = _tone_burst(600)
    speech_500 = _tone_burst(500)
    return synthetic_stream(speech_600, silence_700, speech_500)


@pytest.fixture
def chunk_200ms() -> int:
    return SAMPLE_RATE * 2 * 200 // 1000
