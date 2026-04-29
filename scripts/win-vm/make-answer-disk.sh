#!/usr/bin/env bash
# Rebuild autounattend.img from autounattend.xml.
# Run after editing the answer file.

set -euo pipefail

cd "$(dirname "$0")"

if [[ ! -f autounattend.xml ]]; then
  echo "missing autounattend.xml" >&2
  exit 1
fi

src="$(mktemp -d)"
trap 'rm -rf "$src"' EXIT
cp autounattend.xml "$src/"
# startup.nsh is the UEFI shell autostart — it kicks the Windows ISO
# bootloader so the VM boots without any keypress.
[[ -f startup.nsh ]] && cp startup.nsh "$src/"
# Bundle VirtIO drivers so WinPE setup can load viostor/etc from C:\Drivers
# without depending on whether the UTM tools ISO got mounted.
[[ -d answer-src/Drivers ]] && cp -R answer-src/Drivers "$src/Drivers"

rm -f autounattend.img answer.cdr autounattend.iso

# ISO 9660 (with Joliet for long names). FAT was breaking driver signature
# verification — Windows setup checks .cat against .sys via NTFS-style
# metadata that FAT can't preserve, and emits "Error scanning for drivers"
# on otherwise-valid driver folders. ISO 9660 preserves what's needed.
hdiutil makehybrid -iso -joliet -default-volume-name WINSETUP -o autounattend.iso "$src" >/dev/null
mv autounattend.iso autounattend.img

echo "wrote autounattend.img ($(du -h autounattend.img | cut -f1))"
file autounattend.img
