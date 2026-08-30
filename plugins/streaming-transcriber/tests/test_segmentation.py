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


class _AlwaysSpeech:
    def is_speech(self, frame, sample_rate):
        return True


def test_continuous_speech_commits_at_cap_and_restarts(monkeypatch):
    call_sizes = []

    def transcribe(pcm, sample_rate, settings):
        call_sizes.append(len(pcm))
        return "canned text"

    monkeypatch.setattr(transcribe_mod, "transcribe_utterance", transcribe)
    state = _SegmenterState(
        sample_rate=16000,
        settings=TranscriptionSettings(model="tiny.en", device="cpu", compute_type="int8"),
        vad=_AlwaysSpeech(),
        max_utterance_ms=100,
        partial_interval_ms=100,
    )
    frame = b"\x01\x00" * (16000 * state.frame_ms // 1000)
    events = []
    max_buffer_size = 0
    for _ in range(7):
        events.extend(state.feed(frame))
        max_buffer_size = max(max_buffer_size, len(state._utterance))

    midstream_finals = [event for event in events if event.final]
    assert len(midstream_finals) == 1
    assert not [event for event in events if not event.final]
    assert state._speech_ms == 40
    assert len(state._utterance) == 2 * len(frame)
    assert max_buffer_size <= 5 * len(frame)

    events.extend(state.finish())
    assert len([event for event in events if event.final]) == 2
    assert call_sizes[-1] == 2 * len(frame)


def test_finish_includes_trailing_subframe_audio(monkeypatch):
    call_sizes = []
    monkeypatch.setattr(
        transcribe_mod,
        "transcribe_utterance",
        lambda pcm, sample_rate, settings: call_sizes.append(len(pcm)) or "final",
    )
    state = _SegmenterState(
        sample_rate=1000,
        settings=TranscriptionSettings(model="tiny.en", device="cpu", compute_type="int8"),
        vad=_AlwaysSpeech(),
        partial_interval_ms=1000,
    )
    full_frame = b"\x01\x00" * 20
    trailing = b"\x02\x00" * 5

    state.feed(full_frame + trailing)
    events = state.finish()

    assert call_sizes == [len(full_frame) + len(trailing)]
    assert len(events) == 1 and events[0].final


def test_partial_transcription_uses_bounded_trailing_audio(monkeypatch):
    call_sizes = []

    def transcribe(pcm, sample_rate, settings):
        call_sizes.append(len(pcm))
        return "partial"

    monkeypatch.setattr(transcribe_mod, "transcribe_utterance", transcribe)
    state = _SegmenterState(
        sample_rate=1000,
        settings=TranscriptionSettings(model="tiny.en", device="cpu", compute_type="int8"),
        vad=_AlwaysSpeech(),
        max_utterance_ms=10000,
        partial_interval_ms=1000,
    )
    frame = b"\x01\x00" * (1000 * state.frame_ms // 1000)
    for _ in range(300):
        state.feed(frame)

    assert len(call_sizes) == 6
    assert call_sizes[:5] == [2000, 4000, 6000, 8000, 10000]
    assert call_sizes[-1] == 10000
