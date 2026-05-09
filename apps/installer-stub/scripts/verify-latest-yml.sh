#!/usr/bin/env bash
set -euo pipefail

tag="${1:-${GITHUB_REF_NAME:-}}"

if [[ -z "${tag}" ]]; then
  echo "usage: verify-latest-yml.sh <git-tag>" >&2
  echo "or set GITHUB_REF_NAME" >&2
  exit 1
fi

tag="${tag#refs/tags/}"
version="${tag#v}"
dist_dir="${WUPHF_DIST_DIR:-dist}"
latest_files=()

for candidate in "${dist_dir}/latest.yml" "${dist_dir}/latest-mac.yml"; do
  if [[ -f "${candidate}" ]]; then
    latest_files+=("${candidate}")
  fi
done

if [[ "${#latest_files[@]}" -eq 0 ]]; then
  echo "No latest.yml or latest-mac.yml manifest found under ${dist_dir}" >&2
  exit 1
fi

field_value() {
  local file="$1"
  local field="$2"

  { grep -E "^[[:space:]]*${field}:[[:space:]]*" "${file}" || true; } |
    head -n 1 |
    sed -E "s/^[[:space:]]*${field}:[[:space:]]*['\"]?([^'\"]*)['\"]?.*/\\1/" |
    tr -d '\r'
}

for latest_file in "${latest_files[@]}"; do
  manifest_version="$(field_value "${latest_file}" "version")"
  manifest_path="$(field_value "${latest_file}" "path")"
  manifest_sha512="$(field_value "${latest_file}" "sha512")"

  if [[ "${manifest_version}" != "${version}" ]]; then
    echo "${latest_file} version '${manifest_version}' does not match '${version}'" >&2
    exit 1
  fi

  if [[ -z "${manifest_path}" ]]; then
    echo "${latest_file} has empty or missing path field" >&2
    exit 1
  fi

  if [[ -z "${manifest_sha512}" ]]; then
    echo "${latest_file} has empty or missing sha512 field" >&2
    exit 1
  fi

  echo "Verified ${latest_file} for ${tag}."
done
