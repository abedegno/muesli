from __future__ import annotations

from collections.abc import Iterable

import pytest
import webrtcvad

from streaming_app import transcribe as transcribe_mod
from streaming_app.main import _SegmenterState
from streaming_app.transcribe import TranscriptionSettings

SAMPLE_RATE = 16000
FRAME_MS = 20
FRAME_BYTES = SAMPLE_RATE * 2 * FRAME_MS // 1000


def _chunks(data: bytes, size: int) -> Iterable[bytes]:
    for i in range(0, len(data), size):
        yield data[i : i + size]


class _ScriptedVad:
    """Deterministic stand-in for webrtcvad.Vad.

    Classifies frames speech/silence from a fixed, pre-scripted pattern
    rather than webrtcvad's amplitude/spectral heuristics, so segmentation
    timing tests are exact and don't depend on synthetic-tone envelopes.
    """

    def __init__(self, pattern: list[bool]) -> None:
        self._pattern = pattern
        self._index = 0

    def is_speech(self, frame: bytes, sample_rate: int) -> bool:
        if self._index >= len(self._pattern):
            raise AssertionError("scripted VAD pattern exhausted")
        value = self._pattern[self._index]
        self._index += 1
        return value


def _make_state(pattern: list[bool], **overrides) -> _SegmenterState:
    kwargs = dict(
        sample_rate=SAMPLE_RATE,
        settings=TranscriptionSettings(model="tiny.en", device="cpu", compute_type="int8"),
        vad=_ScriptedVad(pattern),
        silence_threshold_ms=600,
        min_speech_ms=120,
        partial_interval_ms=0,
        max_utterance_ms=30000,
    )
    kwargs.update(overrides)
    return _SegmenterState(**kwargs)


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


def test_long_continuous_speech_commits_and_restarts(monkeypatch):
    """Speech beyond max_utterance_ms is committed as a final segment and a
    fresh segment starts for the remainder -- not a sliding window that drops
    audio, and not unbounded buffer growth."""

    call_sizes: list[int] = []

    def fake_transcribe(pcm, sr, settings):
        call_sizes.append(len(pcm))
        return "text"

    monkeypatch.setattr(transcribe_mod, "transcribe_utterance", fake_transcribe)

    total_frames = 1600  # 32000ms of continuous speech
    state = _make_state([True] * total_frames, max_utterance_ms=30000, partial_interval_ms=0)
    events = list(state.feed(bytes(FRAME_BYTES * total_frames)))
    events.extend(state.finish())

    final_events = [e for e in events if e.final]
    assert len(final_events) == 2

    # First commit fires exactly at the cap: 30000ms of speech, no more.
    assert final_events[0].t0 == pytest.approx(0.0)
    assert final_events[0].t1 == pytest.approx(30.0)
    # Second segment picks up the remainder with no gap and no overlap.
    assert final_events[1].t0 == pytest.approx(final_events[0].t1)
    assert final_events[1].t1 == pytest.approx(32.0)

    # The committed segment carries its full audio (a real commit, not a
    # truncated/sliding window that silently drops audio).
    assert call_sizes[0] == 1500 * FRAME_BYTES
    assert call_sizes[1] == 100 * FRAME_BYTES


def test_finish_preserves_trailing_subframe_audio(monkeypatch):
    captured: list[int] = []

    def fake_transcribe(pcm, sr, settings):
        captured.append(len(pcm))
        return "text"

    monkeypatch.setattr(transcribe_mod, "transcribe_utterance", fake_transcribe)

    speech_frames = 10
    residual = bytes(200)  # shorter than one 20ms frame (640 bytes)
    state = _make_state([True] * speech_frames)
    events = list(state.feed(bytes(FRAME_BYTES * speech_frames) + residual))
    assert events == []  # residual not yet flushed; no VAD frame to process it
    events = state.finish()

    assert len(events) == 1
    assert captured == [FRAME_BYTES * speech_frames + len(residual)]
    assert events[0].t0 == pytest.approx(0.0)
    # The residual is accounted for as one extra frame of coverage.
    assert events[0].t1 == pytest.approx((speech_frames + 1) * FRAME_MS / 1000.0)


def test_partial_decode_cost_stays_bounded_for_long_utterance(monkeypatch):
    call_sizes: list[int] = []

    def fake_transcribe(pcm, sr, settings):
        call_sizes.append(len(pcm))
        return "text"

    monkeypatch.setattr(transcribe_mod, "transcribe_utterance", fake_transcribe)

    total_frames = 1000  # 20000ms, well beyond the 5000ms partial window
    state = _make_state([True] * total_frames, max_utterance_ms=60000, partial_interval_ms=0)
    window_frames = 5000 // FRAME_MS
    window_bytes = window_frames * FRAME_BYTES

    for i in range(total_frames):
        frame = bytes(FRAME_BYTES)
        state.feed(frame)
        # Trigger a partial every 50 frames directly, independent of
        # partial_interval_ms bookkeeping, to sample cost at many buffer sizes.
        if i > 0 and i % 50 == 0:
            state._emit_partial()

    assert call_sizes  # partials were actually emitted
    assert max(call_sizes) <= window_bytes
    # Once the buffer has grown past the window, every call is exactly the
    # bounded window size -- cost is flat, not growing with utterance length.
    assert call_sizes[-1] == window_bytes
    assert len(set(call_sizes[-5:])) == 1


def test_partial_t0_t1_bound_trailing_window_for_continuous_speech(monkeypatch):
    monkeypatch.setattr(transcribe_mod, "transcribe_utterance", lambda pcm, sr, settings: "text")

    total_frames = 400  # 8000ms of continuous speech, no gaps
    state = _make_state([True] * total_frames, max_utterance_ms=60000, partial_interval_ms=0)
    state.feed(bytes(FRAME_BYTES * total_frames))

    events = state._emit_partial()
    assert len(events) == 1
    window_frames = 5000 // FRAME_MS
    expected_t0 = (total_frames - window_frames) * FRAME_MS / 1000.0
    expected_t1 = total_frames * FRAME_MS / 1000.0
    assert events[0].t0 == pytest.approx(expected_t0)
    assert events[0].t1 == pytest.approx(expected_t1)


def test_partial_t0_accounts_for_subthreshold_pause(monkeypatch):
    """Regression test for the bug that sank a prior fix: a sub-threshold
    silence gap (shorter than silence_threshold_ms) inside an otherwise
    continuous segment does not flush, but its frames are never appended to
    the speech buffer -- while wall-clock frame_index keeps advancing. A
    partial's t0 must be derived from the true source frame_index of the
    decoded window, not from `end_frame - window_frames`, which would
    silently assume the buffered speech is wall-clock-contiguous and drift
    by the size of the skipped pause.
    """

    monkeypatch.setattr(transcribe_mod, "transcribe_utterance", lambda pcm, sr, settings: "text")

    speech_a_frames = 300  # 6000ms
    pause_frames = 15  # 300ms: sub-threshold vs. the 600ms silence_threshold_ms
    speech_b_frames = 100  # 2000ms

    pattern = [True] * speech_a_frames + [False] * pause_frames + [True] * speech_b_frames
    state = _make_state(pattern, silence_threshold_ms=600, max_utterance_ms=60000, partial_interval_ms=0)

    total_frames = speech_a_frames + pause_frames + speech_b_frames
    state.feed(bytes(FRAME_BYTES * total_frames))

    # Sanity: the sub-threshold pause must not have flushed the segment.
    assert state._segment_start_frame == 0

    events = state._emit_partial()
    assert len(events) == 1

    window_frames = 5000 // FRAME_MS  # 250
    buffered_speech_frames = speech_a_frames + speech_b_frames  # 400
    start_idx = buffered_speech_frames - window_frames  # 150, still within speech_a

    # Correct: the window starts at the TRUE source frame for buffer
    # position `start_idx`, which is still inside speech_a (before the gap),
    # since the gap's frames were never appended to the buffer.
    expected_t0 = start_idx * FRAME_MS / 1000.0  # 3.0s
    # The buggy formula (`end_frame - window_frames`, assuming contiguity)
    # would instead yield (total_frames - window_frames) * FRAME_MS / 1000.0,
    # i.e. 3.3s -- drifted later by exactly the 300ms skipped pause.
    buggy_t0 = (total_frames - window_frames) * FRAME_MS / 1000.0

    assert events[0].t0 == pytest.approx(expected_t0)
    assert events[0].t0 != pytest.approx(buggy_t0)
    assert events[0].t1 == pytest.approx((total_frames) * FRAME_MS / 1000.0)
