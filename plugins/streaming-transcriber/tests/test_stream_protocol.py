from __future__ import annotations

from collections.abc import Iterable

import pytest
from starlette.websockets import WebSocketDisconnect

from streaming_app import transcribe as transcribe_mod


def _chunks(data: bytes, size: int) -> Iterable[bytes]:
    for i in range(0, len(data), size):
        yield data[i : i + size]


def test_stream_ready_then_two_segments(client, auth_headers, monkeypatch, utterance_pcm):
    monkeypatch.setattr(transcribe_mod, "transcribe_utterance", lambda pcm, sr, settings: "segment text")
    frame_size = 16000 * 2 * 200 // 1000
    with client.websocket_connect("/stream", headers=auth_headers) as ws:
        ws.send_json(
            {
                "type": "start",
                "language_hint": "en",
                "options": {},
                "config": {},
                "sample_rate": 16000,
                "channels": 1,
            }
        )
        assert ws.receive_json() == {"type": "ready"}
        for chunk in _chunks(utterance_pcm, frame_size):
            ws.send_bytes(chunk)
        ws.send_json({"type": "stop"})
        first = ws.receive_json()
        second = ws.receive_json()
        assert first["type"] == "segment"
        assert first["final"] is True
        assert first["text"] == "segment text"
        assert first["speaker"] is None
        assert second["type"] == "segment"
        assert second["final"] is True
        assert second["text"] == "segment text"
        assert second["speaker"] is None
        assert first["t0"] < first["t1"]
        assert second["t0"] < second["t1"]
        assert first["t1"] <= second["t0"]
        with pytest.raises(WebSocketDisconnect):
            ws.receive_json()
