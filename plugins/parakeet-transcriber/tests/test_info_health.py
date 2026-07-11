def test_info_shape(client, auth_headers):
    r = client.get("/info", headers=auth_headers)
    assert r.status_code == 200
    body = r.json()
    assert body["name"] == "muesli-parakeet-transcriber"
    assert body["plugin_api"] == 1
    assert body["kind"] == "transcriber"
    assert isinstance(body["version"], str) and body["version"]
    schema = body["config_schema"]
    assert schema["type"] == "object"
    assert set(["model", "device"]).issubset(schema["properties"].keys())
    assert schema["additionalProperties"] is False


def test_health_ok_without_auth(client):
    r = client.get("/health")
    assert r.status_code == 200
    assert r.json() == {"status": "ok"}
