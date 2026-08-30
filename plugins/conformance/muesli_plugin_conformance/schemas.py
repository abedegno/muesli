"""JSON Schemas for validating plugin responses against the PINNED contract."""

INFO_SCHEMA = {
    "type": "object",
    "required": ["name", "version", "plugin_api", "kind", "config_schema"],
    "properties": {
        "name": {"type": "string", "minLength": 1},
        "version": {"type": "string", "minLength": 1},
        "plugin_api": {"const": 1},
        "kind": {"enum": ["transcriber", "agent", "streaming-transcriber"]},
        "config_schema": {"type": "object"},
    },
}

STREAM_READY_RESPONSE_SCHEMA = {
    "type": "object",
    "required": ["type"],
    "properties": {"type": {"const": "ready"}},
}

STREAM_SEGMENT_RESPONSE_SCHEMA = {
    "type": "object",
    "required": ["type", "final", "text", "t0", "t1", "speaker"],
    "properties": {
        "type": {"const": "segment"},
        "final": {"type": "boolean"},
        "text": {"type": "string"},
        "t0": {"type": "number"},
        "t1": {"type": "number"},
        "speaker": {"type": ["string", "null"]},
    },
}

STREAM_ERROR_RESPONSE_SCHEMA = {
    "type": "object",
    "required": ["type", "message"],
    "properties": {
        "type": {"const": "error"},
        "message": {"type": "string"},
    },
}

_SEGMENT = {
    "type": "object",
    "required": ["start_ms", "end_ms", "text", "source"],
    "properties": {
        "start_ms": {"type": "integer"},
        "end_ms": {"type": "integer"},
        "text": {"type": "string"},
        "source": {"type": "string"},
        "speaker": {"type": ["string", "null"]},
    },
}

TRANSCRIBE_RESPONSE_SCHEMA = {
    "type": "object",
    "required": ["segments", "language", "model", "duration_ms"],
    "properties": {
        "segments": {"type": "array", "items": _SEGMENT},
        "language": {"type": "string"},
        "model": {"type": "string"},
        "duration_ms": {"type": "integer"},
    },
}

GENERATE_RESPONSE_SCHEMA = {
    "type": "object",
    "required": ["summary", "model"],
    "properties": {
        "summary": {
            "type": "object",
            "required": ["sections"],
            "properties": {
                "sections": {
                    "type": "array",
                    "items": {
                        "type": "object",
                        "required": ["heading", "content_markdown"],
                        "properties": {
                            "heading": {"type": "string"},
                            "content_markdown": {"type": "string"},
                            # refs are 0-based integer indices into the transcript array.
                            "refs": {"type": ["array", "null"], "items": {"type": "integer"}},
                        },
                    },
                }
            },
        },
        "model": {"type": "string"},
    },
}
