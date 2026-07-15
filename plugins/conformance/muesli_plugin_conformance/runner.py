import httpx

from .checks import (
    Report,
    check_auth_enforced,
    check_auth_on_work_endpoint,
    check_empty_transcript,
    check_generate_system_prompt_override,
    check_health,
    check_info,
    check_malformed_payload,
    check_roundtrip,
)


def run_conformance(client: httpx.Client, kind: str, token: str) -> Report:
    """Run the full conformance suite against a plugin reachable via `client`.

    `client` is any httpx.Client whose base_url points at the plugin (a live URL
    in the CLI, or an in-process ASGI transport in tests)."""
    if kind not in ("transcriber", "agent"):
        raise ValueError(f"kind must be 'transcriber' or 'agent', got {kind!r}")
    report = Report()
    check_info(client, token, kind, report)
    check_health(client, report)  # health is unauthenticated by contract
    check_auth_enforced(client, kind, report)
    check_auth_on_work_endpoint(client, token, kind, report)
    check_roundtrip(client, token, kind, report)
    check_malformed_payload(client, token, kind, report)
    if kind == "agent":
        check_empty_transcript(client, token, report)
        check_generate_system_prompt_override(client, token, report)
    return report
