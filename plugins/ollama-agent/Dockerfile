FROM python:3.14-slim

WORKDIR /app
# Copy the package alongside pyproject.toml before installing: the manifest pins
# `[tool.setuptools] packages = ["ollama_app"]`, so the package must exist for
# `pip install .` to succeed (no auto-discovery, no deps-only pre-layer).
COPY pyproject.toml ./
COPY ollama_app ./ollama_app
RUN pip install --no-cache-dir .

ENV OLLAMA_URL=http://localhost:11434 \
    OLLAMA_MODEL=llama3.2 \
    PORT=8000

EXPOSE 8000
CMD ["sh", "-c", "uvicorn ollama_app.main:app --factory --host 0.0.0.0 --port ${PORT}"]
