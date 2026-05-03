#!/usr/bin/env bash
# scripts/check-allowlist-forward-only.sh
#
# Enforces CONTRIBUTING.md's "forward-only allowlist" rule at CI time:
# the file-size allowlist must never gain entries. Entries can be
# removed (decomposition done) or commented out (stale), but no PR may
# introduce a NEW line that grants exemption to a not-currently-listed
# file.
#
# Usage: BASE_SHA=<sha> bash scripts/check-allowlist-forward-only.sh
#
# In GitHub Actions, BASE_SHA is github.event.pull_request.base.sha.
# Locally, fall back to origin/main.

set -euo pipefail

ALLOWLIST="scripts/file-size-allowlist.txt"
BASE_SHA="${BASE_SHA:-$(git merge-base HEAD origin/main 2>/dev/null || echo "")}"

if [[ -z "$BASE_SHA" ]]; then
  echo "warn: no base sha; cannot check allowlist drift"
  exit 0
fi

if ! git show "$BASE_SHA:$ALLOWLIST" >/dev/null 2>&1; then
  # Allowlist didn't exist at base — first PR to introduce it.
  exit 0
fi

# Active path entries: lines that aren't blank and don't start with #
# (after leading whitespace strip). Exclude trailing comments via the
# same sed used in check-file-size.sh.
extract_paths() {
  sed -E 's/[[:space:]]*#.*$//; s/^[[:space:]]+//; s/[[:space:]]+$//' "$1" \
    | grep -v '^$' \
    | sort -u
}

before_tmp="$(mktemp)"
after_tmp="$(mktemp)"
trap 'rm -f "$before_tmp" "$after_tmp"' EXIT

git show "$BASE_SHA:$ALLOWLIST" > "$before_tmp"

before_paths="$(extract_paths "$before_tmp")"
after_paths="$(extract_paths "$ALLOWLIST")"

added=$(comm -13 <(echo "$before_paths") <(echo "$after_paths") || true)

if [[ -n "$added" ]]; then
  echo "::error::file-size-allowlist.txt is forward-only (CONTRIBUTING.md)."
  echo "The following entries are NEW in this PR and must be removed:"
  while IFS= read -r line; do
    printf '  %s\n' "$line"
  done <<< "$added"
  echo
  echo "If a file truly cannot be decomposed in this PR, decompose it"
  echo "in a separate PR first and merge that one. The allowlist exists"
  echo "to track legacy debt, not to grant new exemptions."
  exit 1
fi

echo "allowlist forward-only check OK (no new entries)"
