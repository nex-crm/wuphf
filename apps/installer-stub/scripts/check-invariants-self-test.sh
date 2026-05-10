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

prod_deps_fixture="${tmp_root}/prod-deps"
write_fixture "${prod_deps_fixture}"
printf '{"dependencies": {"foo": "1.0.0"}}\n' > "${prod_deps_fixture}/apps/installer-stub/package.json"

expect_status 1 "${prod_deps_fixture}" "apps/installer-stub/scripts/check-invariants.sh" "${prod_deps_fixture}/root.out"
expect_output_contains "${prod_deps_fixture}/root.out" "must have NO 'dependencies'"
expect_status 1 "${prod_deps_fixture}/apps/installer-stub" "scripts/check-invariants.sh" "${prod_deps_fixture}/package.out"
expect_output_contains "${prod_deps_fixture}/package.out" "must have NO 'dependencies'"

echo "installer invariant self-test OK"
