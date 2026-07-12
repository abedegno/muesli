def test_info_shape(client, auth_headers):
    r = client.get("/info", headers=auth_headers)
    assert r.status_code == 200
    body = r.json()
    assert body["name"] == "muesli-streaming-transcriber"
    assert body["plugin_api"] == 1
    assert body["kind"] == "streaming-transcriber"
    assert isinstance(body["version"], str) and body["version"]
    schema = body["config_schema"]
    assert schema["type"] == "object"
    assert set(["model", "device", "compute_type", "vad_aggressiveness", "silence_threshold_ms"]).issubset(
        schema["properties"].keys()
    )
    assert schema["additionalProperties"] is False


def test_health_ok_without_auth(client):
    r = client.get("/health")
    assert r.status_code == 200
    assert r.json() == {"status": "ok"}
