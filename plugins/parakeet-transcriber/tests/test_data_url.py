import pytest

from parakeet_app.transcribe import _decode_data_url


def test_percent_encoded_data_url():
    assert _decode_data_url("data:text/plain,hello%20world") == b"hello world"


def test_malformed_base64_raises():
    with pytest.raises(ValueError):
        _decode_data_url("data:audio/wav;base64,@@@not-base64@@@")


def test_missing_comma_raises():
    with pytest.raises(ValueError):
        _decode_data_url("data:audio/wav;base64NOCOMMA")


def test_non_data_url_raises():
    with pytest.raises(ValueError):
        _decode_data_url("http://example.test/a.wav")
