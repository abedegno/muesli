import os
from dataclasses import dataclass


@dataclass(frozen=True)
class Settings:
    """Runtime settings loaded from the environment."""

    auth_token: str
    default_model: str = "nvidia/parakeet-tdt-1.1b"
    device: str = "cpu"

    @classmethod
    def from_env(cls) -> "Settings":
        return cls(
            auth_token=os.environ.get("MUESLI_PLUGIN_TOKEN", ""),
            default_model=os.environ.get("PARAKEET_MODEL", "nvidia/parakeet-tdt-1.1b"),
            device=os.environ.get("PARAKEET_DEVICE", "cpu"),
        )
