@echo -off
rem UEFI shell autostart. Probe FS0 first (the typical mapping for the
rem installer ISO), then fall back to FS1/FS2 in case UTM enumerates the
rem drives in a different order on a given run.
if exist fs0:\EFI\BOOT\BOOTAA64.EFI then
  echo Booting Windows ARM64 installer from FS0...
  fs0:
  cd \EFI\BOOT
  BOOTAA64.EFI
endif
if exist fs1:\EFI\BOOT\BOOTAA64.EFI then
  echo FS0 missing installer; booting from FS1...
  fs1:
  cd \EFI\BOOT
  BOOTAA64.EFI
endif
if exist fs2:\EFI\BOOT\BOOTAA64.EFI then
  echo FS0/FS1 missing installer; booting from FS2...
  fs2:
  cd \EFI\BOOT
  BOOTAA64.EFI
endif
echo Could not locate \EFI\BOOT\BOOTAA64.EFI on FS0/FS1/FS2.
echo Drop to UEFI shell so you can probe with `map -r` manually.
