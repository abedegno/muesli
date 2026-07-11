"""Tests for _call_openai_compatible's two-path structured-output strategy:
  - json_schema first (Path 1): full schema enforcement for modern endpoints.
  - json_object fallback (Path 2): for endpoints that reject json_schema.
"""
import json

import pytest
import respx
from httpx import Response

from ollama_app.llm import _SECTION_FORMAT, _call_openai_compatible

BASE_URL = "https://api.example.com/v1"
COMPLETIONS_URL = f"{BASE_URL}/chat/completions"


def _ok_response(content: str = None) -> Response:
    """Build a minimal successful OpenAI-compatible chat/completions response."""
    if content is None:
        content = json.dumps({"content_markdown": "Meeting summary.", "refs": [0]})
    return Response(
        200,
        json={
            "model": "gpt-4o-mini",
            "choices": [{"message": {"content": content}}],
        },
    )


def _error_400(message: str) -> Response:
    """Build a 400 response with a message referencing response_format."""
    return Response(
        400,
        json={"error": {"message": message}},
    )


def _error_422(message: str) -> Response:
    """Build a 422 response with a message referencing response_format."""
    return Response(
        422,
        json={"error": {"message": message}},
    )


# ---------------------------------------------------------------------------
# Path 1 — happy path: json_schema is sent and accepted
# ---------------------------------------------------------------------------


@respx.mock
def test_happy_path_sends_json_schema():
    """On success the first request must use json_schema mode with _SECTION_FORMAT."""
    route = respx.post(COMPLETIONS_URL).mock(return_value=_ok_response())

    result, model = _call_openai_compatible(
        BASE_URL, "sk-test", "gpt-4o-mini", "Summarise the meeting.", 0.2
    )

    assert route.call_count == 1, "should make exactly one request on success"
    sent = json.loads(route.calls[0].request.content)

    rf = sent["response_format"]
    assert rf["type"] == "json_schema", "first request must use json_schema type"
    js = rf["json_schema"]
    assert js["name"] == "section"
    assert js["schema"] == _SECTION_FORMAT, "schema must equal _SECTION_FORMAT"
    assert js["strict"] is True

    assert result == json.dumps({"content_markdown": "Meeting summary.", "refs": [0]})
    assert model == "gpt-4o-mini"


@respx.mock
def test_happy_path_no_fallback_needed():
    """When the endpoint accepts json_schema there is no second request."""
    route = respx.post(COMPLETIONS_URL).mock(return_value=_ok_response())
    _call_openai_compatible(BASE_URL, "", "gpt-4o-mini", "Prompt", 0.0)
    assert route.call_count == 1


# ---------------------------------------------------------------------------
# Path 2 — fallback: endpoint rejects json_schema with 400/422 + error body
# ---------------------------------------------------------------------------


@respx.mock
def test_fallback_on_400_with_response_format_in_body():
    """HTTP 400 + 'response_format' in body → retry with json_object, no exception."""
    route = respx.post(COMPLETIONS_URL).mock(
        side_effect=[
            _error_400("response_format json_schema not supported by this model"),
            _ok_response(),
        ]
    )

    # Should NOT raise
    result, _ = _call_openai_compatible(
        BASE_URL, "sk-test", "gpt-4o-mini", "Summarise.", 0.2
    )

    assert route.call_count == 2, "should retry on 400 with response_format error"

    # Second request must use json_object
    sent = json.loads(route.calls[1].request.content)
    assert sent["response_format"] == {"type": "json_object"}

    # Should return the successful response's content
    assert "Meeting summary." in result


@respx.mock
def test_fallback_on_422_with_response_format_in_body():
    """HTTP 422 + 'response_format' in body → retry with json_object, no exception."""
    route = respx.post(COMPLETIONS_URL).mock(
        side_effect=[
            _error_422("Unsupported response_format type: json_schema"),
            _ok_response(),
        ]
    )

    result, _ = _call_openai_compatible(
        BASE_URL, "", "gpt-4o-mini", "Summarise.", 0.2
    )

    assert route.call_count == 2
    sent = json.loads(route.calls[1].request.content)
    assert sent["response_format"] == {"type": "json_object"}
    assert "Meeting summary." in result


@respx.mock
def test_fallback_second_call_uses_json_object_not_json_schema():
    """Regression: the fallback payload must NOT still contain json_schema."""
    route = respx.post(COMPLETIONS_URL).mock(
        side_effect=[
            _error_400("response_format is not supported"),
            _ok_response(),
        ]
    )

    _call_openai_compatible(BASE_URL, "", "model", "Prompt", 0.0)

    fallback_sent = json.loads(route.calls[1].request.content)
    assert fallback_sent["response_format"]["type"] == "json_object"
    assert "json_schema" not in fallback_sent["response_format"]


# ---------------------------------------------------------------------------
# No fallback when the 400/422 is NOT about response_format
# ---------------------------------------------------------------------------


@respx.mock
def test_no_fallback_on_400_unrelated_error():
    """HTTP 400 without 'response_format' in body → raise normally, no retry."""
    route = respx.post(COMPLETIONS_URL).mock(
        return_value=Response(400, json={"error": {"message": "invalid model"}})
    )

    with pytest.raises(Exception):
        _call_openai_compatible(BASE_URL, "", "bad-model", "Prompt", 0.0)
    assert route.call_count == 1  # no retry: only one request should have been made


@respx.mock
def test_no_fallback_on_500():
    """HTTP 500 is not a response_format rejection → raise normally."""
    route = respx.post(COMPLETIONS_URL).mock(
        return_value=Response(500, json={"error": {"message": "internal server error"}})
    )

    with pytest.raises(Exception):
        _call_openai_compatible(BASE_URL, "", "model", "Prompt", 0.0)
    assert route.call_count == 1  # no retry: only one request should have been made


# ---------------------------------------------------------------------------
# Auth header propagation
# ---------------------------------------------------------------------------


@respx.mock
def test_api_key_sent_in_authorization_header():
    """The api_key must always appear in the Authorization header."""
    route = respx.post(COMPLETIONS_URL).mock(return_value=_ok_response())
    _call_openai_compatible(BASE_URL, "sk-secret", "model", "Prompt", 0.0)
    assert route.calls[0].request.headers["authorization"] == "Bearer sk-secret"


@respx.mock
def test_no_auth_header_when_api_key_empty():
    """When api_key is empty no Authorization header should be sent."""
    route = respx.post(COMPLETIONS_URL).mock(return_value=_ok_response())
    _call_openai_compatible(BASE_URL, "", "model", "Prompt", 0.0)
    assert "authorization" not in route.calls[0].request.headers
