#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:-}"
if [ -z "$VERSION" ]; then
  VERSION="$(tr -d '\r\n' < "${ROOT_DIR}/backend/cmd/asterrouter/VERSION")"
fi
VERSION="${VERSION#v}"
TAG_NAME="v${VERSION}"
REPOSITORY="${GITHUB_REPOSITORY:-astercloud/asterrouter}"
COMMIT="${GITHUB_SHA:-}"
if [ -z "$COMMIT" ] && command -v git >/dev/null 2>&1; then
  COMMIT="$(git -C "$ROOT_DIR" rev-parse --short=12 HEAD 2>/dev/null || true)"
fi
COMMIT="${COMMIT:-unknown}"
BUILD_DATE="${BUILD_DATE:-$(date -u +'%Y-%m-%dT%H:%M:%SZ')}"
DIST_DIR="${ROOT_DIR}/dist"
PACKAGE_DIR="${DIST_DIR}/packages"

ldflags() {
  printf '%s' "-s -w"
  printf ' -X github.com/astercloud/asterrouter/backend/internal/buildinfo.Version=%s' "$VERSION"
  printf ' -X github.com/astercloud/asterrouter/backend/internal/buildinfo.Commit=%s' "$COMMIT"
  printf ' -X github.com/astercloud/asterrouter/backend/internal/buildinfo.Date=%s' "$BUILD_DATE"
  printf ' -X github.com/astercloud/asterrouter/backend/internal/buildinfo.BuildType=release'
}

checksum() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1"
    return
  fi
  shasum -a 256 "$1"
}

rm -rf "$DIST_DIR"
mkdir -p "$PACKAGE_DIR"

echo "==> Building frontend"
cd "${ROOT_DIR}/frontend"
if [ -f package-lock.json ]; then
  npm ci
else
  npm install
fi
npm run build

echo "==> Building Linux binaries"
for arch in amd64 arm64; do
  binary_asset="asterrouter_${VERSION}_linux_${arch}"
  binary_path="${DIST_DIR}/${binary_asset}"

  (
    cd "${ROOT_DIR}/backend"
    CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags "$(ldflags)" -o "$binary_path" ./cmd/asterrouter
  )

  archive_root="${PACKAGE_DIR}/${binary_asset}"
  mkdir -p "${archive_root}/frontend"
  cp "$binary_path" "${archive_root}/asterrouter"
  cp -R "${ROOT_DIR}/frontend/dist" "${archive_root}/frontend/dist"
  cp "${ROOT_DIR}/README.md" "${archive_root}/README.md"
  cp "${ROOT_DIR}/LICENSE" "${archive_root}/LICENSE"
  mkdir -p "${archive_root}/deploy"
  cp "${ROOT_DIR}/deploy/install.sh" "${archive_root}/deploy/install.sh"
  cp "${ROOT_DIR}/deploy/asterrouter" "${archive_root}/deploy/asterrouter"
  cp "${ROOT_DIR}/deploy/asterrouter.service" "${archive_root}/deploy/asterrouter.service"
  cp "${ROOT_DIR}/deploy/asterrouter.env.example" "${archive_root}/deploy/asterrouter.env.example"

  chmod 0755 "${archive_root}/asterrouter" "${archive_root}/deploy/install.sh" "${archive_root}/deploy/asterrouter"
  tar -C "$PACKAGE_DIR" -czf "${DIST_DIR}/${binary_asset}.tar.gz" "$binary_asset"
done

echo "==> Writing checksums"
(
  cd "$DIST_DIR"
  : > checksums.txt
  for file in \
    asterrouter_*_linux_amd64 \
    asterrouter_*_linux_arm64 \
    asterrouter_*_linux_amd64.tar.gz \
    asterrouter_*_linux_arm64.tar.gz; do
    [ -f "$file" ] || continue
    checksum "$file" >> checksums.txt
  done
)

echo "==> Writing update manifest"
python3 - "$DIST_DIR" "$VERSION" "$TAG_NAME" "$REPOSITORY" "$BUILD_DATE" <<'PY'
import hashlib
import json
import os
import sys

dist_dir, version, tag_name, repository, build_date = sys.argv[1:]
base_url = f"https://github.com/{repository}/releases/download/{tag_name}"
html_url = f"https://github.com/{repository}/releases/tag/{tag_name}"
assets = []

for arch in ("amd64", "arm64"):
    name = f"asterrouter_{version}_linux_{arch}"
    path = os.path.join(dist_dir, name)
    with open(path, "rb") as f:
        sha256 = hashlib.sha256(f.read()).hexdigest()
    assets.append({
        "name": name,
        "url": f"{base_url}/{name}",
        "os": "linux",
        "arch": arch,
        "sha256": sha256,
        "size": os.path.getsize(path),
    })

release = {
    "version": version,
    "channel": "stable",
    "name": f"AsterRouter {version}",
    "notes": "Customer portal, account security, social OAuth, billing governance, retention cleanup, and backup automation improvements.",
    "published_at": build_date,
    "html_url": html_url,
    "assets": assets,
}
manifest = {
    **release,
    "releases": [release],
}

with open(os.path.join(dist_dir, "asterrouter_update_manifest.json"), "w", encoding="utf-8") as f:
    json.dump(manifest, f, ensure_ascii=False, indent=2)
    f.write("\n")
PY

rm -rf "$PACKAGE_DIR"

echo "==> Release assets are ready in ${DIST_DIR}"
