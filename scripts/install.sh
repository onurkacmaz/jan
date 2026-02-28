#!/usr/bin/env bash
set -euo pipefail

BIN_NAME="jan"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
INSTALL_DIR="${JAN_INSTALL_DIR:-${HOME}/.local/bin}"
SYSTEM_INSTALL=false

usage() {
  cat <<EOF
Usage: $(basename "$0") [options]

Options:
  --bin-dir <dir>   Install target directory (default: \$HOME/.local/bin)
  --system          Install to /usr/local/bin
  -h, --help        Show this help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --bin-dir)
      if [[ $# -lt 2 ]]; then
        echo "error: --bin-dir requires a value" >&2
        exit 2
      fi
      INSTALL_DIR="$2"
      shift 2
      ;;
    --system)
      SYSTEM_INSTALL=true
      INSTALL_DIR="/usr/local/bin"
      shift
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

if ! command -v go >/dev/null 2>&1; then
  echo "error: Go is required but not found in PATH" >&2
  exit 1
fi

TMP_DIR="$(mktemp -d)"
cleanup() {
  rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

VERSION="dev"
if command -v git >/dev/null 2>&1; then
  if git -C "${REPO_ROOT}" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    VERSION="$(git -C "${REPO_ROOT}" describe --tags --always --dirty 2>/dev/null || true)"
    if [[ -z "${VERSION}" ]]; then
      VERSION="$(git -C "${REPO_ROOT}" rev-parse --short HEAD 2>/dev/null || true)"
    fi
    if [[ -z "${VERSION}" ]]; then
      VERSION="dev"
    fi
  fi
fi

echo "Building ${BIN_NAME}..."
echo "Version: ${VERSION}"
(
  cd "${REPO_ROOT}"
  go build -ldflags "-X main.version=${VERSION}" -o "${TMP_DIR}/${BIN_NAME}" ./cmd/jan
)

mkdir -p "${INSTALL_DIR}"

TARGET="${INSTALL_DIR}/${BIN_NAME}"
if [[ -w "${INSTALL_DIR}" ]]; then
  install -m 0755 "${TMP_DIR}/${BIN_NAME}" "${TARGET}"
else
  if command -v sudo >/dev/null 2>&1; then
    echo "Installing with sudo to ${TARGET}..."
    sudo install -m 0755 "${TMP_DIR}/${BIN_NAME}" "${TARGET}"
  else
    echo "error: no write permission for ${INSTALL_DIR} and sudo is unavailable" >&2
    exit 1
  fi
fi

echo "Installed: ${TARGET}"

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
