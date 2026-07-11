# Muesli Parakeet transcriber plugin

Local transcription via [NVIDIA NeMo / Parakeet](https://developer.nvidia.com/nvidia-nemo).
Implements the Muesli transcriber contract (`GET /info`, `GET /health`, `POST /transcribe`).

## Run

    docker build -t muesli/parakeet-transcriber .
    docker run -p 8000:8000 -e MUESLI_PLUGIN_TOKEN=changeme muesli/parakeet-transcriber

The Muesli server sends `Authorization: Bearer $MUESLI_PLUGIN_TOKEN` and
`X-Muesli-Plugin-API: 1` on every request.

## Config (`config` field on /transcribe, schema in /info)

| field  | meaning                                                        |
| ------ | -------------------------------------------------------------- |
| model  | NeMo/Parakeet model name or path                               |
| device | inference device (`cpu` or `cuda`; `cuda` is the intended run) |

## GPU

Parakeet / NeMo is CUDA-oriented. CPU inference is impractical for anything
beyond smoke tests. Build on a CUDA-enabled base image such as
`nvidia/cuda:12-cudnn-runtime` or `nvcr.io/nvidia/nemo:<tag>`, install the
`nemo_toolkit[asr]` extra, and run with `PARAKEET_DEVICE=cuda`.

Expect the larger Parakeet checkpoints to want multiple GB of VRAM; choose the
smallest model that meets your latency and accuracy needs.

## Scale-to-zero

The service is stateless (no DB, no disk state beyond a temp file per request,
deleted immediately). Run a single uvicorn worker so the model stays cached
in-process; deploy on Cloud Run / KEDA with min-instances 0. Cold start = process
start + first model load; keep the model small or bake weights into the image to
cut cold-start latency.
