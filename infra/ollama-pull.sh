#!/usr/bin/env sh
# infra/ollama-pull.sh — retry wrapper around `ollama pull` with exponential backoff.
#
# Usage: ollama-pull.sh <model-name>
#
# Retries up to 5 times, sleeping 2, 4, 8, 16 s between attempts.
# Exits 0 on success, 1 after all attempts are exhausted.

set -e

MODEL="${1:?Usage: $0 <model-name>}"
MAX_ATTEMPTS=5
SLEEP=2

i=1
while [ "$i" -le "$MAX_ATTEMPTS" ]; do
    echo "[ollama-pull] attempt ${i}/${MAX_ATTEMPTS}: pulling ${MODEL}..."
    if ollama pull "${MODEL}"; then
        echo "[ollama-pull] successfully pulled ${MODEL}"
        exit 0
    fi
    if [ "$i" -lt "$MAX_ATTEMPTS" ]; then
        echo "[ollama-pull] attempt ${i} failed, retrying in ${SLEEP}s..."
        sleep "${SLEEP}"
        SLEEP=$((SLEEP * 2))
    fi
    i=$((i + 1))
done

echo "[ollama-pull] ERROR: failed to pull ${MODEL} after ${MAX_ATTEMPTS} attempts"
exit 1
