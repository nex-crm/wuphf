#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
package_root="$(cd -- "${script_dir}/.." && pwd)"
repo_root="$(cd -- "${package_root}/../.." && pwd)"
workflow="${repo_root}/.github/workflows/release-rewrite.yml"

if [[ ! -f "${workflow}" ]]; then
  echo "ERR: workflow file not found at ${workflow}; check-invariants must run from a wuphf checkout" >&2
  exit 2
fi

scan_targets=(
  "${package_root}/.gitignore"
  "${package_root}/package.json"
  "${package_root}/electron-builder.yml"
  "${package_root}/src"
  "${package_root}/build"
  "${package_root}/scripts"
  "${workflow}"
)

for scan_target in "${scan_targets[@]}"; do
  if [[ ! -e "${scan_target}" ]]; then
    echo "ERR: invariant scan target not found at ${scan_target}; check-invariants must run from a wuphf checkout" >&2
    exit 2
  fi
done

violations=()
forbidden_patterns=(
  # String splits keep this file from matching itself when ripgrep scans scripts/.
  "--skip-code""sign"
  "--skip-""sign"
  "skip-""sign"
  "skip-""notarize"
  "skip""Notarize"
  "notarize:"" false"
  "hardenedRuntime:"" false"
)

for pattern in "${forbidden_patterns[@]}"; do
  while IFS= read -r match; do
    violations+=("forbidden literal '${pattern}': ${match}")
  done < <(rg -n -F -- "${pattern}" "${scan_targets[@]}" || true)
done

cert_path_regex='(^|[[:space:]"'"'"'=(])([./~]|[A-Za-z]:\\)[^[:space:]"'"'"']+\.(p12|pfx)([[:space:]"'"'"')]|$)'
while IFS= read -r match; do
  violations+=("hardcoded certificate path: ${match}")
done < <(rg -n --pcre2 "${cert_path_regex}" "${scan_targets[@]}" || true)

# electron-builder.yml sets `npmRebuild: false` to avoid the bun npm_execpath
# leak in CI. That's safe ONLY while the stub has zero production deps;
# adding a native dep would silently ship without the Electron-ABI rebuild.
# Enforce the no-prod-deps invariant here so the band-aid stays safe.
if rg -q '"dependencies"\s*:' "${package_root}/package.json"; then
  violations+=("apps/installer-stub/package.json must have NO 'dependencies' (only devDependencies); npmRebuild: false in electron-builder.yml depends on this invariant")
fi

while IFS= read -r line; do
  action_ref="$(sed -E 's/^([^:]+:)?[0-9]+:.*uses:[[:space:]]*([^[:space:]#]+).*/\2/' <<<"${line}")"

  if [[ "${action_ref}" == ./* || "${action_ref}" == docker://* ]]; then
    continue
  fi

  if [[ "${action_ref}" != *@* ]]; then
    violations+=("GitHub Action is missing an explicit ref: ${line}")
    continue
  fi

  action_sha="${action_ref##*@}"
  if [[ ! "${action_sha}" =~ ^[0-9a-f]{40}$ ]]; then
    violations+=("GitHub Action is not pinned to a full SHA: ${line}")
  fi
done < <(rg -n 'uses:[[:space:]]*[^[:space:]#]+' "${workflow}" || true)

if [[ "${#violations[@]}" -gt 0 ]]; then
  printf "Installer invariant violations:\n" >&2
  printf -- "- %s\n" "${violations[@]}" >&2
  exit 1
fi

echo "Installer invariants passed."
