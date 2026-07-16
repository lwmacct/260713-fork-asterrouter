#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARTIFACT_DIR="${ASTER_TEST_ARTIFACT_DIR:-${TMPDIR:-/tmp}/asterrouter-test-artifacts}"
PORT="${ASTER_CONTAINER_TEST_PORT:-18080}"
RUN_ID="${GITHUB_RUN_ID:-local}-$$"
IMAGE="asterrouter:container-test"
NETWORK="asterrouter-test-${RUN_ID}"
POSTGRES_CONTAINER="asterrouter-postgres-${RUN_ID}"
APP_CONTAINER="asterrouter-app-${RUN_ID}"

mkdir -p "${ARTIFACT_DIR}"

cleanup() {
  docker logs "${APP_CONTAINER}" >"${ARTIFACT_DIR}/container-app.log" 2>&1 || true
  docker logs "${POSTGRES_CONTAINER}" >"${ARTIFACT_DIR}/container-postgres.log" 2>&1 || true
  docker rm -f "${APP_CONTAINER}" "${POSTGRES_CONTAINER}" >/dev/null 2>&1 || true
  docker network rm "${NETWORK}" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

docker build \
  --build-arg ASTER_VERSION=container-test \
  --build-arg ASTER_BUILD_TYPE=release \
  -t "${IMAGE}" "${ROOT_DIR}"

test "$(docker image inspect "${IMAGE}" --format '{{.Config.User}}')" = "asterrouter"

docker network create "${NETWORK}" >/dev/null
docker run -d --name "${POSTGRES_CONTAINER}" --network "${NETWORK}" --network-alias postgres \
  -e POSTGRES_DB=asterrouter_test \
  -e POSTGRES_USER=asterrouter \
  -e POSTGRES_PASSWORD=asterrouter \
  postgres:16-alpine >/dev/null

for _ in $(seq 1 60); do
  if docker exec "${POSTGRES_CONTAINER}" psql -U asterrouter -d asterrouter_test -c 'select 1' >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
docker exec "${POSTGRES_CONTAINER}" psql -U asterrouter -d asterrouter_test -c 'select 1' >/dev/null

if docker run --rm --network "${NETWORK}" "${IMAGE}" >"${ARTIFACT_DIR}/container-fail-closed.log" 2>&1; then
  echo "Release container unexpectedly started without required configuration." >&2
  exit 1
fi
grep -Fq 'server.storage.database-url is required in release builds' "${ARTIFACT_DIR}/container-fail-closed.log"

docker run -d --name "${APP_CONTAINER}" --network "${NETWORK}" -p "127.0.0.1:${PORT}:8080" \
  -e ASTERROUTER_SERVER_BOOTSTRAP_DEMO_MODE=true \
  -e ASTERROUTER_SERVER_SECURITY_SECRET_KEY=asterrouter-container-test-secret \
  -e ASTERROUTER_SERVER_STORAGE_DATABASE_URL='postgres://asterrouter:asterrouter@postgres:5432/asterrouter_test?sslmode=disable' \
  "${IMAGE}" >/dev/null

for _ in $(seq 1 90); do
  if curl -fsS "http://127.0.0.1:${PORT}/ready" 2>/dev/null | grep -Fq '"status":"ready"'; then
    break
  fi
  sleep 1
done

curl -fsS "http://127.0.0.1:${PORT}/ready" | grep -Fq '"status":"ready"'
curl -fsS "http://127.0.0.1:${PORT}/console/overview" | grep -Fq '<div id="app"></div>'
test "$(docker exec "${APP_CONTAINER}" id -u)" = "10001"

docker stop --time 15 "${APP_CONTAINER}" >/dev/null
test "$(docker inspect "${APP_CONTAINER}" --format '{{.State.ExitCode}}')" = "0"

echo "Container acceptance passed on port ${PORT}."
