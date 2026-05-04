#!/usr/bin/env bash
# scripts/check-bundle-size.sh
#
# Bundle-size budget for the web build. Runs after `bun run build` and
# asserts the JS bundle hasn't grown past the documented ceiling.
#
# Two thresholds, both forward-only ratchets:
#   - WARN_KB: warning at this size; next PR should justify the growth
#   - FAIL_KB: hard fail; new PRs cannot push the bundle past here
#
# To raise the ceiling intentionally (e.g. a new feature pulls in a
# library), update the constants below in the SAME PR that introduces
# the regression and document why in the commit message. Without an
# explicit raise, the gate catches accidental bloat (a stray
# import-the-world or a moment.js where date-fns would do).
#
# CONTRIBUTING.md anchors this in the "no perf degradations" rule.

set -euo pipefail

# Calibrated against the current minified bundle (~854 KB).
# Warn band gives ~10% headroom; fail line gives ~40% — past that,
# something has gone seriously wrong (or a major feature has shipped
# that warrants raising the ceiling explicitly).
WARN_KB=950
# Live agent output renders a full terminal emulator in the web UI. That is
# intentional product weight, not an accidental import, so the fail line gives
# this feature room while keeping the ratchet tight around the new bundle.
FAIL_KB=1300

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
dist_assets="$repo_root/web/dist/assets"

if [[ ! -d "$dist_assets" ]]; then
  echo "::error::web/dist/assets/ not found — did 'bun run build' run first?"
  exit 1
fi

# Sum sizes of all .js bundles. We deliberately skip .css and source
# maps — JS is what blocks the main thread on first paint and is the
# axis the chunkSizeWarningLimit rolldown warning already targets.
total_bytes=0
shopt -s nullglob
for f in "$dist_assets"/*.js; do
  # `wc -c` is portable across BSD/macOS and GNU/Linux; `stat -f`/`-c`
  # divergence between the two trips `set -u` inside `(( ))`.
  size=$(wc -c < "$f" | tr -d '[:space:]')
  total_bytes=$(( total_bytes + size ))
done

if (( total_bytes == 0 )); then
  echo "::error::no .js files found in $dist_assets — the build artifact layout changed?"
  exit 1
fi

total_kb=$(( total_bytes / 1024 ))

if (( total_kb >= FAIL_KB )); then
  echo "::error::JS bundle is ${total_kb} KB — over the ${FAIL_KB} KB fail line."
  echo
  echo "Either reduce bundle size (audit recent imports, prefer named"
  echo "imports over default-everything, replace heavyweight libs with"
  echo "lighter alternatives) or raise FAIL_KB in this script with a"
  echo "documented justification in the same PR."
  exit 1
fi

if (( total_kb >= WARN_KB )); then
  echo "::warning::JS bundle is ${total_kb} KB — over the ${WARN_KB} KB warn line."
  echo "  Headroom to fail: $(( FAIL_KB - total_kb )) KB."
  echo "  Audit recent imports if this jumped unexpectedly."
fi

echo "bundle-size check OK: ${total_kb} KB (warn ${WARN_KB}, fail ${FAIL_KB})"
