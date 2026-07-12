from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, Field, field_validator


class StreamStartRequest(BaseModel):
    type: Literal["start"] = "start"
    language_hint: str | None = None
    options: dict[str, Any] = Field(default_factory=dict)
    config: dict[str, Any] = Field(default_factory=dict)
    sample_rate: int = 16000
    channels: int = 1

    @field_validator("sample_rate")
    @classmethod
    def _validate_sample_rate(cls, value: int) -> int:
        if value != 16000:
            raise ValueError("sample_rate must be 16000")
        return value

    @field_validator("channels")
    @classmethod
    def _validate_channels(cls, value: int) -> int:
        if value != 1:
            raise ValueError("channels must be 1")
        return value


class StreamStopRequest(BaseModel):
    type: Literal["stop"] = "stop"


class StreamReadyResponse(BaseModel):
    type: Literal["ready"] = "ready"


class StreamErrorResponse(BaseModel):
    type: Literal["error"] = "error"
    message: str


class StreamSegmentResponse(BaseModel):
    type: Literal["segment"] = "segment"
    final: bool = True
    text: str
    t0: float
    t1: float
    speaker: str | None = None
