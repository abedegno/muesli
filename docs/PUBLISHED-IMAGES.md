# Published Images

Muesli publishes prebuilt container images to GHCR for hosted deployments:

- `ghcr.io/abedegno/muesli-server`
- `ghcr.io/abedegno/muesli-whisper-transcriber`
- `ghcr.io/abedegno/muesli-ollama-agent`
- `ghcr.io/abedegno/muesli-streaming-transcriber`

The images are tagged with the git SHA, `latest`, and release tags such as
`v1.2.3` when published from a matching tag.

Pushing a `v*` tag also attaches a self-contained release asset bundle to the
matching GitHub Release once the images finish publishing: a version-pinned
`docker-compose.prod.yml` (image tags baked in literally), `.env.example`,
`install.sh`, and a `SHA256SUMS` covering all three. This is what
[`scripts/install.sh`](../scripts/install.sh) downloads by default; see the
"Install script" section of [`docs/DEPLOYMENT.md`](./DEPLOYMENT.md).

Each published-image job first builds a local `linux/amd64` image, scans that
actual image with Trivy before any GHCR push, and generates an SPDX JSON SBOM
from the same loaded image. The Trivy step is currently report-only, not
gating, because the base images still carry a backlog of unfixed `HIGH` and
`CRITICAL` OS-package CVEs. The scan output is uploaded as a workflow artifact
named `trivy-<image>`, so the findings stay visible while the publish job keeps
running.

The SBOM is uploaded as a workflow artifact named `sbom-<image>` for the
matrix entry, for example `sbom-server` or `sbom-whisper-transcriber`. Download
it from the workflow run's artifact list after the publish job completes.

When the CVE backlog is triaged, the checked-in [`.trivyignore`](../.trivyignore)
file is the allowlist mechanism for a future fail-hard cutover. Add one
vulnerability ID per line, preceded by a short comment that explains why the
exception is accepted and when it should be removed. Track that enforcement flip
in [issue #238](https://github.com/abedegno/muesli/issues/238).

## Verifying provenance

Each pushed image now carries a keyless GitHub-OIDC build-provenance
attestation. Verify it with:

```bash
gh attestation verify oci://ghcr.io/abedegno/muesli-server:latest --owner abedegno
```

This works for any of the four published images by substituting the image name
or tag in the `oci://` reference.

Example compose override:

```yaml
services:
  server:
    image: ghcr.io/abedegno/muesli-server:latest
```

After adding the image overrides you want, run `docker compose pull` and then
`docker compose up -d` to start with the published images instead of building
locally.
