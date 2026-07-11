from datetime import datetime

import pytest
import respx
from freezegun import freeze_time
from starlette.testclient import TestClient

from ollama_app.config import Settings
from ollama_app.main import create_app

TOKEN = "test-token"


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
    return Settings(
        auth_token=TOKEN,
        default_ollama_url="http://ollama:11434",
        default_model="llama3.2",
    )


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
