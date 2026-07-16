#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [ -f "${ROOT_DIR}/.env" ]; then
  set -a
  # shellcheck disable=SC1091
  . "${ROOT_DIR}/.env"
  set +a
fi

KILL_OCCUPIED="${ASTER_DEV_KILL_OCCUPIED:-1}"
DEMO_MODE="${ASTERROUTER_SERVER_BOOTSTRAP_DEMO_MODE:-false}"

usage() {
  cat <<'EOF'
Usage: ./scripts/dev.sh [--demo] [--kill-occupied|--no-kill-occupied]

Options:
  --demo              Enable the built-in one-click demo account.
  --kill-occupied     Gracefully stop processes listening on the selected ports (default).
  --no-kill-occupied  Refuse to start when a selected port is already occupied.
  -h, --help          Show this help message.

Environment:
  ASTERROUTER_SERVER_BOOTSTRAP_DEMO_MODE=true       Enable the built-in one-click demo account.
  ASTER_DEV_KILL_OCCUPIED=1  Enable automatic port cleanup (default).
  ASTER_DEV_KILL_OCCUPIED=0  Disable automatic port cleanup.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --demo)
      DEMO_MODE=true
      ;;
    --kill-occupied)
      KILL_OCCUPIED=1
      ;;
    --no-kill-occupied)
      KILL_OCCUPIED=0
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

BACKEND_HOST="${ASTER_DEV_BACKEND_HOST:-127.0.0.1}"
BACKEND_PORT="${ASTER_DEV_BACKEND_PORT:-8080}"
FRONTEND_HOST="${ASTER_DEV_FRONTEND_HOST:-0.0.0.0}"
FRONTEND_PORT="${ASTER_DEV_FRONTEND_PORT:-5173}"
BACKEND_URL="http://${BACKEND_HOST}:${BACKEND_PORT}"
PIDS=()

cleanup() {
  if [ "${#PIDS[@]}" -gt 0 ]; then
    local pid
    for pid in "${PIDS[@]}"; do
      kill "${pid}" >/dev/null 2>&1 || true
    done
    for pid in "${PIDS[@]}"; do
      wait "${pid}" >/dev/null 2>&1 || true
    done
  fi
}

trap cleanup EXIT INT TERM

port_in_use() {
  local port="$1"
  if command -v lsof >/dev/null 2>&1; then
    lsof -nP -iTCP:"${port}" -sTCP:LISTEN >/dev/null 2>&1
    return $?
  fi
  if command -v nc >/dev/null 2>&1; then
    nc -z 127.0.0.1 "${port}" >/dev/null 2>&1
    return $?
  fi
  return 1
}

require_free_port() {
  local name="$1"
  local port="$2"
  if ! port_in_use "${port}"; then
    return
  fi

  case "${KILL_OCCUPIED}" in
    1|true|TRUE|yes|YES) ;;
    *)
      echo "${name} port ${port} is already in use." >&2
      echo "Automatic cleanup is disabled. Remove --no-kill-occupied, set ASTER_DEV_KILL_OCCUPIED=1, or override ASTER_DEV_${name}_PORT." >&2
      exit 1
      ;;
  esac

  if ! command -v lsof >/dev/null 2>&1; then
    echo "Cannot stop the process on ${name} port ${port}: lsof is not available." >&2
    exit 1
  fi

  local pids=()
  local pid
  while IFS= read -r pid; do
    if [ -n "${pid}" ]; then
      pids+=("${pid}")
    fi
  done < <(lsof -nP -tiTCP:"${port}" -sTCP:LISTEN 2>/dev/null | sort -u)

  if [ "${#pids[@]}" -eq 0 ]; then
    echo "Cannot identify the process on ${name} port ${port}." >&2
    exit 1
  fi

  echo "${name} port ${port} is occupied; stopping listener(s):"
  for pid in "${pids[@]}"; do
    ps -p "${pid}" -o pid=,command= 2>/dev/null || echo "  PID ${pid}"
  done
  for pid in "${pids[@]}"; do
    kill -TERM "${pid}" 2>/dev/null || true
  done

  local attempt
  for attempt in {1..50}; do
    if ! port_in_use "${port}"; then
      echo "${name} port ${port} is now available."
      return
    fi
    sleep 0.1
  done

  echo "${name} port ${port} is still in use after 5 seconds; refusing to send SIGKILL." >&2
  exit 1
}

require_free_port "BACKEND" "${BACKEND_PORT}"
require_free_port "FRONTEND" "${FRONTEND_PORT}"

echo "AsterRouter API: ${BACKEND_URL}"
echo "AsterRouter UI:  http://${FRONTEND_HOST}:${FRONTEND_PORT}"
if [ "${DEMO_MODE}" = "true" ]; then
  echo "Demo login:      enabled"
fi

(
  cd "${ROOT_DIR}/backend"
  ASTERROUTER_SERVER_HTTP_LISTEN="${ASTERROUTER_SERVER_HTTP_LISTEN:-${BACKEND_HOST}:${BACKEND_PORT}}" \
    ASTERROUTER_SERVER_HTTP_FRONTEND_DIR="${ASTERROUTER_SERVER_HTTP_FRONTEND_DIR:-../frontend/dist}" \
    ASTERROUTER_SERVER_BOOTSTRAP_DEMO_MODE="${DEMO_MODE}" \
    go run ./cmd/asterrouter server
) &
PIDS+=("$!")

(
  cd "${ROOT_DIR}/frontend"
  VITE_DEV_PROXY_TARGET="${VITE_DEV_PROXY_TARGET:-${BACKEND_URL}}" \
    VITE_DEV_PORT="${VITE_DEV_PORT:-${FRONTEND_PORT}}" \
    npm run dev -- --host "${FRONTEND_HOST}" --port "${FRONTEND_PORT}"
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
