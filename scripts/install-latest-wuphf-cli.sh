#!/usr/bin/env bash
set -euo pipefail

if ! command -v npm >/dev/null 2>&1; then
  echo "npm is required to install the latest WUPHF CLI." >&2
  exit 1
fi

PKG="${WUPHF_CLI_PACKAGE:-@wuphf/wuphf}"

echo "Installing latest ${PKG}..."
npm install -g "${PKG}@latest"

echo
echo "Done. Verify with:"
echo "  wuphf --version"
