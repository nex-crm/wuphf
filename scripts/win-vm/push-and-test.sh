#!/usr/bin/env bash
# Push the freshly-built wuphf.exe into the running Win11 VM and run
# smoke tests over SSH. Reports each test pass/fail with full stderr.
#
# Prereqs:
#   1. VM is running and reachable (utmctl status reports "started")
#   2. SSH server is up inside the VM (autounattend.xml installs it on
#      first boot — give Windows ~30s after the desktop shows before the
#      service is actually listening)
#   3. SSH key is set up host-side (see scripts/win-vm/setup-ssh.sh)
#
# Override the target via env:
#   WUPHF_VM_HOST=<ip>     skip utmctl ip-address probing
#   WUPHF_VM_USER=wuphf
#   WUPHF_VM_BIN=dist/windows/arm64/wuphf.exe

set -euo pipefail

cd "$(dirname "$0")/../.."

VM_UUID="${WUPHF_VM_UUID:-BD57713D-0B79-4B87-AD08-88A9BF7922CC}"
VM_USER="${WUPHF_VM_USER:-wuphf}"
VM_BIN="${WUPHF_VM_BIN:-dist/windows/arm64/wuphf.exe}"
UTMCTL=/Applications/UTM.app/Contents/MacOS/utmctl

if [[ ! -f "${VM_BIN}" ]]; then
  echo "no binary at ${VM_BIN} — run scripts/win-vm/build.sh first" >&2
  exit 1
fi

if [[ -z "${WUPHF_VM_HOST:-}" ]]; then
  status="$("${UTMCTL}" status "${VM_UUID}" 2>&1 || true)"
  if [[ "${status}" != *"started"* ]]; then
    echo "VM ${VM_UUID} is not running (status: ${status})" >&2
    echo "boot it with: ${UTMCTL} start ${VM_UUID}" >&2
    exit 1
  fi
  ip="$("${UTMCTL}" ip-address "${VM_UUID}" 2>/dev/null | grep -E '^[0-9]+\.' | grep -v '^169\.254' | head -1 || true)"
  if [[ -z "${ip}" ]]; then
    echo "could not determine VM IP via utmctl. set WUPHF_VM_HOST=<ip> manually." >&2
    exit 1
  fi
  WUPHF_VM_HOST="${ip}"
fi

echo "VM host: ${WUPHF_VM_HOST}"
echo "binary:  ${VM_BIN}"
echo

ssh_target="${VM_USER}@${WUPHF_VM_HOST}"
ssh_opts=(-o StrictHostKeyChecking=accept-new -o UserKnownHostsFile="$HOME/.ssh/known_hosts.wuphf-vm" -o ConnectTimeout=5)

remote_path='C:\wuphf\wuphf.exe'

echo "--- pushing binary ---"
scp "${ssh_opts[@]}" "${VM_BIN}" "${ssh_target}:${remote_path//\\//}"
echo

run() {
  local label="$1"
  shift
  echo "--- ${label} ---"
  if ssh "${ssh_opts[@]}" "${ssh_target}" "$@"; then
    echo "[PASS] ${label}"
  else
    rc=$?
    echo "[FAIL] ${label} (exit ${rc})"
    return ${rc}
  fi
  echo
}

set +e
fails=0
run "wuphf.exe --version"   "${remote_path} --version" || ((fails++))
run "wuphf.exe --help"      "${remote_path} --help"    || ((fails++))
run "wuphf.exe doctor"      "${remote_path} doctor"    || ((fails++))

echo
if (( fails == 0 )); then
  echo "all smoke tests passed"
else
  echo "${fails} smoke test(s) failed" >&2
  exit 1
fi
