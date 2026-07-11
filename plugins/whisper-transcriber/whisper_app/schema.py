from typing import Any, Optional

from pydantic import BaseModel, Field


class WordTimestamp(BaseModel):
    text: str
    start_ms: int
    end_ms: int


class Segment(BaseModel):
    start_ms: int
    end_ms: int
    text: str
    # v1 does not separate mic/system server-side, so every segment is "mixed".
    # When the client uploads separate mic/system tracks (backlog item), the
    # plugin can tag segments per track. See README "Source tagging".
    source: str = "mixed"
    speaker: Optional[str] = None
    words: Optional[list[WordTimestamp]] = None
    confidence: Optional[float] = None


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
    partial: bool = False          # True when some chunks succeeded before a mid-stream error
    partial_reason: Optional[str] = None  # human-readable error that cut transcription short
