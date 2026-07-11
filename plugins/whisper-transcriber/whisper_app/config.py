import os
from dataclasses import dataclass


@dataclass(frozen=True)
class Settings:
    """Runtime settings. auth_token is the per-plugin shared secret the Muesli
    server sends as `Authorization: Bearer <token>`."""

    auth_token: str
    default_model: str = "base"
    device: str = "cpu"          # set to "cuda" for GPU; see README
    compute_type: str = "int8"   # Fallback when the request payload omits compute_type; "float16" is a good GPU default

    # Optional speaker-diarization stage (requires pyannote.audio[diarization]).
    # Set WHISPER_DIARIZATION_ENABLED=1 or =true to enable.
    diarization_enabled: bool = False
    # Hugging Face token with access to the pyannote diarization model.
    diarization_hf_token: str = ""
    # pyannote pipeline model name / HF repo id.
    diarization_model: str = "pyannote/speaker-diarization-3.1"

    # Word-level timestamps (requires faster-whisper with word_timestamps support).
    word_timestamps_enabled: bool = False

    @classmethod
    def from_env(cls) -> "Settings":
        raw_enabled = os.environ.get("WHISPER_DIARIZATION_ENABLED", "").strip().lower()
        diarization_enabled = raw_enabled in ("1", "true")
        raw_words = os.environ.get("WHISPER_WORD_TIMESTAMPS", "").strip().lower()
        word_timestamps_enabled = raw_words in ("1", "true")
        return cls(
            auth_token=os.environ.get("MUESLI_PLUGIN_TOKEN", ""),
            default_model=os.environ.get("WHISPER_MODEL", "base"),
            device=os.environ.get("WHISPER_DEVICE", "cpu"),
            compute_type=os.environ.get("WHISPER_COMPUTE_TYPE", "int8"),
            diarization_enabled=diarization_enabled,
            diarization_hf_token=os.environ.get("WHISPER_DIARIZATION_HF_TOKEN", ""),
            diarization_model=os.environ.get(
                "WHISPER_DIARIZATION_MODEL", "pyannote/speaker-diarization-3.1"
            ),
            word_timestamps_enabled=word_timestamps_enabled,
        )
