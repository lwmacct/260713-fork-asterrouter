#!/usr/bin/env bash

set -euo pipefail

GITHUB_REPO="${ASTERROUTER_GITHUB_REPO:-astercloud/asterrouter}"
INSTALL_DIR="${ASTERROUTER_INSTALL_DIR:-/opt/asterrouter}"
CONFIG_DIR="${ASTERROUTER_CONFIG_DIR:-/etc/asterrouter}"
DATA_DIR="${ASTERROUTER_DATA_DIR:-/var/lib/asterrouter}"
SERVICE_NAME="${ASTERROUTER_SERVICE_NAME:-asterrouter}"
SERVICE_USER="${ASTERROUTER_SERVICE_USER:-asterrouter}"
SERVICE_FILE="${ASTERROUTER_SERVICE_FILE:-/etc/systemd/system/${SERVICE_NAME}.service}"
ENV_FILE="${CONFIG_DIR}/asterrouter.env"
BINARY_NAME="asterrouter"
COMMAND_NAME="asterrouter"
COMMAND_PATH="${ASTERROUTER_COMMAND_PATH:-/usr/local/bin/${COMMAND_NAME}}"
DEFAULT_ADDR="127.0.0.1:8082"
REMOTE_RAW_BASE="${ASTERROUTER_REMOTE_RAW_BASE:-https://raw.githubusercontent.com/${GITHUB_REPO}/main/deploy}"
RELEASE_BASE_URL="${ASTERROUTER_RELEASE_BASE_URL:-https://github.com/${GITHUB_REPO}/releases/download}"
DOWNLOAD_TMP=""

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

info() { echo -e "${BLUE}[INFO]${NC} $*"; }
success() { echo -e "${GREEN}[SUCCESS]${NC} $*"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }

cleanup() {
  if [ -n "${DOWNLOAD_TMP:-}" ]; then
    rm -rf "$DOWNLOAD_TMP"
  fi
}

trap cleanup EXIT

usage() {
  cat <<EOF
Usage: install.sh [command] [options]

Commands:
  install [-v VERSION]      Install AsterRouter
  upgrade [-v VERSION]      Upgrade AsterRouter
  update [-v VERSION]       Alias for upgrade
  rollback VERSION          Install a pinned release as rollback
  versions                  List available release versions
  install-command           Install / refresh ${COMMAND_PATH}
  uninstall [--purge]       Remove service and install dir; --purge also removes config

Environment overrides:
  ASTERROUTER_INSTALL_DIR   Default: ${INSTALL_DIR}
  ASTERROUTER_CONFIG_DIR    Default: ${CONFIG_DIR}
  ASTERROUTER_DATA_DIR      Default: ${DATA_DIR}
  ASTERROUTER_SERVICE_FILE  Default: ${SERVICE_FILE}
  ASTERROUTER_COMMAND_PATH  Default: ${COMMAND_PATH}
  ASTERROUTER_RELEASE_BASE_URL  Default: ${RELEASE_BASE_URL}
EOF
}

require_root() {
  if [ "$(id -u)" -ne 0 ]; then
    error "Please run as root or with sudo."
    exit 1
  fi
}

require_commands() {
  local missing=()
  for cmd in curl tar systemctl; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
      missing+=("$cmd")
    fi
  done
  if [ "${#missing[@]}" -gt 0 ]; then
    error "Missing required commands: ${missing[*]}"
    exit 1
  fi
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    *) error "Unsupported architecture: $(uname -m)"; exit 1 ;;
  esac
}

detect_os() {
  case "$(uname -s | tr '[:upper:]' '[:lower:]')" in
    linux) echo "linux" ;;
    *) error "Only Linux systemd deployments are supported."; exit 1 ;;
  esac
}

random_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 32
    return
  fi
  dd if=/dev/urandom bs=32 count=1 2>/dev/null | od -An -tx1 | tr -d ' \n'
  echo
}

random_password() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 24 | tr -d '/+=' | head -c 24
    echo
    return
  fi
  dd if=/dev/urandom bs=24 count=1 2>/dev/null | base64 | tr -dc 'A-Za-z0-9' | head -c 24
  echo
}

latest_version() {
  curl -fsSL "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" \
    | grep -m1 '"tag_name"' \
    | sed -E 's/.*"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/'
}

list_versions() {
  curl -fsSL "https://api.github.com/repos/${GITHUB_REPO}/releases?per_page=50" \
    | grep '"tag_name"' \
    | sed -E 's/.*"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/'
}

normalize_version() {
  local value="$1"
  if [ -z "$value" ]; then
    latest_version
    return
  fi
  case "$value" in
    v*) echo "$value" ;;
    *) echo "v${value}" ;;
  esac
}

checksum_verify() {
  local checksums_file="$1"
  local asset_name="$2"
  local expected
  expected="$(grep -E "[[:space:]]${asset_name}$" "$checksums_file" | awk '{print $1}' | head -n1)"
  if [ -z "$expected" ]; then
    error "Checksum for ${asset_name} was not found."
    exit 1
  fi

  if command -v sha256sum >/dev/null 2>&1; then
    echo "${expected}  ${asset_name}" | sha256sum -c -
    return
  fi
  echo "${expected}  ${asset_name}" | shasum -a 256 -c -
}

current_version() {
  if [ -x "${INSTALL_DIR}/${BINARY_NAME}" ]; then
    "${INSTALL_DIR}/${BINARY_NAME}" --version 2>/dev/null | awk 'NR==1 {print $2}'
    return
  fi
  echo "not-installed"
}

create_user() {
  if id "$SERVICE_USER" >/dev/null 2>&1; then
    return
  fi
  info "Creating system user ${SERVICE_USER}"
  useradd --system --home-dir "$INSTALL_DIR" --shell /usr/sbin/nologin "$SERVICE_USER"
}

create_dirs() {
  install -d -m 0755 "$INSTALL_DIR" "$DATA_DIR" "$CONFIG_DIR"
  chown -R "${SERVICE_USER}:${SERVICE_USER}" "$INSTALL_DIR" "$DATA_DIR" "$CONFIG_DIR"
}

create_env_if_missing() {
  if [ -f "$ENV_FILE" ]; then
    return
  fi

  local secret admin_password
  secret="$(random_secret)"
  admin_password="$(random_password)"

  cat > "$ENV_FILE" <<EOF
GIN_MODE=release
ASTER_ADDR=${DEFAULT_ADDR}
ASTER_FRONTEND_DIR=${INSTALL_DIR}/frontend/dist
ASTER_PROFILE=enterprise
PUBLIC_BASE_URL=

ASTER_ADMIN_USERNAME=admin
ASTER_ADMIN_PASSWORD=${admin_password}
ASTER_ADMIN_TOKEN=

# Required before the service can start.
DATABASE_URL=
ASTER_SECRET_KEY=${secret}

ASTER_BUILD_TYPE=release
ASTER_UPDATE_MANIFEST_URL=https://github.com/${GITHUB_REPO}/releases/latest/download/asterrouter_update_manifest.json
ASTER_ALLOW_RESTART=true
EOF

  chmod 0640 "$ENV_FILE"
  chown "${SERVICE_USER}:${SERVICE_USER}" "$ENV_FILE"
  warn "Created ${ENV_FILE}. Set DATABASE_URL before starting AsterRouter."
  warn "Generated admin login: admin / ${admin_password}"
}

load_env() {
  if [ -f "$ENV_FILE" ]; then
    set -a
    # shellcheck disable=SC1090
    . "$ENV_FILE"
    set +a
  fi
}

env_ready_for_start() {
  load_env
  if [ -z "${DATABASE_URL:-}" ]; then
    warn "DATABASE_URL is empty in ${ENV_FILE}; service start is skipped."
    return 1
  fi
  if [ -z "${ASTER_SECRET_KEY:-}" ] || [ "${ASTER_SECRET_KEY:-}" = "asterrouter-local-development-secret" ]; then
    warn "ASTER_SECRET_KEY is missing or uses the development default; service start is skipped."
    return 1
  fi
  if [ -z "${ASTER_ADMIN_PASSWORD:-}" ] && [ -z "${ASTER_ADMIN_TOKEN:-}" ]; then
    warn "ASTER_ADMIN_PASSWORD or ASTER_ADMIN_TOKEN is required; service start is skipped."
    return 1
  fi
  return 0
}

health_url() {
  load_env
  local addr host port
  addr="${ASTER_ADDR:-${DEFAULT_ADDR}}"
  case "$addr" in
    :*)
      host="127.0.0.1"
      port="${addr#:}"
      ;;
    *:*)
      host="${addr%:*}"
      port="${addr##*:}"
      if [ "$host" = "0.0.0.0" ] || [ "$host" = "" ]; then
        host="127.0.0.1"
      fi
      ;;
    *)
      host="127.0.0.1"
      port="$addr"
      ;;
  esac
  echo "http://${host}:${port}/health"
}

wait_health() {
  local url
  url="$(health_url)"
  for _ in $(seq 1 30); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      success "Health check passed: ${url}"
      return
    fi
    sleep 1
  done
  error "Service did not become healthy: ${url}"
  journalctl -u "$SERVICE_NAME" -n 80 --no-pager || true
  exit 1
}

install_command() {
  local source_path="${INSTALL_DIR}/deploy/${COMMAND_NAME}"
  if [ ! -f "$source_path" ]; then
    local tmp
    tmp="$(mktemp)"
    curl -fsSL "${REMOTE_RAW_BASE}/${COMMAND_NAME}" -o "$tmp"
    install -m 0755 "$tmp" "$COMMAND_PATH"
    rm -f "$tmp"
  else
    install -m 0755 "$source_path" "$COMMAND_PATH"
  fi
  success "Command wrapper installed to ${COMMAND_PATH}"
}

install_release() {
  local requested="${1:-}"
  local tag version os arch asset base_url tmp archive_name extract_dir backup_dir

  require_root
  require_commands

  os="$(detect_os)"
  arch="$(detect_arch)"
  tag="$(normalize_version "$requested")"
  version="${tag#v}"
  asset="asterrouter_${version}_${os}_${arch}"
  archive_name="${asset}.tar.gz"
  base_url="${RELEASE_BASE_URL}/${tag}"
  DOWNLOAD_TMP="$(mktemp -d)"
  tmp="$DOWNLOAD_TMP"
  extract_dir="${tmp}/${asset}"

  info "Installing AsterRouter ${tag} for ${os}/${arch}"
  curl -fL "${base_url}/${archive_name}" -o "${tmp}/${archive_name}"
  curl -fL "${base_url}/checksums.txt" -o "${tmp}/checksums.txt"
  (
    cd "$tmp"
    checksum_verify "checksums.txt" "$archive_name"
    tar -xzf "$archive_name"
  )

  if [ ! -x "${extract_dir}/${BINARY_NAME}" ]; then
    error "Release archive is missing ${BINARY_NAME}."
    exit 1
  fi
  if [ ! -f "${extract_dir}/frontend/dist/index.html" ]; then
    error "Release archive is missing frontend/dist/index.html."
    exit 1
  fi

  create_user
  create_dirs
  create_env_if_missing

  if systemctl list-unit-files "${SERVICE_NAME}.service" >/dev/null 2>&1; then
    systemctl stop "$SERVICE_NAME" >/dev/null 2>&1 || true
  fi

  if [ -x "${INSTALL_DIR}/${BINARY_NAME}" ]; then
    backup_dir="${INSTALL_DIR}/backups/$(date -u +'%Y%m%dT%H%M%SZ')-$(current_version)"
    install -d -m 0755 "$backup_dir"
    cp -a "${INSTALL_DIR}/${BINARY_NAME}" "$backup_dir/" || true
    [ -d "${INSTALL_DIR}/frontend" ] && cp -a "${INSTALL_DIR}/frontend" "$backup_dir/" || true
    [ -d "${INSTALL_DIR}/deploy" ] && cp -a "${INSTALL_DIR}/deploy" "$backup_dir/" || true
    info "Backup created at ${backup_dir}"
  fi

  install -m 0755 "${extract_dir}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
  rm -rf "${INSTALL_DIR}/frontend" "${INSTALL_DIR}/deploy"
  cp -R "${extract_dir}/frontend" "${INSTALL_DIR}/frontend"
  cp -R "${extract_dir}/deploy" "${INSTALL_DIR}/deploy"
  install -m 0755 "${INSTALL_DIR}/deploy/install.sh" "${INSTALL_DIR}/install.sh"
  install -m 0644 "${INSTALL_DIR}/deploy/asterrouter.service" "$SERVICE_FILE"
  install_command
  chown -R "${SERVICE_USER}:${SERVICE_USER}" "$INSTALL_DIR" "$DATA_DIR" "$CONFIG_DIR"

  systemctl daemon-reload

  if env_ready_for_start; then
    systemctl enable --now "$SERVICE_NAME"
    wait_health
  else
    warn "AsterRouter ${tag} is installed but not started."
    warn "Edit ${ENV_FILE}, then run: systemctl enable --now ${SERVICE_NAME}"
  fi

  success "AsterRouter ${tag} installed."
}

uninstall() {
  local purge="${1:-}"
  require_root

  systemctl disable --now "$SERVICE_NAME" >/dev/null 2>&1 || true
  rm -f "$SERVICE_FILE" "$COMMAND_PATH"
  systemctl daemon-reload || true
  rm -rf "$INSTALL_DIR" "$DATA_DIR"

  if [ "$purge" = "--purge" ]; then
    rm -rf "$CONFIG_DIR"
    warn "Config directory removed: ${CONFIG_DIR}"
  else
    warn "Config directory kept: ${CONFIG_DIR}"
  fi

  success "AsterRouter uninstalled."
}

parse_version_flag() {
  local requested=""
  while [ "$#" -gt 0 ]; do
    case "$1" in
      -v|--version)
        if [ -z "${2:-}" ]; then
          error "$1 requires a version."
          exit 1
        fi
        requested="$2"
        shift 2
        ;;
      *)
        requested="$1"
        shift
        ;;
    esac
  done
  echo "$requested"
}

command="${1:-install}"
if [ "$#" -gt 0 ]; then
  shift
fi

case "$command" in
  ""|help|--help|-h)
    usage
    ;;
  install|upgrade|update)
    install_release "$(parse_version_flag "$@")"
    ;;
  rollback)
    version="$(parse_version_flag "$@")"
    if [ -z "$version" ]; then
      error "rollback requires a target version."
      exit 1
    fi
    install_release "$version"
    ;;
  versions|list-versions)
    list_versions
    ;;
  install-command)
    require_root
    install_command
    ;;
  uninstall|remove)
    uninstall "${1:-}"
    ;;
  *)
    error "Unknown command: ${command}"
    usage
    exit 1
    ;;
esac
