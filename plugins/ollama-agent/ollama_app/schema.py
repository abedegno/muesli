from typing import Any, Optional

from pydantic import BaseModel, Field, field_validator


class Segment(BaseModel):
    start_ms: int
    end_ms: int
    text: str
    source: str = "mixed"
    speaker: Optional[str] = None


class TemplateSection(BaseModel):
    heading: str
    instruction: str


class Template(BaseModel):
    sections: list[TemplateSection]


class PluginConfig(BaseModel):
    model: str = Field(default="")
    ollama_url: str = Field(default="")
    base_url: str = Field(default="")
    api_key: str = Field(default="")
    temperature: float = Field(default=0.2, ge=0, le=2)


class GenerateRequest(BaseModel):
    # Tolerate empty/missing/null transcript (e.g. silent or very short audio):
    # summarise from notes alone rather than rejecting the request with 422.
    transcript: list[Segment] = Field(default_factory=list)
    notes_markdown: str = ""
    template: Template
    options: dict[str, Any] = Field(default_factory=dict)
    config: PluginConfig = Field(default_factory=PluginConfig)

    @field_validator("transcript", mode="before")
    @classmethod
    def _coerce_null_transcript(cls, v: Any) -> Any:
        # A nil Go slice marshals to JSON null; treat it as an empty list.
        return [] if v is None else v


class SummarySection(BaseModel):
    heading: str
    content_markdown: str
    refs: Optional[list[int]] = None


class Summary(BaseModel):
    sections: list[SummarySection]


class GenerateResponse(BaseModel):
    summary: Summary
    model: str
