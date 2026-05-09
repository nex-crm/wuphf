#!/usr/bin/env bash
# check-invariants.sh — protocol-grade invariants Biome cannot express.
#
# Each check enforces a hard rule from packages/protocol/AGENTS.md or a
# documented lesson-learned that has bitten us before. Adding a new
# allowlist entry MUST come with a one-line comment explaining why that
# file is the legitimate exception.
#
# Run from anywhere; this script anchors paths to its own location.
#
# Exits non-zero on any violation. Prints which check fired so a
# reviewer / pre-commit-hook reader can act without grepping further.

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
# Check 1 — No `instanceof FrozenArgs|SanitizedString` outside allowlist.
#
# Hard rule #4: validators re-derive, don't trust `instanceof`.
# Allowlist:
#   - src/frozen-args.ts and src/sanitized-string.ts: where the classes
#     are defined; instanceof on this side of the boundary is fine.
#   - src/receipt-validator.ts: validates already-typed in-process values
#     (NOT untrusted JSON — that path goes through the codecs
#     `FrozenArgs.fromCanonical` / `SanitizedString.fromUnknown`). Each use
#     is paired with a re-derive in the function body.
# ─────────────────────────────────────────────────────────────────────────
check_no_instanceof_outside_class() {
  local pattern='instanceof (FrozenArgs|SanitizedString)\b'
  local violators
  violators=$(grep -rnE --include='*.ts' "$pattern" src/ tests/ scripts/ 2>/dev/null \
    | grep -vE '^src/(frozen-args|sanitized-string|receipt-validator)\.ts:' \
    || true)
  if [ -n "$violators" ]; then
    fail "instanceof FrozenArgs|SanitizedString outside allowlist (frozen-args.ts, sanitized-string.ts, receipt-validator.ts):"
    printf '    %s\n' $violators >&2
    return 1
  fi
  pass "no instanceof FrozenArgs|SanitizedString outside allowlist"
}

# ─────────────────────────────────────────────────────────────────────────
# Check 2 — Single hashing entry point (`src/sha256.ts`).
#
# crypto.subtle / createHash MUST live behind sha256.ts so that a
# follow-on swap (FIPS provider, hardware key, etc.) is one-file work.
# Allowlist:
#   - src/sha256.ts: the entry point itself.
#   - src/audit-event.ts: uses createHash directly for the chain hash
#     (`computeEventHash`). Documented at the call site as the only
#     audit-chain exception. If you broaden hashing usage, refactor
#     through sha256.ts first.
# ─────────────────────────────────────────────────────────────────────────
check_single_hashing_entry_point() {
  local pattern='(crypto\.subtle|createHash\b)'
  local violators
  violators=$(grep -rnE --include='*.ts' "$pattern" src/ scripts/ 2>/dev/null \
    | grep -vE '^src/(sha256|audit-event)\.ts:' \
    || true)
  if [ -n "$violators" ]; then
    fail "crypto.subtle / createHash outside allowlist (sha256.ts, audit-event.ts):"
    printf '    %s\n' $violators >&2
    return 1
  fi
  pass "single hashing entry point preserved"
}

# ─────────────────────────────────────────────────────────────────────────
# Check 3 — No process.env reads in src/.
#
# Protocol package is pure-data; environment is a caller concern. If you
# need build-time configuration, surface it as a function parameter or
# as a typed bootstrap helper that the broker passes in.
# ─────────────────────────────────────────────────────────────────────────
check_no_process_env_in_src() {
  local violators
  violators=$(grep -rnE --include='*.ts' 'process\.env' src/ 2>/dev/null || true)
  if [ -n "$violators" ]; then
    fail "process.env reads in src/:"
    printf '    %s\n' $violators >&2
    return 1
  fi
  pass "no process.env in src/"
}

# ─────────────────────────────────────────────────────────────────────────
# Check 4 — Demo MUST import only from `../src/index.ts`.
#
# Lesson 12 (lessons-learned): demo importing from `src/<sub>.ts` is a
# fake gate — it lets the demo claim coverage that index.ts doesn't
# actually expose, defeating the public-API smoke test.
# ─────────────────────────────────────────────────────────────────────────
check_demo_imports_index_only() {
  local violators
  violators=$(grep -nE 'from "\.\./src/' scripts/demo.ts 2>/dev/null \
    | grep -vE '"\.\./src/index\.ts"' \
    || true)
  if [ -n "$violators" ]; then
    fail "scripts/demo.ts imports from non-index source files:"
    printf '    %s\n' $violators >&2
    return 1
  fi
  pass "demo imports only from ../src/index.ts"
}

# ─────────────────────────────────────────────────────────────────────────
# Check 5 — Every value (function/class/const) export from index.ts must
# be referenced in tests/ OR scripts/demo.ts.
#
# Type-only exports are exempt — they are consumed in interface positions
# that grep-by-symbol won't find. The demo is treated as a legitimate
# public-API smoke for symbols that are exercised end-to-end rather than
# unit-tested in isolation.
# ─────────────────────────────────────────────────────────────────────────
check_index_value_exports_have_coverage() {
  local missing=()
  while IFS= read -r symbol; do
    [ -z "$symbol" ] && continue
    if ! grep -rqE "\b${symbol}\b" tests/ scripts/demo.ts 2>/dev/null; then
      missing+=("$symbol")
    fi
  done < <(awk '
    /^export type \{/ { skip=1 }
    /^\}/ { if (skip) skip=0; next }
    skip { next }
    /^export \{/ { capture=1 }
    capture {
      gsub(/[{},]/, " ")
      for (i=1; i<=NF; i++) {
        if ($i == "from" || $i == "export") { capture=0; break }
        if ($i ~ /^"\.\//) { capture=0; break }
        if ($i ~ /^[a-z][a-zA-Z0-9_]+$/ || $i ~ /^[A-Z][A-Z0-9_]+$/) print $i
      }
      if (/from/) capture=0
    }
  ' src/index.ts | sort -u)
  if [ "${#missing[@]}" -gt 0 ]; then
    fail "value exports from src/index.ts with no reference in tests/ or scripts/demo.ts (${#missing[@]}):"
    printf '    %s\n' "${missing[@]}" >&2
    return 1
  fi
  pass "every value export from src/index.ts has a test or demo reference"
}

# ─────────────────────────────────────────────────────────────────────────
# Check 6 — No `Date.now()` / `Date.parse()` / `performance.now()` in
# src/ or tests/.
#
# Hard rule #14: Date APIs MUST NOT provide uniqueness, ordering,
# deduplication, or monotonic-counter behavior. ms precision collides
# under rapid events. Use EventLsn for ordering, ULID for IDs, explicit
# counters for monotonic state. Date may be used to MARK time
# (record/serialize when something happened, enforce a per-record
# validity window), but never to ORDER across records.
#
# `Date.now()` and `Date.parse()` have no legitimate marking-time use
# in this package — they always return a numeric ms count which is
# exactly the wrong primitive for ordering. Forbid them outright.
#
# `new Date(...)` and `.toISOString()` are allowed because they have
# legitimate per-record uses (validity windows, wire-format emit/parse).
# Their misuse is harder to detect mechanically; AGENTS.md rule #14
# carries the policy.
# ─────────────────────────────────────────────────────────────────────────
check_no_date_now_or_date_parse() {
  local violators
  violators=$(grep -rnE --include='*.ts' '(Date\.now\(|Date\.parse\(|performance\.now\()' src/ tests/ scripts/ 2>/dev/null || true)
  if [ -n "$violators" ]; then
    fail "Date.now() / Date.parse() / performance.now() (use EventLsn for ordering, ULID for IDs, explicit counters for monotonic state — see AGENTS.md rule #14):"
    printf '    %s\n' $violators >&2
    return 1
  fi
  pass "no Date.now() / Date.parse() / performance.now() (date APIs only mark time, never order)"
}

# ─────────────────────────────────────────────────────────────────────────
# Run all checks; collect violations rather than fast-fail so a
# contributor sees every issue per run.
# ─────────────────────────────────────────────────────────────────────────
echo "@wuphf/protocol — invariant checks"
check_no_instanceof_outside_class || true
check_single_hashing_entry_point || true
check_no_process_env_in_src || true
check_demo_imports_index_only || true
check_index_value_exports_have_coverage || true
check_no_date_now_or_date_parse || true

if [ "$violations" -gt 0 ]; then
  echo ""
  printf '\033[31m%d invariant check(s) failed\033[0m\n' "$violations" >&2
  exit 1
fi

echo ""
printf '\033[32mall invariant checks passed\033[0m\n'
