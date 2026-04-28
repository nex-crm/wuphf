#!/usr/bin/env bash
# Cross-compile wuphf.exe for Windows on this Mac.
# Output: dist/windows/{amd64,arm64}/wuphf.exe
#
# ldflags match .goreleaser.yml so the binaries are equivalent to a
# release build, except VERSION = "dev-<git-sha>" so first-run version
# checks don't false-positive against npm.

set -euo pipefail

cd "$(dirname "$0")/../.."

# Always rebuild the web UI bundle so the embedded //go:embed all:web/dist
# matches the source tree on every cross-compile. A previous version of this
# script gated on web/dist/index.html existing — that masked stale assets
# from older builds and silently shipped them to the smoke VM. Skip with
# WUPHF_SKIP_WEB=1 only when you've just run `bun run build` and want to
# avoid the second invocation cost during a tight Go-only iteration loop.
if [[ "${WUPHF_SKIP_WEB:-0}" != "1" ]]; then
  echo "building web UI bundle..."
  ( cd web && bun install && bun run build )
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
