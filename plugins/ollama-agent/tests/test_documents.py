import json
from pathlib import Path

import pytest
import respx
from httpx import Response
from pydantic import ValidationError

from ollama_app.main import OPENAI_PROVIDER_FRAMING_TOKENS
from ollama_app.schema import GenerateRequest

OLLAMA_URL = "http://ollama:11434"
GOLDEN_PATH = Path(__file__).parent / "testdata" / "cross_prompts" / "two_meetings.golden"


def _golden_corpus() -> str:
    return GOLDEN_PATH.read_text(encoding="utf-8")


def _documents_body(notes_markdown: str | None = None) -> dict:
    return {
        "documents": [
            {
                "note_id": "note-1",
                "title": "Sprint planning",
                "occurred_at": "2026-07-01T09:00:00Z",
                "transcript_generation": 2,
                "segments": [
                    {"start_ms": 0, "end_ms": 1000, "text": "Let's ship by Friday.", "speaker": "Alice"},
                    {"start_ms": 1000, "end_ms": 2000, "text": "Agreed.", "speaker": "Bob"},
                ],
            },
            {
                "note_id": "note-2",
                "title": "Retro",
                "occurred_at": "2026-07-08T09:00:00Z",
                "transcript_generation": 1,
                "segments": [
                    {"start_ms": 0, "end_ms": 500, "text": "We missed the deadline."},
                ],
            },
        ],
        "notes_markdown": notes_markdown if notes_markdown is not None else _golden_corpus(),
        "template": {
            "sections": [
                {"heading": "Decisions", "instruction": "List decisions."},
                {"heading": "Risks", "instruction": "List unresolved risks."},
            ]
        },
        "config": {"ollama_url": OLLAMA_URL, "model": "llama3.2"},
    }


def _legacy_transcript_body() -> dict:
    return {
        "transcript": [{"start_ms": 0, "end_ms": 1000, "text": "We ship Friday.", "source": "mixed"}],
        "notes_markdown": "- ship date?",
        "template": {"sections": [{"heading": "Overview", "instruction": "Summarise."}]},
        "config": {"ollama_url": OLLAMA_URL, "model": "llama3.2"},
    }


def _section_json(content: str = "Some section content [1]") -> str:
    return json.dumps({"content_markdown": content})


def test_documents_only_accepted():
    req = GenerateRequest.model_validate(_documents_body())
    assert len(req.documents) == 2
    assert req.transcript == []


def test_legacy_transcript_still_accepted():
    req = GenerateRequest.model_validate(_legacy_transcript_body())
    assert req.documents is None
    assert len(req.transcript) == 1


def test_both_forms_rejected():
    body = _legacy_transcript_body()
    body["documents"] = _documents_body()["documents"]
    with pytest.raises(ValidationError):
        GenerateRequest.model_validate(body)


def test_neither_form_still_tolerated_as_notes_only():
    """Unlike the Go pluginkit HTTP boundary, this pre-existing Python
    contract tolerates a missing/null transcript with no documents (notes-
    only summarisation) -- see test_generate_accepts_missing_transcript /
    test_generate_accepts_null_transcript in test_generate.py, which this
    must not regress."""
    body = _legacy_transcript_body()
    body["transcript"] = None
    req = GenerateRequest.model_validate(body)
    assert req.transcript == []
    assert req.documents is None


def test_duplicate_document_note_ids_rejected():
    body = _documents_body()
    body["documents"].append(body["documents"][0])
    with pytest.raises(ValidationError):
        GenerateRequest.model_validate(body)


@respx.mock
def test_ollama_receives_corpus_verbatim_with_distinct_meetings_and_citations(client, auth_headers):
    """Loads the Task 5 (Go) golden and proves the request forwarded to
    Ollama's /api/generate contains it byte-for-byte unchanged -- distinct
    meeting delimiters, dates, per-meeting note ids, and global citation
    numbers all survive into the actual model prompt, and the run focus
    appears too."""
    golden = _golden_corpus()
    route = respx.post(f"{OLLAMA_URL}/api/generate").mock(
        return_value=Response(200, json={"response": _section_json(), "model": "llama3.2"})
    )
    r = client.post("/generate", json=_documents_body(), headers=auth_headers)
    assert r.status_code == 200, r.text
    assert route.call_count == 2  # one call per template section

    for call in route.calls:
        sent = json.loads(call.request.content)
        assert golden in sent["prompt"], "corpus must appear byte-for-byte in the model prompt"
    # Distinct meeting delimiters/dates/note ids/citations from the golden:
    assert "--- MEETING 1: Sprint planning | 2026-07-01T09:00:00Z | note_id=note-1 ---" in golden
    assert "--- MEETING 2: Retro | 2026-07-08T09:00:00Z | note_id=note-2 ---" in golden
    assert "[1]" in golden and "[2]" in golden and "[3]" in golden
    assert "FOCUS: Compare decisions and unresolved risks" in golden


@respx.mock
def test_openai_compatible_envelope_stays_within_provider_framing_tokens(auth_headers):
    """The fixed OpenAI-compatible chat envelope (role/content JSON wrapper)
    around the verbatim prompt costs no more than the OPENAI_PROVIDER_FRAMING_TOKENS
    constant /info publishes -- proving the published number is not an
    understatement of the real overhead."""
    from starlette.testclient import TestClient

    from ollama_app.config import Settings
    from ollama_app.main import create_app

    base_url = "https://api.example.com"
    route = respx.post(f"{base_url}/chat/completions").mock(
        return_value=Response(
            200,
            json={"choices": [{"message": {"content": _section_json()}}], "model": "gpt-4o"},
        )
    )
    settings = Settings(auth_token="test-token", base_url=base_url, api_key="sk-test")
    app = create_app(settings)
    body = _documents_body()
    body["config"] = {"base_url": base_url, "api_key": "sk-test", "model": "gpt-4o"}
    with TestClient(app, base_url="http://plugin") as c:
        r = c.post("/generate", json=body, headers=auth_headers)
    assert r.status_code == 200, r.text

    sent = json.loads(route.calls[0].request.content)
    prompt = sent["messages"][0]["content"]
    # The envelope's byte overhead is the JSON-encoded messages array minus the
    # raw prompt bytes themselves -- the JSON structure (keys, quoting,
    # escaping) wrapping the same content.
    envelope_bytes = len(json.dumps(sent["messages"]).encode("utf-8")) - len(prompt.encode("utf-8"))
    assert envelope_bytes <= OPENAI_PROVIDER_FRAMING_TOKENS
