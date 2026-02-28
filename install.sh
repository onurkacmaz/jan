#!/usr/bin/env bash
set -euo pipefail

REPO="${JAN_REPO:-onurkacmaz/jan}"
BIN_NAME="${JAN_BIN_NAME:-jan}"
VERSION="${JAN_VERSION:-latest}"
INSTALL_DIR="${JAN_INSTALL_DIR:-${HOME}/.local/bin}"
SYSTEM_INSTALL=false
UNINSTALL=false

usage() {
  cat <<'EOF'
Usage: install.sh [options]

Options:
  --system          Install to /usr/local/bin
  --uninstall       Remove installed binary and exit
  --bin-dir <dir>   Install target directory (default: $HOME/.local/bin)
  --version <ver>   Release version/tag (default: latest)
  -h, --help        Show this help

Environment:
  JAN_REPO          GitHub repo (default: onurkacmaz/jan)
  JAN_BIN_NAME      Binary name (default: jan)
  JAN_VERSION       Release version/tag (default: latest)
  JAN_INSTALL_DIR   Install directory override
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --system)
      SYSTEM_INSTALL=true
      INSTALL_DIR="/usr/local/bin"
      shift
      ;;
    --uninstall)
      UNINSTALL=true
      shift
      ;;
    --bin-dir)
      if [[ $# -lt 2 ]]; then
        echo "error: --bin-dir requires a value" >&2
        exit 2
      fi
      INSTALL_DIR="$2"
      shift 2
      ;;
    --version)
      if [[ $# -lt 2 ]]; then
        echo "error: --version requires a value" >&2
        exit 2
      fi
      VERSION="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "error: unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

TARGET="${INSTALL_DIR}/${BIN_NAME}"
if [[ "${UNINSTALL}" == true ]]; then
  if [[ -w "${INSTALL_DIR}" ]] || [[ ! -d "${INSTALL_DIR}" ]]; then
    if [[ -f "${TARGET}" ]]; then
      rm -f "${TARGET}"
      echo "Removed: ${TARGET}"
    else
      echo "Not found: ${TARGET}"
    fi
    exit 0
  fi

  if command -v sudo >/dev/null 2>&1; then
    if sudo test -f "${TARGET}"; then
      sudo rm -f "${TARGET}"
      echo "Removed: ${TARGET}"
    else
      echo "Not found: ${TARGET}"
    fi
    exit 0
  fi

  echo "error: no write permission for ${INSTALL_DIR} and sudo is unavailable" >&2
  exit 1
fi

if ! command -v curl >/dev/null 2>&1; then
  echo "error: curl is required but not found in PATH" >&2
  exit 1
fi

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH_RAW="$(uname -m)"
case "${ARCH_RAW}" in
  x86_64|amd64)
    ARCH="amd64"
    ;;
  arm64|aarch64)
    ARCH="arm64"
    ;;
  *)
    echo "error: unsupported architecture: ${ARCH_RAW}" >&2
    exit 1
    ;;
esac

case "${OS}" in
  linux|darwin)
    ;;
  *)
    echo "error: unsupported operating system: ${OS}" >&2
    exit 1
    ;;
esac

ASSET="${BIN_NAME}-${OS}-${ARCH}"
if [[ "${VERSION}" == "latest" ]]; then
  URL="https://github.com/${REPO}/releases/latest/download/${ASSET}"
else
  URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET}"
fi

TMP_DIR="$(mktemp -d)"
cleanup() {
  rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

TMP_BIN="${TMP_DIR}/${BIN_NAME}"

echo "Downloading ${URL}..."
curl -fsSL "${URL}" -o "${TMP_BIN}"
chmod +x "${TMP_BIN}"

mkdir -p "${INSTALL_DIR}"

if [[ -w "${INSTALL_DIR}" ]]; then
  install -m 0755 "${TMP_BIN}" "${TARGET}"
else
  if command -v sudo >/dev/null 2>&1; then
    echo "Installing with sudo to ${TARGET}..."
    sudo install -m 0755 "${TMP_BIN}" "${TARGET}"
  else
    echo "error: no write permission for ${INSTALL_DIR} and sudo is unavailable" >&2
    exit 1
  fi
fi

echo "Installed: ${TARGET}"
if "${TARGET}" version >/dev/null 2>&1; then
  "${TARGET}" version
fi

case ":$PATH:" in
  *:"${INSTALL_DIR}":*)
    ;;
  *)
    echo "warning: ${INSTALL_DIR} is not in PATH" >&2
    echo "Add this to your shell profile:" >&2
    echo "  export PATH=\"${INSTALL_DIR}:\$PATH\"" >&2
    ;;
esac

if [[ "${SYSTEM_INSTALL}" == true ]]; then
  echo "Run: ${BIN_NAME} --help"
else
  echo "Run: ${TARGET} --help"
fi
