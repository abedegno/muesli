# CPU-default image. For GPU, see README (CUDA base + WHISPER_DEVICE=cuda).
FROM python:3.14-slim

# faster-whisper (ctranslate2) needs libgomp at runtime. ffmpeg is used by the
# opt-in multitrack (channel-per-speaker) mode to probe/split audio channels.
RUN apt-get update && apt-get install -y --no-install-recommends libgomp1 ffmpeg \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
# Copy the package alongside pyproject.toml before installing: the manifest pins
# `[tool.setuptools] packages = ["whisper_app"]`, so the package must exist for
# `pip install .` to succeed (no auto-discovery, no deps-only pre-layer).
COPY pyproject.toml ./
COPY whisper_app ./whisper_app
RUN pip install --no-cache-dir .

ENV WHISPER_MODEL=base \
    WHISPER_DEVICE=cpu \
    WHISPER_COMPUTE_TYPE=int8 \
    PORT=8000

EXPOSE 8000
# --factory: create_app(Settings.from_env()) per process; single worker keeps the
# model cached in-process and cold start fast (scale-to-zero friendly).
CMD ["sh", "-c", "uvicorn whisper_app.main:app --factory --host 0.0.0.0 --port ${PORT}"]
