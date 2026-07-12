from __future__ import annotations

from fastapi import Header, HTTPException, Request, WebSocket, WebSocketException, status


def _validate_bearer(authorization: str, expected: str) -> bool:
    prefix = "Bearer "
    return bool(expected) and authorization.startswith(prefix) and authorization[len(prefix):] == expected


def require_auth(
    request: Request,
    authorization: str = Header(default=""),
    x_muesli_plugin_api: str = Header(default=""),
) -> None:
    """Enforce the pinned plugin auth envelope on HTTP endpoints."""

    if x_muesli_plugin_api != "1":
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail="missing or unsupported X-Muesli-Plugin-API",
        )
    expected = request.app.state.settings.auth_token
    if not _validate_bearer(authorization, expected):
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="invalid or missing token",
        )


async def require_ws_auth(websocket: WebSocket) -> None:
    """Enforce the pinned plugin auth envelope during the websocket handshake."""

    api_version = websocket.headers.get("x-muesli-plugin-api", "")
    if api_version != "1":
        raise WebSocketException(code=4400, reason="missing or unsupported X-Muesli-Plugin-API")
    expected = websocket.app.state.settings.auth_token
    authorization = websocket.headers.get("authorization", "")
    if not _validate_bearer(authorization, expected):
        raise WebSocketException(code=4401, reason="invalid or missing token")
