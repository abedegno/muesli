FROM python:3.12-slim

WORKDIR /app

# Copy the package alongside pyproject.toml before installing: the manifest pins
# the package list explicitly, so setuptools will not try to discover anything.
COPY pyproject.toml ./
COPY streaming_app ./streaming_app

RUN pip install --no-cache-dir -e .

EXPOSE 8000

CMD ["uvicorn", "streaming_app.main:app", "--factory", "--host", "0.0.0.0", "--port", "8000"]
