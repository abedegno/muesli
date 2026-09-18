# CPU-default image. For GPU, see README (CUDA base + PARAKEET_DEVICE=cuda).
FROM python:3.14-slim

# NeMo / torch often needs libgomp at runtime. The plugin itself only uses a
# temp file per request and pure Python HTTP clients.
RUN apt-get update && apt-get install -y --no-install-recommends libgomp1 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
# Copy the package alongside pyproject.toml before installing: the manifest pins
# `[tool.setuptools] packages = ["parakeet_app"]`, so the package must exist for
# `pip install .` to succeed (no auto-discovery, no deps-only pre-layer).
COPY pyproject.toml ./
COPY parakeet_app ./parakeet_app
RUN pip install --no-cache-dir .

ENV PARAKEET_MODEL=nvidia/parakeet-tdt-1.1b \
    PARAKEET_DEVICE=cpu \
    PORT=8000

EXPOSE 8000
# --factory: create_app(Settings.from_env()) per process; single worker keeps the
# model cached in-process and cold start fast (scale-to-zero friendly).
CMD ["sh", "-c", "uvicorn parakeet_app.main:app --factory --host 0.0.0.0 --port ${PORT}"]
