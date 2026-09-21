#!/usr/bin/env bash
# Runs the issue #767 (View Notes on iOS) integration/opt-in test suites that
# are deliberately NOT part of the default `go test ./...` / `npm test` runs:
# real-Postgres store/API tests, the 100k+ row scale/EXPLAIN proof, and (when
# a macOS/Xcode toolchain is present) the native iOS unit tests.
#
# This script documents prerequisites; it does not fabricate a database or a
# Swift toolchain. Each phase is skipped with a clear message if its
# prerequisite is absent, mirroring the check each phase performs itself.
#
# Usage: scripts/run-integration-tests.sh [--scale]
#   --scale   also run the opt-in 100k+ row store scale/EXPLAIN test
#             (MOBILE_NOTES_SCALE_TEST=1). Slow; requires a disposable
#             Postgres, never run against a shared/production database.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

run_scale=false
for arg in "$@"; do
  case "$arg" in
    --scale) run_scale=true ;;
    *)
      echo "unknown argument: $arg" >&2
      exit 2
      ;;
  esac
done

echo "== server (go): DB-backed store/API tests =="
if [ -z "${TEST_DATABASE_URL:-}" ]; then
  echo "TEST_DATABASE_URL is not set; skipping. See docs/CONFIGURATION.md / \`make test-db\`."
else
  go test ./internal/store/... ./internal/api/... ./cmd/muesli/...
fi

if [ "$run_scale" = true ]; then
  echo "== server (go): mobile notes 100k+ row scale/EXPLAIN proof (opt-in, slow) =="
  if [ -z "${TEST_DATABASE_URL:-}" ]; then
    echo "TEST_DATABASE_URL is not set; skipping." >&2
    exit 1
  fi
  MOBILE_NOTES_SCALE_TEST=1 go test ./internal/store/... -run TestMobileNotesScaleAndExplain -v -timeout 15m
fi

echo "== server (go): iosaccess/listener_controller (real sockets, no DB) =="
go test ./internal/iosaccess/... ./internal/api/... -run 'TestListenerController|TestEligiblePairs|TestControlServer|TestLoadOrCreateCertificate|TestResetCertificate|TestBuildOrigin|TestVerificationPhrase|TestPairingPayload|TestDecodePairingPayload|TestSendControlRequest|TestEvaluateSelectionOutcome|TestInterfaceAddressPairEquality'

echo "== client (node): iOS pairing/network-candidate shared vectors + Electron controller =="
npx vitest run src/main/iosAccess/ src/main/serverSupervisor.test.ts

echo "== native iOS: unit tests (requires macOS + Xcode; see native/ios/README.md) =="
if ! command -v xcodebuild >/dev/null 2>&1; then
  echo "xcodebuild not found; skipping. This phase cannot run outside macOS."
else
  echo "No .xcodeproj is checked in yet (native/ios/README.md explains why and how to create one)."
  echo "Once created: xcodebuild test -project native/ios/Muesli.xcodeproj -scheme Muesli -destination 'platform=iOS Simulator,name=iPhone 15'"
fi

echo "done."
