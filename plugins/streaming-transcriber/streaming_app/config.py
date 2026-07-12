from __future__ import annotations

import os
from dataclasses import dataclass


@dataclass(frozen=True)
class Settings:
    """Runtime settings loaded from the environment."""

    auth_token: str
    default_model: str = "tiny.en"
    device: str = "cpu"
    compute_type: str = "int8"
    vad_aggressiveness: int = 2
    silence_threshold_ms: int = 600
    min_speech_ms: int = 120

    @classmethod
    def from_env(cls) -> "Settings":
        return cls(
            auth_token=os.environ.get("MUESLI_PLUGIN_TOKEN", ""),
            default_model=os.environ.get("STREAMING_TRANSCRIBER_MODEL", "tiny.en"),
            device=os.environ.get("STREAMING_TRANSCRIBER_DEVICE", "cpu"),
            compute_type=os.environ.get("STREAMING_TRANSCRIBER_COMPUTE_TYPE", "int8"),
            vad_aggressiveness=int(os.environ.get("STREAMING_TRANSCRIBER_VAD_AGGRESSIVENESS", "2")),
            silence_threshold_ms=int(os.environ.get("STREAMING_TRANSCRIBER_SILENCE_THRESHOLD_MS", "600")),
            min_speech_ms=int(os.environ.get("STREAMING_TRANSCRIBER_MIN_SPEECH_MS", "120")),
        )
