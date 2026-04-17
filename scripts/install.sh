#!/bin/sh
set -e

REPO="nex-crm/wuphf"
BINARY="wuphf"

# Detect OS
OS="$(uname -s)"
case "$OS" in
  Darwin) OS="darwin" ;;
  Linux)  OS="linux" ;;
  *)
    printf "Error: unsupported OS: %s\n" "$OS" >&2
    exit 1
    ;;
esac

# Detect architecture
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  amd64)   ARCH="amd64" ;;
  arm64)   ARCH="arm64" ;;
  aarch64) ARCH="arm64" ;;
  *)
    printf "Error: unsupported architecture: %s\n" "$ARCH" >&2
    exit 1
    ;;
esac

# WUPHF_INSTALL_URL_OVERRIDE bypasses GitHub release resolution so CI can run
# this script end-to-end against a snapshot tarball without a real release.
URL="${WUPHF_INSTALL_URL_OVERRIDE:-}"
if [ -n "$URL" ]; then
  VERSION="${WUPHF_INSTALL_VERSION_OVERRIDE:-snapshot}"
  ARCHIVE="$(basename "$URL")"
else
  # Resolve latest version tag from GitHub redirect
  VERSION="$(curl -sSL -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest" | rev | cut -d'/' -f1 | rev)"
  if [ -z "$VERSION" ]; then
    printf "Error: could not determine latest version\n" >&2
    exit 1
  fi

  # goreleaser strips the leading 'v' from the tag in archive names
  VERSION_CLEAN="${VERSION#v}"
  ARCHIVE="${BINARY}_${VERSION_CLEAN}_${OS}_${ARCH}.tar.gz"
  URL="https://github.com/${REPO}/releases/download/${VERSION}/${ARCHIVE}"
fi

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

printf "Downloading %s %s (%s/%s)...\n" "$BINARY" "$VERSION" "$OS" "$ARCH"
curl -sSL "$URL" -o "${TMPDIR}/${ARCHIVE}"

printf "Extracting...\n"
tar -xzf "${TMPDIR}/${ARCHIVE}" -C "$TMPDIR"

# Install binary
INSTALL_DIR="/usr/local/bin"
if [ ! -w "$INSTALL_DIR" ]; then
  INSTALL_DIR="${HOME}/.local/bin"
  mkdir -p "$INSTALL_DIR"
  printf "Installing to %s (no write access to /usr/local/bin)\n" "$INSTALL_DIR"
fi

cp "${TMPDIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
chmod +x "${INSTALL_DIR}/${BINARY}"

# macOS: re-apply ad-hoc code signature.
# goreleaser's linker-embedded signature is invalidated by the cp+chmod sequence
# on macOS 15+, causing the kernel to SIGKILL the binary with
# "Code Signature Invalid" / "Taskgated Invalid Signature".
if [ "$OS" = "darwin" ] && command -v codesign >/dev/null 2>&1; then
  codesign --force --sign - "${INSTALL_DIR}/${BINARY}" >/dev/null 2>&1 || true
fi

# Verify
if "${INSTALL_DIR}/${BINARY}" --version >/dev/null 2>&1; then
  printf "Successfully installed %s to %s/%s\n" "$("${INSTALL_DIR}/${BINARY}" --version 2>&1)" "$INSTALL_DIR" "$BINARY"
else
  printf "%s installed to %s/%s\n" "$BINARY" "$INSTALL_DIR" "$BINARY"
fi
