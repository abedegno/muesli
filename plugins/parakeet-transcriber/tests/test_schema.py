from parakeet_app.schema import Segment, TranscribeRequest, TranscribeResponse


def test_request_parses_minimal():
    req = TranscribeRequest(audio_url="https://store/a.wav", config={})
    assert req.audio_url == "https://store/a.wav"
    assert req.language_hint is None
    assert req.options == {}


def test_segment_defaults_source_mixed():
    seg = Segment(start_ms=0, end_ms=500, text="hi")
    assert seg.source == "mixed"
    assert seg.speaker is None


def test_response_roundtrips():
    resp = TranscribeResponse(
        segments=[Segment(start_ms=0, end_ms=10, text="x")],
        language="en",
        model="nvidia/parakeet-tdt-1.1b",
        duration_ms=10,
    )
    dumped = resp.model_dump(exclude_none=True)
    assert dumped["segments"][0]["source"] == "mixed"
    assert "speaker" not in dumped["segments"][0]
    assert dumped["language"] == "en"
