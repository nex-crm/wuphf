#!/usr/bin/env bash
# Self-test for scripts/check-wails-boundary.sh.
#
# The CI gate proves this repository currently passes the Wails boundary. These
# fixture repos prove the check fails on forbidden imports and allows the
# reviewed Wails-only directories.

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
source_script="$script_dir/check-wails-boundary.sh"
tmp_root="$(mktemp -d "${TMPDIR:-/tmp}/wails-boundary-self-test.XXXXXX")"
trap 'rm -rf "$tmp_root"' EXIT

init_fixture_repo() {
  local repo="$1"
  mkdir -p "$repo/scripts"
  cp "$source_script" "$repo/scripts/check-wails-boundary.sh"
  chmod +x "$repo/scripts/check-wails-boundary.sh"
}

expect_failure_containing() {
  local repo="$1"
  local needle="$2"
  local output="$repo/check.out"

  if bash "$repo/scripts/check-wails-boundary.sh" > "$output" 2>&1; then
    echo "expected Wails boundary check to fail in $repo" >&2
    cat "$output" >&2
    exit 1
  fi
  if ! grep -Fq "$needle" "$output"; then
    echo "expected Wails boundary output to contain: $needle" >&2
    cat "$output" >&2
    exit 1
  fi
}

expect_success() {
  local repo="$1"
  local output="$repo/check.out"

  if ! bash "$repo/scripts/check-wails-boundary.sh" > "$output" 2>&1; then
    echo "expected Wails boundary check to pass in $repo" >&2
    cat "$output" >&2
    exit 1
  fi
}

write_forbidden_go_fixture() {
  local path="$1"
  local wails_v2="github.com/wailsapp/wails/v2/pkg/runtime"

  mkdir -p "$(dirname -- "$path")"
  {
    printf '%s\n' "package bad"
    printf '\n'
    printf 'import wails "%s"\n' "$wails_v2"
    printf '\n'
    printf '%s\n' "var _ = wails.EventsEmit"
  } > "$path"
}

write_forbidden_ts_fixture() {
  local path="$1"
  local generated_binding="wailsjs/go/main/App"

  mkdir -p "$(dirname -- "$path")"
  printf 'import { App } from "%s";\n' "$generated_binding" > "$path"
}

test_forbidden_go_import_fails() {
  local repo="$tmp_root/forbidden-go"
  init_fixture_repo "$repo"
  write_forbidden_go_fixture "$repo/internal/bad/wails_bad.go"

  expect_failure_containing "$repo" "internal/bad/wails_bad.go"
}

test_forbidden_ts_import_fails() {
  local repo="$tmp_root/forbidden-ts"
  init_fixture_repo "$repo"
  write_forbidden_ts_fixture "$repo/web/src/bad/wailsBad.ts"

  expect_failure_containing "$repo" "web/src/bad/wailsBad.ts"
}

test_allowlisted_imports_pass() {
  local repo="$tmp_root/allowlisted"
  local wails_v3="github.com/wailsapp/wails/v3/pkg/application"
  local wails_runtime="@wails/runtime"
  local wails_app_runtime="@wailsapp/runtime"

  init_fixture_repo "$repo"

  mkdir -p "$repo/desktop/oswails" "$repo/web/src/desktop" "$repo/web/wailsjs" "$repo/web/dist" "$repo/node_modules/pkg"
  {
    printf '%s\n' "package oswails"
    printf '\n'
    printf 'import app "%s"\n' "$wails_v3"
    printf '\n'
    printf '%s\n' "var _ = app.New"
  } > "$repo/desktop/oswails/runtime.go"
  printf 'import { EventsOn } from "%s";\n' "$wails_runtime" > "$repo/web/src/desktop/runtime.ts"
  printf 'import { EventsOn } from "%s";\n' "$wails_app_runtime" > "$repo/web/wailsjs/generated.ts"
  printf 'import { EventsOn } from "%s";\n' "$wails_app_runtime" > "$repo/web/dist/generated.ts"
  printf 'import { EventsOn } from "%s";\n' "$wails_app_runtime" > "$repo/node_modules/pkg/generated.ts"

  expect_success "$repo"
}

test_forbidden_go_import_fails
test_forbidden_ts_import_fails
test_allowlisted_imports_pass

echo "wails-boundary self-test OK"
