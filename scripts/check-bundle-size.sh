#!/usr/bin/env bash
# scripts/check-bundle-size.sh
#
# Bundle-size budget for the web build. Runs after `bun run build` and
# asserts the entry chunk hasn't grown past the documented ceiling.
#
# Two thresholds, both forward-only ratchets, applied to the **entry
# chunk** (Vite's `index-*.js`) — the JS the browser must download
# before it can paint anything. Lazy-loaded route chunks are tracked
# separately and informational only; they don't block first paint.
#
#   - WARN_KB: warning at this size; next PR should justify the growth
#   - FAIL_KB: hard fail; new PRs cannot push the entry past here
#
# To raise the ceiling intentionally (e.g. a new feature pulls in a
# library), update the constants below in the SAME PR that introduces
# the regression and document why in the commit message. Without an
# explicit raise, the gate catches accidental bloat (a stray
# import-the-world or a moment.js where date-fns would do).
#
# CONTRIBUTING.md anchors this in the "no perf degradations" rule.

set -euo pipefail

# Calibrated against the current entry chunk (~1063 KB after step 5
# of docs/experiments/2026-05-04-web-path-forward.md lazy-loaded the
# app panels). Warn band gives ~3% headroom; fail line gives ~13%.
# Past that, either the entry has accumulated avoidable weight again
# or another major feature is shipping and the ceiling needs an
# explicit raise.
#
# History:
#   * Calibrated to ~854 KB before xterm landed (terminal in /console).
#   * Raised to FAIL=1400 / WARN=1350 when the TanStack Router
#     migration shipped the whole route surface in one chunk.
#   * Lowered here once route panels became lazy chunks: only the
#     conversation surface, sidebar, and shell ride the entry.
WARN_KB=1100
FAIL_KB=1200

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
dist_assets="$repo_root/web/dist/assets"

if [[ ! -d "$dist_assets" ]]; then
  echo "::error::web/dist/assets/ not found — did 'bun run build' run first?"
  exit 1
fi

# Identify the entry chunk. Vite emits it as `index-<hash>.js`; route
# chunks get their component name. If two index files exist (shouldn't
# happen, but guard for the future) take the largest.
shopt -s nullglob
entry_path=""
entry_bytes=0
for f in "$dist_assets"/index-*.js; do
  size=$(wc -c < "$f" | tr -d '[:space:]')
  if (( size > entry_bytes )); then
    entry_path="$f"
    entry_bytes=$size
  fi
done

if (( entry_bytes == 0 )); then
  echo "::error::no entry chunk (index-*.js) found in $dist_assets — has the build artifact layout changed?"
  exit 1
fi

# Track total JS bytes separately, for visibility. Includes every
# lazy-loaded route chunk; useful when triaging worst-case download
# size, but doesn't gate the build.
total_bytes=0
chunk_count=0
for f in "$dist_assets"/*.js; do
  size=$(wc -c < "$f" | tr -d '[:space:]')
  total_bytes=$(( total_bytes + size ))
  chunk_count=$(( chunk_count + 1 ))
done

entry_kb=$(( entry_bytes / 1024 ))
total_kb=$(( total_bytes / 1024 ))

if (( entry_kb >= FAIL_KB )); then
  echo "::error::Entry chunk is ${entry_kb} KB — over the ${FAIL_KB} KB fail line."
  echo
  echo "Either reduce the entry (lazy-load new panels via React.lazy,"
  echo "audit recent imports for accidental top-level pulls, swap a"
  echo "heavyweight lib for a lighter one) or raise FAIL_KB with a"
  echo "documented justification in the same PR."
  echo
  echo "Total JS across all chunks: ${total_kb} KB in ${chunk_count} files."
  exit 1
fi

if (( entry_kb >= WARN_KB )); then
  echo "::warning::Entry chunk is ${entry_kb} KB — over the ${WARN_KB} KB warn line."
  echo "  Headroom to fail: $(( FAIL_KB - entry_kb )) KB."
  echo "  Audit recent imports if this jumped unexpectedly."
fi

echo "bundle-size check OK: entry ${entry_kb} KB (warn ${WARN_KB}, fail ${FAIL_KB}); total ${total_kb} KB across ${chunk_count} chunks."
