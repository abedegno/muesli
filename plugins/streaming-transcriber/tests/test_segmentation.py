from __future__ import annotations

import math
from array import array
from collections.abc import Iterable

import pytest
import webrtcvad

from streaming_app import transcribe as transcribe_mod
from streaming_app.main import _SegmenterState
from streaming_app.transcribe import TranscriptionSettings

SAMPLE_RATE = 16000
FRAME_BYTES = SAMPLE_RATE * 2 * 20 // 1000  # one 20ms VAD frame at 16kHz/16-bit mono


def _chunks(data: bytes, size: int) -> Iterable[bytes]:
    for i in range(0, len(data), size):
        yield data[i : i + size]


def _tone_burst(duration_ms: int, amplitude: int = 16000) -> bytes:
    """Deterministic speech-ish synthetic audio for VAD tests (mirrors conftest's)."""

    total_samples = SAMPLE_RATE * duration_ms // 1000
    data = array("h")
    for i in range(total_samples):
        t = i / SAMPLE_RATE
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


def _state(**overrides) -> _SegmenterState:
    kwargs = dict(
        sample_rate=SAMPLE_RATE,
        settings=TranscriptionSettings(model="tiny.en", device="cpu", compute_type="int8"),
        vad=webrtcvad.Vad(2),
        silence_threshold_ms=600,
        min_speech_ms=120,
        partial_interval_ms=400,
        max_utterance_ms=30000,
    )
    kwargs.update(overrides)
    return _SegmenterState(**kwargs)


def test_vad_segmentation_produces_two_segments(monkeypatch, utterance_pcm):
    monkeypatch.setattr(transcribe_mod, "transcribe_utterance", lambda pcm, sr, settings: "canned text")
    state = _state(partial_interval_ms=200)
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


def test_long_continuous_utterance_exceeds_max_utterance_commits_and_restarts(monkeypatch):
    """A continuous talker who never pauses must not grow `_utterance` without
    bound: hitting max_utterance_ms commits what has accumulated as a final
    and starts a fresh segment on the next speech frame (muesli#722)."""

    monkeypatch.setattr(transcribe_mod, "transcribe_utterance", lambda pcm, sr, settings: "canned text")
    max_utterance_ms = 2000
    state = _state(partial_interval_ms=100_000, max_utterance_ms=max_utterance_ms)  # keep partials out of the way
    speech = _tone_burst(3200)  # one continuous utterance, no VAD pauses
    max_utterance_bytes = SAMPLE_RATE * 2 * max_utterance_ms // 1000

    events = []
    for chunk in _chunks(speech, FRAME_BYTES):
        events.extend(state.feed(chunk))
        assert len(state._utterance) <= max_utterance_bytes

    events.extend(state.finish())

    final_events = [event for event in events if event.final]
    # One cap-triggered mid-stream commit plus finish() draining the
    # remainder: exactly one extra final beyond the single final an
    # uncapped (or shorter) utterance would have produced.
    assert len(final_events) == 2
    assert final_events[0].t1 - final_events[0].t0 <= max_utterance_ms / 1000.0 + 0.05
    assert final_events[1].t0 == pytest.approx(final_events[0].t1, abs=0.05)


def test_partial_decode_bytes_stay_flat_for_long_utterance(monkeypatch):
    """Partial decode cost must stay bounded/flat regardless of how long the
    current utterance has run, instead of re-decoding the whole growing
    buffer on every partial_interval_ms tick."""

    sizes: list[int] = []

    def fake_transcribe(pcm, sample_rate, settings):
        sizes.append(len(pcm))
        return "text"

    monkeypatch.setattr(transcribe_mod, "transcribe_utterance", fake_transcribe)
    state = _state()  # max_utterance_ms high enough that the cap never triggers here
    speech = _tone_burst(9000)  # well beyond the 5s partial window
    for chunk in _chunks(speech, FRAME_BYTES):
        state.feed(chunk)

    window_bytes = SAMPLE_RATE * 2 * 5000 // 1000
    assert len(sizes) >= 4
    assert max(sizes) <= window_bytes
    # Once the utterance has run past the window, decode cost must stop
    # growing with elapsed time -- the last several partials must be flat.
    assert len(set(sizes[-3:])) == 1
    assert sizes[-1] == window_bytes


def test_partial_timestamps_bound_trailing_window_for_contiguous_utterance(monkeypatch):
    """Regression test for PR #741's finding: a partial's t0..t1 must bound
    the trailing window actually decoded, not the whole utterance-so-far."""

    monkeypatch.setattr(transcribe_mod, "transcribe_utterance", lambda pcm, sr, settings: "text")
    state = _state()
    speech = _tone_burst(9000)
    window_s = 5.0
    partials = []
    for chunk in _chunks(speech, FRAME_BYTES):
        partials.extend(state.feed(chunk))

    assert len(partials) >= 4
    for segment in partials:
        assert segment.t1 - segment.t0 <= window_s + 0.05
        assert segment.t0 == pytest.approx(max(0.0, segment.t1 - window_s), abs=0.05)


def test_partial_t0_reflects_true_frame_index_across_sub_threshold_pause(monkeypatch):
    """Regression test for PR #747's residual defect: a VAD-classified pause
    shorter than silence_threshold_ms inside an utterance does not flush the
    segment, but the silent frames are never appended to `_utterance` while
    wall-clock frame_index keeps advancing. A partial's t0 must reflect the
    true source-audio position of the trailing window (via the tracked
    per-frame indices), not a value that assumes buffered frames are
    contiguous with frame_index."""

    monkeypatch.setattr(transcribe_mod, "transcribe_utterance", lambda pcm, sr, settings: "text")
    state = _state(silence_threshold_ms=600, partial_interval_ms=100_000)  # call _emit_partial directly
    speech_before = _tone_burst(6000)
    gap = b"\x00\x00" * (SAMPLE_RATE * 400 // 1000)  # 400ms < silence_threshold_ms: must not flush
    speech_after = _tone_burst(3000)
    audio = speech_before + gap + speech_after

    for chunk in _chunks(audio, FRAME_BYTES):
        state.feed(chunk)

    # The segment must still be open -- the gap was too short to flush it.
    assert state._segment_start_frame is not None

    frame_indices = state._utterance_frame_indices
    # The gap must actually have produced a source-frame discontinuity in
    # `_utterance` for this to be a meaningful regression check.
    gaps = [b - a for a, b in zip(frame_indices, frame_indices[1:]) if b - a > 1]
    assert gaps, "expected the VAD-classified pause to skip appending frames"

    partial_frames = state._partial_window_frames
    assert len(frame_indices) > partial_frames

    partial = state._emit_partial()[0]

    # Correct: looked up directly from the true source frame_index of the
    # first frame in the trailing window.
    expected_start_frame = frame_indices[-partial_frames]
    assert partial.t0 == pytest.approx(expected_start_frame * state.frame_ms / 1000.0)

    # The naive PR #747 formula (end_frame - partial_frames, which assumes
    # buffered frames are contiguous with wall-clock frame_index) must NOT
    # match, and must land later than the true window start: the gap means
    # the true window spans more wall-clock time than partial_frames*frame_ms.
    end_frame = state._last_speech_frame_end
    buggy_t0 = max(state._segment_start_frame, end_frame - partial_frames) * state.frame_ms / 1000.0
    assert partial.t0 < buggy_t0 - 1e-9


def test_finish_flushes_subframe_residual_bytes(monkeypatch):
    """finish() must fold whatever sub-frame remainder is sitting in
    `_buffer` into the final transcription instead of silently dropping it."""

    sizes: list[int] = []

    def fake_transcribe(pcm, sample_rate, settings):
        sizes.append(len(pcm))
        return "text"

    monkeypatch.setattr(transcribe_mod, "transcribe_utterance", fake_transcribe)
    state = _state(partial_interval_ms=100_000)
    speech = _tone_burst(300)  # 15 whole 20ms frames, evenly divides FRAME_BYTES
    residual = b"\x11\x22" * 100  # 200 bytes, well under one 640-byte frame

    for chunk in _chunks(speech, FRAME_BYTES):
        state.feed(chunk)
    state.feed(residual)  # too short to become a whole frame; stays in `_buffer`

    assert len(state._buffer) == len(residual)
    events = state.finish()
    assert len(state._buffer) == 0

    final_events = [event for event in events if event.final]
    assert len(final_events) == 1
    # The residual bytes must have reached the transcriber, not been dropped.
    assert sizes[-1] == 15 * FRAME_BYTES + len(residual)


def test_finish_residual_bytes_extend_t1(monkeypatch):
    """The sub-frame residual `finish()` folds into the transcription must
    also be reflected in the emitted final segment's `t1` -- otherwise the
    segment claims to end before the audio it actually transcribed covers.
    """

    monkeypatch.setattr(transcribe_mod, "transcribe_utterance", lambda pcm, sr, settings: "text")
    state = _state(partial_interval_ms=100_000)
    speech = _tone_burst(300)  # 15 whole 20ms frames, evenly divides FRAME_BYTES
    residual = b"\x11\x22" * 100  # 200 bytes == 6.25ms at 16kHz/16-bit mono

    for chunk in _chunks(speech, FRAME_BYTES):
        state.feed(chunk)
    state.feed(residual)

    pcm_duration = (len(speech) + len(residual)) / (SAMPLE_RATE * 2)

    events = state.finish()
    final_events = [event for event in events if event.final]
    assert len(final_events) == 1
    segment = final_events[0]

    # Naive/buggy behaviour: t1 truncated to the whole-frame boundary,
    # ignoring the residual entirely.
    buggy_t1 = len(speech) / (SAMPLE_RATE * 2)
    assert segment.t1 > buggy_t1 + 1e-9

    assert segment.t1 == pytest.approx(pcm_duration)
    assert segment.t1 >= pcm_duration - 1e-9


def test_finish_residual_t1_accounts_for_intervening_pause(monkeypatch):
    """Regression test for the cross-review finding on i722/PR #752: a
    sub-threshold VAD-classified silence gap between speech and a `finish()`
    residual must not be dropped from the final segment's `t1`. Whole silent
    frames advance `_frame_index` but not `_last_speech_frame_end`, so
    anchoring the residual purely to `_last_speech_frame_end` understates
    where the residual bytes actually land in source time.

    VAD classification is forced deterministically here rather than relying
    on webrtcvad's real noise-floor/hangover timing on synthetic audio,
    which is too undocumented and version-fragile to pin an exact scenario
    (300ms speech, then a 100ms sub-threshold pause, then a residual) to.
    """

    monkeypatch.setattr(transcribe_mod, "transcribe_utterance", lambda pcm, sr, settings: "text")
    state = _state(silence_threshold_ms=600, partial_interval_ms=100_000)

    # 15 frames (300ms) classified as speech, then 5 frames (100ms)
    # classified as non-speech -- sub-threshold, so must not flush.
    classifications = [True] * 15 + [False] * 5
    monkeypatch.setattr(state.vad, "is_speech", lambda frame, sample_rate: classifications.pop(0))

    frame = b"\x00\x00" * (FRAME_BYTES // 2)
    for _ in range(20):
        state.feed(frame)

    # The gap must be too short to flush -- the segment is still open. And
    # this must actually be a meaningful regression check: the 5 silent
    # frames must have been VAD-classified as non-speech (accumulating
    # `_silence_ms`) yet never appended to `_utterance`, so `_frame_index`
    # (20) has advanced past `_last_speech_frame_end` (15) without the
    # latter following it.
    assert state._segment_start_frame is not None
    assert state._silence_ms == 100
    assert state._last_speech_frame_end == 15
    assert state._frame_index == 20
    assert len(state._utterance_frame_indices) == 15

    residual = b"\x11\x22" * 100  # 200 bytes == 6.25ms at 16kHz/16-bit mono
    state.feed(residual)  # too short to become a whole frame; stays in `_buffer`
    events = state.finish()

    final_events = [event for event in events if event.final]
    assert len(final_events) == 1
    segment = final_events[0]

    # Buggy behaviour (pre-fix): t1 anchored to speech end + residual only,
    # silently ignoring the 100ms unflushed silence gap in between.
    buggy_t1 = 15 * state.frame_ms / 1000.0 + len(residual) / (SAMPLE_RATE * 2)
    assert segment.t1 > buggy_t1 + 1e-9

    # Correct: t1 anchored to the true source position of the residual --
    # 300ms speech + 100ms unflushed silence + 6.25ms residual == 0.40625s.
    expected_t1 = 20 * state.frame_ms / 1000.0 + len(residual) / (SAMPLE_RATE * 2)
    assert expected_t1 == pytest.approx(0.40625)
    assert segment.t1 == pytest.approx(expected_t1)
