#!/usr/bin/env bash
# scripts/check-file-size.sh — file-size budget enforcement.
#
# - Warn at 800 LOC.
# - Fail at 1500 LOC, unless the file is on scripts/file-size-allowlist.txt.
# - The allowlist is FORWARD-ONLY in PRs: entries can shrink or disappear,
#   never appear or grow. Adding a new entry is a CONTRIBUTING.md violation.
#
# See docs/CODE-QUALITY.md for the rationale and decomposition patterns.
#
# Skips:
#   - generated files (Go "// Code generated ..."; *.gen.ts, *.generated.ts, etc.)
#   - test files (*_test.go, *.test.ts, *.spec.ts, etc.)
#   - vendor/, node_modules/, dist/, testdata/
#
# Portable to bash 3.2 (macOS stock) — no associative arrays.
#
# Exit codes:
#   0  no failures (warnings allowed)
#   1  one or more files exceed 1500 LOC and are not allowlisted, or the
#      allowlist grows relative to the PR base

set -euo pipefail

WARN=800
FAIL=1500

# Derive repo root from the script's own location, not from cwd. The
# script may be invoked from arbitrary cwd (CI, lefthook, manual run);
# the file we gate is always relative to the script's worktree.
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/.." && pwd)"
allowlist="$repo_root/scripts/file-size-allowlist.txt"

allow_norm="$(mktemp)"
base_allow_norm="$(mktemp)"
allow_seen="$(mktemp)"
failures_f="$(mktemp)"
warnings_f="$(mktemp)"
allowlist_added_f="$(mktemp)"
allowlist_growth_f="$(mktemp)"
trap 'rm -f "$allow_norm" "$base_allow_norm" "$allow_seen" "$failures_f" "$warnings_f" "$allowlist_added_f" "$allowlist_growth_f"' EXIT

normalize_allowlist() {
  # Strip comments (# to end of line), trim whitespace, drop blank lines.
  sed -E 's/[[:space:]]*#.*$//; s/^[[:space:]]+//; s/[[:space:]]+$//' \
    | grep -v '^$' || true
}

if [[ -f "$allowlist" ]]; then
  normalize_allowlist < "$allowlist" > "$allow_norm"
fi

base_ref="${FILE_SIZE_BASE_REF:-}"
if [[ -z "$base_ref" && -n "${GITHUB_BASE_REF:-}" ]]; then
  base_ref="origin/${GITHUB_BASE_REF}"
fi
if [[ -z "$base_ref" ]]; then
  base_ref="$(git -C "$repo_root" rev-parse --abbrev-ref --symbolic-full-name '@{upstream}' 2>/dev/null || true)"
fi

base_commit=""
if [[ -n "$base_ref" ]]; then
  base_commit="$(git -C "$repo_root" merge-base HEAD "$base_ref" 2>/dev/null || true)"
fi
if [[ -n "$base_commit" ]] && git -C "$repo_root" cat-file -e "$base_commit:scripts/file-size-allowlist.txt" 2>/dev/null; then
  git -C "$repo_root" show "$base_commit:scripts/file-size-allowlist.txt" | normalize_allowlist > "$base_allow_norm"
fi

if [[ -s "$allow_norm" && -s "$base_allow_norm" ]]; then
  added_allowlist_entries=$(sort -u "$allow_norm" | comm -13 <(sort -u "$base_allow_norm") - || true)
  if [[ -n "$added_allowlist_entries" ]]; then
    printf '%s\n' "$added_allowlist_entries" > "$allowlist_added_f"
  fi
fi

is_generated() {
  local f="$1"
  case "$f" in
    *.gen.ts|*.gen.tsx|*.generated.ts|*.generated.tsx) return 0 ;;
  esac
  case "$f" in
    *.go)
      # The Go convention: a "// Code generated ... DO NOT EDIT." line
      # within the first few lines marks the file as generated.
      head -3 "$f" 2>/dev/null | grep -q '^// Code generated' && return 0
      ;;
  esac
  return 1
}

# Build the candidate list. We use `git ls-files` so the check is scoped
# to tracked files in this worktree only — naturally excludes other
# worktrees and untracked scratch files. We gate *.go, *.ts, *.tsx; other
# languages are not size-budgeted yet.
while IFS= read -r rel; do
  case "$rel" in
    vendor/*|*/vendor/*|node_modules/*|*/node_modules/*|dist/*|*/dist/*|testdata/*|*/testdata/*) continue ;;
    *_test.go|*.test.ts|*.test.tsx|*.spec.ts|*.spec.tsx) continue ;;
  esac
  f="$repo_root/$rel"
  [[ -f "$f" ]] || continue
  is_generated "$f" && continue
  loc=$(wc -l < "$f" | tr -d ' ')

  if (( loc >= FAIL )); then
    if [[ -s "$allow_norm" ]] && grep -Fxq "$rel" "$allow_norm"; then
      printf '%s\n' "$rel" >> "$allow_seen"
      if [[ -s "$base_allow_norm" ]] && grep -Fxq "$rel" "$base_allow_norm"; then
        if base_loc=$(git -C "$repo_root" show "$base_commit:$rel" 2>/dev/null | wc -l | tr -d ' '); then
          if [[ -n "$base_loc" ]] && (( loc > base_loc )); then
            printf '%s  %d > %d\n' "$rel" "$loc" "$base_loc" >> "$allowlist_growth_f"
          fi
        fi
      fi
      continue
    fi
    printf '%s  %d\n' "$rel" "$loc" >> "$failures_f"
  elif (( loc >= WARN )); then
    printf '%s  %d\n' "$rel" "$loc" >> "$warnings_f"
  fi
done < <(git -C "$repo_root" ls-files '*.go' '*.ts' '*.tsx')

if [[ -s "$warnings_f" ]]; then
  echo "::group::file-size warnings (between $WARN and $FAIL LOC)"
  sort -k2 -rn "$warnings_f" | sed 's/^/  /'
  echo "::endgroup::"
fi

if [[ -s "$allowlist_added_f" ]]; then
  echo "::error::file-size allowlist grew relative to ${base_ref:-the base ref}:"
  sort -u "$allowlist_added_f" | sed 's/^/  /'
  echo
  echo "The allowlist is forward-only. Decompose the file instead of adding"
  echo "a new exemption."
fi

if [[ -s "$allowlist_growth_f" ]]; then
  echo "::error::allowlisted files grew relative to ${base_ref:-the base ref}:"
  sort -k4 -rn "$allowlist_growth_f" | sed 's/^/  /'
  echo
  echo "Allowlisted files may shrink or disappear; they must not grow."
fi

# Stale allowlist entries: still listed but file is no longer over budget
# (or the file was deleted). Surface as a warning so the allowlist can
# shrink. Diff: lines in allow_norm not in allow_seen.
if [[ -s "$allow_norm" ]]; then
  stale=$(sort -u "$allow_norm" | comm -23 - <(sort -u "$allow_seen" 2>/dev/null) || true)
  if [[ -n "$stale" ]]; then
    echo "::group::stale allowlist entries (file shrunk below $FAIL or was deleted)"
    printf '%s\n' "$stale" | sed 's/^/  /'
    echo "  → remove these from scripts/file-size-allowlist.txt"
    echo "::endgroup::"
  fi
fi

if [[ -s "$failures_f" ]]; then
  echo "::error::file-size FAILURES (>= $FAIL LOC, not allowlisted):"
  sort -k2 -rn "$failures_f" | sed 's/^/  /'
  echo
  echo "Either decompose the file (see docs/CODE-QUALITY.md), or - if there is"
  echo "a documented reason - add it to scripts/file-size-allowlist.txt."
  echo "Adding a new allowlist entry without justification is a CONTRIBUTING.md"
  echo "violation. The allowlist is forward-only."
fi

if [[ -s "$failures_f" || -s "$allowlist_added_f" || -s "$allowlist_growth_f" ]]; then
  exit 1
fi

echo "file-size check OK (no files >= $FAIL LOC outside the allowlist)"
