from datetime import datetime
from typing import Optional

import pathlib
import threading
from http.server import HTTPServer, SimpleHTTPRequestHandler

import pytest
import respx
from freezegun import freeze_time
from starlette.testclient import TestClient

from whisper_app.config import Settings
from whisper_app.main import create_app

TOKEN = "test-token"

FIXTURES = pathlib.Path(__file__).parent / "fixtures"


@pytest.fixture(autouse=True)
def _block_real_http():
    """Block all real outbound HTTP in every test — use respx.get/post/... to mock."""
    with respx.mock:
        yield


@pytest.fixture
def frozen_clock():
    """Freeze time at a deterministic instant; use instead of datetime.now()."""
    with freeze_time("2024-01-15 12:00:00"):
        yield datetime(2024, 1, 15, 12, 0, 0)


@pytest.fixture
def settings() -> Settings:
    return Settings(auth_token=TOKEN, default_model="tiny", device="cpu", compute_type="int8")


@pytest.fixture
def client(settings):
    # starlette's TestClient drives the ASGI app in-process. (The plan's original
    # httpx.Client(transport=ASGITransport(...)) sync pattern is unsupported by
    # httpx: ASGITransport is async-only, so a sync Client cannot enter it.)
    app = create_app(settings)
    with TestClient(app, base_url="http://plugin") as c:
        yield c


@pytest.fixture
def auth_headers():
    return {"Authorization": f"Bearer {TOKEN}", "X-Muesli-Plugin-API": "1"}


class _FakeWord:
    """Mimics a faster_whisper WordTiming object."""

    def __init__(self, word: str, start: float, end: float):
        self.word = word
        self.start = start
        self.end = end


class _FakeSegment:
    def __init__(self, start, end, text, words=None):
        self.start, self.end, self.text = start, end, text
        self.words = words  # None when word_timestamps not requested


class _FakeModel:
    """Stand-in for faster_whisper.WhisperModel — no weights, no audio decode."""

    def __init__(self, with_words: bool = False):
        self.loaded_with = None
        self.last_language: Optional[str] = None
        self._with_words = with_words

    def load(self, model, device, compute_type):
        self.loaded_with = (model, device, compute_type)
        return self

    def transcribe(self, audio_path, language=None, beam_size=5, word_timestamps=False):
        self.last_language = language
        if word_timestamps and self._with_words:
            segs = [
                _FakeSegment(
                    0.0, 0.5, " hello",
                    words=[_FakeWord("hello", 0.0, 0.3)],
                ),
                _FakeSegment(
                    0.5, 1.0, " world",
                    words=[_FakeWord("world", 0.35, 0.7)],
                ),
            ]
        else:
            segs = [_FakeSegment(0.0, 0.5, "hello"), _FakeSegment(0.5, 1.0, "world")]
        info = type("Info", (), {"language": language or "en", "duration": 1.0})()
        return segs, info


@pytest.fixture
def fake_model(monkeypatch):
    fake = _FakeModel(with_words=False)
    monkeypatch.setattr("whisper_app.transcribe.load_model", lambda m, d, c: fake.load(m, d, c))
    return fake


@pytest.fixture
def settings_with_words(monkeypatch) -> Settings:
    """Settings with word_timestamps_enabled=True and load_model patched to return word data."""
    fake = _FakeModel(with_words=True)
    monkeypatch.setattr("whisper_app.transcribe.load_model", lambda m, d, c: fake.load(m, d, c))
    return Settings(
        auth_token=TOKEN,
        default_model="tiny",
        device="cpu",
        compute_type="int8",
        word_timestamps_enabled=True,
    )


@pytest.fixture
def served_audio_url():
    """Serve tests/fixtures over HTTP so the plugin's real fetch path runs."""
    handler = lambda *a, **k: SimpleHTTPRequestHandler(*a, directory=str(FIXTURES), **k)
    server = HTTPServer(("127.0.0.1", 0), handler)
    port = server.server_address[1]
    t = threading.Thread(target=server.serve_forever, daemon=True)
    t.start()
    url = f"http://127.0.0.1:{port}/tiny.wav"
    respx.get(url).pass_through()
    try:
        yield url
    finally:
        server.shutdown()
