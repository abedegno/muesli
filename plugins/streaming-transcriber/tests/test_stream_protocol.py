from __future__ import annotations

import math
from array import array
from collections.abc import Iterable

from starlette.websockets import WebSocketDisconnect

from streaming_app import transcribe as transcribe_mod


def _chunks(data: bytes, size: int) -> Iterable[bytes]:
    for i in range(0, len(data), size):
        yield data[i : i + size]


def _tone_burst(duration_ms: int, sample_rate: int = 16000, amplitude: int = 16000) -> bytes:
    total_samples = sample_rate * duration_ms // 1000
    data = array("h")
    for i in range(total_samples):
        t = i / sample_rate
        envelope = min(1.0, max(0.0, t / 0.08)) * min(1.0, max(0.0, (duration_ms / 1000.0 - t) / 0.08))
        carrier = (
            0.45 * math.sin(2 * math.pi * 180 * t)
            + 0.25 * math.sin(2 * math.pi * 320 * t)
            + 0.2 * math.sin(2 * math.pi * 820 * t)
            + 0.1 * math.sin(2 * math.pi * 1200 * t)
        )
        sample = int(amplitude * envelope * carrier)
        data.append(max(-32768, min(32767, sample)))
    return data.tobytes()


def test_stream_emits_partial_then_final_segment(client, auth_headers, monkeypatch):
    speech_pcm = _tone_burst(1200)
    silence_pcm = b"\x00\x00" * (16000 * 700 // 1000)
    audio_pcm = speech_pcm + silence_pcm
    frame_size = 16000 * 2 * 200 // 1000
    full_speech_len = len(speech_pcm)

    def fake_transcribe(pcm, sample_rate, settings):
        if len(pcm) < full_speech_len:
            return "partial text"
        return "final text"

    monkeypatch.setattr(transcribe_mod, "transcribe_utterance", fake_transcribe)

    with client.websocket_connect("/stream", headers=auth_headers) as ws:
        ws.send_json(
            {
                "type": "start",
                "language_hint": "en",
                "options": {},
                "config": {
                    "vad_aggressiveness": 0,
                    "silence_threshold_ms": 600,
                    "min_speech_ms": 120,
                    "partial_interval_ms": 200,
                },
                "sample_rate": 16000,
                "channels": 1,
            }
        )
        assert ws.receive_json() == {"type": "ready"}
        for chunk in _chunks(audio_pcm, frame_size):
            ws.send_bytes(chunk)
        ws.send_json({"type": "stop"})

        messages = []
        while True:
            try:
                messages.append(ws.receive_json())
            except WebSocketDisconnect:
                break

    segments = [message for message in messages if message["type"] == "segment"]
    partial_segments = [segment for segment in segments if segment["final"] is False]
    final_segments = [segment for segment in segments if segment["final"] is True]

    assert len(final_segments) == 1
    assert len(partial_segments) >= 1
    assert final_segments[0]["text"] == "final text"
    assert any(segment["text"] == "partial text" for segment in partial_segments)
    assert final_segments[0] == segments[-1]
    assert final_segments[0]["speaker"] is None
    assert all(segment["speaker"] is None for segment in partial_segments)
    assert final_segments[0]["t0"] < final_segments[0]["t1"]
    assert all(segment["t0"] < segment["t1"] for segment in partial_segments)
