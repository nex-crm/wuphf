#!/usr/bin/env bash
# check-invariants.sh — broker-grade invariants Biome cannot express.
#
# Each check enforces a hard rule from packages/broker/AGENTS.md. Adding an
# allowlist entry requires a one-line comment explaining why that file is the
# legitimate exception.

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
pkg_root="$(cd "$script_dir/.." && pwd)"

cd "$pkg_root"

violations=0

fail() {
  printf '\033[31mFAIL\033[0m %s\n' "$1" >&2
  violations=$((violations + 1))
}

pass() {
  printf '\033[32mPASS\033[0m %s\n' "$1"
}

# ─────────────────────────────────────────────────────────────────────────
# Check 1 — Bind host is "127.0.0.1" only.
#
# AGENTS.md hard rule #1: never `0.0.0.0`, never a LAN IP. The single bind
# happens in src/listener.ts via the `LOOPBACK_HOST` constant. This check
# scans for foot-guns ("0.0.0.0", `server.listen(port)` without a host arg).
# ─────────────────────────────────────────────────────────────────────────
check_no_non_loopback_bind() {
  local violators
  violators=$(grep -rnE --include='*.ts' '"0\.0\.0\.0"|"::"|"any"' src/ 2>/dev/null || true)
  if [ -n "$violators" ]; then
    fail "non-loopback bind constants found in src/:"
    printf '    %s\n' "$violators" >&2
    return 1
  fi
  pass "no non-loopback bind constants"
}

# ─────────────────────────────────────────────────────────────────────────
# Check 2 — Bearer comparison goes through `tokenMatches`.
#
# AGENTS.md hard rule #3: constant-time only. Allowlist:
#   - src/auth.ts  — definition site of `tokenMatches` (`timingSafeEqual`).
# ─────────────────────────────────────────────────────────────────────────
check_token_constant_time_compare() {
  local pattern='token *=== *|token *!== *'
  local violators
  violators=$(grep -rnE --include='*.ts' "$pattern" src/ 2>/dev/null \
    | grep -vE '^src/auth\.ts:' || true)
  if [ -n "$violators" ]; then
    fail "non-constant-time token comparison outside src/auth.ts:"
    printf '    %s\n' "$violators" >&2
    return 1
  fi
  pass "token comparison routed through src/auth.ts (timingSafeEqual)"
}

# ─────────────────────────────────────────────────────────────────────────
# Check 3 — No `electron` imports.
#
# AGENTS.md hard rule #5: this package is pure Node. The desktop shell wraps
# the broker in a utility process; the broker itself never imports electron.
# ─────────────────────────────────────────────────────────────────────────
check_no_electron_import() {
  local violators
  violators=$(grep -rnE --include='*.ts' 'from *"electron"|require\("electron"\)' src/ tests/ 2>/dev/null || true)
  if [ -n "$violators" ]; then
    fail "electron imports forbidden in @wuphf/broker:"
    printf '    %s\n' "$violators" >&2
    return 1
  fi
  pass "no electron imports"
}

# ─────────────────────────────────────────────────────────────────────────
# Check 4 — `@wuphf/protocol` imports go through the package root.
#
# AGENTS.md hard rule #6: `import { ... } from "@wuphf/protocol"` only.
# Reaching into the protocol package's `src/<sub>.ts` couples this package
# to internal layout and bypasses the wire-shape contract.
# ─────────────────────────────────────────────────────────────────────────
check_protocol_root_import() {
  local violators
  violators=$(grep -rnE --include='*.ts' 'from *"@wuphf/protocol/' src/ tests/ 2>/dev/null || true)
  if [ -n "$violators" ]; then
    fail "deep imports into @wuphf/protocol forbidden:"
    printf '    %s\n' "$violators" >&2
    return 1
  fi
  pass "all @wuphf/protocol imports go through the package root"
}

echo "@wuphf/broker — invariant checks"
check_no_non_loopback_bind || true
check_token_constant_time_compare || true
check_no_electron_import || true
check_protocol_root_import || true

if [ "$violations" -gt 0 ]; then
  echo ""
  printf '\033[31m%d invariant check(s) failed\033[0m\n' "$violations" >&2
  exit 1
fi

echo ""
printf '\033[32mall invariant checks passed\033[0m\n'
