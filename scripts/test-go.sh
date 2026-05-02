#!/usr/bin/env bash
# test-go.sh — local package-isolated Go test runner.
#
# Why this script exists: a few integration-heavy packages have worker
# lifecycles and filesystem state that make one big `go test ./...` harder
# to debug than package-isolated invocations. CI runs the race detector for
# everything except the known `internal/teammcp` carve-out; keep this helper
# aligned so local pre-push gives roughly the same signal before GitHub runs.
#
# Without a sanctioned local entry point, developers hit the same flakes
# CI carved away and lose hours bisecting "their" change.
#
# What this does — same shape as CI:
#   1. Lists every package under ./... that has test files.
#   2. Runs each package's tests in its own `go test` invocation
#      (separate processes = no cross-package state leakage).
#   3. Adds -race to every package EXCEPT internal/teammcp.
#
# Usage:
#   bash scripts/test-go.sh                # all packages
#   bash scripts/test-go.sh ./internal/team  # one package, still race-enabled
#   COUNT=3 bash scripts/test-go.sh        # -count=3 for flake hunting
#   PARALLEL=1 bash scripts/test-go.sh     # -p 1 inside go test
#
# Exit code: number of failed packages (0 = green).

set -euo pipefail

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

# Mirror ci.yml :: go-test: skip -race only on internal/teammcp and its
# subpackages.
race_carveout='/internal/teammcp($|/)'

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
