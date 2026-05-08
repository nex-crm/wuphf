#!/usr/bin/env bash
# test-protocol.sh - canonical local @wuphf/protocol unit/property test runner.
#
# Why this script exists: `packages/protocol/package.json` uses Vitest, but
# `bun test` invokes Bun's native test runner and does not behave the same way
# for fast-check-driven property tests. This wrapper gives agents and humans one
# root-level command for both full and focused protocol test runs.
#
# Usage:
#   bash scripts/test-protocol.sh
#   bash scripts/test-protocol.sh tests/frozen-args.spec.ts
#   bash scripts/test-protocol.sh packages/protocol/tests/frozen-args.spec.ts
#
# Exit code: Vitest's exit code.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
pkg_root="$repo_root/packages/protocol"

cd "$pkg_root" || exit 2

if [ "$#" -eq 0 ]; then
  echo "=== vitest @wuphf/protocol full suite ==="
  exec bun run test
fi

args=()
for arg in "$@"; do
  case "$arg" in
    packages/protocol/*)
      args+=("${arg#packages/protocol/}")
      ;;
    *)
      args+=("$arg")
      ;;
  esac
done

exec bunx vitest run "${args[@]}"
