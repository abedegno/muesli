"""Self-check tests for the determinism gate fixtures.

These tests verify that:
1. frozen_clock makes datetime.now() return the frozen instant.
2. _block_real_http (autouse) intercepts httpx calls — mocked routes work,
   unmocked routes raise a respx-specific error (not a DNS failure).

NOTE: These tests deliberately do NOT use @respx.mock — the autouse
_block_real_http fixture is what provides the mock context.
"""
from datetime import datetime

import httpx
import pytest
import respx


def test_frozen_clock_returns_frozen_instant(frozen_clock):
    """frozen_clock fixture freezes datetime.now() at 2024-01-15 12:00:00."""
    now = datetime.now()  # noqa: TID251
    assert now == frozen_clock
    assert now == datetime(2024, 1, 15, 12, 0, 0)


def test_block_real_http_mocked_route_works():
    """A respx-mocked route succeeds inside the autouse _block_real_http context."""
    respx.get("http://determinism-gate-test.invalid/check").mock(
        return_value=httpx.Response(200, json={"gate": "ok"})
    )
    resp = httpx.get("http://determinism-gate-test.invalid/check")
    assert resp.status_code == 200
    assert resp.json() == {"gate": "ok"}


def test_block_real_http_unmocked_route_raises():
    """An unmocked httpx call is blocked by respx, not by DNS.

    respx raises AllMockedAssertionError (subclass of AssertionError) with a
    message containing "not mocked" — this distinguishes it from a real DNS
    failure and proves _block_real_http is actively intercepting.
    """
    with pytest.raises(Exception) as exc_info:
        httpx.get("http://determinism-gate-test.invalid/no-mock-here")
    assert "not mocked" in str(exc_info.value)
