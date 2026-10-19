"""Exercise an installed plugin and its native imports inside its built image."""

import asyncio
import importlib
import sys

import httpx


async def main():
    module = importlib.import_module(sys.argv[1])
    for name in sys.argv[2:]:
        importlib.import_module(name)
    transport = httpx.ASGITransport(app=module.app())
    async with httpx.AsyncClient(transport=transport, base_url="http://plugin") as client:
        response = await client.get("/health")
        assert response.status_code == 200, response.text
        assert response.json() == {"status": "ok"}, response.text
    print(f"{sys.argv[1]}: installed imports and health request passed")


asyncio.run(main())
