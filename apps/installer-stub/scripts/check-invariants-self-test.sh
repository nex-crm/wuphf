#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
source_script="${script_dir}/check-invariants.sh"
tmp_root="$(mktemp -d "${TMPDIR:-/tmp}/installer-invariants-self-test.XXXXXX")"
trap 'rm -rf "${tmp_root}"' EXIT

write_fixture() {
  local fixture_root="$1"

  mkdir -p \
    "${fixture_root}/.github/workflows" \
    "${fixture_root}/apps/installer-stub/build" \
    "${fixture_root}/apps/installer-stub/scripts" \
    "${fixture_root}/apps/installer-stub/src"

  cp "${source_script}" "${fixture_root}/apps/installer-stub/scripts/check-invariants.sh"
  chmod +x "${fixture_root}/apps/installer-stub/scripts/check-invariants.sh"

  : > "${fixture_root}/apps/installer-stub/.gitignore"
  printf '{}\n' > "${fixture_root}/apps/installer-stub/package.json"
  printf 'appId: ai.nex.wuphf.installer-stub\n' > "${fixture_root}/apps/installer-stub/electron-builder.yml"
  printf 'console.log("fixture");\n' > "${fixture_root}/apps/installer-stub/src/main.js"
  printf 'name: Release Rewrite\n' > "${fixture_root}/.github/workflows/release-rewrite.yml"
}

run_check() {
  local cwd="$1"
  local command="$2"
  local output="$3"
  local status=0

  (cd "${cwd}" && bash "${command}") > "${output}" 2>&1 || status=$?
  return "${status}"
}

expect_status() {
  local expected="$1"
  local cwd="$2"
  local command="$3"
  local output="$4"
  local status=0

  run_check "${cwd}" "${command}" "${output}" || status=$?
  if [[ "${status}" -ne "${expected}" ]]; then
    echo "expected ${command} from ${cwd} to exit ${expected}, got ${status}" >&2
    cat "${output}" >&2
    exit 1
  fi
}

expect_output_contains() {
  local output="$1"
  local needle="$2"

  if ! grep -Fq "${needle}" "${output}"; then
    echo "expected output to contain: ${needle}" >&2
    cat "${output}" >&2
    exit 1
  fi
}

passing_fixture="${tmp_root}/passing"
write_fixture "${passing_fixture}"
expect_status 0 "${passing_fixture}" "apps/installer-stub/scripts/check-invariants.sh" "${passing_fixture}/root.out"
expect_status 0 "${passing_fixture}/apps/installer-stub" "scripts/check-invariants.sh" "${passing_fixture}/package.out"

violating_fixture="${tmp_root}/violating"
write_fixture "${violating_fixture}"
forbidden_literal="--skip-code""sign"
printf '%s\n' "${forbidden_literal}" >> "${violating_fixture}/apps/installer-stub/electron-builder.yml"

expect_status 1 "${violating_fixture}" "apps/installer-stub/scripts/check-invariants.sh" "${violating_fixture}/root.out"
expect_output_contains "${violating_fixture}/root.out" "forbidden literal '${forbidden_literal}'"
expect_status 1 "${violating_fixture}/apps/installer-stub" "scripts/check-invariants.sh" "${violating_fixture}/package.out"
expect_output_contains "${violating_fixture}/package.out" "forbidden literal '${forbidden_literal}'"

missing_workflow_fixture="${tmp_root}/missing-workflow"
write_fixture "${missing_workflow_fixture}"
rm "${missing_workflow_fixture}/.github/workflows/release-rewrite.yml"
expect_status 2 "${missing_workflow_fixture}" "apps/installer-stub/scripts/check-invariants.sh" "${missing_workflow_fixture}/missing.out"
expect_output_contains "${missing_workflow_fixture}/missing.out" "ERR: workflow file not found"

unpinned_action_fixture="${tmp_root}/unpinned-action"
write_fixture "${unpinned_action_fixture}"
{
  printf 'jobs:\n'
  printf '  unpinned:\n'
  printf '    runs-on: ubuntu-24.04\n'
  printf '    steps:\n'
  printf '      - uses: owner/repo@main\n'
} >> "${unpinned_action_fixture}/.github/workflows/release-rewrite.yml"

expect_status 1 "${unpinned_action_fixture}" "apps/installer-stub/scripts/check-invariants.sh" "${unpinned_action_fixture}/root.out"
expect_output_contains "${unpinned_action_fixture}/root.out" "GitHub Action is not pinned to a full SHA"
expect_status 1 "${unpinned_action_fixture}/apps/installer-stub" "scripts/check-invariants.sh" "${unpinned_action_fixture}/package.out"
expect_output_contains "${unpinned_action_fixture}/package.out" "GitHub Action is not pinned to a full SHA"

cert_path_fixture="${tmp_root}/cert-path"
write_fixture "${cert_path_fixture}"
printf 'certificateFile: ./cert.p12\n' >> "${cert_path_fixture}/apps/installer-stub/electron-builder.yml"

expect_status 1 "${cert_path_fixture}" "apps/installer-stub/scripts/check-invariants.sh" "${cert_path_fixture}/root.out"
expect_output_contains "${cert_path_fixture}/root.out" "hardcoded certificate path"
expect_status 1 "${cert_path_fixture}/apps/installer-stub" "scripts/check-invariants.sh" "${cert_path_fixture}/package.out"
expect_output_contains "${cert_path_fixture}/package.out" "hardcoded certificate path"

dev_deps_fixture="${tmp_root}/dev-deps"
write_fixture "${dev_deps_fixture}"
printf '{"devDependencies": {"foo": "1.0.0"}}\n' > "${dev_deps_fixture}/apps/installer-stub/package.json"
expect_status 0 "${dev_deps_fixture}" "apps/installer-stub/scripts/check-invariants.sh" "${dev_deps_fixture}/root.out"
expect_status 0 "${dev_deps_fixture}/apps/installer-stub" "scripts/check-invariants.sh" "${dev_deps_fixture}/package.out"

expect_dependency_block_failure() {
  local block_name="$1"
  local fixture="${tmp_root}/${block_name}"

  write_fixture "${fixture}"
  printf '{"%s": {"foo": "1.0.0"}}\n' "${block_name}" > "${fixture}/apps/installer-stub/package.json"

  expect_status 1 "${fixture}" "apps/installer-stub/scripts/check-invariants.sh" "${fixture}/root.out"
  expect_output_contains "${fixture}/root.out" "forbidden dependency block: ${block_name}"
  expect_status 1 "${fixture}/apps/installer-stub" "scripts/check-invariants.sh" "${fixture}/package.out"
  expect_output_contains "${fixture}/package.out" "forbidden dependency block: ${block_name}"
}

# peerDependencies and optionalDependencies remain forbidden outright; the
# `dependencies` block is now an allowlist (per #771 fix).
expect_dependency_block_failure "peerDependencies"
expect_dependency_block_failure "optionalDependencies"

# dependencies entry that is on the allowlist passes the gate.
allowed_dep_fixture="${tmp_root}/allowed-dep"
write_fixture "${allowed_dep_fixture}"
printf '{"dependencies": {"electron-updater": "6.3.9"}, "wuphfRuntimeDependenciesAllowlist": ["electron-updater"]}\n' \
  > "${allowed_dep_fixture}/apps/installer-stub/package.json"
expect_status 0 "${allowed_dep_fixture}" "apps/installer-stub/scripts/check-invariants.sh" "${allowed_dep_fixture}/root.out"
expect_status 0 "${allowed_dep_fixture}/apps/installer-stub" "scripts/check-invariants.sh" "${allowed_dep_fixture}/package.out"

# dependencies entry that is NOT in APPROVED_RUNTIME_DEPS fails the gate.
unallowlisted_dep_fixture="${tmp_root}/unallowlisted-dep"
write_fixture "${unallowlisted_dep_fixture}"
printf '{"dependencies": {"some-other-pkg": "1.0.0"}, "wuphfRuntimeDependenciesAllowlist": ["electron-updater"]}\n' \
  > "${unallowlisted_dep_fixture}/apps/installer-stub/package.json"
expect_status 1 "${unallowlisted_dep_fixture}" "apps/installer-stub/scripts/check-invariants.sh" "${unallowlisted_dep_fixture}/root.out"
expect_output_contains "${unallowlisted_dep_fixture}/root.out" "dependencies.some-other-pkg is not in APPROVED_RUNTIME_DEPS"

# package.json allowlist entry that is NOT in APPROVED_RUNTIME_DEPS also fails.
# This is the "policy is the script, not the json" guarantee: a future PR
# cannot widen the surface by editing only package.json's allowlist field.
package_only_widen_fixture="${tmp_root}/package-only-widen"
write_fixture "${package_only_widen_fixture}"
printf '{"dependencies": {"electron-updater": "6.3.9", "some-other-pkg": "1.0.0"}, "wuphfRuntimeDependenciesAllowlist": ["electron-updater", "some-other-pkg"]}\n' \
  > "${package_only_widen_fixture}/apps/installer-stub/package.json"
expect_status 1 "${package_only_widen_fixture}" "apps/installer-stub/scripts/check-invariants.sh" "${package_only_widen_fixture}/root.out"
expect_output_contains "${package_only_widen_fixture}/root.out" "wuphfRuntimeDependenciesAllowlist contains \"some-other-pkg\""

# Approved dep without a corresponding package.json allowlist entry still
# fails — the script demands BOTH the policy approval AND the package-json
# declaration, so docs/intent stay in sync with the dependencies block.
empty_allowlist_fixture="${tmp_root}/empty-allowlist"
write_fixture "${empty_allowlist_fixture}"
printf '{"dependencies": {"electron-updater": "6.3.9"}}\n' \
  > "${empty_allowlist_fixture}/apps/installer-stub/package.json"
expect_status 1 "${empty_allowlist_fixture}" "apps/installer-stub/scripts/check-invariants.sh" "${empty_allowlist_fixture}/root.out"
expect_output_contains "${empty_allowlist_fixture}/root.out" "approved by APPROVED_RUNTIME_DEPS but not declared in package.json"

# Stale allowlist entry (allowlist names a pkg that's not in dependencies)
# fails — keeps the two in sync.
stale_allowlist_fixture="${tmp_root}/stale-allowlist"
write_fixture "${stale_allowlist_fixture}"
printf '{"wuphfRuntimeDependenciesAllowlist": ["electron-updater"]}\n' \
  > "${stale_allowlist_fixture}/apps/installer-stub/package.json"
expect_status 1 "${stale_allowlist_fixture}" "apps/installer-stub/scripts/check-invariants.sh" "${stale_allowlist_fixture}/root.out"
expect_output_contains "${stale_allowlist_fixture}/root.out" "stale entry"

# Source-scan invariant: if src/main.js requires a package, that name MUST
# be in both `dependencies` AND `wuphfRuntimeDependenciesAllowlist`. This
# closes the "remove dep block but leave require in source" loophole that
# would re-introduce the #771 crash-on-launch failure mode.
require_in_source_fixture="${tmp_root}/require-in-source"
write_fixture "${require_in_source_fixture}"
printf 'const x = require("electron-updater");\n' \
  > "${require_in_source_fixture}/apps/installer-stub/src/main.js"
printf '{"dependencies": {"electron-updater": "6.8.3"}, "wuphfRuntimeDependenciesAllowlist": ["electron-updater"]}\n' \
  > "${require_in_source_fixture}/apps/installer-stub/package.json"
expect_status 0 "${require_in_source_fixture}" "apps/installer-stub/scripts/check-invariants.sh" "${require_in_source_fixture}/root.out"

# require() with no matching dep entry fails — exact #771 regression mode
require_no_dep_fixture="${tmp_root}/require-no-dep"
write_fixture "${require_no_dep_fixture}"
printf 'const x = require("electron-updater");\n' \
  > "${require_no_dep_fixture}/apps/installer-stub/src/main.js"
printf '{}\n' > "${require_no_dep_fixture}/apps/installer-stub/package.json"
expect_status 1 "${require_no_dep_fixture}" "apps/installer-stub/scripts/check-invariants.sh" "${require_no_dep_fixture}/root.out"
expect_output_contains "${require_no_dep_fixture}/root.out" "src/main.js require(\"electron-updater\") but the package is not in dependencies"

# require() with dep but missing from allowlist fails — closes the
# "approved by APPROVED_RUNTIME_DEPS but absent allowlist" gap from R2
require_no_allowlist_fixture="${tmp_root}/require-no-allowlist"
write_fixture "${require_no_allowlist_fixture}"
printf 'const x = require("electron-updater");\n' \
  > "${require_no_allowlist_fixture}/apps/installer-stub/src/main.js"
printf '{"dependencies": {"electron-updater": "6.8.3"}}\n' \
  > "${require_no_allowlist_fixture}/apps/installer-stub/package.json"
expect_status 1 "${require_no_allowlist_fixture}" "apps/installer-stub/scripts/check-invariants.sh" "${require_no_allowlist_fixture}/root.out"
expect_output_contains "${require_no_allowlist_fixture}/root.out" "wuphfRuntimeDependenciesAllowlist"

echo "installer invariant self-test OK"
