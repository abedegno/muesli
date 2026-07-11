from dataclasses import dataclass, field
from typing import Optional

import httpx
from jsonschema import ValidationError, validate

from . import schemas


@dataclass
class Result:
    name: str
    passed: bool
    detail: str = ""


@dataclass
class Report:
    results: list[Result] = field(default_factory=list)

    @property
    def ok(self) -> bool:
        return all(r.passed for r in self.results)

    def add(self, name: str, passed: bool, detail: str = "") -> None:
        self.results.append(Result(name, passed, detail))

    def summary(self) -> str:
        lines = [f"[{'PASS' if r.passed else 'FAIL'}] {r.name}"
                 + (f" — {r.detail}" if r.detail else "")
                 for r in self.results]
        return "\n".join(lines)


def _auth(token: str) -> dict:
    return {"Authorization": f"Bearer {token}", "X-Muesli-Plugin-API": "1"}


def check_info(client: httpx.Client, token: str, kind: str, report: Report) -> Optional[dict]:
    try:
        r = client.get("/info", headers=_auth(token))
    except Exception as e:  # noqa: BLE001
        report.add("info.request", False, str(e))
        return None
    if r.status_code != 200:
        report.add("info.status", False, f"got {r.status_code}")
        return None
    body = r.json()
    try:
        validate(body, schemas.INFO_SCHEMA)
    except ValidationError as e:
        report.add("info.schema", False, e.message)
        return None
    if body["kind"] != kind:
        report.add("info.kind", False, f"info.kind={body['kind']} but --kind {kind}")
        return body
    report.add("info.shape", True)
    return body


def check_health(client: httpx.Client, report: Report) -> None:
    # /health MUST be reachable WITHOUT auth: scale-to-zero / k8s readiness probes
    # send no auth headers. We deliberately send no token here and require 200.
    try:
        r = client.get("/health")
    except Exception as e:  # noqa: BLE001
        report.add("health.request", False, str(e))
        return
    report.add(
        "health.unauthenticated",
        r.status_code == 200,
        f"got {r.status_code} (health must be 200 without auth)",
    )


def check_auth_enforced(client: httpx.Client, kind: str, report: Report) -> None:
    path = "/info"
    # No token at all must be rejected.
    no_token = client.get(path, headers={"X-Muesli-Plugin-API": "1"})
    wrong_token = client.get(
        path, headers={"Authorization": "Bearer definitely-wrong", "X-Muesli-Plugin-API": "1"}
    )
    ok = no_token.status_code == 401 and wrong_token.status_code == 401
    report.add(
        "auth.enforced",
        ok,
        f"no-token={no_token.status_code} wrong-token={wrong_token.status_code} (want 401/401)",
    )


def check_auth_on_work_endpoint(
    client: httpx.Client, token: str, kind: str, report: Report
) -> None:
    """Auth must be enforced on the work endpoint (/transcribe or /generate), not just /info."""
    path = "/transcribe" if kind == "transcriber" else "/generate"
    # Use an empty body — auth must be checked before body parsing.
    no_token = client.post(
        path, json={}, headers={"X-Muesli-Plugin-API": "1"}
    )
    wrong_token = client.post(
        path,
        json={},
        headers={"Authorization": "Bearer definitely-wrong", "X-Muesli-Plugin-API": "1"},
    )
    ok = no_token.status_code == 401 and wrong_token.status_code == 401
    report.add(
        "auth.work_endpoint",
        ok,
        f"no-token={no_token.status_code} wrong-token={wrong_token.status_code} (want 401/401) on {path}",
    )


def check_empty_transcript(client: httpx.Client, token: str, report: Report) -> None:
    """POST /generate with transcript:[] must return 200 + valid schema.

    The Go client coerces nil transcript slices to [] before calling /generate,
    so the plugin must accept an empty array without error.
    """
    payload = {
        "transcript": [],
        "notes_markdown": "- ship date?",
        "template": {"sections": [{"heading": "Overview", "instruction": "Summarise."}]},
        "config": {},
    }
    try:
        r = client.post("/generate", json=payload, headers=_auth(token))
    except Exception as e:  # noqa: BLE001
        report.add("generate.empty_transcript", False, str(e))
        return
    if r.status_code != 200:
        report.add(
            "generate.empty_transcript",
            False,
            f"/generate with transcript:[] got {r.status_code} (want 200)",
        )
        return
    try:
        validate(r.json(), schemas.GENERATE_RESPONSE_SCHEMA)
    except ValidationError as e:
        report.add("generate.empty_transcript", False, e.message)
        return
    report.add("generate.empty_transcript", True)


def check_malformed_payload(
    client: httpx.Client, token: str, kind: str, report: Report
) -> None:
    """Sending {} (all required fields missing) must be rejected with 4xx, not 200/500."""
    path = "/transcribe" if kind == "transcriber" else "/generate"
    try:
        r = client.post(path, json={}, headers=_auth(token))
    except Exception as e:  # noqa: BLE001
        report.add("payload.malformed_rejected", False, str(e))
        return
    ok = 400 <= r.status_code <= 499
    report.add(
        "payload.malformed_rejected",
        ok,
        f"{path} with {{}} got {r.status_code} (want 4xx)",
    )


def canonical_transcribe_payload() -> dict:
    # A tiny silent WAV (RIFF header + 0 frames) base64 data URL the plugin can fetch.
    return {
        "audio_url": "data:audio/wav;base64,UklGRiQAAABXQVZFZm10IBAAAAABAAEAQB8AAIA+AAACABAAZGF0YQAAAAA=",
        "language_hint": "en",
        "config": {},
    }


def canonical_generate_payload() -> dict:
    return {
        "transcript": [{"start_ms": 0, "end_ms": 1000, "text": "We ship Friday.", "source": "mixed"}],
        "notes_markdown": "- ship date?",
        "template": {"sections": [{"heading": "Overview", "instruction": "Summarise."}]},
        "config": {},
    }


def check_roundtrip(client: httpx.Client, token: str, kind: str, report: Report) -> None:
    if kind == "transcriber":
        path, payload, schema = "/transcribe", canonical_transcribe_payload(), schemas.TRANSCRIBE_RESPONSE_SCHEMA
    else:
        path, payload, schema = "/generate", canonical_generate_payload(), schemas.GENERATE_RESPONSE_SCHEMA
    try:
        r = client.post(path, json=payload, headers=_auth(token))
    except Exception as e:  # noqa: BLE001
        report.add("roundtrip.request", False, str(e))
        return
    if r.status_code != 200:
        report.add("roundtrip.status", False, f"{path} got {r.status_code}: {r.text[:200]}")
        return
    try:
        validate(r.json(), schema)
    except ValidationError as e:
        report.add("roundtrip.schema", False, e.message)
        return
    report.add("roundtrip.ok", True)
