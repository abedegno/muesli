import os

import pytest

pytestmark = pytest.mark.skipif(
    os.environ.get("RUN_OLLAMA_INTEGRATION") != "1",
    reason="set RUN_OLLAMA_INTEGRATION=1 and run a local Ollama to exercise the real model",
)


def test_real_ollama_generates(client, auth_headers):
    body = {
        "transcript": [{"start_ms": 0, "end_ms": 1000, "text": "We ship Friday.", "source": "mixed"}],
        "notes_markdown": "- ship date?",
        "template": {"sections": [{"heading": "Overview", "instruction": "Summarise."}]},
        "config": {
            "ollama_url": os.environ.get("OLLAMA_URL", "http://localhost:11434"),
            "model": os.environ.get("OLLAMA_MODEL", "llama3.2"),
        },
    }
    r = client.post("/generate", json=body, headers=auth_headers)
    assert r.status_code == 200
    assert r.json()["summary"]["sections"][0]["heading"] == "Overview"
