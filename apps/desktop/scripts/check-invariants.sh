#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
violations=()

record_violation() {
  violations+=("$1")
}

kebab_to_pascal() {
  local input="$1"
  local output=""
  local part=""
  local first=""
  local rest=""
  local IFS='-'
  read -r -a parts <<< "${input}"

  for part in "${parts[@]}"; do
    first="$(printf '%s' "${part:0:1}" | tr '[:lower:]' '[:upper:]')"
    rest="${part:1}"
    output="${output}${first}${rest}"
  done

  printf '%s' "${output}"
}

while IFS= read -r ipc_file; do
  stem="$(basename "${ipc_file}" .ts)"
  if [[ "${stem}" == _* || "${stem}" == "register-handlers" ]]; then
    continue
  fi
  if [[ ! "${stem}" =~ ^[a-z-]+$ ]]; then
    continue
  fi

  handler_name="handle$(kebab_to_pascal "${stem}")"
  if ! grep -Eq "export (async )?function ${handler_name}\b|export const ${handler_name}\b" "${ipc_file}"; then
    record_violation "${ipc_file}: expected exported handler ${handler_name}"
  fi
done < <(find "${root_dir}/src/main/ipc" -maxdepth 1 -type f -name '*.ts' | sort)

check_forbidden_regex() {
  local search_dir="$1"
  local pattern="$2"
  local label="$3"
  while IFS= read -r match; do
    record_violation "${match}: forbidden ${label}"
  done < <(grep -RInE "${pattern}" "${search_dir}" --include '*.ts' || true)
}

check_forbidden_regex "${root_dir}/src/main" 'Date[.]now[(]' "Date.now()"
check_forbidden_regex "${root_dir}/src/main" 'new[[:space:]]+Date[[:space:]]*[(]' "new Date()"
check_forbidden_regex "${root_dir}/src/preload" 'Date[.]now[(]' "Date.now()"
check_forbidden_regex "${root_dir}/src/preload" 'new[[:space:]]+Date[[:space:]]*[(]' "new Date()"

config_file="${root_dir}/electron.vite.config.ts"
if [[ ! -f "${config_file}" ]]; then
  record_violation "${config_file}: missing electron-vite config"
else
  for entry in main preload renderer; do
    if ! grep -Eq "${entry}:[[:space:]]*\\{" "${config_file}"; then
      record_violation "${config_file}: missing ${entry} build entry"
    fi
  done
fi

if [[ "${#violations[@]}" -gt 0 ]]; then
  printf 'Invariant check failed:\n' >&2
  for violation in "${violations[@]}"; do
    printf ' - %s\n' "${violation}" >&2
  done
  exit 1
fi
