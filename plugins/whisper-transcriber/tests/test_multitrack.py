"""Tests for the opt-in multitrack (channel-per-speaker) transcription path.

ffmpeg/ffprobe and the whisper model are all mocked — no real audio files, no
real subprocess invocations, no real model downloads.
"""
import base64
import os
import subprocess

import pytest

from whisper_app import transcribe as transcribe_mod
from whisper_app.schema import TranscribeRequest
from whisper_app.transcribe import run_transcribe

# Minimal valid WAV (44 bytes) encoded as a data: URL — the fake model never
# decodes the audio, but _download() must succeed.
_MINIMAL_WAV = bytes([
    0x52, 0x49, 0x46, 0x46,  # "RIFF"
    0x24, 0x00, 0x00, 0x00,  # chunk size = 36
    0x57, 0x41, 0x56, 0x45,  # "WAVE"
    0x66, 0x6D, 0x74, 0x20,  # "fmt "
    0x10, 0x00, 0x00, 0x00,  # subchunk1 size = 16
    0x01, 0x00,               # PCM format
    0x01, 0x00,               # 1 channel
    0x44, 0xAC, 0x00, 0x00,  # 44100 Hz
    0x88, 0x58, 0x01, 0x00,  # byte rate
    0x02, 0x00,               # block align
    0x10, 0x00,               # bits per sample = 16
    0x64, 0x61, 0x74, 0x61,  # "data"
    0x00, 0x00, 0x00, 0x00,  # data size = 0
])
_AUDIO_URL = f"data:audio/wav;base64,{base64.b64encode(_MINIMAL_WAV).decode()}"


def _seg(start, end, text):
    """Mimics a faster_whisper segment object."""
    return type("S", (), {"start": start, "end": end, "text": text, "words": None})()


def _fake_split_factory():
    """Returns (fake_split, calls) — fake_split creates a real tempfile so
    os.unlink() in production code works unmodified, and tags the path with
    the channel index so the fake model can key segments off it."""
    calls: list[tuple[str, int]] = []

    def fake_split(path: str, channel_index: int) -> str:
        calls.append((path, channel_index))
        import tempfile
        fd, out = tempfile.mkstemp(suffix=f".ch{channel_index}.wav")
        os.close(fd)
        return out

    return fake_split, calls


class _ChannelAwareModel:
    """Stand-in for faster_whisper.WhisperModel — returns different segments
    depending on which channel-split path it's asked to transcribe."""

    def __init__(self, segments_by_channel: dict[int, list]):
        self._segments_by_channel = segments_by_channel
        self.transcribed_paths: list[str] = []

    def load(self, model, device, compute_type):
        self.loaded_with = (model, device, compute_type)
        return self

    def transcribe(self, audio_path, language=None, beam_size=5, word_timestamps=False):
        self.transcribed_paths.append(audio_path)
        for channel_index, segs in self._segments_by_channel.items():
            if f".ch{channel_index}.wav" in audio_path:
                info = type("Info", (), {"language": language or "en", "duration": 2.0})()
                return iter(segs), info
        raise AssertionError(f"unexpected transcribe path: {audio_path}")


@pytest.fixture
def multitrack_model(monkeypatch):
    """Two channels: channel 0 has one segment, channel 1 has one segment
    that starts earlier — used to exercise interleaving by start_ms."""
    fake = _ChannelAwareModel({
        0: [_seg(1.0, 1.5, "hello from mic one")],
        1: [_seg(0.2, 0.6, "hello from mic two")],
    })
    monkeypatch.setattr(transcribe_mod, "load_model", lambda m, d, c: fake.load(m, d, c))
    return fake


def test_multitrack_splits_each_non_silent_channel(monkeypatch, settings, multitrack_model):
    fake_split, split_calls = _fake_split_factory()
    monkeypatch.setattr(transcribe_mod, "probe_channel_count", lambda path: 2)
    monkeypatch.setattr(transcribe_mod, "measure_channel_volume_dbfs", lambda path, ch: (-10.0, -5.0))
    monkeypatch.setattr(transcribe_mod, "split_channel", fake_split)

    req = TranscribeRequest(audio_url=_AUDIO_URL, config={"multitrack": True})
    resp = run_transcribe(req, settings)

    assert [ch for (_path, ch) in split_calls] == [0, 1]
    assert len(resp.segments) == 2


def test_near_silent_channel_is_skipped(monkeypatch, settings, multitrack_model):
    fake_split, split_calls = _fake_split_factory()
    monkeypatch.setattr(transcribe_mod, "probe_channel_count", lambda path: 2)

    def fake_volume(path, channel_index):
        if channel_index == 0:
            return (-10.0, -5.0)  # loud — keep
        return (-65.0, -62.0)  # near-silent — skip

    monkeypatch.setattr(transcribe_mod, "measure_channel_volume_dbfs", fake_volume)
    monkeypatch.setattr(transcribe_mod, "split_channel", fake_split)

    req = TranscribeRequest(audio_url=_AUDIO_URL, config={"multitrack": True})
    resp = run_transcribe(req, settings)

    # Only channel 0 was split and transcribed.
    assert [ch for (_path, ch) in split_calls] == [0]
    assert len(resp.segments) == 1
    assert resp.segments[0].text == "hello from mic one"
    assert resp.segments[0].speaker == "You"
    assert resp.segments[0].source == "mic"


def test_merged_segments_interleaved_by_start_ms_with_speaker_labels(
    monkeypatch, settings, multitrack_model
):
    fake_split, _calls = _fake_split_factory()
    monkeypatch.setattr(transcribe_mod, "probe_channel_count", lambda path: 2)
    monkeypatch.setattr(transcribe_mod, "measure_channel_volume_dbfs", lambda path, ch: (-10.0, -5.0))
    monkeypatch.setattr(transcribe_mod, "split_channel", fake_split)

    req = TranscribeRequest(audio_url=_AUDIO_URL, config={"multitrack": True})
    resp = run_transcribe(req, settings)

    assert len(resp.segments) == 2
    # Channel 1's segment starts at 0.2s (200ms), channel 0's at 1.0s (1000ms):
    # merged output must be sorted by start_ms across channels.
    assert resp.segments[0].start_ms == 200
    assert resp.segments[0].text == "hello from mic two"
    assert resp.segments[0].speaker == "Them"
    assert resp.segments[0].source == "system"
    assert resp.segments[1].start_ms == 1000
    assert resp.segments[1].text == "hello from mic one"
    assert resp.segments[1].speaker == "You"
    assert resp.segments[1].source == "mic"


def test_multitrack_disabled_takes_single_pass_path(monkeypatch, settings, fake_model):
    """multitrack absent from config: ffprobe/ffmpeg helpers must never be
    invoked, and behavior must match the existing single-pass output."""

    def _must_not_be_called(*args, **kwargs):
        raise AssertionError("probe_channel_count must not be called when multitrack is not requested")

    monkeypatch.setattr(transcribe_mod, "probe_channel_count", _must_not_be_called)
    monkeypatch.setattr(transcribe_mod, "measure_channel_volume_dbfs", _must_not_be_called)
    monkeypatch.setattr(transcribe_mod, "split_channel", _must_not_be_called)

    req = TranscribeRequest(audio_url=_AUDIO_URL, config={})
    resp = run_transcribe(req, settings)

    assert len(resp.segments) == 2
    assert resp.segments[0].text == "hello"
    assert resp.segments[0].source == "mixed"
    assert resp.segments[0].speaker is None


def test_multitrack_enabled_but_single_channel_takes_single_pass_path(
    monkeypatch, settings, fake_model
):
    """multitrack=True but ffprobe reports a single channel: falls through to
    the existing single-pass path unchanged (still probes once to check)."""
    probe_calls = []

    def fake_probe(path):
        probe_calls.append(path)
        return 1

    def _must_not_be_called(*args, **kwargs):
        raise AssertionError("must not be called for a single-channel input")

    monkeypatch.setattr(transcribe_mod, "probe_channel_count", fake_probe)
    monkeypatch.setattr(transcribe_mod, "measure_channel_volume_dbfs", _must_not_be_called)
    monkeypatch.setattr(transcribe_mod, "split_channel", _must_not_be_called)

    req = TranscribeRequest(audio_url=_AUDIO_URL, config={"multitrack": True})
    resp = run_transcribe(req, settings)

    assert len(probe_calls) == 1
    assert len(resp.segments) == 2
    assert resp.segments[0].text == "hello"
    assert resp.segments[0].speaker is None


def test_multitrack_probe_failure_falls_back_to_single_pass(monkeypatch, settings, fake_model):
    """If ffprobe itself raises (e.g. binary missing), fall back gracefully to
    the existing single-pass path rather than erroring the whole request."""

    def fake_probe(path):
        raise FileNotFoundError("ffprobe not found")

    monkeypatch.setattr(transcribe_mod, "probe_channel_count", fake_probe)

    req = TranscribeRequest(audio_url=_AUDIO_URL, config={"multitrack": True})
    resp = run_transcribe(req, settings)

    assert len(resp.segments) == 2
    assert resp.segments[0].text == "hello"


# ---------------------------------------------------------------------------
# Regression tests for cross-review findings
# ---------------------------------------------------------------------------


def test_measure_channel_volume_uses_single_chained_filter(monkeypatch):
    """`-filter:a` and `-af` are the same ffmpeg option — passing both means
    the second one silently wins and discards the first, so `pan` and
    `volumedetect` MUST be combined into a single comma-separated filtergraph
    on one filter flag, not passed as two separate/competing filter flags."""
    captured = {}

    class _FakeResult:
        stdout = ""
        stderr = "mean_volume: -20.0 dB\nmax_volume: -10.0 dB\n"

    def fake_run(cmd, **kwargs):
        captured["cmd"] = cmd
        return _FakeResult()

    monkeypatch.setattr(transcribe_mod.subprocess, "run", fake_run)

    mean_v, max_v = transcribe_mod.measure_channel_volume_dbfs("/tmp/in.wav", 1)

    assert mean_v == -20.0
    assert max_v == -10.0

    cmd = captured["cmd"]
    filter_flag_positions = [i for i, arg in enumerate(cmd) if arg in ("-af", "-filter:a")]
    assert len(filter_flag_positions) == 1, (
        f"expected exactly one audio-filter flag (-af/-filter:a), got argv: {cmd!r}"
    )
    filter_value = cmd[filter_flag_positions[0] + 1]
    assert "pan=mono|c0=c1" in filter_value
    assert "volumedetect" in filter_value
    # Both stages must be chained together in ONE filtergraph (comma-separated
    # pan,volumedetect), not split across two competing filter flags.
    assert "," in filter_value


def test_split_channel_cleans_up_tempfile_on_ffmpeg_failure(monkeypatch):
    """If the ffmpeg invocation inside split_channel() fails, the tempfile it
    just created must not be left behind on disk."""
    created_paths: list[str] = []
    orig_mkstemp = transcribe_mod.tempfile.mkstemp

    def spy_mkstemp(*args, **kwargs):
        fd, out_path = orig_mkstemp(*args, **kwargs)
        created_paths.append(out_path)
        return fd, out_path

    def fake_run(cmd, **kwargs):
        raise subprocess.CalledProcessError(1, cmd)

    monkeypatch.setattr(transcribe_mod.tempfile, "mkstemp", spy_mkstemp)
    monkeypatch.setattr(transcribe_mod.subprocess, "run", fake_run)

    with pytest.raises(subprocess.CalledProcessError):
        transcribe_mod.split_channel("/tmp/in.wav", 0)

    assert len(created_paths) == 1
    assert not os.path.exists(created_paths[0]), (
        "leftover tempfile from a failed channel split must be cleaned up, not leaked"
    )


def test_config_schema_exposes_multitrack(client, auth_headers):
    r = client.get("/info", headers=auth_headers)
    assert r.status_code == 200
    schema = r.json()["config_schema"]
    assert "multitrack" in schema["properties"]
    prop = schema["properties"]["multitrack"]
    assert prop["type"] == "boolean"
    assert prop["default"] is False
