from datetime import datetime

import pytest
from freezegun import freeze_time


@pytest.fixture
def frozen_clock():
    """Freeze time at a deterministic instant; use instead of datetime.now()."""
    with freeze_time("2024-01-15 12:00:00"):
        yield datetime(2024, 1, 15, 12, 0, 0)
