from typing import Any, Optional

from pydantic import BaseModel, Field


class Segment(BaseModel):
    start_ms: int
    end_ms: int
    text: str
    source: str = "mixed"
    speaker: Optional[str] = None


class TranscribeRequest(BaseModel):
    audio_url: str
    language_hint: Optional[str] = None
    options: dict[str, Any] = Field(default_factory=dict)
    config: dict[str, Any] = Field(default_factory=dict)


class TranscribeResponse(BaseModel):
    segments: list[Segment]
    language: str
    model: str
    duration_ms: int
