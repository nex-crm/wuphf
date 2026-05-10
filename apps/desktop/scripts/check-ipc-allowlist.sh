#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
contract_file="${root_dir}/src/shared/api-contract.ts"
violations=()

allowed_channels=()
channel_member_names=()
channel_member_values=()

trim() {
  local value="$1"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "${value}"
}

record_violation() {
  violations+=("$1")
}

is_allowed_channel() {
  local candidate="$1"
  local channel=""
  for channel in "${allowed_channels[@]}"; do
    if [[ "${channel}" == "${candidate}" ]]; then
      return 0
    fi
  done
  return 1
}

resolve_channel_member() {
  local candidate="$1"
  local index=0
  while [[ "${index}" -lt "${#channel_member_names[@]}" ]]; do
    if [[ "${channel_member_names[${index}]}" == "${candidate}" ]]; then
      printf '%s' "${channel_member_values[${index}]}"
      return 0
    fi
    index=$((index + 1))
  done
  return 1
}

while IFS= read -r line; do
  if [[ "${line}" =~ ^[[:space:]]*([A-Za-z0-9_]+):[[:space:]]*\"([^\"]+)\" ]]; then
    member="${BASH_REMATCH[1]}"
    channel="${BASH_REMATCH[2]}"
    allowed_channels+=("${channel}")
    channel_member_names+=("${member}")
    channel_member_values+=("${channel}")
  fi
done < "${contract_file}"

if [[ "${#allowed_channels[@]}" -eq 0 ]]; then
  record_violation "No IPC channels found in ${contract_file}"
fi

while IFS= read -r match; do
  location="${match%%:ipcMain.handle(*}"
  argument="${match#*ipcMain.handle(}"
  argument="${argument%%,*}"
  argument="$(trim "${argument}")"
  channel=""

  if [[ "${argument}" =~ ^IpcChannel\.([A-Za-z0-9_]+)$ ]]; then
    member="${BASH_REMATCH[1]}"
    channel="$(resolve_channel_member "${member}" || true)"
  elif [[ "${argument}" =~ ^\"([^\"]+)\"$ ]]; then
    channel="${BASH_REMATCH[1]}"
  elif [[ "${argument}" =~ ^\'([^\']+)\'$ ]]; then
    channel="${BASH_REMATCH[1]}"
  fi

  if [[ -z "${channel}" ]]; then
    record_violation "${location}: unable to resolve ipcMain.handle channel argument: ${argument}"
  elif ! is_allowed_channel "${channel}"; then
    record_violation "${location}: ipcMain.handle channel is not allowlisted: ${channel}"
  fi
done < <(grep -RInE 'ipcMain[.]handle[(]' "${root_dir}/src/main" --include '*.ts' || true)

while IFS= read -r match; do
  location="${match%%:contextBridge.exposeInMainWorld(*}"
  argument="${match#*contextBridge.exposeInMainWorld(}"
  argument="${argument%%,*}"
  argument="$(trim "${argument}")"

  if [[ "${argument}" != "WUPHF_GLOBAL_KEY" && "${argument}" != "\"wuphf\"" && "${argument}" != "'wuphf'" ]]; then
    record_violation "${location}: contextBridge exposes non-allowlisted global: ${argument}"
  fi
done < <(grep -RInE 'contextBridge[.]exposeInMainWorld[(]' "${root_dir}/src/preload" --include '*.ts' || true)

check_forbidden_regex() {
  local search_dir="$1"
  local pattern="$2"
  local label="$3"
  while IFS= read -r match; do
    record_violation "${match}: forbidden ${label}"
  done < <(grep -RInE "${pattern}" "${search_dir}" --include '*.ts' || true)
}

check_forbidden_fixed() {
  local search_dir="$1"
  local pattern="$2"
  local label="$3"
  while IFS= read -r match; do
    record_violation "${match}: forbidden ${label}"
  done < <(grep -RInF "${pattern}" "${search_dir}" --include '*.ts' || true)
}

tilde_character='~'
app_data_path_pattern="${tilde_character}/.wuphf"
check_forbidden_fixed "${root_dir}/src/main" "${app_data_path_pattern}" "main-process app data access"
check_forbidden_regex "${root_dir}/src/main" '(^|[^A-Za-z0-9_])homedir[(]' "homedir access"
check_forbidden_fixed "${root_dir}/src/main" "os.homedir(" "homedir access"
check_forbidden_fixed "${root_dir}/src/main" "child_process.spawn" "child_process spawn"
check_forbidden_fixed "${root_dir}/src/main" "child_process.fork" "child_process fork"
check_forbidden_fixed "${root_dir}/src/main" "@electron/remote" "remote module"
check_forbidden_regex "${root_dir}/src/main" 'nodeIntegration:[[:space:]]*true' "nodeIntegration true"
check_forbidden_regex "${root_dir}/src/main" 'contextIsolation:[[:space:]]*false' "contextIsolation false"

while IFS= read -r match; do
  file_path="${match%%:*}"
  if [[ "${file_path}" != "${root_dir}/src/main/window.ts" ]]; then
    record_violation "${match}: raw BrowserWindow construction must go through createSecureWindow"
  fi
done < <(grep -RInF "new BrowserWindow" "${root_dir}/src/main" --include '*.ts' || true)

if [[ "${#violations[@]}" -gt 0 ]]; then
  printf 'IPC allowlist check failed:\n' >&2
  for violation in "${violations[@]}"; do
    printf ' - %s\n' "${violation}" >&2
  done
  exit 1
fi
