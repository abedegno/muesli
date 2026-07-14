# Published Images

Muesli publishes prebuilt container images to GHCR for hosted deployments:

- `ghcr.io/abedegno/muesli-server`
- `ghcr.io/abedegno/muesli-whisper-transcriber`
- `ghcr.io/abedegno/muesli-ollama-agent`
- `ghcr.io/abedegno/muesli-streaming-transcriber`

The images are tagged with the git SHA, `latest`, and release tags such as
`v1.2.3` when published from a matching tag.

Example compose override:

```yaml
services:
  server:
    image: ghcr.io/abedegno/muesli-server:latest
```

After adding the image overrides you want, run `docker compose pull` and then
`docker compose up -d` to start with the published images instead of building
locally.
