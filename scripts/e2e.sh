#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
UPSTREAM_PORT="${ASTER_E2E_UPSTREAM_PORT:-29000}"
PIDS=()

cleanup() {
  local pid
  for pid in "${PIDS[@]}"; do
    kill -TERM "${pid}" >/dev/null 2>&1 || true
  done
  for pid in "${PIDS[@]}"; do
    wait "${pid}" >/dev/null 2>&1 || true
  done
}

trap cleanup EXIT INT TERM

if command -v lsof >/dev/null 2>&1 && lsof -nP -iTCP:"${UPSTREAM_PORT}" -sTCP:LISTEN >/dev/null 2>&1; then
  echo "Fake upstream port ${UPSTREAM_PORT} is already in use." >&2
  exit 1
fi

(
  cd "${ROOT_DIR}"
  ASTER_E2E_UPSTREAM_PORT="${UPSTREAM_PORT}" node "scripts/fake-openai.mjs"
) &
PIDS+=("$!")

(
  cd "${ROOT_DIR}"
  bash "scripts/dev.sh" --no-kill-occupied
) &
PIDS+=("$!")

while true; do
  for pid in "${PIDS[@]}"; do
    if ! kill -0 "${pid}" >/dev/null 2>&1; then
      wait "${pid}"
      exit "$?"
    fi
  done
  sleep 1
done
