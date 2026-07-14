# Published Images

Muesli publishes prebuilt container images to GHCR for hosted deployments:

- `ghcr.io/abedegno/muesli-server`
- `ghcr.io/abedegno/muesli-whisper-transcriber`
- `ghcr.io/abedegno/muesli-ollama-agent`
- `ghcr.io/abedegno/muesli-streaming-transcriber`

The images are tagged with the git SHA, `latest`, and release tags such as
`v1.2.3` when published from a matching tag.

Each published-image job first builds a local `linux/amd64` image, scans that
actual image with Trivy before any GHCR push, and generates an SPDX JSON SBOM
from the same loaded image. The scan fails the workflow on `HIGH` or
`CRITICAL` findings unless the vulnerability ID is listed in the checked-in
[`.trivyignore`](../.trivyignore) file with a short comment explaining why the
exception is accepted.

The SBOM is uploaded as a workflow artifact named `sbom-<image>` for the
matrix entry, for example `sbom-server` or `sbom-whisper-transcriber`. Download
it from the workflow run's artifact list after the publish job completes.

Example compose override:

```yaml
services:
  server:
    image: ghcr.io/abedegno/muesli-server:latest
```

After adding the image overrides you want, run `docker compose pull` and then
`docker compose up -d` to start with the published images instead of building
locally.
