#!/usr/bin/env bash
set -euo pipefail

workflow=".github/workflows/release-rewrite.yml"
scan_targets=(
  "apps/installer-stub/.gitignore"
  "apps/installer-stub/package.json"
  "apps/installer-stub/electron-builder.yml"
  "apps/installer-stub/src"
  "apps/installer-stub/build"
  "apps/installer-stub/scripts"
)

if [[ -f "${workflow}" ]]; then
  scan_targets+=("${workflow}")
fi

violations=()
forbidden_patterns=(
  "--skip-code""sign"
  "--skip-""sign"
  "skip""Notarize"
  "notarize:"" false"
  "hardenedRuntime:"" false"
)

for pattern in "${forbidden_patterns[@]}"; do
  while IFS= read -r match; do
    violations+=("forbidden literal '${pattern}': ${match}")
  done < <(rg -n -F "${pattern}" "${scan_targets[@]}" 2>/dev/null || true)
done

cert_path_regex='(^|[[:space:]"'"'"'=(])([./~]|[A-Za-z]:\\)[^[:space:]"'"'"']+\.(p12|pfx)([[:space:]"'"'"')]|$)'
while IFS= read -r match; do
  violations+=("hardcoded certificate path: ${match}")
done < <(rg -n --pcre2 "${cert_path_regex}" "${scan_targets[@]}" 2>/dev/null || true)

if [[ -f "${workflow}" ]]; then
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
fi

if [[ "${#violations[@]}" -gt 0 ]]; then
  printf "Installer invariant violations:\n" >&2
  printf -- "- %s\n" "${violations[@]}" >&2
  exit 1
fi

echo "Installer invariants passed."
