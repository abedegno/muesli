from __future__ import annotations

import base64
import os
import tempfile
import urllib.parse
from typing import Any

import httpx

from .config import Settings
from .schema import Segment, TranscribeRequest, TranscribeResponse


def load_model(model: str, device: str):
    """Lazily import and construct the NeMo/Parakeet ASR model.

    TODO: verify the exact NeMo API against real GPU hardware and actual weights.
    This import is intentionally inside the function body so CI can monkeypatch
    `parakeet_app.transcribe.load_model` and avoid importing nemo_toolkit at all.
    """
    # Heavy-path gate: keep the NeMo import inside this function so tests never
    # import nemo_toolkit or download weights when load_model is monkeypatched.
    import nemo.collections.asr as nemo_asr  # type: ignore[import]

    return nemo_asr.models.ASRModel.from_pretrained(model_name=model, map_location=device)


def _decode_data_url(audio_url: str) -> bytes:
    if not audio_url.startswith("data:"):
        raise ValueError(f"not a data: URL: {audio_url[:32]!r}")
    try:
        meta, payload = audio_url[len("data:"):].split(",", 1)
    except ValueError as exc:
        raise ValueError("malformed data: URL (missing comma)") from exc
    if meta.endswith(";base64"):
        try:
            return base64.b64decode(payload, validate=True)
        except ValueError as exc:
            raise ValueError("malformed base64 in data: URL") from exc
    return urllib.parse.unquote_to_bytes(payload)


def _download(audio_url: str) -> str:
    if audio_url.startswith("data:"):
        content = _decode_data_url(audio_url)
    elif audio_url.startswith(("http://", "https://")):
        with httpx.Client(timeout=60.0) as c:
            resp = c.get(audio_url)
            resp.raise_for_status()
        content = resp.content
    else:
        raise ValueError(f"unsupported audio_url scheme: {audio_url[:32]!r}")
    fd, path = tempfile.mkstemp(suffix=".audio")
    with os.fdopen(fd, "wb") as f:
        f.write(content)
    return path


def _value(obj: Any, key: str, default: Any = None) -> Any:
    if isinstance(obj, dict):
        return obj.get(key, default)
    return getattr(obj, key, default)


def _to_ms(value: Any) -> int:
    if value is None:
        return 0
    if isinstance(value, float):
        return int(round(value * 1000))
    if isinstance(value, int):
        return value
    return int(value)


def _coerce_segments(raw_segments: Any) -> list[Segment]:
    segments: list[Segment] = []
    if raw_segments is None:
        return segments
    if isinstance(raw_segments, dict):
        raw_segments = raw_segments.get("segments") or raw_segments.get("chunks") or []
    for item in raw_segments:
        segments.append(
            Segment(
                start_ms=_to_ms(_value(item, "start_ms", _value(item, "start", 0))),
                end_ms=_to_ms(_value(item, "end_ms", _value(item, "end", 0))),
                text=str(_value(item, "text", _value(item, "transcript", ""))),
                source=str(_value(item, "source", "mixed") or "mixed"),
                speaker=_value(item, "speaker"),
            )
        )
    return segments


def _extract_result_and_meta(result: Any) -> tuple[Any, Any]:
    if isinstance(result, tuple) and len(result) == 2:
        return result[0], result[1]
    if isinstance(result, list) and len(result) == 2 and not isinstance(result[0], (str, bytes)):
        return result[0], result[1]
    return result, None


def _coerce_language(transcript: Any, meta: Any, hint: str | None) -> str:
    for obj in (meta, transcript):
        language = _value(obj, "language")
        if language:
            return str(language)
    return hint or "en"


def _coerce_duration_ms(transcript: Any, meta: Any) -> int:
    for obj in (meta, transcript):
        duration_ms = _value(obj, "duration_ms")
        if duration_ms is not None:
            return _to_ms(duration_ms)
        duration = _value(obj, "duration")
        if duration is not None:
            return _to_ms(duration)
    return 0


def _coerce_text(transcript: Any) -> str:
    text = _value(transcript, "text")
    return str(text) if text is not None else ""


def run_transcribe(req: TranscribeRequest, settings: Settings) -> TranscribeResponse:
    model_name = str(req.config.get("model") or settings.default_model)
    device = str(req.config.get("device") or settings.device)

    path = _download(req.audio_url)
    try:
        model = load_model(model_name, device)
        # The real NeMo/Parakeet API is not exercised in CI. Tests monkeypatch
        # load_model with a fake; this path should remain a documented best-effort
        # integration point until it is verified on GPU hardware.
        result = model.transcribe(path, language=req.language_hint)
        transcript, meta = _extract_result_and_meta(result)
        segments = _coerce_segments(_value(transcript, "segments", transcript))
        if not segments:
            text = _coerce_text(transcript)
            if text:
                segments = [Segment(start_ms=0, end_ms=_coerce_duration_ms(transcript, meta), text=text)]
        language = _coerce_language(transcript, meta, req.language_hint)
        duration_ms = _coerce_duration_ms(transcript, meta)
        return TranscribeResponse(
            segments=segments,
            language=language,
            model=model_name,
            duration_ms=duration_ms,
        )
    finally:
        try:
            os.unlink(path)
        except OSError:
            pass
