from datetime import datetime
import pathlib
import threading
from http.server import HTTPServer, SimpleHTTPRequestHandler

import pytest
import respx
from freezegun import freeze_time
from starlette.testclient import TestClient

from parakeet_app.config import Settings
from parakeet_app.main import create_app

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
    return Settings(auth_token=TOKEN, default_model="nvidia/parakeet-tdt-1.1b", device="cpu")


@pytest.fixture
def client(settings):
    app = create_app(settings)
    with TestClient(app, base_url="http://plugin") as c:
        yield c


@pytest.fixture
def auth_headers():
    return {"Authorization": f"Bearer {TOKEN}", "X-Muesli-Plugin-API": "1"}


class _FakeSegment:
    def __init__(self, start, end, text, source="mixed", speaker=None):
        self.start, self.end, self.text = start, end, text
        self.source = source
        self.speaker = speaker


class _FakeResult:
    def __init__(self, language=None, duration=1.0):
        self.language = language or "en"
        self.duration = duration
        self.segments = [
            _FakeSegment(0.0, 0.5, "hello"),
            _FakeSegment(0.5, 1.0, "world"),
        ]


class _FakeModel:
    """Stand-in for NeMo/Parakeet — no weights, no audio decode."""

    def __init__(self):
        self.loaded_with = None
        self.last_language = None

    def load(self, model, device):
        self.loaded_with = (model, device)
        return self

    def transcribe(self, audio_path, language=None):
        self.last_language = language
        return _FakeResult(language=language, duration=1.0)


@pytest.fixture
def fake_model(monkeypatch):
    fake = _FakeModel()
    monkeypatch.setattr("parakeet_app.transcribe.load_model", lambda m, d: fake.load(m, d))
    return fake


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
