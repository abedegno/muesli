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
                "Force-commit an in-progress utterance as a final segment once it "
                "reaches this length, then start a fresh segment for subsequent "
                "audio. Bounds buffer growth and re-transcription cost for long, "
                "continuous speech."
            ),
            "minimum": 1000,
            "default": 30000,
        },
    },
    "additionalProperties": False,
}


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

    # Trailing window of buffered speech actually re-decoded for partial
    # transcripts. Keeps partial cost flat regardless of utterance length.
    _PARTIAL_WINDOW_MS = 5000

    def __post_init__(self) -> None:
        self.frame_bytes = self.sample_rate * 2 * self.frame_ms // 1000
        self._buffer = bytearray()
        self._frame_index = 0
        self._segment_start_frame: int | None = None
        self._last_speech_frame_end: int | None = None
        self._silence_ms = 0
        self._speech_ms = 0
        self._speech_since_partial_ms = 0
        self._utterance = bytearray()
        # Parallel to `_utterance`: the true source frame_index that each
        # buffered frame came from. `_utterance` only ever contains
        # VAD-classified speech frames, so wall-clock frame_index and buffer
        # position diverge whenever the segment contains a sub-threshold
        # silence gap (one shorter than `silence_threshold_ms`, so the
        # segment doesn't flush). This mapping lets us recover the real
        # timestamp of any trailing window we decode instead of assuming the
        # buffered frames are wall-clock-contiguous.
        self._utterance_frames: list[int] = []

    def feed(self, chunk: bytes) -> list[StreamSegmentResponse]:
        self._buffer.extend(chunk)
        events: list[StreamSegmentResponse] = []
        while len(self._buffer) >= self.frame_bytes:
            frame = bytes(self._buffer[: self.frame_bytes])
            del self._buffer[: self.frame_bytes]
            events.extend(self._process_frame(frame))
        return events

    def finish(self) -> list[StreamSegmentResponse]:
        if self._buffer and self._segment_start_frame is not None:
            # Flush any sub-frame residual (shorter than one 20ms VAD frame)
            # into the utterance so trailing audio isn't dropped on stop.
            residual = bytes(self._buffer)
            self._buffer.clear()
            frame_index = self._frame_index
            self._frame_index += 1
            self._utterance.extend(residual)
            self._utterance_frames.append(frame_index)
            self._last_speech_frame_end = frame_index + 1
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
                self._utterance_frames.clear()
            self._utterance.extend(frame)
            self._utterance_frames.append(frame_index)
            self._last_speech_frame_end = frame_index + 1
            self._silence_ms = 0
            self._speech_ms += self.frame_ms
            self._speech_since_partial_ms += self.frame_ms
            if self.max_utterance_ms > 0 and self._speech_ms >= self.max_utterance_ms:
                # Commit-and-restart: force-flush the utterance accumulated so
                # far as a final segment. The next speech frame starts a
                # fresh segment, so the buffer never grows unbounded and no
                # audio is silently dropped.
                return self._flush(force=True)
            events = []
            if self.partial_interval_ms > 0 and self._speech_since_partial_ms >= self.partial_interval_ms:
                events.extend(self._emit_partial())
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
        window_frame_count = max(1, self._PARTIAL_WINDOW_MS // self.frame_ms)
        total_frames = len(self._utterance_frames)
        start_idx = max(0, total_frames - window_frame_count)
        window_pcm = bytes(self._utterance[start_idx * self.frame_bytes :])
        text = transcribe_module.transcribe_utterance(
            window_pcm, self.sample_rate, self.settings
        ).strip()
        self._speech_since_partial_ms = 0
        if not text:
            return []
        if self._utterance_frames:
            window_start_frame = self._utterance_frames[start_idx]
        else:
            window_start_frame = self._segment_start_frame
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
        self._utterance_frames.clear()


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
