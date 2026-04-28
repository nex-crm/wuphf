#!/usr/bin/env bash
# Cross-compile wuphf.exe for Windows on this Mac.
# Output: dist/windows/{amd64,arm64}/wuphf.exe
#
# ldflags match .goreleaser.yml so the binaries are equivalent to a
# release build, except VERSION = "dev-<git-sha>" so first-run version
# checks don't false-positive against npm.

set -euo pipefail

cd "$(dirname "$0")/../.."

# Build the web UI bundle first if it isn't already populated. Without this,
# the embedded //go:embed all:web/dist picks up only .gitkeep, which makes
# the binary's web UI 404 on every route. Skip when WUPHF_SKIP_WEB=1 (set
# this when iterating only on Go code to keep cycles fast).
if [[ "${WUPHF_SKIP_WEB:-0}" != "1" ]]; then
  if [[ ! -f web/dist/index.html ]]; then
    echo "building web UI bundle..."
    ( cd web && bun install && bun run build )
  fi
fi

VERSION="${WUPHF_VERSION:-dev-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"
TIMESTAMP="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

LDFLAGS=(
  -s -w
  "-X github.com/nex-crm/wuphf/internal/buildinfo.Version=${VERSION}"
  "-X github.com/nex-crm/wuphf/internal/buildinfo.BuildTimestamp=${TIMESTAMP}"
)

mkdir -p dist/windows/amd64 dist/windows/arm64

for arch in amd64 arm64; do
  out="dist/windows/${arch}/wuphf.exe"
  echo "building ${out}..."
  CGO_ENABLED=0 GOOS=windows GOARCH="${arch}" \
    go build -trimpath -ldflags "${LDFLAGS[*]}" -o "${out}" ./cmd/wuphf
  ls -lh "${out}"
done

echo
echo "done. version=${VERSION}"
