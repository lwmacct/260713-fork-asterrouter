#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:-$(tr -d '\r\n' < "${ROOT_DIR}/backend/cmd/asterrouter/VERSION")}"
VERSION="${VERSION#v}"
DIST_DIR="${ASTER_DIST_DIR:-${ROOT_DIR}/dist}"

for arch in amd64 arm64; do
  binary="${DIST_DIR}/asterrouter_${VERSION}_linux_${arch}"
  archive="${binary}.tar.gz"
  test -x "${binary}"
  test -s "${archive}"
  archive_listing="$(tar -tzf "${archive}")"
  grep -Fxq "asterrouter_${VERSION}_linux_${arch}/asterrouter" <<<"${archive_listing}"
  grep -Fxq "asterrouter_${VERSION}_linux_${arch}/frontend/dist/index.html" <<<"${archive_listing}"
  grep -Fxq "asterrouter_${VERSION}_linux_${arch}/deploy/asterrouter.service" <<<"${archive_listing}"
done

(
  cd "${DIST_DIR}"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum -c checksums.txt
  else
    shasum -a 256 -c checksums.txt
  fi
)

file "${DIST_DIR}/asterrouter_${VERSION}_linux_amd64" | grep -Eq 'x86-64|x86_64'
file "${DIST_DIR}/asterrouter_${VERSION}_linux_arm64" | grep -Eq 'ARM aarch64|arm64'

if [ "$(uname -s)" = "Linux" ] && [ "$(uname -m)" = "x86_64" ]; then
  "${DIST_DIR}/asterrouter_${VERSION}_linux_amd64" --version | grep -Fq "asterrouter ${VERSION}"
fi

if [ "${ASTER_TEST_ARM64_RUNTIME:-0}" = "1" ]; then
  docker run --rm --platform linux/arm64 \
    -v "${DIST_DIR}:/dist:ro" \
    alpine:3.22 \
    "/dist/asterrouter_${VERSION}_linux_arm64" --version | grep -Fq "asterrouter ${VERSION}"
fi

python3 - "${DIST_DIR}" "${VERSION}" <<'PY'
import hashlib
import json
import os
import sys

dist_dir, version = sys.argv[1:]
with open(os.path.join(dist_dir, "asterrouter_update_manifest.json"), encoding="utf-8") as handle:
    manifest = json.load(handle)

assert manifest["version"] == version
assert manifest["channel"] == "stable"
assets = {asset["arch"]: asset for asset in manifest["assets"]}
assert set(assets) == {"amd64", "arm64"}

for arch, asset in assets.items():
    name = f"asterrouter_{version}_linux_{arch}"
    path = os.path.join(dist_dir, name)
    with open(path, "rb") as handle:
        digest = hashlib.sha256(handle.read()).hexdigest()
    assert asset["name"] == name
    assert asset["os"] == "linux"
    assert asset["sha256"] == digest
    assert asset["size"] == os.path.getsize(path)
PY

echo "Release artifact validation passed for ${VERSION}."
