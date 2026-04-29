#!/usr/bin/env bash
# One-time SSH setup: install the host's public key into the VM's
# administrators_authorized_keys so push-and-test.sh runs passwordless.
#
# Run once after the VM finishes its first boot AND OpenSSH is reachable.
# Until then the autologin user is wuphf with a blank password — sshd is
# enabled by autounattend.xml but the service may take ~30s after first
# desktop paint to actually accept connections.

set -euo pipefail

VM_UUID="${WUPHF_VM_UUID:-BD57713D-0B79-4B87-AD08-88A9BF7922CC}"
VM_USER="${WUPHF_VM_USER:-wuphf}"
UTMCTL=/Applications/UTM.app/Contents/MacOS/utmctl

# Pick the user's first available SSH public key.
pubkey_file=""
for candidate in "$HOME/.ssh/id_ed25519.pub" "$HOME/.ssh/id_rsa.pub"; do
  if [[ -f "${candidate}" ]]; then
    pubkey_file="${candidate}"
    break
  fi
done
if [[ -z "${pubkey_file}" ]]; then
  echo "no SSH public key found at ~/.ssh/id_ed25519.pub or id_rsa.pub" >&2
  echo "generate one first:  ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519" >&2
  exit 1
fi
pubkey="$(cat "${pubkey_file}")"

# Resolve the VM's IP. utmctl ip-address requires the QEMU/SPICE guest
# agent to be running inside the VM. The UTM guest tools ISO is already
# attached as a CD-ROM so the user can install them post-OOBE.
if [[ -z "${WUPHF_VM_HOST:-}" ]]; then
  ip="$("${UTMCTL}" ip-address "${VM_UUID}" 2>/dev/null | grep -E '^[0-9]+\.' | grep -v '^169\.254' | head -1 || true)"
  if [[ -z "${ip}" ]]; then
    cat >&2 <<EOF
Could not resolve the VM IP via utmctl. The QEMU guest agent isn't
running yet — install the UTM guest tools (the second CD-ROM) inside
Windows, reboot, and try again. Or set WUPHF_VM_HOST=<ip> manually
(check inside Windows: Settings → Network or 'ipconfig').
EOF
    exit 1
  fi
  WUPHF_VM_HOST="${ip}"
fi

cat <<EOF
About to install this public key into ${VM_USER}@${WUPHF_VM_HOST}:

  ${pubkey_file}

Windows will prompt for the ${VM_USER} password — enter the wuphf account password.
EOF

read -r -p "Continue? [y/N] " confirm
[[ "${confirm}" == "y" || "${confirm}" == "Y" ]] || exit 1

# Windows OpenSSH server reads admin keys from a system-wide path
# (NOT the user's ~/.ssh) when the account is in the Administrators group.
# That's our case for `wuphf`. We pipe a small PowerShell program over
# stdin so we don't have to escape the pubkey through nested quoting.
ssh -o StrictHostKeyChecking=accept-new \
    -o UserKnownHostsFile="$HOME/.ssh/known_hosts.wuphf-vm" \
    "${VM_USER}@${WUPHF_VM_HOST}" \
    'powershell -NoProfile -ExecutionPolicy Bypass -Command -' <<EOF
\$path = 'C:\ProgramData\ssh\administrators_authorized_keys'
New-Item -ItemType Directory -Force -Path 'C:\ProgramData\ssh' | Out-Null
@'
${pubkey}
'@ | Set-Content -Path \$path -Encoding ascii
icacls \$path /inheritance:r /grant 'Administrators:F' /grant 'SYSTEM:F' | Out-Null
Restart-Service sshd
'installed'
EOF

echo
echo "key installed. verify with:"
echo "  ssh ${VM_USER}@${WUPHF_VM_HOST} hostname"
