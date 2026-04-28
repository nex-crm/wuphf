#!/usr/bin/env bash
# Build the Wuphf-Win11-ARM UTM VM from scratch.
#
# Idempotent-ish: if a VM with the same name already exists, errors out
# rather than clobbering it. Delete the old one first if you want to
# rebuild:
#   /Applications/UTM.app/Contents/MacOS/utmctl delete <UUID>
#
# Requires:
#   - UTM 4.6+ installed at /Applications/UTM.app
#   - A Windows 11 ARM64 ISO (downloadable via CrystalFetch)
#   - autounattend.img beside this script (run make-answer-disk.sh first)

set -euo pipefail

cd "$(dirname "$0")"

VM_NAME="${VM_NAME:-Wuphf-Win11-ARM}"
WIN_ISO="${WIN_ISO:-$HOME/Documents/windows/26100.4349.250607-1500.ge_release_svc_refresh_CLIENTCONSUMER_RET_A64FRE_en-us.iso}"
GUEST_TOOLS_ISO="${GUEST_TOOLS_ISO:-$HOME/Library/Containers/com.utmapp.UTM/Data/Library/Application Support/GuestSupportTools/utm-guest-tools-latest.iso}"
ANSWER_IMG="${ANSWER_IMG:-$(pwd)/autounattend.img}"
DISK_GB="${DISK_GB:-64}"
RAM_MB="${RAM_MB:-8192}"
CPU_CORES="${CPU_CORES:-4}"

UTMCTL=/Applications/UTM.app/Contents/MacOS/utmctl

for f in "${WIN_ISO}" "${GUEST_TOOLS_ISO}" "${ANSWER_IMG}"; do
  if [[ ! -f "${f}" ]]; then
    echo "missing required file: ${f}" >&2
    exit 1
  fi
done

# Bail if a VM with this name already exists. Forcing rebuilds via
# `utmctl delete` is intentionally a separate, explicit step.
existing="$("${UTMCTL}" list 2>/dev/null | awk -v name="${VM_NAME}" '$NF == name {print $1}')"
if [[ -n "${existing}" ]]; then
  echo "VM '${VM_NAME}' already exists (UUID ${existing}). Aborting." >&2
  echo "Delete it first if you want to rebuild:" >&2
  echo "  ${UTMCTL} delete ${existing}" >&2
  exit 1
fi

disk_mb=$(( DISK_GB * 1024 ))

uuid="$(osascript <<EOF 2>&1
tell application "UTM"
    set newVM to make new virtual machine with properties { ¬
        backend:qemu, ¬
        configuration:{ ¬
            name:"${VM_NAME}", ¬
            architecture:"aarch64", ¬
            memory:${RAM_MB}, ¬
            cpu cores:${CPU_CORES}, ¬
            hypervisor:true, ¬
            uefi:true, ¬
            directory share mode:VirtFS, ¬
            drives:{ ¬
                {interface:NVMe, guest size:${disk_mb}, raw:false}, ¬
                {interface:USB, source:(POSIX file "${WIN_ISO}")}, ¬
                {interface:USB, source:(POSIX file "${GUEST_TOOLS_ISO}")}, ¬
                {interface:USB, source:(POSIX file "${ANSWER_IMG}")} ¬
            }, ¬
            network interfaces:{ ¬
                {mode:shared} ¬
            }, ¬
            displays:{ ¬
                {hardware:"virtio-ramfb-gl", dynamic resolution:true} ¬
            } ¬
        } ¬
    }
    return id of newVM
end tell
EOF
)"

if [[ -z "${uuid}" || "${uuid}" == *"error"* ]]; then
  echo "failed to create VM: ${uuid}" >&2
  exit 1
fi

echo "created VM ${VM_NAME} (UUID ${uuid})"

# Patch the bundle: UTM's AppleScript bridge does not preserve the
# ImageType/ReadOnly fields needed to mount ISOs as actual CD-ROMs.
# Without this, Windows setup never sees a bootable disc.
plist="$HOME/Library/Containers/com.utmapp.UTM/Data/Documents/${VM_NAME}.utm/config.plist"
if [[ ! -f "${plist}" ]]; then
  echo "VM bundle not found at ${plist} — UTM may have used a different name" >&2
  exit 1
fi

/usr/libexec/PlistBuddy \
  -c "Set :Drive:1:ImageType CD" \
  -c "Set :Drive:1:ReadOnly true" \
  -c "Set :Drive:2:ImageType CD" \
  -c "Set :Drive:2:ReadOnly true" \
  -c "Set :QEMU:TPMDevice true" \
  "${plist}" >/dev/null

echo "bundle patched: CD-ROMs marked read-only, TPM 2.0 enabled"
echo
echo "Boot it with:"
echo "  ${UTMCTL} start ${uuid}"
echo
echo "Then wait for unattended install to finish (~15-20 min on M-series)."
