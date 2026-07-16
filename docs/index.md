# Muesli — Master Documentation Index

All project documentation organised by use case. Use this file as your
starting point; every linked path below is a working relative link from
within the `docs/` directory.

---

## Understand the system

| Document                                         | What's in it                                                                    |
| ------------------------------------------------ | ------------------------------------------------------------------------------- |
| [`../README.md`](../README.md)                   | Project intro, quickstart, and Docker Compose setup.                            |
| [`GETTING_STARTED.md`](GETTING_STARTED.md)       | First-run walkthrough for setup, client connect, and a first meeting.           |
| [`DESKTOP-ONBOARDING.md`](DESKTOP-ONBOARDING.md) | Desktop onboarding for embedded mode, degraded mode, and Ollama setup.          |
| [`ARCHITECTURE.md`](ARCHITECTURE.md)             | Components, processing pipeline, package map, and plugin contract.              |
| [`../CONTEXT.md`](../CONTEXT.md)                 | Shared language, domain glossary, and architecture map for contributors.        |
| [`../CHANGELOG.md`](../CHANGELOG.md)             | What's changed across releases.                                                 |
| [`../ROADMAP.md`](../ROADMAP.md)                 | High-level milestones (v2 → v5) closing the gap to Granola and into enterprise. |

---

## Configure & deploy

| Document                                                     | What's in it                                                        |
| ------------------------------------------------------------ | ------------------------------------------------------------------- |
| [`CONFIGURATION.md`](CONFIGURATION.md)                       | Complete reference for all `MUESLI_*` environment variables.        |
| [`DESKTOP-RELEASE.md`](DESKTOP-RELEASE.md)                   | Signed and notarized macOS desktop release runbook.                 |
| [`EMBEDDED-POSTGRES-BUNDLE.md`](EMBEDDED-POSTGRES-BUNDLE.md) | Vendored embedded Postgres bundle, pinned checksum, and bump steps. |
| [`DEPLOYMENT.md`](DEPLOYMENT.md)                             | Production TLS and reverse-proxy guide.                             |
| [`PUBLISHED-IMAGES.md`](PUBLISHED-IMAGES.md)                 | GHCR image names for hosted deployments and a compose pull example. |
| [`PLUGINS.md`](PLUGINS.md)                                   | How to write a transcriber or agent plugin.                         |
| [`STREAMING-SMOKE.md`](STREAMING-SMOKE.md)                   | Manual smoke checklist for the optional streaming transcript path.  |
| [`BACKUP.md`](BACKUP.md)                                     | Backup and restore procedures for data and configuration.           |
| [`UPGRADING.md`](UPGRADING.md)                               | Step-by-step guide for upgrading an existing deployment.            |
| [`API.md`](API.md)                                           | HTTP API reference.                                                 |

---

## Contribute

| Document                                   | What's in it                                      |
| ------------------------------------------ | ------------------------------------------------- |
| [`../CONTRIBUTING.md`](../CONTRIBUTING.md) | Dev setup, conventions, and how to land a change. |

---

## Debug

| Document                                   | What's in it                                                                      |
| ------------------------------------------ | --------------------------------------------------------------------------------- |
| [`../SECURITY.md`](../SECURITY.md)         | How to report a vulnerability privately, plus operator security responsibilities. |
| [`TROUBLESHOOTING.md`](TROUBLESHOOTING.md) | Common issues: slow first boot, port conflicts, reading logs.                     |
