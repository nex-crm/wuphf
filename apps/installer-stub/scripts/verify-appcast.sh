#!/usr/bin/env bash
set -euo pipefail

tag="${1:-${GITHUB_REF_NAME:-}}"

if [[ -z "${tag}" ]]; then
  echo "usage: verify-appcast.sh <git-tag>" >&2
  echo "or set GITHUB_REF_NAME" >&2
  exit 1
fi

tag="${tag#refs/tags/}"
version="${tag#v}"
dist_dir="${WUPHF_DIST_DIR:-dist}"

candidates=(
  "${dist_dir}/appcast.xml"
  "${dist_dir}/mac/appcast.xml"
)

appcast=""
for candidate in "${candidates[@]}"; do
  if [[ -f "${candidate}" ]]; then
    appcast="${candidate}"
    break
  fi
done

if [[ -z "${appcast}" ]]; then
  while IFS= read -r found; do
    appcast="${found}"
    break
  done < <(find "${dist_dir}" -type f -name "appcast.xml" 2>/dev/null | sort)
fi

if [[ -z "${appcast}" ]]; then
  echo "appcast.xml not found under ${dist_dir}" >&2
  exit 1
fi

if ! grep -Fq "<sparkle:version>${version}</sparkle:version>" "${appcast}" &&
  ! grep -Fq "sparkle:version=\"${version}\"" "${appcast}"; then
  echo "${appcast} does not contain Sparkle version '${version}'" >&2
  exit 1
fi

url="$(
  sed -nE 's/.*<enclosure[^>]*url="([^"]+)".*/\1/p' "${appcast}" | head -n 1
)"

if [[ -z "${url}" ]]; then
  echo "${appcast} does not contain an enclosure URL" >&2
  exit 1
fi

if [[ ! "${url}" =~ ^https://github\.com/.+/releases/download/.+ ]]; then
  echo "${appcast} enclosure URL is not a GitHub release asset URL: ${url}" >&2
  exit 1
fi

if [[ "${url}" != *"/releases/download/${tag}/"* ]]; then
  echo "${appcast} enclosure URL does not reference tag '${tag}': ${url}" >&2
  exit 1
fi

echo "Verified Sparkle appcast ${appcast} for ${tag}."
