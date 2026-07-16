#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:-$(tr -d '\r\n' < "${ROOT_DIR}/backend/cmd/asterrouter/VERSION")}"
VERSION="${VERSION#v}"
DIST_DIR="${ASTER_DIST_DIR:-${ROOT_DIR}/dist}"
RUN_DIR="${ASTER_RELEASE_RUNTIME_DIR:-${TMPDIR:-/tmp}/asterrouter-release-runtime-$$}"
PORT="${ASTER_RELEASE_RUNTIME_PORT:-18086}"
DATABASE_URL="${ASTER_RELEASE_TEST_DATABASE_URL:-}"
PACKAGE_NAME="asterrouter_${VERSION}_linux_amd64"
ARCHIVE="${DIST_DIR}/${PACKAGE_NAME}.tar.gz"
PACKAGE_DIR="${RUN_DIR}/${PACKAGE_NAME}"
PID=""

if [ "$(uname -s)" != "Linux" ] || [ "$(uname -m)" != "x86_64" ]; then
  echo "Release runtime acceptance requires Linux amd64." >&2
  exit 1
fi
if [ -z "${DATABASE_URL}" ]; then
  echo "ASTER_RELEASE_TEST_DATABASE_URL must point to a dedicated test database." >&2
  exit 1
fi
python3 - "${DATABASE_URL}" <<'PY'
import sys
from urllib.parse import urlparse

parsed = urlparse(sys.argv[1])
database = parsed.path.lstrip("/")
if parsed.scheme not in {"postgres", "postgresql"} or not parsed.hostname:
    raise SystemExit("Release runtime database URL must use PostgreSQL.")
if database != "asterrouter_release_test" and not database.startswith("asterrouter_release_test_"):
    raise SystemExit("Release runtime database name must be asterrouter_release_test or use that prefix.")
PY
if [ ! -s "${ARCHIVE}" ]; then
  echo "Release archive is missing: ${ARCHIVE}" >&2
  exit 1
fi
if [ -d "${RUN_DIR}" ] && find "${RUN_DIR}" -mindepth 1 -maxdepth 1 -print -quit | grep -q .; then
  echo "Refusing to overwrite non-empty runtime directory: ${RUN_DIR}" >&2
  exit 1
fi

mkdir -p "${RUN_DIR}"
tar -C "${RUN_DIR}" -xzf "${ARCHIVE}"

if "${PACKAGE_DIR}/asterrouter" server >"${RUN_DIR}/fail-closed.log" 2>&1; then
  echo "Release binary unexpectedly started without required configuration." >&2
  exit 1
fi
grep -q 'server.storage.database-url is required in release builds' "${RUN_DIR}/fail-closed.log"

cleanup() {
  if [ -n "${PID}" ] && kill -0 "${PID}" >/dev/null 2>&1; then
    kill -TERM "${PID}" >/dev/null 2>&1 || true
    wait "${PID}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT INT TERM

(
  cd "${PACKAGE_DIR}"
  exec env \
    ASTERROUTER_SERVER_HTTP_LISTEN="127.0.0.1:${PORT}" \
    ASTERROUTER_SERVER_HTTP_FRONTEND_DIR="${PACKAGE_DIR}/frontend/dist" \
    ASTERROUTER_SERVER_BOOTSTRAP_DEMO_MODE=true \
    ASTERROUTER_SERVER_SECURITY_SECRET_KEY=asterrouter-release-runtime-test-secret \
    ASTERROUTER_SERVER_PLUGINS_CACHE_DIR="${RUN_DIR}/data/plugin-cache" \
    ASTERROUTER_SERVER_PLUGINS_ACTIVE_DIR="${RUN_DIR}/data/plugin-active" \
    ASTERROUTER_SERVER_MAINTENANCE_BACKUP_DIR="${RUN_DIR}/data/backups" \
    ASTERROUTER_SERVER_MAINTENANCE_DIAGNOSTIC_DIR="${RUN_DIR}/data/diagnostics" \
    ASTERROUTER_SERVER_STORAGE_DATABASE_URL="${DATABASE_URL}" \
    ./asterrouter server >"${RUN_DIR}/runtime.log" 2>&1
) &
PID="$!"

for _ in $(seq 1 120); do
  if curl -fsS "http://127.0.0.1:${PORT}/ready" 2>/dev/null | grep -q '"status":"ready"'; then
    break
  fi
  if ! kill -0 "${PID}" >/dev/null 2>&1; then
    wait "${PID}" || true
    echo "Release runtime exited before becoming ready." >&2
    exit 1
  fi
  sleep 0.25
done

curl -fsS "http://127.0.0.1:${PORT}/ready" | grep -q '"status":"ready"'
curl -fsS "http://127.0.0.1:${PORT}/api/v1/settings/public" | grep -q '"demo_mode":true'
for path in /login /console/overview /admin/dashboard /portal/overview; do
  curl -fsS "http://127.0.0.1:${PORT}${path}" | grep -q '<div id="app"></div>'
done

kill -TERM "${PID}"
if ! wait "${PID}"; then
  echo "Release runtime did not exit cleanly after SIGTERM." >&2
  exit 1
fi
PID=""

{
  echo 'release_runtime_acceptance=passed'
  echo "version=${VERSION}"
  echo 'platform=linux/amd64'
  echo 'fail_closed=passed'
  echo 'postgres_ready=passed'
  echo 'single_origin=passed'
  echo 'sigterm_exit=0'
} >"${RUN_DIR}/report.txt"

echo "Release runtime acceptance passed. Evidence: ${RUN_DIR}/report.txt"
