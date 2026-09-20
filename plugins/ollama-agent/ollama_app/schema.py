from typing import Any, Optional

from pydantic import BaseModel, Field, field_validator, model_validator


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
    # Extra keys (e.g. the admission-only context_tokens/output_reserve_tokens/
    # provider_framing_tokens/byte_fallback_tokenizer fields published in /info
    # and stored alongside the connection settings -- see main.py's /info and
    # the accepted plan's Task 6) are silently ignored here (pydantic's default
    # "ignore" extra-field policy): they must never reach an upstream Ollama/
    # OpenAI-compatible request, and PluginConfig simply never looks at them.


class Document(BaseModel):
    """One ordered input document (one selected meeting) for a cross-meeting
    analysis request (issue #765) -- mirrors internal/pluginkit.Document /
    internal/plugin.Document."""

    note_id: str
    title: str = ""
    occurred_at: str = ""
    transcript_generation: int = 0
    segments: list[Segment] = Field(default_factory=list)
    notes_markdown: str = ""


class GenerateRequest(BaseModel):
    # Tolerate empty/missing/null transcript (e.g. silent or very short audio):
    # summarise from notes alone rather than rejecting the request with 422.
    # Transcript and documents are mutually exclusive -- see
    # _validate_transcript_documents_exclusive below, which inspects the RAW
    # payload (before defaulting) so it can tell "transcript omitted" apart
    # from "transcript explicitly null" apart from "transcript explicitly []".
    transcript: list[Segment] = Field(default_factory=list)
    documents: Optional[list[Document]] = None
    notes_markdown: str = ""
    template: Template
    options: dict[str, Any] = Field(default_factory=dict)
    config: PluginConfig = Field(default_factory=PluginConfig)

    @field_validator("transcript", mode="before")
    @classmethod
    def _coerce_null_transcript(cls, v: Any) -> Any:
        # A nil Go slice marshals to JSON null; treat it as an empty list.
        return [] if v is None else v

    @model_validator(mode="before")
    @classmethod
    def _validate_transcript_documents_exclusive(cls, data: Any) -> Any:
        if not isinstance(data, dict):
            return data
        has_transcript = data.get("transcript") is not None
        has_documents = bool(data.get("documents"))
        if has_transcript and has_documents:
            raise ValueError("exactly one of transcript or documents may be supplied, not both")
        # A missing/null transcript with no documents is pre-existing,
        # accepted behaviour (notes-only summarisation, see
        # test_generate_accepts_missing_transcript /
        # test_generate_accepts_null_transcript) -- unlike the Go pluginkit
        # HTTP boundary (internal/pluginkit's validateGenerateRequest), this
        # legacy Python contract does NOT require at least one of the two.
        if has_documents:
            seen: set[str] = set()
            for doc in data["documents"]:
                note_id = doc.get("note_id") if isinstance(doc, dict) else getattr(doc, "note_id", None)
                if not note_id:
                    raise ValueError("documents[].note_id is required")
                if note_id in seen:
                    raise ValueError(f"duplicate document note_id {note_id!r}")
                seen.add(note_id)
        return data


class SummarySection(BaseModel):
    heading: str
    content_markdown: str
    refs: Optional[list[int]] = None


class Summary(BaseModel):
    sections: list[SummarySection]


class GenerateResponse(BaseModel):
    summary: Summary
    model: str
