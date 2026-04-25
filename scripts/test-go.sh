#!/usr/bin/env bash
# test-go.sh — local mirror of CI's go-test-matrix job.
#
# Why this script exists: the team test suite (internal/team and
# internal/teammcp) has known goroutine-leak patterns where a worker spawned
# by one test outlives that test and races against the next test's setup.
# The race detector is correct to flag those, but they make
# `go test -race ./...` non-deterministically fail on Mac under any system
# load. CI works around this by fanning out per-package and disabling
# -race for the two known-bad packages (see ci.yml :: go-test-list).
#
# Without a sanctioned local entry point, developers hit the same flakes
# CI carved away and lose hours bisecting "their" change.
#
# What this does — same shape as CI:
#   1. Lists every package under ./... that has test files.
#   2. Runs each package's tests in its own `go test` invocation
#      (separate processes = no cross-package state leakage).
#   3. Adds -race to every package EXCEPT internal/team(mcp)?.
#
# Usage:
#   bash scripts/test-go.sh                # all packages
#   bash scripts/test-go.sh internal/team  # one package (still no -race
#                                          # if it matches the carve-out)
#   COUNT=3 bash scripts/test-go.sh        # -count=3 for flake hunting
#   PARALLEL=1 bash scripts/test-go.sh     # -p 1 inside go test
#
# Exit code: number of failed packages (0 = green).

set -uo pipefail

count="${COUNT:-1}"
parallel="${PARALLEL:-}"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root" || exit 2

# macOS ships bash 3.2, so no `mapfile` / no `readarray`. Use a tempfile +
# while-read loop, which works on any POSIX bash.
pkg_list="$(mktemp -t wuphf-test-go.XXXXXX)"
trap 'rm -f "$pkg_list"' EXIT

if [ "$#" -gt 0 ]; then
  # Caller passed explicit package patterns. Default to repo-relative
  # paths (./internal/team) — a typo will surface as "no Go files" from
  # go itself, not as a silent skip here.
  go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' "$@" > "$pkg_list"
else
  go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./... > "$pkg_list"
fi

if [ ! -s "$pkg_list" ]; then
  echo "test-go: no packages with tests under ${*:-./...}" >&2
  exit 0
fi
total=$(wc -l < "$pkg_list" | tr -d ' ')

# Mirror ci.yml :: go-test-list: skip -race on internal/team(mcp)? and
# their subpackages until the test-isolation work in those packages
# (goroutine leaks in headless_codex.go's enqueueHeadlessCodexTurnRecord +
# pam dispatcher) lands. Keep this regex byte-identical to the jq filter
# in .github/workflows/ci.yml.
race_carveout='/internal/team(mcp)?($|/)'

failures=0
while IFS= read -r pkg; do
  [ -z "$pkg" ] && continue
  args="-timeout 5m -count=$count"
  if [ -n "$parallel" ]; then
    args="$args -p $parallel -parallel $parallel"
  fi
  if [[ ! "$pkg" =~ $race_carveout ]]; then
    args="$args -race"
  fi

  printf '\n=== go test %s ===\n' "$pkg"
  # shellcheck disable=SC2086  # word-splitting on $args is intentional
  if ! go test $args "$pkg"; then
    failures=$((failures + 1))
  fi
done < "$pkg_list"

echo
if [ "$failures" -eq 0 ]; then
  echo "test-go: all ${total} packages green"
else
  echo "test-go: ${failures} of ${total} packages failed" >&2
fi
exit "$failures"
