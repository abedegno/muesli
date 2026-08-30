# Muesli plugin conformance suite

The conformance suite is a Python-based test harness that verifies any Muesli
transcriber, streaming-transcriber, or agent plugin correctly implements the
[published Muesli plugin contract](../../docs/ARCHITECTURE.md#the-plugin-contract).
Run it before registering a plugin with a Muesli server — or as part of your
own plugin's CI pipeline.

## Contents

1. [What the conformance suite checks](#what-the-conformance-suite-checks)
2. [Prerequisites](#prerequisites)
3. [Running against a custom plugin](#running-against-a-custom-plugin)
4. [Interpreting results](#interpreting-results)
5. [Adding new conformance tests](#adding-new-conformance-tests)

---

## What the conformance suite checks

The suite exercises every normative requirement in the contract:

| Area                          | What is checked                                                                                                                                                                                                                                                                                      |
| ----------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Endpoints**                 | `GET /info` returns `{name, version, plugin_api: 1, kind, config_schema}` and `kind` matches `--kind`. `GET /health` returns 200. The work endpoint is `POST /transcribe` (transcriber), `POST /generate` (agent), or WebSocket `/stream` (streaming-transcriber).                                   |
| **Authentication**            | Requests with no bearer token **and** requests with a wrong bearer token are rejected on `GET /info` and the work endpoint. WebSocket authentication failures reject the handshake; HTTP authentication failures return 401. The `X-Muesli-Plugin-API: 1` header is required on authenticated calls. |
| **Response JSON schema**      | Each HTTP response and streaming `ready`, `segment`, or `error` message is validated against the contract JSON Schema (`schemas.py`) — field names, types, and required keys are checked.                                                                                                            |
| **Streaming protocol**        | A valid `start` receives `{"type":"ready"}`; malformed first messages produce an error or close; `stop` completes and closes the stream. Any emitted segment is schema-validated, but silence is allowed to emit none.                                                                               |
| **Lifecycle / health probes** | `GET /health` must return 200 **without** any `Authorization` header. Scale-to-zero infrastructure and Kubernetes readiness probes send no credentials; gating health on auth will cause the plugin to be killed or never start.                                                                     |

---

## Prerequisites

- **Python 3.12+** (`requires-python = ">=3.12"` per `pyproject.toml`).
- **A running plugin** reachable over HTTP (the CLI runs against a live URL, not
  in-process).

### Install the conformance runner

```bash
cd plugins/conformance
pip install -e .
```

### Also run the pytest self-certification suite

The test suite imports the reference Whisper and streaming transcriber plugins
in-process and self-certifies them. To run those tests you need the extra test
dependencies and the corresponding optional reference plugins:

```bash
pip install -e '.[test]'
pip install -e ../whisper-transcriber   # optional — only needed for reference-plugin tests
pip install -e ../streaming-transcriber # optional — only needed for reference-plugin tests
pytest
```

No special environment variables are required for the CLI. The plugin URL and
bearer token are passed as arguments (see the next section).

---

## Running against a custom plugin

Start your plugin so that it is listening on a known URL, then run:

```bash
cd plugins/conformance
pip install -e .

# Validate a transcriber plugin
python -m muesli_plugin_conformance http://localhost:8000 --kind transcriber --token <your-token>

# Validate an agent plugin
python -m muesli_plugin_conformance http://localhost:8001 --kind agent --token <your-token>

# Validate a streaming transcriber plugin
python -m muesli_plugin_conformance http://localhost:8002 --kind streaming-transcriber --token <your-token>
```

### Arguments

| Argument           | Description                                                                                                                             |
| ------------------ | --------------------------------------------------------------------------------------------------------------------------------------- |
| `url` (positional) | Base URL of the running plugin, e.g. `http://localhost:8000`. Trailing slashes are stripped automatically.                              |
| `--kind`           | `transcriber`, `streaming-transcriber`, or `agent`. Must match the `kind` field the plugin returns from `GET /info`.                    |
| `--token`          | The per-plugin bearer token the server was configured to accept. Sent as `Authorization: Bearer <token>` plus `X-Muesli-Plugin-API: 1`. |
| `--timeout`        | Request timeout in seconds. Defaults to **300**. Increase this if your plugin performs a heavy warm-up on its first work request.       |

---

## Interpreting results

Each conformance check prints one line:

```
[PASS] check.name
[FAIL] check.name — detail explaining what went wrong
```

The suite ends with a summary line and exits with the appropriate code:

| Summary          | Exit code | Meaning                                             |
| ---------------- | --------- | --------------------------------------------------- |
| `CONFORMANT`     | `0`       | All checks passed. The plugin honours the contract. |
| `NON-CONFORMANT` | `1`       | One or more checks failed.                          |

### Example output — fully conformant transcriber

```
[PASS] info.shape
[PASS] health.unauthenticated
[PASS] auth.enforced
[PASS] roundtrip.ok

CONFORMANT
```

### Example output — plugin that fails authentication enforcement

```
[PASS] info.shape
[PASS] health.unauthenticated
[FAIL] auth.enforced — no-token=200 wrong-token=200 (want 401/401)
[FAIL] roundtrip.status — /transcribe got 500: Internal Server Error

NON-CONFORMANT
```

### What a failing check means

A `[FAIL]` means the plugin does not honour that part of the published contract
and **will not be accepted by the Muesli server**. Common failures:

- **`[FAIL] auth.enforced`** — the plugin accepts requests without
  authentication. Fix the auth middleware before registering the plugin; any
  unauthenticated caller could submit arbitrary work to it.
- **`[FAIL] health.unauthenticated`** — the plugin requires auth on `GET
/health`. Remove auth from that route so that scale-to-zero and readiness
  probes work correctly.
- **`[FAIL] info.schema`** — `GET /info` returns a body that does not match the
  schema (e.g., missing `config_schema` key or wrong `plugin_api` value).
- **`[FAIL] roundtrip.schema`** — the work endpoint returns a body with missing
  or wrongly-typed fields.

---

## Adding new conformance tests

### Source layout

```
plugins/conformance/
├── muesli_plugin_conformance/
│   ├── checks.py      # individual check functions
│   ├── runner.py      # run_conformance() — orchestrates the checks
│   ├── schemas.py     # JSON Schema definitions for /info and work-endpoint responses
│   └── __main__.py    # CLI entry point
└── tests/
    ├── test_checks_transcriber.py  # checks for transcriber plugins
    ├── test_checks_streaming.py    # checks for streaming-transcriber plugins
    ├── test_checks_agent.py        # checks for agent plugins
    └── test_failure_modes.py       # error-handling and edge-case scenarios
```

### Writing a new check

1. **Add the check function** to `muesli_plugin_conformance/checks.py`.
   Each check receives an `httpx.Client`, the relevant arguments, and a
   `Report`, and calls `report.add(name, passed, detail)` to record its
   verdict:

   ```python
   def check_content_type(
       client: httpx.Client, token: str, kind: str, report: Report
   ) -> None:
       r = client.get("/info", headers=_auth(token))
       ok = "application/json" in r.headers.get("content-type", "")
       report.add(
           "info.content_type",
           ok,
           f"content-type={r.headers.get('content-type', '(missing)')}",
       )
   ```

2. **Register the check** by calling it from `run_conformance()` in
   `muesli_plugin_conformance/runner.py`, in the order you want it to run:

   ```python
   from .checks import check_content_type  # add to existing imports

   def run_conformance(client: httpx.Client, kind: str, token: str) -> Report:
       ...
       check_content_type(client, token, kind, report)
       return report
   ```

3. **Write pytest tests** that exercise the new check. Pick the right file:

   | File                               | Use for                                             |
   | ---------------------------------- | --------------------------------------------------- |
   | `tests/test_checks_transcriber.py` | Checks specific to transcriber plugins              |
   | `tests/test_checks_agent.py`       | Checks specific to agent plugins                    |
   | `tests/test_failure_modes.py`      | Error-handling and edge-case scenarios for any kind |

   Use `respx` to mock HTTP responses, or the FastAPI in-process ASGI transport
   (`httpx.AsyncClient(app=app, base_url="http://test")`) to test against a real
   plugin app:

   ```python
   # tests/test_checks_transcriber.py
   import respx
   import httpx
   from muesli_plugin_conformance.checks import check_content_type, Report

   @respx.mock
   def test_content_type_missing():
       respx.get("http://plugin/info").respond(200, json={...}, headers={})
       with httpx.Client(base_url="http://plugin") as client:
           report = Report()
           check_content_type(client, token="tok", kind="transcriber", report=report)
       assert not report.ok
       assert "content-type" in report.results[0].detail
   ```

   Name test functions `test_<check_name>_<scenario>()` for easy discovery, e.g.
   `test_auth_enforced_no_token()`, `test_auth_enforced_wrong_token()`,
   `test_roundtrip_schema_missing_field()`.
