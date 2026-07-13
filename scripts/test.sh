#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARTIFACT_DIR="${ASTER_TEST_ARTIFACT_DIR:-${TMPDIR:-/tmp}/asterrouter-test-artifacts}"

mkdir -p "${ARTIFACT_DIR}"

backend() {
  local unformatted
  unformatted="$(cd "${ROOT_DIR}/backend" && gofmt -l .)"
  if [ -n "${unformatted}" ]; then
    echo "Go files require gofmt:" >&2
    echo "${unformatted}" >&2
    return 1
  fi

  (
    cd "${ROOT_DIR}/backend"
    go vet ./...
    go test -covermode=atomic -coverprofile="${ARTIFACT_DIR}/backend-coverage.out" ./...
  )
}

frontend() {
  (
    cd "${ROOT_DIR}/frontend"
    npm run check:enterprise-surface
    npm run typecheck
    npm run test:unit:coverage
    npm run build
  )
}

e2e() {
  (
    cd "${ROOT_DIR}/frontend"
    npm run test:e2e
  )
}

case "${1:-all}" in
  backend) backend ;;
  frontend) frontend ;;
  e2e) e2e ;;
  all)
    backend
    frontend
    ;;
  *)
    echo "Usage: $0 [backend|frontend|e2e|all]" >&2
    exit 2
    ;;
esac

echo "Test artifacts: ${ARTIFACT_DIR}"
