#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:-$(tr -d '\r\n' < "${ROOT_DIR}/backend/cmd/asterrouter/VERSION")}"
VERSION="${VERSION#v}"
DIST_DIR="${ASTER_DIST_DIR:-${ROOT_DIR}/dist}"
RUN_DIR="${ASTER_RELEASE_JOURNEY_DIR:-${TMPDIR:-/tmp}/asterrouter-release-journeys-$$}"
BACKEND_PORT="${ASTER_RELEASE_JOURNEY_PORT:-18087}"
UPSTREAM_PORT="${ASTER_RELEASE_JOURNEY_UPSTREAM_PORT:-19087}"
DATABASE_URL="${ASTER_RELEASE_TEST_DATABASE_URL:-}"
PACKAGE_NAME="asterrouter_${VERSION}_linux_amd64"
ARCHIVE="${DIST_DIR}/${PACKAGE_NAME}.tar.gz"
PACKAGE_DIR="${RUN_DIR}/${PACKAGE_NAME}"
PIDS=()
RUNTIME_PID=""

if [ "$(uname -s)" != "Linux" ] || [ "$(uname -m)" != "x86_64" ]; then
  echo "Release browser journeys require Linux amd64." >&2
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
    raise SystemExit("Release journey database URL must use PostgreSQL.")
if database != "asterrouter_release_test" and not database.startswith("asterrouter_release_test_"):
    raise SystemExit("Release journey database URL must use the asterrouter_release_test prefix.")
PY
if [ ! -s "${ARCHIVE}" ]; then
  echo "Release archive is missing: ${ARCHIVE}" >&2
  exit 1
fi
if [ -d "${RUN_DIR}" ] && find "${RUN_DIR}" -mindepth 1 -maxdepth 1 -print -quit | grep -q .; then
  echo "Refusing to overwrite non-empty journey directory: ${RUN_DIR}" >&2
  exit 1
fi

require_free_port() {
  local port="$1"
  if command -v lsof >/dev/null 2>&1 && lsof -nP -iTCP:"${port}" -sTCP:LISTEN >/dev/null 2>&1; then
    echo "Required release journey port ${port} is already in use." >&2
    exit 1
  fi
}

database_url_for() {
  local suffix="$1"
  python3 - "${DATABASE_URL}" "${suffix}" <<'PY'
import sys
from urllib.parse import urlparse

parsed = urlparse(sys.argv[1])
database = parsed.path.lstrip("/")
print(parsed._replace(path="/" + database + "_" + sys.argv[2]).geturl())
PY
}

cleanup() {
  local pid
  for pid in "${PIDS[@]}"; do
    kill -TERM "${pid}" >/dev/null 2>&1 || true
  done
  for pid in "${PIDS[@]}"; do
    wait "${pid}" >/dev/null 2>&1 || true
  done
}

start_runtime() {
  local profile="$1"
  local port="$2"
  local database_url="$3"
  local profile_dir="$4"

  (
    cd "${PACKAGE_DIR}"
    exec env \
      "ASTERROUTER_SERVER_HTTP_LISTEN=127.0.0.1:${port}" \
      "ASTERROUTER_SERVER_HTTP_FRONTEND_DIR=${PACKAGE_DIR}/frontend/dist" \
      "ASTERROUTER_SERVER_SECURITY_ADMIN_PASSWORD=release-browser-test-password" \
      "ASTERROUTER_SERVER_SECURITY_SECRET_KEY=asterrouter-release-journey-test-secret" \
      "ASTERROUTER_SERVER_BOOTSTRAP_DEPLOYMENT_ROLE=${profile}" \
      "ASTERROUTER_SERVER_PLUGINS_CACHE_DIR=${profile_dir}/data/plugin-cache" \
      "ASTERROUTER_SERVER_PLUGINS_ACTIVE_DIR=${profile_dir}/data/plugin-active" \
      "ASTERROUTER_SERVER_MAINTENANCE_BACKUP_DIR=${profile_dir}/data/backups" \
      "ASTERROUTER_SERVER_MAINTENANCE_DIAGNOSTIC_DIR=${profile_dir}/data/diagnostics" \
      "ASTERROUTER_SERVER_STORAGE_DATABASE_URL=${database_url}" \
      ./asterrouter server
  ) >"${profile_dir}/runtime.log" 2>&1 &
  RUNTIME_PID="$!"
  PIDS+=("${RUNTIME_PID}")
}

wait_for_ready() {
  local pid="$1"
  local port="$2"
  for _ in $(seq 1 120); do
    if curl -fsS "http://127.0.0.1:${port}/ready" 2>/dev/null | grep -q '"status":"ready"'; then
      return 0
    fi
    if ! kill -0 "${pid}" >/dev/null 2>&1; then
      wait "${pid}" || true
      echo "Release candidate exited before becoming ready." >&2
      return 1
    fi
    sleep 0.25
  done
  curl -fsS "http://127.0.0.1:${port}/ready" | grep -q '"status":"ready"'
}

stop_runtime() {
  local pid="$1"
  local item
  local remaining=()
  kill -TERM "${pid}" >/dev/null 2>&1 || true
  wait "${pid}" >/dev/null 2>&1 || true
  for item in "${PIDS[@]}"; do
    if [ "${item}" != "${pid}" ]; then
      remaining+=("${item}")
    fi
  done
  PIDS=("${remaining[@]}")
}

run_profile_journey() {
  local profile="$1"
  local grep_pattern="$2"
  local port="$3"
  local profile_dir="${RUN_DIR}/profiles/${profile}"
  local profile_database
  profile_database="$(database_url_for "${profile}")"
  mkdir -p "${profile_dir}"
  require_free_port "${port}"
  start_runtime "${profile}" "${port}" "${profile_database}" "${profile_dir}"
  local pid="${RUNTIME_PID}"
  wait_for_ready "${pid}" "${port}"
  (
    cd "${ROOT_DIR}"
    ASTER_E2E_EXTERNAL_URL="http://127.0.0.1:${port}" \
      ASTER_E2E_EXPECT_PROFILE="${profile}" \
      node scripts/configure-e2e-profiles.mjs
  )
  (
    cd "${ROOT_DIR}/frontend"
    CI=true \
      ASTER_E2E_EXTERNAL_URL="http://127.0.0.1:${port}" \
      ASTER_E2E_UPSTREAM_PORT="${UPSTREAM_PORT}" \
      ASTER_E2E_ARTIFACT_DIR="${profile_dir}/playwright" \
      ASTER_E2E_USERNAME=admin \
      ASTER_E2E_PASSWORD=release-browser-test-password \
      ASTER_E2E_EXPECT_DEMO_MODE=false \
      ASTER_E2E_EXPECT_PROFILE="${profile}" \
      npx playwright test --grep "${grep_pattern}"
  )
  stop_runtime "${pid}"
}

require_free_port "${BACKEND_PORT}"
require_free_port "${UPSTREAM_PORT}"
trap cleanup EXIT INT TERM

mkdir -p "${RUN_DIR}"
tar -C "${RUN_DIR}" -xzf "${ARCHIVE}"

(
  cd "${ROOT_DIR}"
  ASTER_E2E_UPSTREAM_PORT="${UPSTREAM_PORT}" node "scripts/fake-openai.mjs"
) >"${RUN_DIR}/fake-upstream.log" 2>&1 &
PIDS+=("$!")

ASTER_SETUP_JOURNEY_DATABASE_URL="$(database_url_for platform_setup)" \
  ASTER_SETUP_JOURNEY_DIR="${RUN_DIR}/setup" \
  ASTER_SETUP_JOURNEY_PORT="${BACKEND_PORT}" \
  ASTER_SETUP_JOURNEY_BINARY="${PACKAGE_DIR}/asterrouter" \
  bash "${ROOT_DIR}/scripts/test-setup-browser-journey.sh"

run_profile_journey enterprise '@surface-smoke|@j01|@j02|@j03|@j04|@j05' "$((BACKEND_PORT + 1))"
run_profile_journey relay_operator '@surface-smoke|@j06|customer and account sessions' "$((BACKEND_PORT + 2))"
run_profile_journey personal '@surface-smoke|console overview has no serious accessibility violations' "$((BACKEND_PORT + 3))"
run_profile_journey platform '@surface-smoke|@platform' "$((BACKEND_PORT + 4))"

{
  echo 'release_browser_journeys=passed'
  echo "version=${VERSION}"
  echo 'platform=linux/amd64'
  echo 'execution=candidate_archive'
  echo 'deployment_profiles=enterprise,relay_operator,personal,platform'
  echo 'isolation=one_postgresql_database_and_runtime_per_profile'
  echo 'journeys=J01,J02,J03,J04,J05,J06,J09'
  echo 'first_install_profile=platform'
  echo 'browser=chromium'
} >"${RUN_DIR}/report.txt"

echo "Release browser journeys passed. Evidence: ${RUN_DIR}/report.txt"
