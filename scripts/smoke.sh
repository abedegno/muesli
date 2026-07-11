#!/usr/bin/env bash
# smoke.sh — end-to-end smoke test for the Muesli server critical path.
# Dependencies: curl, jq, sleep (POSIX coreutils), bash builtins.
# Usage: bash scripts/smoke.sh
#   MUESLI_URL      (default: http://localhost:8080)
#   MUESLI_EMAIL    (default: smoke@example.com)
#   MUESLI_PASSWORD (default: smokepassword123)
set -euo pipefail
trap 'rc=$?; echo "FAIL: script aborted at line ${LINENO}: ${BASH_COMMAND} (exit ${rc})" >&2' ERR

MUESLI_URL="${MUESLI_URL:-http://localhost:8080}"
MUESLI_EMAIL="${MUESLI_EMAIL:-smoke@example.com}"
MUESLI_PASSWORD="${MUESLI_PASSWORD:-smokepassword123}"

POLL_INTERVAL=5
POLL_TIMEOUT=180

# ── helpers ─────────────────────────────────────────────────────────────────
_step() { echo "[${1}] ${2}"; }
_fail() { echo "FAIL: ${*}" >&2; exit 1; }

_curl() {
  curl -s -S "$@"
}

# ── Step 1/7 – Check setup status ───────────────────────────────────────────
_step "1/7" "Checking setup status at ${MUESLI_URL} …"
SETUP_RESP=$(_curl "${MUESLI_URL}/api/setup/status")
NEEDS_SETUP=$(echo "${SETUP_RESP}" | jq -r '.needs_setup // empty') \
  || _fail "Could not parse /api/setup/status response: ${SETUP_RESP}"
[[ "${NEEDS_SETUP}" == "true" || "${NEEDS_SETUP}" == "false" ]] \
  || _fail "/api/setup/status returned unexpected needs_setup value: ${NEEDS_SETUP}"

# ── Step 2a/7 – First-time setup (if needed) ────────────────────────────────
if [[ "${NEEDS_SETUP}" == "true" ]]; then
  _step "2a/7" "Running first-time setup with ${MUESLI_EMAIL} …"
  SETUP_CODE=$(
    _curl -o /dev/null -w "%{http_code}" \
      -X POST "${MUESLI_URL}/api/setup" \
      -H "Content-Type: application/json" \
      -d "{\"email\": \"${MUESLI_EMAIL}\", \"password\": \"${MUESLI_PASSWORD}\"}"
  )
  [[ "${SETUP_CODE}" == "201" ]] \
    || _fail "/api/setup returned HTTP ${SETUP_CODE} (expected 201)"
  echo "       Setup complete (HTTP 201)."
else
  _step "2a/7" "Server already configured — skipping setup."
fi

# ── Step 2b/7 – Login ────────────────────────────────────────────────────────
_step "2b/7" "Logging in as ${MUESLI_EMAIL} …"
LOGIN_RESP=$(
  _curl -X POST "${MUESLI_URL}/api/login" \
    -H "Content-Type: application/json" \
    -d "{\"email\": \"${MUESLI_EMAIL}\", \"password\": \"${MUESLI_PASSWORD}\"}"
)
TOKEN=$(echo "${LOGIN_RESP}" | jq -r '.token // empty') \
  || _fail "Could not parse /api/login response: ${LOGIN_RESP}"
[[ -n "${TOKEN}" ]] \
  || _fail "/api/login did not return a token. Response: ${LOGIN_RESP}"
echo "       Login OK — token acquired."

# ── Step 3/7 – Create note ───────────────────────────────────────────────────
_step "3/7" "Creating smoke-test note …"
NOTE_RESP=$(
  _curl -X POST "${MUESLI_URL}/api/notes" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${TOKEN}" \
    -d '{"title": "Smoke test note"}'
)
NOTE_ID=$(echo "${NOTE_RESP}" | jq -r '.id // empty') \
  || _fail "Could not parse /api/notes response: ${NOTE_RESP}"
[[ -n "${NOTE_ID}" ]] \
  || _fail "/api/notes did not return an id. Response: ${NOTE_RESP}"
echo "       Note created — id=${NOTE_ID}"

# ── Step 4/7 – Get presigned upload URL ─────────────────────────────────────
_step "4/7" "Requesting presigned audio upload URL for note ${NOTE_ID} …"
PRESIGN_RESP=$(
  _curl -X POST "${MUESLI_URL}/api/notes/${NOTE_ID}/audio-upload-url" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${TOKEN}" \
    -d '{}'
)
UPLOAD_URL=$(echo "${PRESIGN_RESP}" | jq -r '.url // empty') \
  || _fail "Could not parse audio-upload-url response: ${PRESIGN_RESP}"
UPLOAD_KEY=$(echo "${PRESIGN_RESP}" | jq -r '.key // empty') \
  || _fail "Could not parse audio-upload-url response: ${PRESIGN_RESP}"
[[ -n "${UPLOAD_URL}" ]] \
  || _fail "audio-upload-url response missing 'url'. Response: ${PRESIGN_RESP}"
[[ -n "${UPLOAD_KEY}" ]] \
  || _fail "audio-upload-url response missing 'key'. Response: ${PRESIGN_RESP}"
echo "       Upload URL obtained — key=${UPLOAD_KEY}"

# ── Step 5/7 – Upload fixture audio ─────────────────────────────────────────
_step "5/7" "Uploading 16-byte fixture to presigned URL …"
UPLOAD_CODE=$(
  printf '\x00%.0s' {1..16} \
  | _curl -o /dev/null -w "%{http_code}" \
      -X PUT "${UPLOAD_URL}" \
      -H "Content-Type: application/octet-stream" \
      --data-binary @-
)
# S3-compatible presigned PUTs return 200 or 204
[[ "${UPLOAD_CODE}" == "200" || "${UPLOAD_CODE}" == "204" ]] \
  || _fail "Presigned PUT returned HTTP ${UPLOAD_CODE} (expected 200 or 204)"
echo "       Upload complete (HTTP ${UPLOAD_CODE})."

# ── Step 6/7 – Confirm upload ────────────────────────────────────────────────
_step "6/7" "Confirming upload for note ${NOTE_ID} …"
CONFIRM_CODE=$(
  _curl -o /dev/null -w "%{http_code}" \
    -X POST "${MUESLI_URL}/api/notes/${NOTE_ID}/audio-uploaded" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${TOKEN}" \
    -d "{\"key\": \"${UPLOAD_KEY}\"}"
)
[[ "${CONFIRM_CODE}" == "200" || "${CONFIRM_CODE}" == "204" ]] \
  || _fail "/api/notes/${NOTE_ID}/audio-uploaded returned HTTP ${CONFIRM_CODE} (expected 200 or 204)"
echo "       Upload confirmed (HTTP ${CONFIRM_CODE})."

# ── Step 7/7 – Poll until ready ──────────────────────────────────────────────
_step "7/7" "Polling note ${NOTE_ID} for status=ready (timeout ${POLL_TIMEOUT}s) …"
ELAPSED=0
while true; do
  NOTE_STATUS_RESP=$(
    _curl "${MUESLI_URL}/api/notes/${NOTE_ID}" \
      -H "Authorization: Bearer ${TOKEN}"
  )
  STATUS=$(echo "${NOTE_STATUS_RESP}" | jq -r '.status // empty') \
    || _fail "Could not parse note status response: ${NOTE_STATUS_RESP}"

  if [[ "${STATUS}" == "ready" ]]; then
    echo ""
    echo "PASS: note ${NOTE_ID} reached ready in ${ELAPSED}s"
    exit 0
  fi

  if (( ELAPSED >= POLL_TIMEOUT )); then
    _fail "Timed out after ${POLL_TIMEOUT}s waiting for status=ready (last status=${STATUS})"
  fi

  printf "       [%3ds] status=%s — waiting %ds …\r" "${ELAPSED}" "${STATUS}" "${POLL_INTERVAL}"
  sleep "${POLL_INTERVAL}"
  ELAPSED=$(( ELAPSED + POLL_INTERVAL ))
done
