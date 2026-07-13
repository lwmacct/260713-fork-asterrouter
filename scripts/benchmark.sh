#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARTIFACT_DIR="${ASTER_TEST_ARTIFACT_DIR:-${TMPDIR:-/tmp}/asterrouter-test-artifacts}"

mkdir -p "${ARTIFACT_DIR}"
(
  cd "${ROOT_DIR}/backend"
  go test -run '^$' -bench '^BenchmarkGateway' -benchmem -count=5 ./internal/server | tee "${ARTIFACT_DIR}/gateway-benchmark.txt"
)

echo "Benchmark artifact: ${ARTIFACT_DIR}/gateway-benchmark.txt"
