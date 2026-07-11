from fastapi import Header, HTTPException, Request, status


def require_auth(
    request: Request,
    authorization: str = Header(default=""),
    x_muesli_plugin_api: str = Header(default=""),
) -> None:
    if x_muesli_plugin_api != "1":
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail="missing or unsupported X-Muesli-Plugin-API",
        )
    expected = request.app.state.settings.auth_token
    prefix = "Bearer "
    if not authorization.startswith(prefix) or authorization[len(prefix):] != expected or not expected:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED, detail="invalid or missing token"
        )
