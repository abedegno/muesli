from __future__ import annotations

import json
from dataclasses import dataclass

import webrtcvad
from fastapi import Depends, FastAPI, WebSocket
from pydantic import ValidationError
from starlette.websockets import WebSocketDisconnect

from .auth import require_auth, require_ws_auth
from .config import Settings
from .schema import (
    StreamErrorResponse,
    StreamReadyResponse,
    StreamSegmentResponse,
    StreamStartRequest,
    StreamStopRequest,
)
from . import transcribe as transcribe_module
from .transcribe import TranscriptionSettings

PLUGIN_API = 1
NAME = "muesli-streaming-transcriber"
VERSION = "0.1.0"

CONFIG_SCHEMA = {
    "type": "object",
    "properties": {
        "model": {
            "type": "string",
            "title": "Whisper model",
            "description": "faster-whisper model name or local path.",
            "default": "tiny.en",
        },
        "device": {
            "type": "string",
            "title": "Device",
            "description": "Inference device.",
            "enum": ["cpu", "cuda"],
            "default": "cpu",
        },
        "compute_type": {
            "type": "string",
            "title": "Compute type",
            "description": "faster-whisper compute type.",
            "enum": ["int8", "int8_float16", "float16", "float32"],
            "default": "int8",
        },
        "vad_aggressiveness": {
            "type": "integer",
            "title": "VAD aggressiveness",
            "minimum": 0,
            "maximum": 3,
            "default": 2,
        },
        "silence_threshold_ms": {
            "type": "integer",
            "title": "Trailing silence threshold (ms)",
            "minimum": 100,
            "default": 600,
        },
        "min_speech_ms": {
            "type": "integer",
            "title": "Minimum speech duration (ms)",
            "minimum": 20,
            "default": 120,
        },
        "partial_interval_ms": {
            "type": "integer",
            "title": "Partial segment interval (ms)",
            "minimum": 100,
            "default": 400,
        },
        "max_utterance_ms": {
            "type": "integer",
            "title": "Maximum utterance duration (ms)",
            "description": (
                "Hard cap on a single utterance's accumulated speech. Once reached, the "
                "utterance-so-far is committed as a final segment and a fresh segment starts "
                "on the next speech frame, instead of the buffer growing without bound."
            ),
            "minimum": 1000,
            "default": 30000,
        },
    },
    "additionalProperties": False,
}

# Trailing window (ms) of a partial's accumulated utterance that is actually
# decoded. Bounding this keeps partial decode cost flat regardless of how
# long the current utterance has run, instead of re-decoding the whole
# growing buffer on every partial_interval_ms tick.
_PARTIAL_WINDOW_MS = 5000


@dataclass
class _SegmenterState:
    sample_rate: int
    settings: TranscriptionSettings
    vad: webrtcvad.Vad
    frame_ms: int = 20
    silence_threshold_ms: int = 600
    min_speech_ms: int = 120
    partial_interval_ms: int = 400
    max_utterance_ms: int = 30000

    def __post_init__(self) -> None:
        self.frame_bytes = self.sample_rate * 2 * self.frame_ms // 1000
        # Fixed frame count for the partial decode window rather than a byte
        # count derived from ms/bytes division, so slicing `_utterance` for a
        # partial always lands on exact frame boundaries.
        self._partial_window_frames = max(1, _PARTIAL_WINDOW_MS // self.frame_ms)
        self._buffer = bytearray()
        self._frame_index = 0
        self._segment_start_frame: int | None = None
        self._last_speech_frame_end: int | None = None
        self._silence_ms = 0
        self._speech_ms = 0
        self._speech_since_partial_ms = 0
        self._utterance = bytearray()
        # Parallel to `_utterance`: the true source `frame_index` of every
        # frame appended to it. `_utterance` only ever holds speech frames,
        # so a VAD-classified pause shorter than `silence_threshold_ms`
        # advances `_frame_index` without a corresponding append -- this
        # list lets a partial's trailing window look up the *actual* source
        # position of its first frame instead of inferring it by subtracting
        # a frame count, which silently assumes (and breaks on) contiguity.
        self._utterance_frame_indices: list[int] = []
        # Duration (seconds) of a sub-frame residual folded into `_utterance`
        # by `finish()`. `_last_speech_frame_end` only tracks whole VAD
        # frames, so this extra, less-than-one-frame duration would
        # otherwise be silently dropped from the emitted final segment's
        # `t1` even though the residual bytes are transcribed.
        self._residual_seconds = 0.0
        # Source `frame_index` at the moment a `finish()` residual was
        # appended. A VAD-classified pause too short to trigger a flush
        # advances `_frame_index` without advancing `_last_speech_frame_end`
        # (whole silent frames aren't appended to `_utterance`), so anchoring
        # the residual's `t1` to `_last_speech_frame_end` would ignore that
        # unflushed gap and understate where the residual bytes actually
        # land in source time. This records the *current* source position
        # instead, which is correct whether or not such a gap occurred.
        self._residual_frame_index: int | None = None

    def feed(self, chunk: bytes) -> list[StreamSegmentResponse]:
        self._buffer.extend(chunk)
        events: list[StreamSegmentResponse] = []
        while len(self._buffer) >= self.frame_bytes:
            frame = bytes(self._buffer[: self.frame_bytes])
            del self._buffer[: self.frame_bytes]
            events.extend(self._process_frame(frame))
        return events

    def finish(self) -> list[StreamSegmentResponse]:
        # `feed` only classifies whole `frame_bytes`-sized frames, so a
        # trailing remainder shorter than one VAD frame (e.g. the client's
        # final chunk before `stop`) sits unclassified in `self._buffer`
        # forever unless it is folded into the active utterance here. It
        # cannot be run through the VAD (which requires an exact frame
        # size), so it is appended as-is, best-effort, rather than dropped.
        if self._buffer and self._segment_start_frame is not None:
            self._utterance.extend(self._buffer)
            self._utterance_frame_indices.append(self._frame_index)
            # 16-bit mono PCM: 2 bytes per sample, `sample_rate` samples/sec.
            self._residual_seconds = len(self._buffer) / (self.sample_rate * 2)
            self._residual_frame_index = self._frame_index
            self._buffer.clear()
        return self._flush(force=True)

    def _process_frame(self, frame: bytes) -> list[StreamSegmentResponse]:
        frame_index = self._frame_index
        self._frame_index += 1
        is_speech = self.vad.is_speech(frame, self.sample_rate)
        if is_speech:
            if self._segment_start_frame is None:
                self._segment_start_frame = frame_index
                self._speech_ms = 0
                self._silence_ms = 0
                self._speech_since_partial_ms = 0
                self._utterance.clear()
                self._utterance_frame_indices.clear()
            self._utterance.extend(frame)
            self._utterance_frame_indices.append(frame_index)
            self._last_speech_frame_end = frame_index + 1
            self._silence_ms = 0
            self._speech_ms += self.frame_ms
            self._speech_since_partial_ms += self.frame_ms
            events = []
            if self.partial_interval_ms > 0 and self._speech_since_partial_ms >= self.partial_interval_ms:
                events.extend(self._emit_partial())
            if self._speech_ms >= self.max_utterance_ms:
                # Commit-and-restart: a continuous talker who never pauses
                # long enough to hit the silence flush must not grow
                # `_utterance` without bound. Commit what has accumulated as
                # a final segment; the next speech frame starts a fresh one.
                events.extend(self._flush(force=True))
            return events
        if self._segment_start_frame is None:
            return []
        self._silence_ms += self.frame_ms
        if self._silence_ms >= self.silence_threshold_ms:
            return self._flush(force=False)
        return []

    def _emit_partial(self) -> list[StreamSegmentResponse]:
        if self._segment_start_frame is None:
            return []
        total_frames = len(self._utterance_frame_indices)
        partial_frames = self._partial_window_frames
        if total_frames <= partial_frames:
            window_start_frame = self._segment_start_frame
            window_pcm = bytes(self._utterance)
        else:
            # The true source frame_index of the first frame in the trailing
            # window, looked up directly rather than inferred by subtracting
            # a frame count -- correct even if a sub-threshold VAD pause
            # inside this utterance means buffered frames aren't contiguous
            # with wall-clock frame_index.
            window_start_frame = self._utterance_frame_indices[-partial_frames]
            window_bytes = partial_frames * self.frame_bytes
            window_pcm = bytes(self._utterance[-window_bytes:])
        text = transcribe_module.transcribe_utterance(
            window_pcm, self.sample_rate, self.settings
        ).strip()
        self._speech_since_partial_ms = 0
        if not text:
            return []
        t0 = window_start_frame * self.frame_ms / 1000.0
        end_frame = self._last_speech_frame_end or self._frame_index
        t1 = end_frame * self.frame_ms / 1000.0
        return [
            StreamSegmentResponse(
                text=text,
                t0=t0,
                t1=t1,
                speaker=None,
                final=False,
            )
        ]

    def _flush(self, force: bool) -> list[StreamSegmentResponse]:
        if self._segment_start_frame is None:
            return []
        if not force and self._speech_ms < self.min_speech_ms:
            self._reset()
            return []
        text = transcribe_module.transcribe_utterance(
            bytes(self._utterance), self.sample_rate, self.settings
        ).strip()
        t0 = self._segment_start_frame * self.frame_ms / 1000.0
        if self._residual_seconds and self._residual_frame_index is not None:
            # A `finish()` residual's source position is the current source
            # frame index, not `_last_speech_frame_end` -- an intervening
            # VAD-classified pause too short to flush advances `_frame_index`
            # without advancing `_last_speech_frame_end`, so the latter would
            # understate where the residual bytes actually land.
            end_frame = self._residual_frame_index
            t1 = end_frame * self.frame_ms / 1000.0 + self._residual_seconds
        else:
            end_frame = self._last_speech_frame_end or self._frame_index
            t1 = end_frame * self.frame_ms / 1000.0
        self._reset()
        if not text:
            return []
        return [
            StreamSegmentResponse(
                text=text,
                t0=t0,
                t1=t1,
                speaker=None,
                final=True,
            )
        ]

    def _reset(self) -> None:
        self._segment_start_frame = None
        self._last_speech_frame_end = None
        self._silence_ms = 0
        self._speech_ms = 0
        self._speech_since_partial_ms = 0
        self._utterance.clear()
        self._utterance_frame_indices.clear()
        self._residual_seconds = 0.0
        self._residual_frame_index = None


def create_app(settings: Settings) -> FastAPI:
    app = FastAPI(title=NAME, version=VERSION)
    app.state.settings = settings

    @app.get("/info", dependencies=[Depends(require_auth)])
    def info() -> dict:
        return {
            "name": NAME,
            "version": VERSION,
            "plugin_api": PLUGIN_API,
            "kind": "streaming-transcriber",
            "config_schema": CONFIG_SCHEMA,
        }

    @app.get("/health")
    def health() -> dict:
        return {"status": "ok"}

    @app.websocket("/stream", dependencies=[Depends(require_ws_auth)])
    async def stream(websocket: WebSocket) -> None:
        await websocket.accept()
        try:
            start_payload = await websocket.receive_json()
            start = StreamStartRequest.model_validate(start_payload)
            runtime = TranscriptionSettings(
                model=str(start.config.get("model") or app.state.settings.default_model),
                device=str(start.config.get("device") or app.state.settings.device),
                compute_type=str(start.config.get("compute_type") or app.state.settings.compute_type),
                language_hint=start.language_hint,
            )
            segmenter = _SegmenterState(
                sample_rate=start.sample_rate,
                settings=runtime,
                vad=webrtcvad.Vad(int(start.config.get("vad_aggressiveness", app.state.settings.vad_aggressiveness))),
                silence_threshold_ms=int(
                    start.config.get("silence_threshold_ms", app.state.settings.silence_threshold_ms)
                ),
                min_speech_ms=int(start.config.get("min_speech_ms", app.state.settings.min_speech_ms)),
                partial_interval_ms=int(
                    start.config.get("partial_interval_ms", app.state.settings.partial_interval_ms)
                ),
                max_utterance_ms=int(
                    start.config.get("max_utterance_ms", app.state.settings.max_utterance_ms)
                ),
            )
            await websocket.send_json(StreamReadyResponse().model_dump(exclude_none=True))
            while True:
                message = await websocket.receive()
                if message.get("bytes") is not None:
                    for event in segmenter.feed(message["bytes"]):
                        await websocket.send_json(event.model_dump())
                    continue
                text = message.get("text")
                if text is None:
                    continue
                control = json.loads(text)
                stop = StreamStopRequest.model_validate(control)
                if stop.type != "stop":
                    raise ValueError("unsupported control message")
                for event in segmenter.finish():
                    await websocket.send_json(event.model_dump())
                await websocket.close()
                return
        except (ValidationError, ValueError, json.JSONDecodeError) as exc:
            try:
                await websocket.send_json(StreamErrorResponse(message=str(exc)).model_dump(exclude_none=True))
            finally:
                await websocket.close()
        except WebSocketDisconnect:
            return

    return app


def app() -> FastAPI:  # uvicorn entrypoint: `uvicorn streaming_app.main:app --factory`
    return create_app(Settings.from_env())
