#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
app_dir="$(cd "${script_dir}/.." && pwd)"
raw_ref="${1:-${GITHUB_REF:-${GITHUB_REF_NAME:-}}}"
tag=""

if [[ -n "${raw_ref}" ]]; then
  candidate="${raw_ref#refs/tags/}"
  if [[ "${raw_ref}" == refs/tags/* ]]; then
    tag="${candidate}"
  elif [[ "${candidate}" =~ ^v?[0-9]+(\.[0-9]+)*([.-].+)?$ ]]; then
    tag="${candidate}"
  elif [[ "${1+x}" == "x" ]]; then
    echo "Invalid release tag: ${raw_ref}" >&2
    exit 1
  fi
fi

if [[ -n "${tag}" ]]; then
  if [[ "${tag}" == v* ]]; then
    version="${tag#v}"
  else
    version="${tag}"
    tag="v${tag}"
  fi
  version="${version%-rewrite}"
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

manifest_file_entries() {
  local file="$1"

  cd "${app_dir}" &&
    bun -e '
      const fs = require("node:fs");
      const yaml = require("js-yaml");
      const manifest = yaml.load(fs.readFileSync(process.argv[1], "utf8"));
      const files = Array.isArray(manifest?.files) ? manifest.files : [];
      for (const file of files) {
        if (!file || typeof file !== "object") {
          continue;
        }

        const entryPath = file.url ?? file.path;
        if (typeof entryPath !== "string" || entryPath.length === 0) {
          continue;
        }

        process.stdout.write([entryPath, file.sha512 ?? "", file.size ?? ""].join("\u001f") + "\0");
      }
    ' "${file}"
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

verify_artifact_entry() {
  local latest_file="$1"
  local label="$2"
  local entry_path="$3"
  local expected_sha512="$4"
  local expected_size="$5"
  local artifact
  local actual_sha512
  local actual_size

  if [[ -z "${entry_path}" ]]; then
    echo "${latest_file} has empty or missing ${label} path" >&2
    exit 1
  fi

  if [[ "${entry_path}" == /* || "${entry_path}" == *".."* ]]; then
    echo "${latest_file} ${label} path must stay inside dist: ${entry_path}" >&2
    exit 1
  fi

  artifact="${dist_dir}/${entry_path}"
  if [[ ! -f "${artifact}" ]]; then
    echo "${latest_file} ${label} points at missing artifact: ${entry_path}" >&2
    exit 1
  fi

  if [[ -z "${expected_sha512}" ]]; then
    echo "${latest_file} ${label} has empty or missing sha512 field" >&2
    exit 1
  fi

  actual_sha512="$(sha512_base64 "${artifact}")"
  if [[ "${expected_sha512}" != "${actual_sha512}" ]]; then
    echo "${latest_file} ${label} sha512 does not match ${entry_path}" >&2
    exit 1
  fi

  if [[ -n "${expected_size}" ]]; then
    actual_size="$(wc -c < "${artifact}" | tr -d ' ')"
    if [[ "${expected_size}" != "${actual_size}" ]]; then
      echo "${latest_file} ${label} size '${expected_size}' does not match '${actual_size}'" >&2
      exit 1
    fi
  fi
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

  verify_artifact_entry "${latest_file}" "top-level" "${manifest_path}" "${manifest_sha512}" "${manifest_size}"

  while IFS=$'\x1f' read -r -d '' entry_path entry_sha512 entry_size; do
    verify_artifact_entry "${latest_file}" "files[] entry" "${entry_path}" "${entry_sha512}" "${entry_size}"
  done < <(manifest_file_entries "${latest_file}")

  echo "Verified ${latest_file} for ${tag}."
done
