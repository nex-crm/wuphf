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

go_import_pattern='(^|[[:space:]])([[:alpha:]_][[:alnum:]_]*|\.|_)?[[:space:]]*["`]github\.com/wailsapp/wails/v[23](/[^"`[:space:]]*)?["`]'
ts_import_pattern='(from[[:space:]]*|import[[:space:]]*(\([[:space:]]*)?|require[[:space:]]*\([[:space:]]*|export[^"`'\'';]*from[[:space:]]*)["`'\''](@wails/runtime|@wailsapp/runtime|wails-bindings)(/[^"`'\''[:space:]]*)?["`'\'']|(from[[:space:]]*|import[[:space:]]*(\([[:space:]]*)?|require[[:space:]]*\([[:space:]]*|export[^"`'\'';]*from[[:space:]]*)["`'\'']wailsjs/[^"`'\''[:space:]]+["`'\'']'

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
    . || true
)"

if [[ -n "$hits" ]]; then
  echo "::error::Wails imports are only allowed in desktop/oswails/ and web/src/desktop/." >&2
  echo >&2
  echo "$hits" >&2
  echo >&2
  echo "Route app data through internal/team/broker_web_proxy.go over loopback HTTP/SSE/WebSocket." >&2
  exit 1
fi

echo "wails-boundary check OK"
