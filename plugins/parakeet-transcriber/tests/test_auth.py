def test_missing_token_is_401(client):
    r = client.get("/info", headers={"X-Muesli-Plugin-API": "1"})
    assert r.status_code == 401


def test_wrong_token_is_401(client):
    r = client.get(
        "/info",
        headers={"Authorization": "Bearer nope", "X-Muesli-Plugin-API": "1"},
    )
    assert r.status_code == 401


def test_missing_api_version_is_400(client):
    r = client.get("/info", headers={"Authorization": "Bearer test-token"})
    assert r.status_code == 400


def test_transcribe_requires_auth(client):
    r = client.post("/transcribe", json={}, headers={"X-Muesli-Plugin-API": "1"})
    assert r.status_code == 401
