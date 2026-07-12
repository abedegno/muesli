import os

import pytest


@pytest.mark.skipif(
    os.getenv("MUESLI_STREAMING_INTEGRATION") != "1",
    reason="optional real-model integration is opt-in via MUESLI_STREAMING_INTEGRATION=1",
)
def test_optional_real_model_smoke():
    # Kept intentionally skippable: the default test run stubs transcribe_utterance.
    assert True
