"""Tests for LLM_BASE_URL and LLM_API_KEY env-var startup defaults in Settings."""
import pytest

from ollama_app.config import Settings


def test_llm_base_url_env(monkeypatch):
    """LLM_BASE_URL is picked up as settings.base_url."""
    monkeypatch.setenv("LLM_BASE_URL", "https://api.example.com")
    config = Settings.from_env()
    assert config.base_url == "https://api.example.com"


def test_llm_api_key_env(monkeypatch):
    """LLM_API_KEY is picked up as settings.api_key."""
    monkeypatch.setenv("LLM_API_KEY", "sk-test")
    config = Settings.from_env()
    assert config.api_key == "sk-test"


def test_defaults_empty(monkeypatch):
    """Without either env var set, both default to empty string."""
    monkeypatch.delenv("LLM_BASE_URL", raising=False)
    monkeypatch.delenv("LLM_API_KEY", raising=False)
    config = Settings.from_env()
    assert config.base_url == ""
    assert config.api_key == ""


def test_ollama_env_vars_unaffected(monkeypatch):
    """OLLAMA_URL and OLLAMA_MODEL still work as before."""
    monkeypatch.setenv("OLLAMA_URL", "http://my-ollama:11434")
    monkeypatch.setenv("OLLAMA_MODEL", "mistral")
    config = Settings.from_env()
    assert config.default_ollama_url == "http://my-ollama:11434"
    assert config.default_model == "mistral"
