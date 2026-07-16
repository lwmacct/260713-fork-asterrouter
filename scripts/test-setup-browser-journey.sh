#!/usr/bin/env bash
set -euo pipefail

_pid=""

__cleanup() {
  if [ -n "${_pid}" ]; then
    kill -TERM "${_pid}" >/dev/null 2>&1 || true
    wait "${_pid}" >/dev/null 2>&1 || true
  fi
}

__run_runtime() {
  local -a _app_command
  local -a _runtime_env
  local _frontend_dir

  if [ -n "${_binary}" ]; then
    cd "$(dirname "${_binary}")"
    _app_command=("${_binary}" server)
    _frontend_dir="$(dirname "${_binary}")/frontend/dist"
  else
    cd "${_root_dir}/backend"
    _app_command=(go run ./cmd/asterrouter server)
    _frontend_dir="${_root_dir}/frontend/dist"
  fi
  _runtime_env=(
    "ASTERROUTER_SERVER_HTTP_LISTEN=127.0.0.1:${_port}"
    "ASTERROUTER_SERVER_HTTP_FRONTEND_DIR=${_frontend_dir}"
    'ASTERROUTER_SERVER_SECURITY_ADMIN_PASSWORD=setup-browser-test-password'
    'ASTERROUTER_SERVER_SECURITY_SECRET_KEY=asterrouter-setup-browser-test-secret'
    "ASTERROUTER_SERVER_PLUGINS_CACHE_DIR=${_run_dir}/data/plugin-cache"
    "ASTERROUTER_SERVER_PLUGINS_ACTIVE_DIR=${_run_dir}/data/plugin-active"
    "ASTERROUTER_SERVER_MAINTENANCE_BACKUP_DIR=${_run_dir}/data/backups"
    "ASTERROUTER_SERVER_MAINTENANCE_DIAGNOSTIC_DIR=${_run_dir}/data/diagnostics"
  )
  if [ -n "${ASTER_SETUP_JOURNEY_DATABASE_URL:-}" ]; then
    _runtime_env+=("ASTERROUTER_SERVER_STORAGE_DATABASE_URL=${ASTER_SETUP_JOURNEY_DATABASE_URL}")
  fi
  exec env "${_runtime_env[@]}" "${_app_command[@]}"
}

__main() {
  local _attempt
  local _execution

  _root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
  _run_dir="${ASTER_SETUP_JOURNEY_DIR:-${TMPDIR:-/tmp}/asterrouter-setup-journey-$$}"
  _port="${ASTER_SETUP_JOURNEY_PORT:-18088}"
  _binary="${ASTER_SETUP_JOURNEY_BINARY:-}"

  if [ -d "${_run_dir}" ] && find "${_run_dir}" -mindepth 1 -maxdepth 1 -print -quit | grep -q .; then
    echo "Refusing to overwrite non-empty setup journey directory: ${_run_dir}" >&2
    exit 1
  fi
  if command -v lsof >/dev/null 2>&1 && lsof -nP -iTCP:"${_port}" -sTCP:LISTEN >/dev/null 2>&1; then
    echo "Setup journey port ${_port} is already in use." >&2
    exit 1
  fi

  trap __cleanup EXIT INT TERM

  mkdir -p "${_run_dir}"
  if [ -z "${_binary}" ]; then
    (
      cd "${_root_dir}/frontend"
      npm run build
    )
  elif [ ! -x "${_binary}" ]; then
    echo "ASTER_SETUP_JOURNEY_BINARY must point to an executable: ${_binary}" >&2
    exit 1
  fi

  __run_runtime >"${_run_dir}/runtime.log" 2>&1 &
  _pid="$!"

  for _attempt in $(seq 1 120); do
    if curl -fsS "http://127.0.0.1:${_port}/ready" 2>/dev/null | grep -q '"status":"ready"'; then
      break
    fi
    if ! kill -0 "${_pid}" >/dev/null 2>&1; then
      wait "${_pid}" || true
      echo "Setup journey runtime exited before becoming ready." >&2
      exit 1
    fi
    sleep 0.25
  done
  curl -fsS "http://127.0.0.1:${_port}/api/v1/setup/status" | grep -q '"setup_completed":false'

  (
    cd "${_root_dir}/frontend"
    CI=true \
      ASTER_E2E_INCLUDE_SETUP=1 \
      ASTER_E2E_EXTERNAL_URL="http://127.0.0.1:${_port}" \
      ASTER_E2E_ARTIFACT_DIR="${_run_dir}/playwright" \
      npx playwright test --grep '@setup'
  )

  curl -fsS "http://127.0.0.1:${_port}/api/v1/setup/status" | grep -q '"default_profile":"platform"'
  curl -fsS "http://127.0.0.1:${_port}/api/v1/setup/status" | grep -q '"setup_completed":true'

  if [ -n "${_binary}" ]; then
    _execution="candidate_binary"
  else
    _execution="source_runtime"
  fi
  {
    echo 'setup_browser_journey=passed'
    echo "execution=${_execution}"
    echo 'profile=platform'
    echo 'browser=chromium'
  } >"${_run_dir}/report.txt"

  echo "Setup browser journey passed. Evidence: ${_run_dir}/report.txt"
}

__main "$@"
