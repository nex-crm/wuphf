#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
app_dir="$(cd "${script_dir}/.." && pwd)"

write_self_test_manifest() {
  local manifest_path="$1"
  local artifact_name="$2"
  local artifact_sha512="$3"
  local artifact_size="$4"
  local include_size="$5"

  {
    printf 'version: 0.0.0\n'
    printf 'files:\n'
    printf '  - url: %s\n' "${artifact_name}"
    printf '    sha512: %s\n' "${artifact_sha512}"
    if [[ "${include_size}" == "true" ]]; then
      printf '    size: %s\n' "${artifact_size}"
    fi
    printf 'path: %s\n' "${artifact_name}"
    printf 'sha512: %s\n' "${artifact_sha512}"
    if [[ "${include_size}" == "true" ]]; then
      printf 'size: %s\n' "${artifact_size}"
    fi
    printf "releaseDate: '2026-05-09T00:00:00.000Z'\n"
  } > "${manifest_path}"
}

write_self_test_malformed_files_manifest() {
  local manifest_path="$1"
  local artifact_name="$2"
  local artifact_sha512="$3"
  local artifact_size="$4"
  local files_entry="$5"

  {
    printf 'version: 0.0.0\n'
    printf 'files:\n'
    printf '  - %s\n' "${files_entry}"
    printf 'path: %s\n' "${artifact_name}"
    printf 'sha512: %s\n' "${artifact_sha512}"
    printf 'size: %s\n' "${artifact_size}"
    printf "releaseDate: '2026-05-09T00:00:00.000Z'\n"
  } > "${manifest_path}"
}

run_self_test() (
  set -euo pipefail

  local tmp_root
  local good_dist
  local malformed_empty_object_dist
  local malformed_string_dist
  local missing_size_dist
  local artifact_name
  local artifact_sha512
  local artifact_size
  local source_script
  local good_output
  local missing_size_output
  local malformed_empty_object_output
  local malformed_string_output
  local malformed_ref_output
  local status=0

  tmp_root="$(mktemp -d "${TMPDIR:-/tmp}/verify-latest-yml-self-test.XXXXXX")"
  trap 'rm -rf "${tmp_root}"' EXIT

  good_dist="${tmp_root}/good-dist"
  malformed_empty_object_dist="${tmp_root}/malformed-empty-object-dist"
  malformed_string_dist="${tmp_root}/malformed-string-dist"
  missing_size_dist="${tmp_root}/missing-size-dist"
  artifact_name="wuphf-installer-stub-0.0.0-mac-universal.zip"
  source_script="${script_dir}/verify-latest-yml.sh"
  good_output="${tmp_root}/good.out"
  malformed_empty_object_output="${tmp_root}/malformed-empty-object.out"
  malformed_string_output="${tmp_root}/malformed-string.out"
  malformed_ref_output="${tmp_root}/malformed-ref.out"
  missing_size_output="${tmp_root}/missing-size.out"

  mkdir -p "${good_dist}" "${malformed_empty_object_dist}" "${malformed_string_dist}" "${missing_size_dist}"
  printf 'fixture artifact bytes\n' > "${good_dist}/${artifact_name}"
  cp "${good_dist}/${artifact_name}" "${malformed_empty_object_dist}/${artifact_name}"
  cp "${good_dist}/${artifact_name}" "${malformed_string_dist}/${artifact_name}"
  cp "${good_dist}/${artifact_name}" "${missing_size_dist}/${artifact_name}"

  artifact_sha512="$(
    cd "${app_dir}" &&
      bun -e '
        const crypto = require("node:crypto");
        const fs = require("node:fs");
        process.stdout.write(
          crypto.createHash("sha512").update(fs.readFileSync(process.argv[1])).digest("base64"),
        );
      ' "${good_dist}/${artifact_name}"
  )"
  artifact_size="$(wc -c < "${good_dist}/${artifact_name}" | tr -d ' ')"

  write_self_test_manifest "${good_dist}/latest-mac.yml" "${artifact_name}" "${artifact_sha512}" "${artifact_size}" "true"
  write_self_test_malformed_files_manifest "${malformed_empty_object_dist}/latest-mac.yml" "${artifact_name}" "${artifact_sha512}" "${artifact_size}" "{}"
  write_self_test_malformed_files_manifest "${malformed_string_dist}/latest-mac.yml" "${artifact_name}" "${artifact_sha512}" "${artifact_size}" "string-not-object"
  write_self_test_manifest "${missing_size_dist}/latest-mac.yml" "${artifact_name}" "${artifact_sha512}" "${artifact_size}" "false"

  env WUPHF_VERIFY_LATEST_YML_RUN_SELF_TEST= WUPHF_VERIFY_LATEST_YML_SELF_TEST_ONLY= WUPHF_DIST_DIR="${good_dist}" \
    bash "${source_script}" "0.0.0" > "${good_output}" 2>&1

  status=0
  env WUPHF_VERIFY_LATEST_YML_RUN_SELF_TEST= WUPHF_VERIFY_LATEST_YML_SELF_TEST_ONLY= WUPHF_DIST_DIR="${good_dist}" GITHUB_REF="refs/tags/not-a-rewrite-tag" \
    bash "${source_script}" > "${malformed_ref_output}" 2>&1 || status=$?

  if [[ "${status}" -eq 0 ]]; then
    echo "expected malformed GITHUB_REF fixture to fail" >&2
    cat "${malformed_ref_output}" >&2
    exit 1
  fi

  if ! grep -Fq "Invalid rewrite release tag in GITHUB_REF: refs/tags/not-a-rewrite-tag" "${malformed_ref_output}"; then
    echo "expected malformed GITHUB_REF fixture to report invalid rewrite tag" >&2
    cat "${malformed_ref_output}" >&2
    exit 1
  fi

  status=0
  env WUPHF_VERIFY_LATEST_YML_RUN_SELF_TEST= WUPHF_VERIFY_LATEST_YML_SELF_TEST_ONLY= WUPHF_DIST_DIR="${malformed_empty_object_dist}" \
    bash "${source_script}" "0.0.0" > "${malformed_empty_object_output}" 2>&1 || status=$?

  if [[ "${status}" -eq 0 ]]; then
    echo "expected malformed empty-object fixture to fail" >&2
    cat "${malformed_empty_object_output}" >&2
    exit 1
  fi

  if ! grep -Fq "latest-mac.yml files[0] is missing url/path" "${malformed_empty_object_output}"; then
    echo "expected malformed empty-object fixture to report missing url/path" >&2
    cat "${malformed_empty_object_output}" >&2
    exit 1
  fi

  status=0
  env WUPHF_VERIFY_LATEST_YML_RUN_SELF_TEST= WUPHF_VERIFY_LATEST_YML_SELF_TEST_ONLY= WUPHF_DIST_DIR="${malformed_string_dist}" \
    bash "${source_script}" "0.0.0" > "${malformed_string_output}" 2>&1 || status=$?

  if [[ "${status}" -eq 0 ]]; then
    echo "expected malformed string fixture to fail" >&2
    cat "${malformed_string_output}" >&2
    exit 1
  fi

  if ! grep -Fq "latest-mac.yml files[0] is not an object" "${malformed_string_output}"; then
    echo "expected malformed string fixture to report non-object entry" >&2
    cat "${malformed_string_output}" >&2
    exit 1
  fi

  status=0
  env WUPHF_VERIFY_LATEST_YML_RUN_SELF_TEST= WUPHF_VERIFY_LATEST_YML_SELF_TEST_ONLY= WUPHF_DIST_DIR="${missing_size_dist}" \
    bash "${source_script}" "0.0.0" > "${missing_size_output}" 2>&1 || status=$?

  if [[ "${status}" -eq 0 ]]; then
    echo "expected missing-size fixture to fail" >&2
    cat "${missing_size_output}" >&2
    exit 1
  fi

  if ! grep -Fq "latest-mac.yml top-level has empty or missing size field" "${missing_size_output}"; then
    echo "expected missing-size fixture to report missing size" >&2
    cat "${missing_size_output}" >&2
    exit 1
  fi

  echo "verify-latest-yml self-test OK"
)

if [[ "${WUPHF_VERIFY_LATEST_YML_RUN_SELF_TEST:-}" == "1" ]]; then
  run_self_test
  if [[ "${WUPHF_VERIFY_LATEST_YML_SELF_TEST_ONLY:-}" == "1" ]]; then
    exit 0
  fi
fi

raw_ref="${1:-}"
if [[ -z "${raw_ref}" ]]; then
  raw_ref="${GITHUB_REF:-${GITHUB_REF_NAME:-}}"
fi
tag=""

if [[ -n "${raw_ref}" ]]; then
  candidate="${raw_ref#refs/tags/}"
  if [[ "${raw_ref}" == refs/tags/* ]]; then
    if [[ ! "${candidate}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?-rewrite$ ]]; then
      echo "Invalid rewrite release tag in GITHUB_REF: ${raw_ref}" >&2
      exit 1
    fi
    tag="${candidate}"
  elif [[ "${candidate}" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(-rewrite)?$ ]]; then
    tag="${candidate}"
  else
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
      for (const [index, entry] of files.entries()) {
        if (!entry || typeof entry !== "object") {
          console.error(process.argv[1] + " files[" + index + "] is not an object");
          process.exit(1);
        }

        const entryPath = entry.url ?? entry.path;
        if (typeof entryPath !== "string" || entryPath.length === 0) {
          console.error(process.argv[1] + " files[" + index + "] is missing url/path");
          process.exit(1);
        }

        process.stdout.write([entryPath, entry.sha512 ?? "", entry.size ?? ""].join("\u001f") + "\0");
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

  if [[ -z "${expected_size}" ]]; then
    echo "${latest_file} ${label} has empty or missing size field" >&2
    exit 1
  fi

  actual_size="$(wc -c < "${artifact}" | tr -d ' ')"
  if [[ "${expected_size}" != "${actual_size}" ]]; then
    echo "${latest_file} ${label} size '${expected_size}' does not match '${actual_size}'" >&2
    exit 1
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

  entries_output="$(mktemp "${TMPDIR:-/tmp}/verify-latest-yml-entries.XXXXXX")"
  if ! manifest_file_entries "${latest_file}" > "${entries_output}"; then
    rm -f "${entries_output}"
    exit 1
  fi

  while IFS=$'\x1f' read -r -d '' entry_path entry_sha512 entry_size; do
    verify_artifact_entry "${latest_file}" "files[] entry" "${entry_path}" "${entry_sha512}" "${entry_size}"
  done < "${entries_output}"
  rm -f "${entries_output}"

  echo "Verified ${latest_file} for ${tag}."
done
