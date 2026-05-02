#!/usr/bin/env bash
# Self-test for scripts/check-file-size.sh.
#
# The CI gate proves this repository currently passes the file-size budget.
# These fixture repos prove the ratchet fails in the cases it exists to catch.

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
source_script="$script_dir/check-file-size.sh"
tmp_root="$(mktemp -d "${TMPDIR:-/tmp}/file-size-self-test.XXXXXX")"
trap 'rm -rf "$tmp_root"' EXIT

write_lines() {
  local path="$1"
  local count="$2"
  mkdir -p "$(dirname -- "$path")"
  awk -v n="$count" 'BEGIN { for (i = 1; i <= n; i++) print "// line " i }' > "$path"
}

init_fixture_repo() {
  local repo="$1"
  mkdir -p "$repo/scripts"
  cp "$source_script" "$repo/scripts/check-file-size.sh"
  chmod +x "$repo/scripts/check-file-size.sh"
  : > "$repo/scripts/file-size-allowlist.txt"

  git -C "$repo" init >/dev/null 2>&1
  git -C "$repo" checkout -b main >/dev/null 2>&1
  git -C "$repo" config user.email "file-size-self-test@example.invalid"
  git -C "$repo" config user.name "file-size self-test"
}

commit_fixture_base() {
  local repo="$1"
  git -C "$repo" add .
  git -C "$repo" commit -m "base" >/dev/null
}

expect_failure_containing() {
  local repo="$1"
  local needle="$2"
  local output="$repo/check.out"

  if GITHUB_BASE_REF='' FILE_SIZE_BASE_REF=main bash "$repo/scripts/check-file-size.sh" > "$output" 2>&1; then
    echo "expected file-size check to fail in $repo" >&2
    cat "$output" >&2
    exit 1
  fi
  if ! grep -Fq "$needle" "$output"; then
    echo "expected file-size check output to contain: $needle" >&2
    cat "$output" >&2
    exit 1
  fi
}

expect_success() {
  local repo="$1"
  local output="$repo/check.out"

  if ! GITHUB_BASE_REF='' FILE_SIZE_BASE_REF='' bash "$repo/scripts/check-file-size.sh" > "$output" 2>&1; then
    echo "expected file-size check to pass in $repo" >&2
    cat "$output" >&2
    exit 1
  fi
}

test_first_allowlist_addition_fails() {
  local repo="$tmp_root/first-allowlist-addition"
  init_fixture_repo "$repo"
  write_lines "$repo/pkg/small.go" 10
  commit_fixture_base "$repo"

  write_lines "$repo/pkg/large.go" 1500
  printf '%s\n' "pkg/large.go" >> "$repo/scripts/file-size-allowlist.txt"
  git -C "$repo" add .

  expect_failure_containing "$repo" "file-size allowlist grew"
}

test_allowlisted_growth_fails() {
  local repo="$tmp_root/allowlisted-growth"
  init_fixture_repo "$repo"
  write_lines "$repo/pkg/large.go" 1500
  printf '%s\n' "pkg/large.go" >> "$repo/scripts/file-size-allowlist.txt"
  commit_fixture_base "$repo"

  write_lines "$repo/pkg/large.go" 1501
  git -C "$repo" add .

  expect_failure_containing "$repo" "allowlisted files grew"
}

test_origin_main_fallback_when_branch_has_no_upstream() {
  local repo="$tmp_root/origin-main-fallback"
  init_fixture_repo "$repo"
  write_lines "$repo/pkg/large.go" 1500
  printf '%s\n' "pkg/large.go" >> "$repo/scripts/file-size-allowlist.txt"
  commit_fixture_base "$repo"
  git -C "$repo" update-ref refs/remotes/origin/main HEAD
  git -C "$repo" checkout -b local-review >/dev/null 2>&1

  expect_success "$repo"
}

test_first_allowlist_addition_fails
test_allowlisted_growth_fails
test_origin_main_fallback_when_branch_has_no_upstream

echo "file-size self-test OK"
