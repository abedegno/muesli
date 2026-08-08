#!/usr/bin/env bash
# test-whisper-cgo.sh — runner-safe entrypoint for the whisper.cpp cgo build/
# test path (cmd/whisper-cpp-transcriber's `whisper_cgo`-tagged engine).
#
# Why this exists: `go build/test -tags whisper_cgo` links against the
# vendored whisper.cpp static libs via cgo LDFLAGS. Linking happens as part
# of the Go toolchain invocation itself, before any Go-level code (including
# a `t.Skip` in a test) ever runs. So a Go-source-level guard can NEVER make
# a raw `go test -tags whisper_cgo ./cmd/whisper-cpp-transcriber/...` exit
# cleanly when the libs are absent -- it will fail to link, full stop. That
# is expected, documented behaviour for this cgo-gated path and does not
# affect the default (untagged) build, which stays pure Go.
#
# This script is the runner-safe wrapper: it checks for the vendored
# artifacts *before* invoking the Go toolchain at all, so nothing ever
# reaches the link step when they're missing. Use this (or
# `make test-whisper-cgo`) instead of invoking `go test -tags whisper_cgo`
# directly in any context where the vendored libs might not be present.
#
# Usage: bash scripts/test-whisper-cgo.sh
#   Env (matches this repo's documented whisper.cpp cgo prereqs):
#     C_INCLUDE_PATH  — ':'-separated dirs; one must contain whisper.h
#     LIBRARY_PATH    — ':'-separated dirs; one must contain libwhisper.a
#
#   If either artifact can't be found: prints a clear skip message and
#   exits 0 without invoking `go test`.
#   If both are found: runs `CGO_ENABLED=1 go test -tags whisper_cgo
#   ./cmd/whisper-cpp-transcriber/...` and exits with its status.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

find_in_path_list() {
    # find_in_path_list <name> <colon-separated-dirs>
    local name="$1"
    local list="$2"
    local IFS=':'
    local dir
    for dir in $list; do
        [[ -z "$dir" ]] && continue
        if [[ -f "${dir}/${name}" ]]; then
            return 0
        fi
    done
    return 1
}

if ! find_in_path_list "whisper.h" "${C_INCLUDE_PATH:-}"; then
    echo "skipping: whisper.cpp lib/headers not found (whisper.h not under any C_INCLUDE_PATH entry: '${C_INCLUDE_PATH:-<unset>}')"
    echo "provision the vendored whisper.cpp headers/libs (see .github/workflows/build-whisper-lib.yml) to run this test for real."
    exit 0
fi

if ! find_in_path_list "libwhisper.a" "${LIBRARY_PATH:-}"; then
    echo "skipping: whisper.cpp lib/headers not found (libwhisper.a not under any LIBRARY_PATH entry: '${LIBRARY_PATH:-<unset>}')"
    echo "provision the vendored whisper.cpp headers/libs (see .github/workflows/build-whisper-lib.yml) to run this test for real."
    exit 0
fi

cd "${REPO_ROOT}"
export CGO_ENABLED=1
exec go test -tags whisper_cgo ./cmd/whisper-cpp-transcriber/... ./cmd/whisper-cpp-streaming/...
