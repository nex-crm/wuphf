#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
app_dir="$(cd "${script_dir}/.." && pwd)"
tag="${1:-${GITHUB_REF_NAME:-}}"

if [[ -n "${tag}" ]]; then
  tag="${tag#refs/tags/}"
  version="${tag#v}"
else
  version="$(cd "${app_dir}" && bun -e 'process.stdout.write(require("./package.json").version)')"
  tag="v${version}"
fi

if [[ -n "${WUPHF_DIST_DIR:-}" ]]; then
  dist_dir="${WUPHF_DIST_DIR}"
elif [[ -d "dist" ]]; then
  dist_dir="dist"
else
  dist_dir="${app_dir}/dist"
fi

if [[ ! -d "${dist_dir}" ]]; then
  echo "dist directory not found: ${dist_dir}" >&2
  exit 1
fi

dist_dir="$(cd "${dist_dir}" && pwd)"
latest_files=()

while IFS= read -r -d '' latest_file; do
  latest_files+=("${latest_file}")
done < <(find "${dist_dir}" -maxdepth 1 -type f \( -name "latest.yml" -o -name "latest-*.yml" \) -print0 | sort -z)

if [[ "${#latest_files[@]}" -eq 0 ]]; then
  echo "No latest*.yml manifest found under ${dist_dir}" >&2
  exit 1
fi

manifest_field() {
  local file="$1"
  local field="$2"

  cd "${app_dir}" &&
    bun -e '
      const fs = require("node:fs");
      const yaml = require("js-yaml");
      const manifest = yaml.load(fs.readFileSync(process.argv[1], "utf8"));
      const value = manifest?.[process.argv[2]];
      if (value !== undefined && value !== null) {
        process.stdout.write(String(value));
      }
    ' "${file}" "${field}"
}

sha512_base64() {
  local artifact="$1"
  local hex

  if command -v shasum >/dev/null 2>&1 && command -v xxd >/dev/null 2>&1; then
    hex="$(shasum -a 512 -b "${artifact}" | awk '{print $1}')"
    printf "%s" "${hex}" | xxd -r -p | base64 | tr -d '\n'
    return
  fi

  if command -v openssl >/dev/null 2>&1; then
    openssl dgst -sha512 -binary "${artifact}" | base64 | tr -d '\n'
    return
  fi

  cd "${app_dir}" &&
    bun -e '
      const crypto = require("node:crypto");
      const fs = require("node:fs");
      process.stdout.write(
        crypto.createHash("sha512").update(fs.readFileSync(process.argv[1])).digest("base64"),
      );
    ' "${artifact}"
}

for latest_file in "${latest_files[@]}"; do
  manifest_name="$(basename "${latest_file}")"
  manifest_version="$(manifest_field "${latest_file}" "version")"
  manifest_path="$(manifest_field "${latest_file}" "path")"
  manifest_sha512="$(manifest_field "${latest_file}" "sha512")"
  manifest_size="$(manifest_field "${latest_file}" "size")"

  if [[ "${manifest_version}" != "${version}" ]]; then
    echo "${latest_file} version '${manifest_version}' does not match '${version}'" >&2
    exit 1
  fi

  if [[ -z "${manifest_path}" ]]; then
    echo "${latest_file} has empty or missing path field" >&2
    exit 1
  fi

  if [[ "${manifest_path}" == /* || "${manifest_path}" == *".."* ]]; then
    echo "${latest_file} path must stay inside dist: ${manifest_path}" >&2
    exit 1
  fi

  case "${manifest_name}" in
    latest-mac.yml)
      if [[ "${manifest_path}" != *.zip ]]; then
        echo "${latest_file} must point at a macOS .zip artifact, got ${manifest_path}" >&2
        exit 1
      fi
      ;;
    latest-linux.yml)
      if [[ "${manifest_path}" != *.AppImage ]]; then
        echo "${latest_file} must point at a Linux .AppImage artifact, got ${manifest_path}" >&2
        exit 1
      fi
      ;;
    latest.yml)
      if [[ "${manifest_path}" != *.exe ]]; then
        echo "${latest_file} must point at a Windows .exe artifact, got ${manifest_path}" >&2
        exit 1
      fi
      ;;
  esac

  artifact="${dist_dir}/${manifest_path}"
  if [[ ! -f "${artifact}" ]]; then
    echo "${latest_file} points at missing artifact: ${manifest_path}" >&2
    exit 1
  fi

  if [[ -z "${manifest_sha512}" ]]; then
    echo "${latest_file} has empty or missing sha512 field" >&2
    exit 1
  fi

  actual_sha512="$(sha512_base64 "${artifact}")"
  if [[ "${manifest_sha512}" != "${actual_sha512}" ]]; then
    echo "${latest_file} sha512 does not match ${manifest_path}" >&2
    exit 1
  fi

  if [[ -n "${manifest_size}" ]]; then
    actual_size="$(wc -c < "${artifact}" | tr -d ' ')"
    if [[ "${manifest_size}" != "${actual_size}" ]]; then
      echo "${latest_file} size '${manifest_size}' does not match '${actual_size}'" >&2
      exit 1
    fi
  fi

  echo "Verified ${latest_file} for ${tag}."
done
