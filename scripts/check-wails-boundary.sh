#!/usr/bin/env bash
# Guard the Wails import boundary before any Wails product code lands.
#
# App data must keep using the loopback HTTP/SSE/WebSocket broker transport.
# Wails imports are reserved for OS verbs in desktop/oswails/ and
# web/src/desktop/. A deliberately broken Go probe that aliases the v2 runtime
# import outside desktop/oswails/ is caught twice: depguard validates compiled
# Go imports, and this repo-wide grep catches text references in generated or
# non-src files that Biome does not lint.

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/.." && pwd)"

if ! command -v rg >/dev/null 2>&1; then
  echo "ripgrep (rg) is required for the Wails boundary check" >&2
  exit 1
fi

# The Ubuntu 22.04 apt ripgrep is built without PCRE2 support.
# Verify before proceeding so the check fails loudly rather than silently
# passing with no matches (an empty scan is indistinguishable from a clean repo).
if ! rg --pcre2-version >/dev/null 2>&1; then
  echo "ripgrep is installed but was built without PCRE2 support" >&2
  echo "Install a PCRE2-enabled ripgrep (e.g. via cargo or ubuntu-24.04 apt)." >&2
  exit 1
fi

# shellcheck disable=SC2016 # Backticks are literal regex tokens, not command substitution.
go_import_pattern='(^|[[:space:]])([[:alpha:]_][[:alnum:]_]*|\.|_)?[[:space:]]*["`]github\.com/wailsapp/wails/v[23](/[^"`[:space:]]*)?["`]'
# shellcheck disable=SC2016 # Backticks are literal regex tokens, not command substitution.
ts_import_pattern='(from[[:space:]]*|import[[:space:]]*(\([[:space:]]*)?|require[[:space:]]*\([[:space:]]*|export[^"`'\'';]*from[[:space:]]*)["`'\''](@wails/runtime|@wailsapp/runtime|wails-bindings)(/[^"`'\''[:space:]]*)?["`'\'']|(from[[:space:]]*|import[[:space:]]*(\([[:space:]]*)?|require[[:space:]]*\([[:space:]]*|export[^"`'\'';]*from[[:space:]]*)["`'\'']wailsjs/[^"`'\''[:space:]]+["`'\'']'

set +e
hits="$(
  cd "$repo_root"
  rg \
    --hidden \
    --no-heading \
    --line-number \
    --color=never \
    --pcre2 \
    --glob '!.git/**' \
    --glob '!**/node_modules/**' \
    --glob '!web/dist/**' \
    --glob '!desktop/oswails/**' \
    --glob '!web/src/desktop/**' \
    --glob '!wailsjs/**' \
    --glob '!desktop/wailsjs/**' \
    --glob '!web/wailsjs/**' \
    -e "$go_import_pattern" \
    -e "$ts_import_pattern" \
    .
)"
rg_status=$?
set -e

if [[ $rg_status -gt 1 ]]; then
  echo "::error::wails-boundary check failed to execute ripgrep." >&2
  exit 1
fi

if [[ -n "$hits" ]]; then
  echo "::error::Wails imports are only allowed in desktop/oswails/ and web/src/desktop/." >&2
  echo >&2
  echo "$hits" >&2
  echo >&2
  echo "Route app data through internal/team/broker_web_proxy.go over loopback HTTP/SSE/WebSocket." >&2
  exit 1
fi

echo "wails-boundary check OK"
