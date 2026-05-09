#!/usr/bin/env bash
# changed-files-since-base.sh — list files changed in the local branch
# relative to the upstream base, with safe fallbacks.
#
# lefthook scopes pre-push hooks via `files: <command>` + `glob:`. The natural
# command is `git diff --name-only origin/main...HEAD`, but on first-time
# clones, shallow fetches, or repos whose default is renamed, `origin/main` may
# not be a known ref and `git diff` exits 128. lefthook treats a non-zero
# exit as "no files" and silently SKIPS the hook — exactly the opposite of
# what we want when the base is unavailable.
#
# This script returns:
#   1. The diff against `origin/main` if that ref exists locally.
#   2. Otherwise the diff against `origin/HEAD` (the remote's default branch
#      pointer, which respects forks and renames).
#   3. Otherwise every tracked file — fail safe to "run all hooks" rather
#      than fail open to "skip all hooks".
#
# Usage from lefthook.yml:
#   files: bash scripts/changed-files-since-base.sh

set -euo pipefail

if git rev-parse --verify --quiet origin/main >/dev/null; then
  if git diff --name-only origin/main...HEAD; then
    exit 0
  fi
fi

if git rev-parse --verify --quiet origin/HEAD >/dev/null; then
  if git diff --name-only origin/HEAD...HEAD; then
    exit 0
  fi
fi

# Fail-safe fallback: list every tracked file so glob-scoped hooks still
# run. This is correct for a fresh clone — slower than the diff, but the
# alternative (silently skipping hooks) is worse.
exec git ls-files
