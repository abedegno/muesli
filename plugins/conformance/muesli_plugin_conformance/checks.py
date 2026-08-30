import json
from contextlib import contextmanager
from dataclasses import dataclass, field
from typing import Iterator, Optional
from urllib.parse import urljoin, urlparse, urlunparse

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


def _http_work_endpoint(kind: str) -> str:
    endpoints = {"transcriber": "/transcribe", "agent": "/generate"}
    try:
        return endpoints[kind]
    except KeyError as e:
        raise ValueError(f"{kind!r} does not have an HTTP work endpoint") from e


def _is_websocket_close(error: Exception) -> bool:
    return error.__class__.__name__ in {"WebSocketDisconnect", "ConnectionClosedOK", "ConnectionClosedError"}


class _NetworkWebSocket:
    def __init__(self, socket, receive_timeout: float | None) -> None:
        self._socket = socket
        self._receive_timeout = receive_timeout

    def send_json(self, message: dict) -> None:
        self._socket.send(json.dumps(message))

    def send_bytes(self, data: bytes) -> None:
        self._socket.send(data)

    def receive_json(self) -> dict:
        message = self._socket.recv(timeout=self._receive_timeout)
        if isinstance(message, bytes):
            message = message.decode()
        return json.loads(message)


@contextmanager
def _websocket_connect(client: httpx.Client, path: str, headers: dict) -> Iterator[object]:
    """Open a stream using Starlette TestClient or a live websocket connection."""
    if hasattr(client, "websocket_connect"):
        with client.websocket_connect(path, headers=headers) as socket:
            yield socket
        return

    from websockets.sync.client import connect

    base = urlparse(str(client.base_url))
    scheme = "wss" if base.scheme == "https" else "ws"
    websocket_url = urljoin(urlunparse(base._replace(scheme=scheme)), path.lstrip("/"))
    with connect(websocket_url, additional_headers=headers, open_timeout=client.timeout.connect) as socket:
        yield _NetworkWebSocket(socket, client.timeout.read)


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
    path = _http_work_endpoint(kind)
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
    path = _http_work_endpoint(kind)
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


def check_generate_system_prompt_override(client: httpx.Client, token: str, report: Report) -> None:
    """POST /generate with the optional system_prompt/model/temperature agent
    overrides set must still return 200 + a schema-valid response.

    These are OPTIONAL per-template overrides threaded from the Muesli server
    (see internal/pluginkit.GenerateRequest / internal/plugin.GenerateRequest).
    A plugin that does not support them must tolerate the extra fields
    (ignore them) rather than reject the request; a plugin that DOES support
    them must still conform to the same response schema.
    """
    payload = canonical_generate_payload()
    payload["system_prompt"] = "You are a terse bullet-point summarizer."
    payload["model"] = "llama3.2:3b"
    payload["temperature"] = 0.1
    try:
        r = client.post("/generate", json=payload, headers=_auth(token))
    except Exception as e:  # noqa: BLE001
        report.add("generate.system_prompt_override", False, str(e))
        return
    if r.status_code != 200:
        report.add(
            "generate.system_prompt_override",
            False,
            f"/generate with system_prompt/model/temperature set got {r.status_code} (want 200)",
        )
        return
    try:
        validate(r.json(), schemas.GENERATE_RESPONSE_SCHEMA)
    except ValidationError as e:
        report.add("generate.system_prompt_override", False, e.message)
        return
    report.add("generate.system_prompt_override", True)


def check_roundtrip(client: httpx.Client, token: str, kind: str, report: Report) -> None:
    if kind == "transcriber":
        path, payload, schema = "/transcribe", canonical_transcribe_payload(), schemas.TRANSCRIBE_RESPONSE_SCHEMA
    elif kind == "agent":
        path, payload, schema = "/generate", canonical_generate_payload(), schemas.GENERATE_RESPONSE_SCHEMA
    else:
        raise ValueError(f"{kind!r} does not have an HTTP roundtrip")
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


def check_streaming_auth(client: httpx.Client, token: str, report: Report) -> None:
    attempts = [
        ("no-token", {}),
        ("wrong-token", _auth("definitely-wrong")),
    ]
    outcomes = []
    for label, headers in attempts:
        rejected = False
        try:
            with _websocket_connect(client, "/stream", headers) as socket:
                socket.send_json({"type": "start"})
                socket.receive_json()
        except Exception:  # noqa: BLE001 - rejection differs by websocket transport
            rejected = True
        outcomes.append((label, rejected))
    ok = all(rejected for _, rejected in outcomes)
    detail = " ".join(f"{label}={'rejected' if rejected else 'accepted'}" for label, rejected in outcomes)
    report.add("auth.streaming_work_endpoint", ok, detail)


def check_streaming_malformed_payload(client: httpx.Client, token: str, report: Report) -> None:
    closed_after_send = False
    message = None
    try:
        with _websocket_connect(client, "/stream", _auth(token)) as socket:
            socket.send_json({"type": "start", "sample_rate": 8000})
            try:
                message = socket.receive_json()
            except Exception as e:  # noqa: BLE001 - transports use different close exceptions
                if not _is_websocket_close(e):
                    raise
                closed_after_send = True
    except Exception as e:  # noqa: BLE001
        if closed_after_send:
            report.add("payload.streaming_malformed_rejected", True)
            return
        if message is None:
            report.add("payload.streaming_malformed_rejected", False, f"valid-auth connect failed: {e}")
            return

    if closed_after_send:
        report.add("payload.streaming_malformed_rejected", True)
        return
    assert message is not None
    if message.get("type") == "error":
        try:
            validate(message, schemas.STREAM_ERROR_RESPONSE_SCHEMA)
        except ValidationError as e:
            report.add("payload.streaming_malformed_rejected", False, e.message)
            return
        report.add("payload.streaming_malformed_rejected", True)
        return
    report.add(
        "payload.streaming_malformed_rejected",
        False,
        f"invalid start produced {message!r} (want error or close)",
    )


def check_streaming_roundtrip(client: httpx.Client, token: str, report: Report) -> None:
    try:
        with _websocket_connect(client, "/stream", _auth(token)) as socket:
            socket.send_json({"type": "start"})
            ready = socket.receive_json()
            validate(ready, schemas.STREAM_READY_RESPONSE_SCHEMA)
            socket.send_json({"type": "stop"})
            while True:
                try:
                    message = socket.receive_json()
                except Exception as e:  # noqa: BLE001 - transports use different close exceptions
                    if not _is_websocket_close(e):
                        raise
                    break
                if message.get("type") == "segment":
                    validate(message, schemas.STREAM_SEGMENT_RESPONSE_SCHEMA)
                elif message.get("type") == "error":
                    validate(message, schemas.STREAM_ERROR_RESPONSE_SCHEMA)
                    report.add("roundtrip.streaming", False, message["message"])
                    return
                else:
                    report.add("roundtrip.streaming", False, f"unexpected message: {message!r}")
                    return
    except (ValidationError, ValueError, TypeError, KeyError) as e:
        report.add("roundtrip.streaming", False, str(e))
        return
    except Exception as e:  # noqa: BLE001
        report.add("roundtrip.streaming", False, f"stream failed: {e}")
        return
    report.add("roundtrip.streaming", True)
