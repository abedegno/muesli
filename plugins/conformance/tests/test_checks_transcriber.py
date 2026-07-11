"""The reference whisper plugin self-certifies: run the conformance suite
against the *actual* whisper-transcriber app and assert it is CONFORMANT."""
import pytest
from starlette.testclient import TestClient

from muesli_plugin_conformance.runner import run_conformance


@pytest.fixture
def whisper_client(monkeypatch):
    from whisper_app.config import Settings  # whisper-transcriber package
    from whisper_app.main import create_app

    # Stub the model so no weights are downloaded and any audio bytes "transcribe".
    class _Seg:
        def __init__(self, s, e, t):
            self.start, self.end, self.text = s, e, t

    def fake_load(model, device, compute_type):
        class _M:
            def transcribe(self, path, language=None, beam_size=5):
                segs = [_Seg(0.0, 0.5, "hello")]
                info = type("I", (), {"language": language or "en", "duration": 0.5})()
                return segs, info

        return _M()

    monkeypatch.setattr("whisper_app.transcribe.load_model", fake_load)

    app = create_app(Settings(auth_token="t", default_model="tiny"))
    # The plan's `httpx.Client(ASGITransport(...))` pattern is broken for in-process
    # ASGI testing; starlette's TestClient (an httpx.Client subclass) is the
    # supported equivalent and works unchanged with run_conformance.
    with TestClient(app, base_url="http://plugin") as c:
        yield c


@pytest.fixture
def parakeet_client(monkeypatch):
    from parakeet_app.config import Settings
    from parakeet_app.main import create_app

    class _Seg:
        def __init__(self, s, e, t):
            self.start, self.end, self.text = s, e, t

    def fake_load(model, device):
        class _M:
            def transcribe(self, path, language=None):
                segs = [_Seg(0.0, 0.5, "hello")]
                info = type("I", (), {"language": language or "en", "duration": 0.5})()
                return segs, info

        return _M()

    monkeypatch.setattr("parakeet_app.transcribe.load_model", fake_load)

    app = create_app(Settings(auth_token="t", default_model="nvidia/parakeet-tdt-1.1b"))
    with TestClient(app, base_url="http://plugin") as c:
        yield c


def test_whisper_plugin_is_conformant(whisper_client):
    report = run_conformance(whisper_client, kind="transcriber", token="t")
    assert report.ok, report.summary()


def test_parakeet_plugin_is_conformant(parakeet_client):
    assert run_conformance(parakeet_client, kind="transcriber", token="t").ok is True
