import os
from dataclasses import dataclass


@dataclass(frozen=True)
class Settings:
    auth_token: str
    default_ollama_url: str = "http://localhost:11434"
    default_model: str = "llama3.2"
    base_url: str = ""
    api_key: str = ""

    @classmethod
    def from_env(cls) -> "Settings":
        return cls(
            auth_token=os.environ.get("MUESLI_PLUGIN_TOKEN", ""),
            default_ollama_url=os.environ.get("OLLAMA_URL", "http://localhost:11434"),
            default_model=os.environ.get("OLLAMA_MODEL", "llama3.2"),
            base_url=os.environ.get("LLM_BASE_URL", ""),
            api_key=os.environ.get("LLM_API_KEY", ""),
        )
