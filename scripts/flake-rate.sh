#!/usr/bin/env bash
# scripts/flake-rate.sh
#
# Advisory flake-rate harness for suspected flaky Go tests. Reads
# tests/flaky/quarantine.txt, re-runs each entry with test caching disabled,
# and reports pass/fail counts as a markdown table.
#
# This is intentionally not wired into CI yet; it is for investigation while
# cross-platform desktop baselines are collected.

set -euo pipefail

RUNS=5
THRESHOLD="${FLAKE_RATE_THRESHOLD:-0.20}"

script_dir="$(dirname -- "${BASH_SOURCE[0]}")"
repo_root="$(cd -- "$script_dir/.." && pwd)"
quarantine_file="${FLAKE_QUARANTINE_FILE:-$repo_root/tests/flaky/quarantine.txt}"
dry_run=false

usage() {
  echo "usage: scripts/flake-rate.sh [--dry-run]"
}

while (($# > 0)); do
  case "$1" in
    --dry-run)
      dry_run=true
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      echo "::error::unknown argument: $1"
      usage
      exit 2
      ;;
  esac
  shift
done

if [[ ! -f "$quarantine_file" ]]; then
  echo "::error::quarantine file not found: $quarantine_file"
  exit 1
fi

if ! awk -v threshold="$THRESHOLD" 'BEGIN {
  if (threshold !~ /^[0-9]+([.][0-9]+)?$/ || threshold < 0 || threshold > 1) {
    exit 1
  }
}'; then
  echo "::error::FLAKE_RATE_THRESHOLD must be a number between 0 and 1; got '$THRESHOLD'"
  exit 2
fi

entries=()
while IFS= read -r line || [[ -n "$line" ]]; do
  if [[ "$line" =~ ^[[:space:]]*$ || "$line" =~ ^[[:space:]]*# ]]; then
    continue
  fi
  entries+=("$line")
done < "$quarantine_file"

if ((${#entries[@]} == 0)); then
  echo "flake-rate: no tests in quarantine ($quarantine_file)"
  exit 0
fi

echo "| Test | Passes | Fails | Rate |"
echo "|---|---:|---:|---:|"

threshold_exceeded=false
for entry in "${entries[@]}"; do
  display_entry="${entry//|/\\|}"

  if [[ "$dry_run" == true ]]; then
    echo "| \`$display_entry\` | - | - | - |"
    continue
  fi

  read -r -a test_args <<< "$entry"

  passes=0
  fails=0
  for ((run = 1; run <= RUNS; run++)); do
    if (cd "$repo_root" && go test -timeout 120s -count=1 "${test_args[@]}") 1>/dev/null; then
      passes=$((passes + 1))
    else
      fails=$((fails + 1))
    fi
  done

  rate="$(awk -v fails="$fails" -v runs="$RUNS" 'BEGIN { printf "%.2f", fails / runs }')"
  echo "| \`$display_entry\` | $passes | $fails | $rate |"

  if awk -v rate="$rate" -v threshold="$THRESHOLD" 'BEGIN { exit !(rate >= threshold) }'; then
    threshold_exceeded=true
  fi
done

if [[ "$threshold_exceeded" == true ]]; then
  echo "::error::one or more quarantined tests met or exceeded flake rate threshold $THRESHOLD"
  exit 1
fi

if [[ "$dry_run" == false ]]; then
  echo "flake-rate check OK: all quarantined tests below $THRESHOLD failure rate over $RUNS runs."
fi
