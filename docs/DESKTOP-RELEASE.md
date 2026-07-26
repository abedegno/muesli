# Desktop Release

This runbook covers the signed and notarized macOS arm64 `.dmg` release path.
Packaging lives in [../electron-builder.yml](../electron-builder.yml), and CI
lives in [../.github/workflows/desktop-release.yml](../.github/workflows/desktop-release.yml).

## What It Builds

- Signed macOS arm64 `.dmg`
- Resources staged into `build/resources` by
  [../scripts/assemble-desktop-resources.sh](../scripts/assemble-desktop-resources.sh)
- Electron packaging controlled by [../electron-builder.yml](../electron-builder.yml)

## Triggers

The workflow comment block matches the actual trigger behavior:

- `pull_request` on packaging files
  - Paths: `electron-builder.yml`, `build/entitlements.mac.*`,
    `scripts/assemble-desktop-resources.sh`,
    `.github/workflows/desktop-release.yml`
  - Runs build + sign only
  - Skips notarization for fast validation
- `push` tag `desktop-v*`
  - Runs the full release path
  - Builds, signs, notarizes, staples, and attaches the `.dmg` to a GitHub
    Release
- `workflow_dispatch`
  - Runs the same full release path as a tag push

## Required Secrets

The macOS release job expects these repository secrets:

- `CSC_LINK`
  - Code signing certificate material
- `CSC_KEY_PASSWORD`
  - Password for the signing certificate
- `APPLE_API_KEY`
  - Base64-encoded App Store Connect API key material
  - The workflow receives it as `APPLE_API_KEY_B64`, decodes it with
    `printf '%s' "$APPLE_API_KEY_B64" | base64 -d > "$RUNNER_TEMP/AuthKey.p8"`,
    then sets `APPLE_API_KEY="$RUNNER_TEMP/AuthKey.p8"`
- `APPLE_API_KEY_ID`
  - App Store Connect API key ID
- `APPLE_API_ISSUER`
  - App Store Connect issuer ID
- `APPLE_TEAM_ID`
  - Apple Developer team ID used during signing and notarization

## Release Cut

1. Bump `version` in [../package.json](../package.json).
   Also move the current `## [Unreleased]` content in
   [../CHANGELOG.md](../CHANGELOG.md) into a new dated section for that
   version so the accumulated release notes do not get stranded.
2. Create and push the release tag:
   `git tag desktop-vX.Y.Z && git push origin desktop-vX.Y.Z`
3. CI runs the macOS release job on `macos-14`.
4. The job builds, signs, notarizes, staples, and uploads the `.dmg`.
5. [softprops/action-gh-release](https://github.com/softprops/action-gh-release)
   attaches `dist-desktop/*.dmg` to the GitHub Release.

## Local Build

For unsigned local testing, use the same build sequence as the workflow:

- Build the pinned whisper.cpp static libs first and export `WLIB` the same way
  the workflow does, if you need the transcriber binary.
- Build the Go binaries into `build/bin/`
  ```sh
  make build
  cp bin/muesli build/bin/muesli
  go build -o build/bin/ollama-agent ./cmd/ollama-agent
  C_INCLUDE_PATH="$WLIB/include" LIBRARY_PATH="$WLIB/lib" CGO_ENABLED=1 \
    go build -tags whisper_cgo -o build/bin/whisper-cpp-transcriber \
    ./cmd/whisper-cpp-transcriber
  ```
- Run `npm ci`
- Assemble the desktop resources:
  `bash scripts/assemble-desktop-resources.sh darwin-arm64`
- Build the Electron app and package output:
  `npm run dist`

`npm run dist` expands to `electron-vite build && electron-builder` in
[../package.json](../package.json).

## Verify Signature And Notarization

The release job checks:

- `codesign --verify --deep --strict --verbose=2 "$APP"`
- `xcrun stapler validate "$APP"`
- `xcrun stapler validate "$DMG"`
- `spctl -a -t exec -vv "$APP"`

The `spctl` check is informational only. The workflow does not fail on it
because `spctl` assessment of a dmg can reject a correctly stapled artifact,
so `stapler validate` is the release gate.

## Scope

This pipeline is macOS arm64 `.dmg` only today.
[../scripts/assemble-desktop-resources.sh](../scripts/assemble-desktop-resources.sh)
has a separate `linux-x64` branch, but the signed desktop release path uses the
`darwin-arm64` build.

## Bump Process

The pinned whisper.cpp ref is the workflow-level `WHISPER_REF` value in
[../.github/workflows/desktop-release.yml](../.github/workflows/desktop-release.yml).
Update that commit hash there when bumping whisper.cpp, then re-run the desktop
release workflow.

For the matching pinned-artifact pattern, see
[EMBEDDED-POSTGRES-BUNDLE.md](EMBEDDED-POSTGRES-BUNDLE.md).
