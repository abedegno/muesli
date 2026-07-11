"""Tests for fetch_openai_models() — pure-logic, all HTTP mocked via respx."""
import pytest
import respx
from httpx import ConnectError, Response

from ollama_app.llm import fetch_openai_models

BASE_URL = "https://api.example.com"
MODELS_URL = f"{BASE_URL}/v1/models"


@respx.mock
def test_fetch_models_success():
    """Happy path: returns list of model IDs from response data."""
    respx.get(MODELS_URL).mock(return_value=Response(
        200,
        json={"data": [{"id": "gpt-4o"}, {"id": "gpt-4o-mini"}]},
    ))
    result = fetch_openai_models(BASE_URL, "")
    assert result == ["gpt-4o", "gpt-4o-mini"]


@respx.mock
def test_fetch_models_includes_auth_header():
    """Authorization: Bearer <api_key> header is sent when api_key is non-empty."""
    route = respx.get(MODELS_URL).mock(return_value=Response(
        200,
        json={"data": [{"id": "gpt-4o"}]},
    ))
    fetch_openai_models(BASE_URL, "sk-test")
    assert route.calls[0].request.headers["authorization"] == "Bearer sk-test"


@respx.mock
def test_fetch_models_no_auth_when_no_key():
    """No Authorization header is sent when api_key is empty."""
    route = respx.get(MODELS_URL).mock(return_value=Response(
        200,
        json={"data": []},
    ))
    fetch_openai_models(BASE_URL, "")
    assert "authorization" not in route.calls[0].request.headers


@respx.mock
def test_fetch_models_network_error():
    """Network error (ConnectError) returns []."""
    respx.get(MODELS_URL).mock(side_effect=ConnectError("connection refused"))
    result = fetch_openai_models(BASE_URL, "")
    assert result == []


@respx.mock
def test_fetch_models_non_200():
    """Non-2xx status (e.g. 404) returns []."""
    respx.get(MODELS_URL).mock(return_value=Response(404, json={"error": "not found"}))
    result = fetch_openai_models(BASE_URL, "")
    assert result == []


@respx.mock
def test_fetch_models_bad_json():
    """Non-JSON body returns []."""
    respx.get(MODELS_URL).mock(return_value=Response(200, content=b"not valid json"))
    result = fetch_openai_models(BASE_URL, "")
    assert result == []


@respx.mock
def test_fetch_models_missing_data_key():
    """Response with wrong key (no 'data') returns []."""
    respx.get(MODELS_URL).mock(return_value=Response(
        200,
        json={"models": [{"id": "gpt-4o"}]},
    ))
    result = fetch_openai_models(BASE_URL, "")
    assert result == []
