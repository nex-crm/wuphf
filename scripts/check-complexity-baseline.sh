#!/usr/bin/env bash
# scripts/check-complexity-baseline.sh — forward-only baseline for Biome
# complexity suppressions.
#
# The baseline lets migration PRs land existing complexity debt without making
# future `biome-ignore lint/complexity/...` additions invisible. Removing an
# ignore is encouraged and reported as stale baseline drift; adding one requires
# an explicit scripts/complexity-baseline.txt diff.

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/.." && pwd)"
baseline="$repo_root/scripts/complexity-baseline.txt"

current_f="$(mktemp)"
baseline_norm="$(mktemp)"
trap 'rm -f "$current_f" "$baseline_norm"' EXIT

collect_current() {
  { git -C "$repo_root" grep -nE 'biome-ignore(-all)?[[:space:]]+lint/complexity/' -- 'web/src' || true; } |
    while IFS=: read -r rel_path _line_no text; do
      [[ -n "${rel_path:-}" ]] || continue
      rule=$(
        printf '%s\n' "$text" |
          sed -E 's/.*biome-ignore(-all)?[[:space:]]+(lint\/complexity\/[^:[:space:]}]+).*/\2/'
      )
      [[ "$rule" == lint/complexity/* ]] || continue
      reason=$(
        printf '%s\n' "$text" |
          sed -E 's/.*biome-ignore(-all)?[[:space:]]+lint\/complexity\/[^:]+:[[:space:]]*//; s/[[:space:]]*\*\/\}?$//; s/[[:space:]]*\}$//; s/[[:space:]]+$//'
      )
      printf '%s|%s|%s\n' "$rel_path" "$rule" "$reason"
    done |
    awk -F '|' '{ key = $0; seen[key]++; printf "%s|%s|%d|%s\n", $1, $2, seen[key], $3 }'
}

if [[ "${1:-}" == "--print-current" ]]; then
  collect_current
  exit 0
fi

collect_current > "$current_f"

if [[ -f "$baseline" ]]; then
  sed -E '/^[[:space:]]*(#|$)/d; s/[[:space:]]+$//' "$baseline" > "$baseline_norm"
else
  : > "$baseline_norm"
fi

new_entries=$(sort "$current_f" | comm -23 - <(sort "$baseline_norm") || true)
stale_entries=$(sort "$baseline_norm" | comm -23 - <(sort "$current_f") || true)

if [[ -n "$stale_entries" ]]; then
  echo "::group::stale complexity baseline entries"
  printf '%s\n' "$stale_entries" | sed 's/^/  /'
  echo "  -> remove these from scripts/complexity-baseline.txt"
  echo "::endgroup::"
fi

if [[ -n "$new_entries" ]]; then
  echo "::error::new Biome complexity ignores outside scripts/complexity-baseline.txt:"
  printf '%s\n' "$new_entries" | sed 's/^/  /'
  echo
  echo "Either refactor the new complexity, or add a justified baseline entry."
  exit 1
fi

count=$(wc -l < "$current_f" | tr -d ' ')
echo "complexity baseline check OK ($count tracked complexity ignores)"
