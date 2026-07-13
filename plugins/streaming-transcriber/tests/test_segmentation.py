from __future__ import annotations

from collections.abc import Iterable

import webrtcvad

from streaming_app import transcribe as transcribe_mod
from streaming_app.main import _SegmenterState
from streaming_app.transcribe import TranscriptionSettings


def _chunks(data: bytes, size: int) -> Iterable[bytes]:
    for i in range(0, len(data), size):
        yield data[i : i + size]


def test_vad_segmentation_produces_two_segments(monkeypatch, utterance_pcm):
    monkeypatch.setattr(transcribe_mod, "transcribe_utterance", lambda pcm, sr, settings: "canned text")
    state = _SegmenterState(
        sample_rate=16000,
        settings=TranscriptionSettings(model="tiny.en", device="cpu", compute_type="int8"),
        vad=webrtcvad.Vad(2),
        silence_threshold_ms=600,
        min_speech_ms=120,
        partial_interval_ms=200,
    )
    events = []
    for chunk in _chunks(utterance_pcm, 16000 * 2 * 200 // 1000):
        events.extend(state.feed(chunk))
    events.extend(state.finish())
    assert all(event.type == "segment" for event in events)
    final_events = [event for event in events if event.final]
    partial_events = [event for event in events if not event.final]
    assert len(final_events) == 2
    assert partial_events
    assert all(event.text == "canned text" for event in final_events)
    assert all(event.text == "canned text" for event in partial_events)
    assert final_events[0].t0 < final_events[0].t1
    assert final_events[1].t0 < final_events[1].t1
    assert final_events[0].t1 <= final_events[1].t0
