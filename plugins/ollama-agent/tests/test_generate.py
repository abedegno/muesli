import json

import respx
from httpx import Response

OLLAMA_URL = "http://ollama:11434"


def _body():
    return {
        "transcript": [
            {"start_ms": 0, "end_ms": 1000, "text": "We ship Friday.", "source": "mixed"}
        ],
        "notes_markdown": "- ship date?",
        "template": {
            "sections": [
                {"heading": "Overview", "instruction": "Summarise."},
                {"heading": "Action items", "instruction": "Owners + deadlines."},
            ]
        },
        "config": {"ollama_url": OLLAMA_URL, "model": "llama3.2"},
    }


def _section_json():
    # The agent generates ONE section per model call, so the model returns a single
    # content object (no heading — the server fills it from the template).
    return json.dumps({"content_markdown": "Team ships Friday.", "refs": [0]})


@respx.mock
def test_generate_maps_ollama_response(client, auth_headers):
    # Template has 2 sections → 2 model calls; each returns the single object.
    route = respx.post(f"{OLLAMA_URL}/api/generate").mock(
        return_value=Response(200, json={"response": _section_json(), "model": "llama3.2"})
    )
    r = client.post("/generate", json=_body(), headers=auth_headers)
    assert r.status_code == 200
    out = r.json()
    assert out["model"] == "llama3.2"
    # Headings come from the template, in order; content from the model.
    assert [s["heading"] for s in out["summary"]["sections"]] == ["Overview", "Action items"]
    assert out["summary"]["sections"][0]["content_markdown"] == "Team ships Friday."
    assert out["summary"]["sections"][0]["refs"] == [0]

    # one model call per section
    assert route.call_count == 2
    sent = json.loads(route.calls.last.request.content)
    assert sent["model"] == "llama3.2"
    assert sent["stream"] is False
    # Structured-output schema (not the bare "json" string) enforces the shape so
    # the prompt needs no copyable example.
    assert isinstance(sent["format"], dict)
    assert sent["format"]["required"] == ["content_markdown"]
    assert "content_markdown" in sent["format"]["properties"]
    assert "We ship Friday." in sent["prompt"]


@respx.mock
def test_generate_uses_openai_compatible_when_base_url_set(client, auth_headers):
    base = "https://api.example.com/v1"
    route = respx.post(f"{base}/chat/completions").mock(
        return_value=Response(
            200,
            json={
                "model": "gpt-4o-mini",
                "choices": [{"message": {"content": _section_json()}}],
            },
        )
    )
    body = _body()
    body["config"] = {"base_url": base, "api_key": "sk-test", "model": "gpt-4o-mini"}
    r = client.post("/generate", json=body, headers=auth_headers)
    assert r.status_code == 200
    out = r.json()
    assert out["model"] == "gpt-4o-mini"
    assert route.called
    assert route.calls.last.request.headers["authorization"] == "Bearer sk-test"


@respx.mock
def test_generate_accepts_empty_transcript(client, auth_headers):
    # Silent/very short audio yields zero transcript segments. The agent must
    # summarise from notes alone and return 200, not reject with 422.
    route = respx.post(f"{OLLAMA_URL}/api/generate").mock(
        return_value=Response(200, json={"response": _section_json(), "model": "llama3.2"})
    )
    body = _body()
    body["transcript"] = []
    r = client.post("/generate", json=body, headers=auth_headers)
    assert r.status_code == 200, r.text
    assert r.json()["model"] == "llama3.2"
    assert route.called
    sent = json.loads(route.calls.last.request.content)
    assert "(empty)" in sent["prompt"]


@respx.mock
def test_generate_accepts_missing_transcript(client, auth_headers):
    # transcript omitted entirely → treated as empty, returns 200.
    respx.post(f"{OLLAMA_URL}/api/generate").mock(
        return_value=Response(200, json={"response": _section_json(), "model": "llama3.2"})
    )
    body = _body()
    del body["transcript"]
    r = client.post("/generate", json=body, headers=auth_headers)
    assert r.status_code == 200, r.text


@respx.mock
def test_generate_accepts_null_transcript(client, auth_headers):
    # transcript: null (a nil Go slice marshals to JSON null) → treated as empty.
    respx.post(f"{OLLAMA_URL}/api/generate").mock(
        return_value=Response(200, json={"response": _section_json(), "model": "llama3.2"})
    )
    body = _body()
    body["transcript"] = None
    r = client.post("/generate", json=body, headers=auth_headers)
    assert r.status_code == 200, r.text


@respx.mock
def test_generate_heading_comes_from_template_not_model(client, auth_headers):
    # Even if a model emits a stray "heading", the server ignores it and uses the
    # template heading by position — so a weak model can never produce a wrong or
    # placeholder heading.
    stray = json.dumps({"heading": "WRONG", "content_markdown": "real content", "refs": [0]})
    respx.post(f"{OLLAMA_URL}/api/generate").mock(
        return_value=Response(200, json={"response": stray, "model": "llama3.2"})
    )
    r = client.post("/generate", json=_body(), headers=auth_headers)
    assert r.status_code == 200, r.text
    secs = r.json()["summary"]["sections"]
    assert [s["heading"] for s in secs] == ["Overview", "Action items"]
    assert "WRONG" not in [s["heading"] for s in secs]


@respx.mock
def test_generate_rejects_placeholder_content_to_502(client, auth_headers):
    # The exact bug we fixed: a weak model echoes the scaffold instead of filling
    # it. Placeholder content (a lone <...> token) must be rejected as 502, never
    # stored as a degenerate summary.
    scaffold = json.dumps({"content_markdown": "<markdown>"})
    respx.post(f"{OLLAMA_URL}/api/generate").mock(
        return_value=Response(200, json={"response": scaffold, "model": "llama3.2"})
    )
    r = client.post("/generate", json=_body(), headers=auth_headers)
    assert r.status_code == 502
    assert "malformed" in r.json()["detail"].lower()


@respx.mock
def test_generate_rejects_missing_content_to_502(client, auth_headers):
    # A section object with no usable content_markdown is an upstream failure (502),
    # not a silent empty section.
    nocontent = json.dumps({"refs": [0]})
    respx.post(f"{OLLAMA_URL}/api/generate").mock(
        return_value=Response(200, json={"response": nocontent, "model": "llama3.2"})
    )
    r = client.post("/generate", json=_body(), headers=auth_headers)
    assert r.status_code == 502


@respx.mock
def test_generate_maps_non_json_model_output_to_502(client, auth_headers):
    respx.post(f"{OLLAMA_URL}/api/generate").mock(
        return_value=Response(200, json={"response": "sorry, I cannot", "model": "llama3.2"})
    )
    r = client.post("/generate", json=_body(), headers=auth_headers)
    assert r.status_code == 502


@respx.mock
def test_generate_uses_settings_base_url_when_config_empty(auth_headers):
    """LLM_BASE_URL / LLM_API_KEY in settings act as fallback when request config is empty.

    When a request arrives with no base_url / api_key in its config, the handler
    must resolve those from settings — otherwise the env vars are a no-op for
    actual inference and the operator docs are misleading.
    """
    from starlette.testclient import TestClient

    from ollama_app.config import Settings
    from ollama_app.main import create_app

    base_url = "https://api.example.com"
    settings = Settings(auth_token="test-token", base_url=base_url, api_key="sk-env")
    app = create_app(settings)

    route = respx.post(f"{base_url}/chat/completions").mock(
        return_value=Response(
            200,
            json={
                "model": "gpt-4o",
                "choices": [{"message": {"content": _section_json()}}],
            },
        )
    )

    # Request deliberately omits base_url and api_key — they should come from settings.
    body = _body()
    body["config"] = {}

    with TestClient(app, base_url="http://plugin") as c:
        r = c.post("/generate", json=body, headers=auth_headers)

    assert r.status_code == 200, r.text
    assert route.called, "request should have gone to the OpenAI-compatible endpoint"
    assert route.calls.last.request.headers["authorization"] == "Bearer sk-env"
