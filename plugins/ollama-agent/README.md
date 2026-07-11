# Muesli Ollama agent plugin

Local meeting-summary agent. Builds a prompt from the template sections + the
user's notes + the transcript, calls a configurable model, and returns one
summary section per template section. Implements the Muesli agent contract
(`GET /info`, `GET /health`, `POST /generate`).

## Run (local Ollama — default, private)

    docker build -t muesli/ollama-agent .
    docker run -p 8000:8000 \
      -e MUESLI_PLUGIN_TOKEN=changeme \
      -e OLLAMA_URL=http://host.docker.internal:11434 \
      -e OLLAMA_MODEL=llama3.2 \
      muesli/ollama-agent

Pull a model first: `ollama pull llama3.2`.

## Run (BYO cloud — opt-in egress)

To use an OpenAI-compatible provider at container startup (instead of per-request
`base_url` config), set the following env vars:

    docker run -p 8000:8000 \
      -e MUESLI_PLUGIN_TOKEN=changeme \
      -e LLM_BASE_URL=https://api.openai.com \
      -e LLM_API_KEY=sk-... \
      muesli/ollama-agent

The plugin will use `LLM_BASE_URL` and `LLM_API_KEY` as defaults for all requests
that do not override them via the per-request `config` field.

## Environment variables

| Variable              | Default                  | Description                                                                                                                                                                           |
| --------------------- | ------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `MUESLI_PLUGIN_TOKEN` | _(none — required)_      | Bearer token the Muesli server sends; plugin refuses requests without it.                                                                                                             |
| `OLLAMA_URL`          | `http://localhost:11434` | Base URL of the local Ollama server (used when `base_url` is not set in request config and `LLM_BASE_URL` is empty).                                                                  |
| `OLLAMA_MODEL`        | `llama3.2`               | Default model name for local Ollama requests.                                                                                                                                         |
| `LLM_BASE_URL`        | _(empty)_                | Startup default for the OpenAI-compatible base URL. When set, the plugin routes generation requests to `{LLM_BASE_URL}/chat/completions` instead of Ollama. Empty = use local Ollama. |
| `LLM_API_KEY`         | _(empty)_                | Startup default API key sent as `Authorization: Bearer <key>` to the provider set by `LLM_BASE_URL`. Empty = no auth header added.                                                    |

## Config (`config` field on /generate, schema in /info)

| field       | meaning                                                  |
| ----------- | -------------------------------------------------------- |
| ollama_url  | local Ollama base URL (default http://localhost:11434)   |
| model       | model name                                               |
| base_url    | **opt-in** OpenAI-compatible base URL → sends to a cloud |
| api_key     | secret for base_url (server stores it encrypted)         |
| temperature | sampling temperature (default 0.2)                       |

## BYO cloud (opt-in egress)

Privacy default is local Ollama: nothing leaves the box. To use a cloud,
OpenAI-compatible API instead, set `base_url` (and `api_key`) in the plugin
config. This is a deliberate, surfaced egress choice — see spec §8. Leave
`base_url` empty to stay fully local.

Alternatively, set `LLM_BASE_URL` / `LLM_API_KEY` at container startup to
pre-configure the provider for all requests (operators can use this in
Docker Compose or Kubernetes without exposing secrets to end users).
