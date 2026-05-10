# Linux Distribution

Last updated: 2026-05-09 / Owner: @FranDias

Linux artifacts are unsigned in the installer-stub pipeline. The release
produces AppImage and deb artifacts and publishes SHA-256 checksums in the draft
release notes.

## User Verification

Download the Linux artifact and `release-checksums.txt` from the same GitHub
release into one directory, then verify locally:

```bash
sha256sum --ignore-missing --check release-checksums.txt
```

The checksum file is generated from the exact release assets after publish
staging, with basename-relative entries such as:

```text
wuphf-installer-stub-0.0.0-linux-x64.AppImage
wuphf-installer-stub-0.0.0-linux-x64.deb
latest-linux.yml
```

To verify one downloaded AppImage manually:

```bash
grep 'wuphf-installer-stub-.*-linux-.*\.AppImage' release-checksums.txt
sha256sum wuphf-installer-stub-*-linux-*.AppImage
```

The two SHA-256 values must match.

## AppImage Install

```bash
chmod +x wuphf-installer-stub-*-linux-*.AppImage
./wuphf-installer-stub-*-linux-*.AppImage
```

The first-launch smoke test is one visible window showing
`WUPHF installer-stub v<version>`.

## deb Install

```bash
sudo dpkg -i wuphf-installer-stub-*-linux-*.deb
sudo apt-get install -f
wuphf-installer-stub
```

If dependencies were missing, `apt-get install -f` installs them and completes
the package configuration.

## Auto-update Behavior

AppImage installs auto-update through electron-updater and `latest-linux.yml`.
Deb installs do not auto-update in v1; users who install the deb must download
and install a newer deb for each release. The v1 pipeline does not maintain an
apt repository.

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `sha256sum --check` reports missing macOS/Windows files | Old checksum file contained paths for every platform | Download the current `release-checksums.txt`; entries should be basename-relative and `--ignore-missing` should verify only downloaded files |
| AppImage will not execute | Missing executable bit | Run `chmod +x wuphf-installer-stub-*-linux-*.AppImage` |
| AppImage asks about desktop integration | AppImageLauncher or distro integration prompt | Either accept integration or run the AppImage directly; both are acceptable for v1 |
| AppImage fails because FUSE is unavailable | Distro does not have AppImage FUSE support installed | Install the distro's FUSE/AppImage support package or extract the AppImage for local testing |
| deb install reports missing dependencies | `dpkg` does not resolve dependencies automatically | Run `sudo apt-get install -f` or `sudo apt --fix-broken install` |
| Electron window fails under Wayland | Distro/Electron graphics compatibility issue | Retry under X11 or launch with the distro's documented Wayland fallback flags |

## Future Options

If Linux users ask for stronger package provenance, add deb signing in a
separate release-hardening PR. That PR should provision a dedicated GPG key,
document key rotation, update the release workflow, and keep AppImage behavior
explicitly documented.
