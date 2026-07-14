#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARTIFACT_DIR="${ASTER_TEST_ARTIFACT_DIR:-${TMPDIR:-/tmp}/asterrouter-test-artifacts}"

mkdir -p "${ARTIFACT_DIR}"

backend_checks() {
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
    go test ./...
  )
}

backend_coverage() {
  (
    cd "${ROOT_DIR}/backend"
    go test -covermode=atomic -coverprofile="${ARTIFACT_DIR}/backend-coverage.out" ./...
  )
}

backend() {
  backend_checks
  backend_coverage
}

frontend() {
  (
    cd "${ROOT_DIR}/frontend"
    npm run check:public-doc-links
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
  backend-checks) backend_checks ;;
  backend-coverage) backend_coverage ;;
  frontend) frontend ;;
  e2e) e2e ;;
  all)
    backend
    frontend
    e2e
    ;;
  *)
    echo "Usage: $0 [backend|backend-checks|backend-coverage|frontend|e2e|all]" >&2
    exit 2
    ;;
esac

echo "Test artifacts: ${ARTIFACT_DIR}"
