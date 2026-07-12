import array
import io
import os
import wave

import pytest

from streaming_app.config import Settings
from streaming_app.transcribe import transcribe_utterance


@pytest.mark.skipif(
    os.getenv("MUESLI_STREAMING_INTEGRATION") != "1",
    reason="optional real-model integration is opt-in via MUESLI_STREAMING_INTEGRATION=1",
)
def test_optional_real_model_smoke():
    settings = Settings()
    sample_rate = 16000
    duration_s = 2
    pcm = array.array("h", [0] * (sample_rate * duration_s))

    wav_bytes = io.BytesIO()
    with wave.open(wav_bytes, "wb") as wav_file:
        wav_file.setnchannels(1)
        wav_file.setsampwidth(2)
        wav_file.setframerate(sample_rate)
        wav_file.writeframes(pcm.tobytes())

    wav_bytes.seek(0)
    with wave.open(wav_bytes, "rb") as wav_file:
        raw_pcm = wav_file.readframes(wav_file.getnframes())

    result = transcribe_utterance(raw_pcm, sample_rate, settings)
    assert isinstance(result, str)
    assert result is not None
