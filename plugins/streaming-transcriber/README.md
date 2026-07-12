# Muesli Streaming Transcriber

Reference transcriber plugin for the streaming transcription protocol used by
Slice 2 of issue #107.

## What this plugin implements

- `GET /info` - bearer-authenticated metadata endpoint.
- `GET /health` - unauthenticated liveness probe.
- `GET /stream` - bearer-authenticated websocket endpoint that accepts a
  `start` control message, raw 16 kHz mono PCM `s16le` audio frames, and a final
  `stop` control message.

The plugin segments incoming audio with `webrtcvad` and transcribes each
completed utterance with `faster-whisper`.

## Runtime configuration

The plugin reads these environment variables:

- `MUESLI_PLUGIN_TOKEN` - bearer token expected on authenticated requests.
- `STREAMING_TRANSCRIBER_MODEL` - faster-whisper model name or path.
- `STREAMING_TRANSCRIBER_DEVICE` - `cpu` or `cuda`.
- `STREAMING_TRANSCRIBER_COMPUTE_TYPE` - faster-whisper compute type.
- `STREAMING_TRANSCRIBER_VAD_AGGRESSIVENESS` - integer `0` to `3`.
- `STREAMING_TRANSCRIBER_SILENCE_THRESHOLD_MS` - trailing silence required to
  flush a segment.
- `STREAMING_TRANSCRIBER_MIN_SPEECH_MS` - minimum speech duration before a
  buffered utterance is emitted.

Defaults are chosen for local CPU use and testability. Production deployments
can override them per environment.

## HTTP contract

### `GET /info`

Request headers:

- `Authorization: Bearer <token>`
- `X-Muesli-Plugin-API: 1`

Response body:

```json
{
  "name": "muesli-streaming-transcriber",
  "version": "0.1.0",
  "plugin_api": 1,
  "kind": "streaming-transcriber",
  "config_schema": {
    "type": "object",
    "properties": { "...": "..." },
    "additionalProperties": false
  }
}
```

`config_schema` is a JSON Schema object describing the plugin config stored by
the server. The schema in this reference plugin includes model, device, compute
type, and VAD tuning knobs.

### `GET /health`

No auth required. Returns `{"status":"ok"}`.

## Websocket streaming protocol

### Authentication

The websocket handshake uses the same bearer envelope as the HTTP endpoints:

- `Authorization: Bearer <token>`
- `X-Muesli-Plugin-API: 1`

The server should include those headers on the websocket handshake request.
If the token or API version is missing or wrong, the plugin rejects the
handshake.

### Message flow

1. Server opens `GET /stream`.
2. Plugin authenticates the handshake.
3. Server sends the first websocket text frame: a JSON `start` message.
4. Plugin validates the `start` payload and immediately replies with
   `{"type":"ready"}`.
5. Server sends binary websocket frames containing raw 16 kHz mono PCM
   `s16le` audio.
6. When the plugin sees trailing silence beyond the configured threshold, it
   finalizes the current utterance, transcribes it, and sends a
   `segment` message.
7. Server sends a final JSON `stop` message.
8. Plugin flushes any buffered speech as one final segment, then closes the
   websocket cleanly.

### `start` message

The first text frame must be:

```json
{
  "type": "start",
  "language_hint": "en",
  "options": {},
  "config": {},
  "sample_rate": 16000,
  "channels": 1
}
```

Fields:

- `type` - required string, must be `"start"`.
- `language_hint` - optional string language hint.
- `options` - optional object for per-request knobs.
- `config` - optional object for the plugin config snapshot used by this stream.
- `sample_rate` - required integer, must be `16000`.
- `channels` - required integer, must be `1`.

The plugin expects binary audio frames to be raw PCM `s16le` matching that
shape. In tests we use ~200 ms frames, but the server may batch frames however it
wants as long as they are PCM-aligned.

### `segment` message

When an utterance is complete, the plugin sends:

```json
{
  "type": "segment",
  "final": true,
  "text": "hello world",
  "t0": 1.2,
  "t1": 3.8,
  "speaker": null
}
```

Fields:

- `type` - `"segment"`.
- `final` - boolean. This slice only emits `true`, but `false` is reserved and
  remains schema-compatible.
- `text` - transcription text for the utterance.
- `t0` - float start time in seconds from stream start.
- `t1` - float end time in seconds from stream start.
- `speaker` - null in this slice.

### `ready` message

Immediately after a valid `start` message, the plugin sends:

```json
{ "type": "ready" }
```

### `stop` message

The final control message is a JSON text frame:

```json
{ "type": "stop" }
```

On `stop`, the plugin flushes any buffered speech as one last final segment and
then closes the websocket cleanly.

### Error handling

If the `start` payload is invalid, the sample rate/channels are wrong, a binary
frame arrives before `start`, or transcription fails, the plugin sends:

```json
{ "type": "error", "message": "..." }
```

and closes the websocket.

## Local development

```bash
cd plugins/streaming-transcriber
python -m pip install --upgrade pip
pip install -e '.[test]'
pytest
```
