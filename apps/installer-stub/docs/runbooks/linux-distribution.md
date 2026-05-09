# Linux Distribution

Linux artifacts are unsigned in the installer-stub pipeline. The release
produces AppImage and deb artifacts and publishes SHA-256 checksums in the draft
release notes.

## User Verification

Download the Linux artifact and `release-checksums.txt` from the same GitHub
release, then verify locally:

```bash
sha256sum --check release-checksums.txt
```

The checksum file is generated in the publish job from the exact assets that are
attached to the draft release.

## Auto-update Behavior

AppImage installs auto-update through electron-updater and `latest-linux.yml`.
Deb installs do not auto-update in v1; users who install the deb must download
and install a newer deb for each release. The v1 pipeline does not maintain an
apt repository.

## Future Options

If Linux users ask for stronger package provenance, add deb signing in a
separate release-hardening PR. That PR should provision a dedicated GPG key,
document key rotation, update the release workflow, and keep AppImage behavior
explicitly documented.
