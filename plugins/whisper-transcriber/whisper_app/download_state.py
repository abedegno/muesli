"""Thread-safe download-progress tracker for model weights.

A module-level singleton (``_state``) is created at import time and shared
between the FastAPI app and the ``load_model`` function.  The app exposes it
on ``GET /status`` so the Go server (and ultimately the Electron UI) can poll
for first-run download progress.
"""
import threading
from dataclasses import dataclass, field


@dataclass
class DownloadState:
    """Holds the current model-download state.

    All public attributes are protected by a ``threading.Lock`` so they can be
    written from the background model-load thread and read from the ASGI worker
    thread without tearing.
    """

    status: str = "idle"  # 'idle' | 'downloading' | 'ready'
    model: str = ""
    percent: int = 0
    bytes_downloaded: int = 0
    bytes_total: int = 0
    _lock: threading.Lock = field(
        default_factory=threading.Lock, repr=False, compare=False
    )

    def update(
        self,
        status: str,
        model: str = "",
        percent: int = 0,
        bytes_downloaded: int = 0,
        bytes_total: int = 0,
    ) -> None:
        """Atomically replace all fields."""
        with self._lock:
            self.status = status
            self.model = model
            self.percent = percent
            self.bytes_downloaded = bytes_downloaded
            self.bytes_total = bytes_total

    def snapshot(self) -> dict:
        """Return a consistent read of the three public API fields."""
        with self._lock:
            return {
                "status": self.status,
                "model": self.model,
                "percent": self.percent,
            }


# Module-level singleton — shared across the entire process lifetime.
_state = DownloadState()


def get_state() -> DownloadState:
    """Return the module-level singleton."""
    return _state
