import pytest
from pydantic import ValidationError

from ollama_app.schema import PluginConfig


def test_valid_temperature_accepted():
    """A temperature of 1.0 (within [0, 2]) must not raise."""
    cfg = PluginConfig(temperature=1.0)
    assert cfg.temperature == 1.0


def test_temperature_below_zero_rejected():
    """A temperature of -0.1 (below ge=0) must raise ValidationError."""
    with pytest.raises(ValidationError):
        PluginConfig(temperature=-0.1)


def test_temperature_above_two_rejected():
    """A temperature of 2.1 (above le=2) must raise ValidationError."""
    with pytest.raises(ValidationError):
        PluginConfig(temperature=2.1)
