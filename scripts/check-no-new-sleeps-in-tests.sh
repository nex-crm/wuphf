#!/usr/bin/env bash
# scripts/check-no-new-sleeps-in-tests.sh
#
# CONTRIBUTING.md rule: no time.Sleep in tests. This gate enforces it
# at the PR diff level — existing offenders (see internal/agent/
# service_test.go, internal/openclaw/client_test.go, etc.) are
# grandfathered, but any new addition is rejected.
#
# Why diff-only: a hard ban would require fixing ~80 existing call
# sites in one PR. Forward-only via diff lets the rule lock now while
# the existing offenders get fixed in their own paced PRs.
#
# Usage: BASE_SHA=<sha> bash scripts/check-no-new-sleeps-in-tests.sh
# In GitHub Actions, BASE_SHA is github.event.pull_request.base.sha.

set -euo pipefail

BASE_SHA="${BASE_SHA:-$(git merge-base HEAD origin/main 2>/dev/null || echo "")}"

if [[ -z "$BASE_SHA" ]]; then
  echo "warn: no base sha; cannot check sleep drift"
  exit 0
fi

# Diff filter:
#   --diff-filter=AM        only added/modified lines
#   -- '*_test.go'          test files only
#   git log -p              produces unified diff
# Then keep only added lines (^+, not ^++ which is the file header)
# that contain time.Sleep, and ignore the file with the centralized
# documentation about why we don't use sleep.
# Match `time.Sleep(` (call invocation) on lines that are NOT comments.
# A line is a comment if its first non-whitespace char (after the `+`)
# is `//`. Block-comment lines don't include the keyword in normal style
# so the // check covers the realistic comment cases.
new_sleeps=$(
  git diff "$BASE_SHA"...HEAD --diff-filter=AM -- '*_test.go' \
    | grep -E '^\+[^+]' \
    | grep -E 'time\.Sleep\(' \
    | grep -vE '^\+[[:space:]]*//' \
    || true
)

if [[ -n "$new_sleeps" ]]; then
  echo "::error::CONTRIBUTING.md violation: new time.Sleep added in test files"
  echo
  echo "$new_sleeps"
  echo
  echo "Tests with time.Sleep flake. Use a manual clock fixture instead:"
  echo "  - internal/team/scheduler.go    — clock interface + manualClock"
  echo "  - internal/team/tmux_runner.go  — fakeTmuxRunner pattern"
  echo
  echo "If a test legitimately must wait on real time (live network test"
  echo "for example), gate it behind a // slow build tag and document why."
  exit 1
fi

echo "no-sleep-in-new-tests check OK"
