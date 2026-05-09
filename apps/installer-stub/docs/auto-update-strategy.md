# Auto-update Strategy

The installer-stub v1 pipeline ships full-download updates only.

## macOS

macOS publishes `latest-mac.yml`, a notarized updater `.zip`, and a stapled
`.dmg`. The `.app` inside the updater `.zip` is stapled before packaging, and
the release workflow validates the extracted `.app` before upload.

## Windows

Windows publishes `latest.yml` and the signed NSIS installer. Differential
updates are disabled with `nsis.differentialPackage: false`, so no `.blockmap`
files are produced or uploaded in v1. This avoids stale blockmaps after Azure
Trusted Signing modifies installer bytes.

## Linux

Linux publishes `latest-linux.yml`, an AppImage, and a deb. AppImage installs
can use electron-updater. Deb installs are manual-update only in v1.

## Future Re-evaluation

Re-enable differential updates only if signing happens before blockmap
generation, or if the pipeline regenerates and uploads blockmaps after signing.
