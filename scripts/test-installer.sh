#!/usr/bin/env bash
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "Installer acceptance must run as root inside an isolated Linux runner." >&2
  exit 1
fi
if [ "$(uname -s)" != "Linux" ]; then
  echo "Installer acceptance requires Linux." >&2
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK_DIR="${ASTER_INSTALLER_TEST_DIR:-${TMPDIR:-/tmp}/asterrouter-installer-test-$$}"
RELEASE_DIR="${WORK_DIR}/releases"
FAKE_BIN="${WORK_DIR}/fake-bin"
INSTALL_DIR="${WORK_DIR}/install"
CONFIG_DIR="${WORK_DIR}/config"
DATA_DIR="${WORK_DIR}/data"
SERVICE_FILE="${WORK_DIR}/systemd/asterrouter-test.service"
COMMAND_PATH="${WORK_DIR}/bin/asterrouter"
SYSTEMCTL_LOG="${WORK_DIR}/systemctl.log"
REPORT="${WORK_DIR}/report.txt"

mkdir -p "${RELEASE_DIR}" "${FAKE_BIN}" "$(dirname "${SERVICE_FILE}")" "$(dirname "${COMMAND_PATH}")"

ARCH="$(uname -m)"
case "${ARCH}" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "Unsupported test architecture: ${ARCH}" >&2; exit 1 ;;
esac

create_release() {
  local version="$1"
  local tag="v${version}"
  local asset="asterrouter_${version}_linux_${ARCH}"
  local package_dir="${WORK_DIR}/package-${version}/${asset}"
  local release_dir="${RELEASE_DIR}/${tag}"

  mkdir -p "${package_dir}/frontend/dist" "${package_dir}/deploy" "${release_dir}"
  printf '#!/usr/bin/env sh\nprintf "asterrouter %s\\n"\n' "${version}" >"${package_dir}/asterrouter"
  chmod 0755 "${package_dir}/asterrouter"
  printf '<!doctype html><div id="app"></div>\n' >"${package_dir}/frontend/dist/index.html"
  cp "${ROOT_DIR}/deploy/install.sh" "${package_dir}/deploy/install.sh"
  cp "${ROOT_DIR}/deploy/asterrouter" "${package_dir}/deploy/asterrouter"
  cp "${ROOT_DIR}/deploy/asterrouter.service" "${package_dir}/deploy/asterrouter.service"
  cp "${ROOT_DIR}/deploy/asterrouter.env.example" "${package_dir}/deploy/asterrouter.env.example"
  chmod 0755 "${package_dir}/deploy/install.sh" "${package_dir}/deploy/asterrouter"
  tar -C "$(dirname "${package_dir}")" -czf "${release_dir}/${asset}.tar.gz" "${asset}"
  (
    cd "${release_dir}"
    sha256sum "${asset}.tar.gz" >checksums.txt
  )
}

create_release 0.3.0
create_release 0.4.0
create_release 0.5.0

cat >"${FAKE_BIN}/curl" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
output=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) output="$2"; shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
test -n "${output}" && test -n "${url}"
tag="$(basename "$(dirname "${url}")")"
asset="$(basename "${url}")"
cp "${ASTER_INSTALLER_TEST_RELEASE_DIR}/${tag}/${asset}" "${output}"
SH
chmod 0755 "${FAKE_BIN}/curl"

cat >"${FAKE_BIN}/systemctl" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${ASTER_INSTALLER_TEST_SYSTEMCTL_LOG}"
exit 0
SH
chmod 0755 "${FAKE_BIN}/systemctl"

export PATH="${FAKE_BIN}:${PATH}"
export ASTER_INSTALLER_TEST_RELEASE_DIR="${RELEASE_DIR}"
export ASTER_INSTALLER_TEST_SYSTEMCTL_LOG="${SYSTEMCTL_LOG}"
export ASTERROUTER_INSTALL_DIR="${INSTALL_DIR}"
export ASTERROUTER_CONFIG_DIR="${CONFIG_DIR}"
export ASTERROUTER_DATA_DIR="${DATA_DIR}"
export ASTERROUTER_SERVICE_NAME=asterrouter-test
export ASTERROUTER_SERVICE_USER=root
export ASTERROUTER_SERVICE_FILE="${SERVICE_FILE}"
export ASTERROUTER_COMMAND_PATH="${COMMAND_PATH}"
export ASTERROUTER_RELEASE_BASE_URL=https://release.test

if bash "${ROOT_DIR}/deploy/install.sh" install -v 0.3.0 >"${WORK_DIR}/deployment-role-rejection.log" 2>&1; then
  echo "Installer accepted a new installation without a deployment role." >&2
  exit 1
fi
grep -q 'deployment role is required' "${WORK_DIR}/deployment-role-rejection.log"

bash "${ROOT_DIR}/deploy/install.sh" install -v 0.3.0 --deployment platform
test "$("${INSTALL_DIR}/asterrouter" --version)" = "asterrouter 0.3.0"
test -f "${INSTALL_DIR}/frontend/dist/index.html"
test -x "${COMMAND_PATH}"
test -f "${SERVICE_FILE}"
grep -q '^ASTERROUTER_SERVER_BOOTSTRAP_DEPLOYMENT_ROLE=platform$' "${CONFIG_DIR}/asterrouter.env"

bash "${ROOT_DIR}/deploy/install.sh" upgrade -v 0.4.0
test "$("${INSTALL_DIR}/asterrouter" --version)" = "asterrouter 0.4.0"
previous_binary="$(find "${INSTALL_DIR}/backups" -path '*-0.3.0/asterrouter' -type f -print -quit)"
test -n "${previous_binary}"
test "$("${previous_binary}" --version)" = "asterrouter 0.3.0"

bash "${ROOT_DIR}/deploy/install.sh" rollback 0.3.0
test "$("${INSTALL_DIR}/asterrouter" --version)" = "asterrouter 0.3.0"
upgraded_binary="$(find "${INSTALL_DIR}/backups" -path '*-0.4.0/asterrouter' -type f -print -quit)"
test -n "${upgraded_binary}"
test "$("${upgraded_binary}" --version)" = "asterrouter 0.4.0"

printf 'tampered\n' >>"${RELEASE_DIR}/v0.5.0/asterrouter_0.5.0_linux_${ARCH}.tar.gz"
if bash "${ROOT_DIR}/deploy/install.sh" upgrade -v 0.5.0 >"${WORK_DIR}/checksum-rejection.log" 2>&1; then
  echo "Installer accepted a release archive with a mismatched checksum." >&2
  exit 1
fi
grep -Eq 'FAILED|checksum' "${WORK_DIR}/checksum-rejection.log"

grep -q '^daemon-reload$' "${SYSTEMCTL_LOG}"
grep -q '^stop asterrouter-test$' "${SYSTEMCTL_LOG}"
if grep -q '^enable --now asterrouter-test$' "${SYSTEMCTL_LOG}"; then
  echo "Installer started the service even though ASTERROUTER_SERVER_STORAGE_DATABASE_URL was empty." >&2
  exit 1
fi

{
  echo 'installer_acceptance=passed'
  echo 'install_version=0.3.0'
  echo 'upgrade_version=0.4.0'
  echo 'rollback_version=0.3.0'
  echo 'deployment_role_rejection=passed'
  echo 'checksum_rejection=passed'
} >"${REPORT}"

echo "Installer acceptance passed. Evidence: ${REPORT}"
