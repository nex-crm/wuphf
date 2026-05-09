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

echo "installer invariant self-test OK"
