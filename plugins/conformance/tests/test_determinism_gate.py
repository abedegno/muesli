"""Self-check tests for the determinism gate fixtures.

Conformance tests use starlette.TestClient (ASGI transport, no real HTTP),
so only frozen_clock is tested here.
"""
from datetime import datetime


def test_frozen_clock_returns_frozen_instant(frozen_clock):
    """frozen_clock fixture freezes datetime.now() at 2024-01-15 12:00:00."""
    now = datetime.now()  # noqa: TID251
    assert now == frozen_clock
    assert now == datetime(2024, 1, 15, 12, 0, 0)
