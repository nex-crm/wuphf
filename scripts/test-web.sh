#!/usr/bin/env bash
# test-web.sh - canonical local Web unit/component test runner.
#
# Why this script exists: `web/package.json` uses Vitest, but `bun test`
# invokes Bun's native test runner and does not behave the same way for this
# suite. This wrapper gives agents and humans one root-level command for both
# full and focused Web test runs.
#
# Usage:
#   bash scripts/test-web.sh
#   bash scripts/test-web.sh src/api/platform.test.ts
#   bash scripts/test-web.sh web/src/api/platform.test.ts
#
# Exit code: Vitest's exit code.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
web_root="$repo_root/web"

cd "$web_root" || exit 2

if [ "$#" -eq 0 ]; then
  echo "=== vitest web full suite ==="
  exec bun run test
fi

args=()
for arg in "$@"; do
  case "$arg" in
    "$repo_root"/web/*)
      args+=("${arg#"$repo_root"/web/}")
      ;;
    ./web/*)
      args+=("${arg#./web/}")
      ;;
    web/*)
      args+=("${arg#web/}")
      ;;
    *)
      args+=("$arg")
      ;;
  esac
done

echo "=== vitest ${args[*]} ==="
exec bunx vitest run "${args[@]}"
