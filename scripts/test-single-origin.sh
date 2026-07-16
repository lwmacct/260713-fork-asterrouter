#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT="${ASTER_SINGLE_ORIGIN_TEST_PORT:-28085}"
PID=""

cleanup() {
  if [ -n "${PID}" ]; then
    kill -TERM "${PID}" >/dev/null 2>&1 || true
    wait "${PID}" >/dev/null 2>&1 || true
  fi
}

trap cleanup EXIT INT TERM

if command -v lsof >/dev/null 2>&1 && lsof -nP -iTCP:"${PORT}" -sTCP:LISTEN >/dev/null 2>&1; then
  echo "Single-origin test port ${PORT} is already in use." >&2
  exit 1
fi

(
  cd "${ROOT_DIR}/frontend"
  npm run build
)

(
  cd "${ROOT_DIR}/backend"
  ASTERROUTER_SERVER_HTTP_LISTEN="127.0.0.1:${PORT}" \
  ASTERROUTER_SERVER_HTTP_FRONTEND_DIR="../frontend/dist" \
  ASTERROUTER_SERVER_BOOTSTRAP_DEMO_MODE=true \
  ASTERROUTER_SERVER_SECURITY_SECRET_KEY=asterrouter-single-origin-test-secret \
  go run ./cmd/asterrouter server
) &
PID="$!"

for _ in $(seq 1 120); do
  if curl -fsS "http://127.0.0.1:${PORT}/ready" 2>/dev/null | grep -Fq '"status":"ready"'; then
    break
  fi
  sleep 0.25
done

curl -fsS "http://127.0.0.1:${PORT}/ready" | grep -Fq '"status":"ready"'
curl -fsS "http://127.0.0.1:${PORT}/api/v1/settings/public" | grep -Fq '"demo_mode":true'
for path in / /login /console/overview /admin/dashboard /portal/overview; do
  curl -fsS "http://127.0.0.1:${PORT}${path}" | grep -Fq '<div id="app"></div>'
done

echo "Single-origin smoke passed on port ${PORT}."
