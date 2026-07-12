import pytest
from starlette.websockets import WebSocketDisconnect


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


def test_stream_requires_auth(client):
    with pytest.raises(WebSocketDisconnect) as exc_info:
        with client.websocket_connect("/stream", headers={"X-Muesli-Plugin-API": "1"}):
            pass
    assert exc_info.value.code in {403, 4401}


def test_stream_rejects_wrong_token(client):
    with pytest.raises(WebSocketDisconnect) as exc_info:
        with client.websocket_connect(
            "/stream",
            headers={
                "Authorization": "Bearer nope",
                "X-Muesli-Plugin-API": "1",
            },
        ):
            pass
    assert exc_info.value.code in {403, 4401}
