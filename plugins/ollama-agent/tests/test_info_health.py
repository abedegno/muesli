import httpx
import respx
from httpx import Response
from starlette.testclient import TestClient

from ollama_app.config import Settings
from ollama_app.main import create_app


def test_info_shape(client, auth_headers):
    r = client.get("/info", headers=auth_headers)
    assert r.status_code == 200
    body = r.json()
    assert body["name"] == "muesli-ollama-agent"
    assert body["plugin_api"] == 1
    assert body["kind"] == "agent"
    schema = body["config_schema"]
    assert schema["type"] == "object"
    # config fields the admin UI (Plan 4) renders:
    assert {"ollama_url", "model", "base_url", "api_key", "temperature"}.issubset(
        schema["properties"].keys()
    )
    # api_key is the BYO-cloud secret -> flagged so the UI masks + encrypts it.
    assert schema["properties"]["api_key"].get("writeOnly") is True
    # ...and hinted as a password field for broader form-renderer support.
    assert schema["properties"]["api_key"].get("format") == "password"


def test_health_ok_without_auth(client):
    # Health is intentionally UNAUTHENTICATED: scale-to-zero / k8s readiness
    # probes send no auth headers. It must return 200 with no token.
    r = client.get("/health")
    assert r.status_code == 200
    assert r.json() == {"status": "ok"}


@respx.mock
def test_info_available_models_with_base_url(auth_headers):
    """When settings.base_url is set, /info returns available_models from /v1/models."""
    base_url = "https://api.example.com"
    respx.get(f"{base_url}/v1/models").mock(return_value=Response(
        200,
        json={"data": [{"id": "gpt-4o"}]},
    ))
    settings = Settings(auth_token="test-token", base_url=base_url, api_key="sk-test")
    app = create_app(settings)
    with TestClient(app, base_url="http://plugin") as c:
        r = c.get("/info", headers=auth_headers)
    assert r.status_code == 200
    body = r.json()
    assert body["available_models"] == ["gpt-4o"]


def test_info_available_models_empty_in_ollama_mode(client, auth_headers):
    """In local Ollama mode (no base_url), /info returns available_models: []."""
    r = client.get("/info", headers=auth_headers)
    assert r.status_code == 200
    body = r.json()
    assert body["available_models"] == []


@respx.mock
def test_info_available_models_from_ollama_tags(auth_headers):
    """In local Ollama mode, /info discovers installed models via /api/tags and
    exposes them both as available_models and as config_schema.model.enum."""
    ollama_url = "http://ollama:11434"
    respx.get(f"{ollama_url}/api/tags").mock(return_value=Response(
        200,
        json={"models": [{"name": "llama3.2:latest"}, {"name": "mistral:latest"}]},
    ))
    settings = Settings(auth_token="test-token", default_ollama_url=ollama_url)
    app = create_app(settings)
    with TestClient(app, base_url="http://plugin") as c:
        r = c.get("/info", headers=auth_headers)
    assert r.status_code == 200
    body = r.json()
    assert body["available_models"] == ["llama3.2:latest", "mistral:latest"]
    assert body["config_schema"]["properties"]["model"]["enum"] == [
        "llama3.2:latest",
        "mistral:latest",
    ]


@respx.mock
def test_info_ollama_unreachable_falls_back_to_free_text_model(auth_headers):
    """When Ollama is unreachable, /info tolerates it: available_models is []
    and config_schema.model stays a plain free-text string with no enum."""
    ollama_url = "http://ollama:11434"
    respx.get(f"{ollama_url}/api/tags").mock(
        side_effect=httpx.ConnectError("connection refused")
    )
    settings = Settings(auth_token="test-token", default_ollama_url=ollama_url)
    app = create_app(settings)
    with TestClient(app, base_url="http://plugin") as c:
        r = c.get("/info", headers=auth_headers)
    assert r.status_code == 200
    body = r.json()
    assert body["available_models"] == []
    assert "enum" not in body["config_schema"]["properties"]["model"]
