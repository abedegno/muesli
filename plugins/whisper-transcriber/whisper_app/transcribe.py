import base64
import json
import logging
import os
import subprocess
import tempfile
import urllib.parse

import httpx

from . import download_state
from .config import Settings
from .schema import Segment, TranscribeRequest, TranscribeResponse, WordTimestamp

logger = logging.getLogger(__name__)

# Percentage milestones at which we emit an INFO log line.
_PROGRESS_MILESTONES = {25, 50, 75, 100}

# Channels at or below this level (dBFS, per ffmpeg's volumedetect filter) are
# treated as near-silent and skipped in multitrack mode.
_MULTITRACK_SILENCE_THRESHOLD_DBFS = -60.0


def load_model(model: str, device: str, compute_type: str):
    """Lazily import and construct the faster-whisper model. Patched in tests so
    CI never downloads weights."""
    from faster_whisper import WhisperModel

    state = download_state.get_state()
    state.update(status="downloading", model=model, percent=0)
    logger.info("Download started: model=%s device=%s compute_type=%s", model, device, compute_type)

    logged_milestones: set[int] = set()

    # --- Best-effort tqdm monkey-patch so we can track huggingface_hub progress ---
    # huggingface_hub wraps tqdm internally; we replace it temporarily so each
    # update() call flows into our DownloadState.  If the internal API ever
    # changes (different module path, etc.) the try/except swallows the failure
    # and the model still loads — progress simply stays at 0%.
    _orig_tqdm = None
    try:
        import huggingface_hub.utils._tqdm as _hf_tqdm_mod  # type: ignore[import]

        _orig_cls = _hf_tqdm_mod.tqdm

        class _ProgressTqdm(_orig_cls):  # type: ignore[valid-type]
            def update(self, n=1):  # type: ignore[override]
                super().update(n)
                try:
                    total = self.total or 0
                    completed = self.n or 0
                    pct = min(100, int(completed * 100 / total)) if total > 0 else 0
                    state.update(
                        status="downloading",
                        model=model,
                        percent=pct,
                        bytes_downloaded=completed,
                        bytes_total=total,
                    )
                    for milestone in sorted(_PROGRESS_MILESTONES):
                        if pct >= milestone and milestone not in logged_milestones:
                            logged_milestones.add(milestone)
                            logger.info(
                                "Download progress: model=%s %d%%", model, milestone
                            )
                except Exception:  # pragma: no cover — defensive
                    pass

        _orig_tqdm = _orig_cls
        _hf_tqdm_mod.tqdm = _ProgressTqdm
    except Exception:  # pragma: no cover — huggingface_hub not installed or API changed
        pass

    try:
        result = WhisperModel(model, device=device, compute_type=compute_type)
    finally:
        # Restore original tqdm regardless of whether the model load succeeded.
        if _orig_tqdm is not None:
            try:
                import huggingface_hub.utils._tqdm as _hf_tqdm_mod  # type: ignore[import]
                _hf_tqdm_mod.tqdm = _orig_tqdm
            except Exception:  # pragma: no cover
                pass

    state.update(status="ready", model=model, percent=100)
    logger.info("Download complete: model=%s", model)
    return result


def load_diarization_pipeline(model_name: str, hf_token: str):
    """Load a pyannote speaker-diarization pipeline.

    This is a module-level function so tests can monkeypatch it without
    importing pyannote.audio at import time.
    """
    from pyannote.audio import Pipeline  # type: ignore[import]

    return Pipeline.from_pretrained(model_name, use_auth_token=hf_token)


def _decode_data_url(audio_url: str) -> bytes:
    """Decode an RFC 2397 ``data:`` URL into raw bytes.

    Branches on the ``;base64`` marker in the media-type segment: base64 payloads
    are b64-decoded, everything else is treated as percent-encoded text. Raises on
    anything that is not a well-formed ``data:`` URL.
    """
    if not audio_url.startswith("data:"):
        raise ValueError(f"not a data: URL: {audio_url[:32]!r}")
    try:
        meta, payload = audio_url[len("data:"):].split(",", 1)
    except ValueError as exc:  # no comma -> malformed
        raise ValueError("malformed data: URL (missing comma)") from exc
    if meta.endswith(";base64"):
        try:
            # validate=True so non-base64 garbage raises instead of being silently stripped.
            return base64.b64decode(payload, validate=True)
        except ValueError as exc:  # binascii.Error subclasses ValueError
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


def _assign_speakers(segments: list[Segment], diarization) -> list[Segment]:
    """Return new Segment list with speaker labels and diarization confidence.

    Confidence is best_overlap / segment_duration (clamped [0, 1]). A value
    close to 1.0 means the top diarization turn covers most of the segment;
    0.0 means no diarization turn overlapped at all.
    """
    result = []
    for seg in segments:
        start_s = seg.start_ms / 1000.0
        end_s = seg.end_ms / 1000.0
        duration = end_s - start_s
        best_speaker: str | None = None
        best_overlap = 0.0
        for turn, _, speaker in diarization.itertracks(yield_label=True):
            overlap = min(turn.end, end_s) - max(turn.start, start_s)
            if overlap > best_overlap:
                best_overlap = overlap
                best_speaker = speaker
        confidence = min(1.0, best_overlap / duration) if duration > 0 else 0.0
        result.append(seg.model_copy(update={"speaker": best_speaker, "confidence": confidence}))
    return result


# ---------------------------------------------------------------------------
# Multitrack (channel-per-speaker) support
# ---------------------------------------------------------------------------
#
# Opt-in via req.config["multitrack"]. When the source audio has more than one
# channel, each channel is probed for silence, split to its own mono WAV via
# ffmpeg, and transcribed independently through the same faster-whisper model
# used for the single-pass path. Segments from all channels are then merged,
# sorted by start_ms, and labelled with a deterministic per-channel speaker
# ("Speaker 1", "Speaker 2", ...). This is orthogonal to the diarization
# feature above -- it relies on channel isolation in the source recording
# rather than voice-activity modelling.


def probe_channel_count(path: str) -> int:
    """Return the number of audio channels in the file at `path` via ffprobe.

    Module-level so tests can monkeypatch it without invoking a real ffprobe
    binary. Raises if ffprobe is missing or the file has no audio stream.
    """
    result = subprocess.run(
        [
            "ffprobe",
            "-v", "error",
            "-select_streams", "a:0",
            "-show_entries", "stream=channels",
            "-of", "json",
            path,
        ],
        capture_output=True,
        text=True,
        check=True,
    )
    data = json.loads(result.stdout)
    streams = data.get("streams") or []
    if not streams:
        return 1
    return int(streams[0].get("channels", 1))


def measure_channel_volume_dbfs(path: str, channel_index: int) -> tuple[float, float]:
    """Return (mean_volume, max_volume) in dBFS for one channel of `path`.

    Splits the channel out with ffmpeg's `pan` filter and measures it with
    `volumedetect`, which logs its result to stderr (ffmpeg writes its normal
    progress/filter output there regardless of exit status). Module-level so
    tests can monkeypatch it without invoking a real ffmpeg binary.

    IMPORTANT: `-filter:a` and `-af` are the same ffmpeg option (aliases for
    the audio filter chain on the primary output) -- passing both means the
    later flag silently wins and discards the other, so both stages MUST be
    combined into a single comma-separated filtergraph on one `-af`/`-filter:a`
    flag (`pan` then `volumedetect`, applied in sequence), never as two
    separate filter flags.
    """
    result = subprocess.run(
        [
            "ffmpeg",
            "-i", path,
            "-af", f"pan=mono|c0=c{channel_index},volumedetect",
            "-f", "null",
            "-",
        ],
        capture_output=True,
        text=True,
    )
    mean_volume = 0.0
    max_volume = 0.0
    for line in result.stderr.splitlines():
        line = line.strip()
        if "mean_volume:" in line:
            mean_volume = float(line.split("mean_volume:")[1].strip().split(" ")[0])
        elif "max_volume:" in line:
            max_volume = float(line.split("max_volume:")[1].strip().split(" ")[0])
    return mean_volume, max_volume


def _is_near_silent(mean_volume: float, max_volume: float) -> bool:
    return (
        mean_volume <= _MULTITRACK_SILENCE_THRESHOLD_DBFS
        or max_volume <= _MULTITRACK_SILENCE_THRESHOLD_DBFS
    )


def split_channel(path: str, channel_index: int) -> str:
    """Split one channel out of `path` into its own mono WAV tempfile and
    return its path. Caller is responsible for os.unlink-ing the result on
    success. If the ffmpeg invocation itself fails, this function cleans up
    the tempfile it just created before re-raising, so no partial/empty file
    is ever left behind for the caller to leak. Module-level so tests can
    monkeypatch it without invoking real ffmpeg.
    """
    fd, out_path = tempfile.mkstemp(suffix=f".ch{channel_index}.wav")
    os.close(fd)
    try:
        subprocess.run(
            [
                "ffmpeg",
                "-y",
                "-i", path,
                "-filter:a", f"pan=mono|c0=c{channel_index}",
                out_path,
            ],
            capture_output=True,
            check=True,
        )
    except Exception:
        try:
            os.unlink(out_path)
        except OSError:
            pass
        raise
    return out_path


def _run_multitrack(
    path: str,
    channel_count: int,
    model,
    model_name: str,
    req: TranscribeRequest,
    beam_size: int,
    use_words: bool,
) -> TranscribeResponse:
    all_segments: list[Segment] = []
    language = req.language_hint or ""
    duration_s = 0.0

    for channel in range(channel_count):
        mean_volume, max_volume = measure_channel_volume_dbfs(path, channel)
        if _is_near_silent(mean_volume, max_volume):
            logger.info(
                "multitrack: channel %d is near-silent (mean=%.1f max=%.1f dBFS) — skipping",
                channel, mean_volume, max_volume,
            )
            continue

        speaker_label = "You" if channel == 0 else "Them" if channel == 1 else f"Speaker {channel + 1}"
        channel_source = "mic" if channel == 0 else "system" if channel == 1 else f"channel {channel}"

        channel_path = split_channel(path, channel)
        try:
            if use_words:
                segments_iter, info = model.transcribe(
                    channel_path, language=req.language_hint, beam_size=beam_size, word_timestamps=True
                )
            else:
                segments_iter, info = model.transcribe(
                    channel_path, language=req.language_hint, beam_size=beam_size
                )

            for s in segments_iter:
                words = []
                if use_words and s.words:
                    words = [
                        WordTimestamp(
                            text=w.word.strip(),
                            start_ms=int(round(w.start * 1000)),
                            end_ms=int(round(w.end * 1000)),
                        )
                        for w in s.words
                    ]
                all_segments.append(
                    Segment(
                        start_ms=int(round(s.start * 1000)),
                        end_ms=int(round(s.end * 1000)),
                        text=s.text.strip(),
                        source=channel_source,
                        speaker=speaker_label,
                        words=words or None,
                    )
                )
            language = getattr(info, "language", language) or language
            duration_s = max(duration_s, getattr(info, "duration", 0.0))
        finally:
            os.unlink(channel_path)

    all_segments.sort(key=lambda seg: seg.start_ms)

    return TranscribeResponse(
        segments=all_segments,
        language=language,
        model=model_name,
        duration_ms=int(round(duration_s * 1000)),
    )


def run_transcribe(req: TranscribeRequest, settings: Settings) -> TranscribeResponse:
    model_name = req.config.get("model") or settings.default_model
    beam_size = int(req.config.get("beam_size", 5))
    raw_compute_type = req.config.get("compute_type")
    compute_type = settings.compute_type if (not raw_compute_type or raw_compute_type == "default") else raw_compute_type
    # CPU safety fallback: float16 requires a CUDA device.
    if settings.device == "cpu" and compute_type == "float16":
        compute_type = "int8"
    model = load_model(model_name, settings.device, compute_type)

    use_words = settings.word_timestamps_enabled

    path = _download(req.audio_url)
    try:
        # --- Optional multitrack (channel-per-speaker) stage ---
        # Opt-in and strictly additive: only takes over when requested AND the
        # source has more than one channel. Any other case (disabled, mono
        # input, or a failed probe) falls through unchanged to the existing
        # single-pass code below.
        if req.config.get("multitrack"):
            try:
                channel_count = probe_channel_count(path)
            except Exception:
                logger.warning(
                    "multitrack: ffprobe channel-count probe failed — falling back to single-pass",
                    exc_info=True,
                )
                channel_count = 1
            if channel_count > 1:
                return _run_multitrack(
                    path, channel_count, model, model_name, req, beam_size, use_words
                )

        # language_hint is forwarded as the language argument so Whisper skips auto-detection
        if use_words:
            segments_iter, info = model.transcribe(
                path, language=req.language_hint, beam_size=beam_size, word_timestamps=True
            )
        else:
            segments_iter, info = model.transcribe(
                path, language=req.language_hint, beam_size=beam_size
            )

        out: list[Segment] = []
        chunk_error: Exception | None = None
        seg_iter = iter(segments_iter)
        while True:
            try:
                s = next(seg_iter)
            except StopIteration:
                break
            except Exception as exc:
                chunk_error = exc
                break
            words = []
            if use_words and s.words:
                words = [
                    WordTimestamp(
                        text=w.word.strip(),
                        start_ms=int(round(w.start * 1000)),
                        end_ms=int(round(w.end * 1000)),
                    )
                    for w in s.words
                ]
            out.append(
                Segment(
                    start_ms=int(round(s.start * 1000)),
                    end_ms=int(round(s.end * 1000)),
                    text=s.text.strip(),
                    source="mixed",
                    words=words or None,   # None when empty -> omitted by exclude_none
                )
            )

        # --- Optional speaker-diarization stage (only on complete transcriptions) ---
        if chunk_error is None and settings.diarization_enabled and settings.diarization_hf_token:
            try:
                pipeline = load_diarization_pipeline(
                    settings.diarization_model, settings.diarization_hf_token
                )
                diarization = pipeline(path)
                out = _assign_speakers(out, diarization)
            except Exception:
                logger.warning(
                    "Speaker diarization failed — continuing without speaker labels",
                    exc_info=True,
                )
    finally:
        os.unlink(path)

    if chunk_error is not None:
        if not out:  # first-chunk failure — no segments at all
            raise chunk_error
        # Partial: some chunks succeeded before the error
        return TranscribeResponse(
            segments=out,
            language=getattr(info, "language", req.language_hint or ""),
            model=model_name,
            duration_ms=int(round(getattr(info, "duration", 0.0) * 1000)),
            partial=True,
            partial_reason=str(chunk_error),
        )

    return TranscribeResponse(
        segments=out,
        language=getattr(info, "language", req.language_hint or ""),
        model=model_name,
        duration_ms=int(round(getattr(info, "duration", 0.0) * 1000)),
    )
