import json
import re

import httpx

from .config import Settings
from .prompt import build_section_prompt
from .schema import GenerateRequest, GenerateResponse, Summary, SummarySection


class ModelOutputError(Exception):
    """The model returned output we cannot map to the contract Summary (not JSON,
    wrong shape, or missing/placeholder content). Mapped to HTTP 502 in main.py —
    it's an upstream/model failure, never the caller's fault."""


# A lone angle-bracket token (e.g. "<markdown>", "<content>") is scaffold the model
# echoed instead of filling in — never valid content.
_PLACEHOLDER = re.compile(r"^<[^>]*>$")

# JSON schema for one section's output. Passed to the model's structured-output
# `format` so the shape is enforced WITHOUT an in-prompt example (which a weak model
# would copy verbatim instead of writing real content).
_SECTION_FORMAT = {
    "type": "object",
    "properties": {
        "content_markdown": {"type": "string"},
        "refs": {"type": "array", "items": {"type": "integer"}},
    },
    "required": ["content_markdown"],
}


def _parse_section_json(raw: str, heading: str) -> SummarySection:
    """Parse one section's JSON object ({content_markdown, refs?}) into a
    SummarySection. The heading is supplied by the caller from the template — the
    model never produces it, so it can't echo a placeholder heading. Tolerates extra
    prose around the JSON object. Raises ModelOutputError on malformed/empty/
    placeholder output (mapped to 502) rather than leaking a 500 or storing scaffold."""
    start, end = raw.find("{"), raw.rfind("}")
    if start == -1 or end == -1:
        raise ModelOutputError("model did not return a JSON object")
    try:
        data = json.loads(raw[start : end + 1])
    except json.JSONDecodeError as e:
        raise ModelOutputError(f"model output was not valid JSON: {e}") from e
    if not isinstance(data, dict):
        raise ModelOutputError("model output is not a JSON object")
    content = data.get("content_markdown")
    if not isinstance(content, str) or not content.strip():
        raise ModelOutputError("model output has empty 'content_markdown'")
    if _PLACEHOLDER.match(content.strip()):
        raise ModelOutputError("'content_markdown' is placeholder text, not real content")
    refs = data.get("refs")
    if refs is not None and not isinstance(refs, list):
        raise ModelOutputError("model output has a non-list 'refs'")
    return SummarySection(heading=heading, content_markdown=content, refs=refs)


def fetch_openai_models(base_url: str, api_key: str) -> list[str]:
    """Fetch available model IDs from an OpenAI-compatible /v1/models endpoint.

    GETs {base_url}/v1/models with optional Bearer auth. Returns a list of model
    ID strings. Returns [] on ANY error: network error, non-2xx status, JSON parse
    error, or missing/unexpected keys in the response.
    """
    url = f"{base_url.rstrip('/')}/v1/models"
    headers = {"Authorization": f"Bearer {api_key}"} if api_key else {}
    try:
        with httpx.Client(timeout=10.0, trust_env=False) as c:
            resp = c.get(url, headers=headers)
            resp.raise_for_status()
            data = resp.json()
            return [item["id"] for item in data["data"]]
    except Exception:
        return []


def fetch_ollama_models(ollama_url: str) -> list[str]:
    """Fetch installed model names from a local Ollama server's /api/tags endpoint.

    GETs {ollama_url}/api/tags. Returns a list of model name strings. Returns []
    on ANY error: network error, timeout, non-2xx status, JSON parse error, or
    missing/unexpected keys in the response — Ollama being unreachable must
    degrade gracefully and never break /info.
    """
    url = f"{ollama_url.rstrip('/')}/api/tags"
    try:
        # trust_env=False: never route local model traffic through an ambient
        # HTTP(S)/SOCKS proxy picked up from the environment — keep it direct/local.
        with httpx.Client(timeout=10.0, trust_env=False) as c:
            resp = c.get(url)
            resp.raise_for_status()
            data = resp.json()
            return [m["name"] for m in data["models"]]
    except Exception:
        return []


def generate(req: GenerateRequest, settings: Settings) -> GenerateResponse:
    """Generate the summary one section at a time. A single focused prompt per
    section is far more reliable for small local models than asking for a whole
    multi-section JSON array at once (which truncates or drops sections)."""
    cfg = req.config
    model = cfg.model or settings.default_model
    temperature = cfg.temperature
    base_url = cfg.base_url  # set => opt-in cloud egress
    ollama_url = cfg.ollama_url or settings.default_ollama_url
    api_key = cfg.api_key

    sections: list[SummarySection] = []
    used_model = model
    for tsec in req.template.sections:
        prompt = build_section_prompt(req, tsec.heading, tsec.instruction)
        if base_url:
            raw, used_model = _call_openai_compatible(base_url, api_key, model, prompt, temperature)
        else:
            raw, used_model = _call_ollama(ollama_url, model, prompt, temperature)
        sections.append(_parse_section_json(raw, tsec.heading))

    return GenerateResponse(summary=Summary(sections=sections), model=used_model)


def _call_ollama(ollama_url: str, model: str, prompt: str, temperature: float):
    payload = {
        "model": model,
        "prompt": prompt,
        "stream": False,
        # Structured-output JSON schema (not just "json"): constrains the shape so
        # the prompt needs no copyable example. Ollama fills it with the model's own
        # generated content.
        "format": _SECTION_FORMAT,
        "options": {"temperature": temperature},
    }
    # trust_env=False: never route local model traffic through an ambient
    # HTTP(S)/SOCKS proxy picked up from the environment — keep it direct/local.
    with httpx.Client(timeout=300.0, trust_env=False) as c:
        resp = c.post(f"{ollama_url.rstrip('/')}/api/generate", json=payload)
        resp.raise_for_status()
        data = resp.json()
    return data["response"], data.get("model", model)


def _call_openai_compatible(base_url: str, api_key: str, model: str, prompt: str, temperature: float):
    # Two-path structured-output strategy:
    #   1. First attempt: json_schema mode with full schema enforcement.
    #      Supported by OpenAI, vLLM, LM Studio, and other modern endpoints.
    #   2. Graceful fallback: if the endpoint returns HTTP 400/422 and mentions
    #      "response_format" in the error body (indicating it does not understand
    #      the json_schema type), retry with the simpler json_object mode.
    url = f"{base_url.rstrip('/')}/chat/completions"
    headers = {"Authorization": f"Bearer {api_key}"} if api_key else {}

    # trust_env=False: the caller's base_url is the explicit egress target; don't
    # silently tunnel it through an ambient proxy from the environment.
    with httpx.Client(timeout=300.0, trust_env=False) as c:
        # --- Path 1: json_schema (full schema enforcement) ---
        payload = {
            "model": model,
            "messages": [{"role": "user", "content": prompt}],
            "temperature": temperature,
            "response_format": {
                "type": "json_schema",
                "json_schema": {
                    "name": "section",
                    "schema": _SECTION_FORMAT,
                    "strict": True,
                },
            },
        }
        resp = c.post(url, json=payload, headers=headers)

        # --- Path 2: json_object fallback ---
        # If the endpoint rejects json_schema (400/422) and explicitly mentions
        # "response_format" in the error body, it does not support that mode.
        # Retry with the widely-supported json_object mode instead.
        if resp.status_code in (400, 422):
            try:
                err_body = resp.text
            except Exception:
                err_body = ""
            if "response_format" in err_body:
                payload["response_format"] = {"type": "json_object"}
                resp = c.post(url, json=payload, headers=headers)

        resp.raise_for_status()
        data = resp.json()
    return data["choices"][0]["message"]["content"], data.get("model", model)
