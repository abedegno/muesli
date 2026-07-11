def test_info_shape(client, auth_headers):
    r = client.get("/info", headers=auth_headers)
    assert r.status_code == 200
    body = r.json()
    assert body["name"] == "muesli-whisper-transcriber"
    assert body["plugin_api"] == 1
    assert body["kind"] == "transcriber"
    assert isinstance(body["version"], str) and body["version"]
    schema = body["config_schema"]
    assert schema["type"] == "object"
    # config fields the admin UI (Plan 4) will render:
    assert set(["model", "language_hint"]).issubset(schema["properties"].keys())
    assert "compute_type" in schema["properties"], "compute_type must be exposed in config_schema"


def test_health_ok_without_auth(client):
    # Health is intentionally UNAUTHENTICATED: scale-to-zero / k8s readiness
    # probes send no auth headers. It must return 200 with no token.
    r = client.get("/health")
    assert r.status_code == 200
    assert r.json() == {"status": "ok"}
