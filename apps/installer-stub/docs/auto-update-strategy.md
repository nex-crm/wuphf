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

## v1 Caveats

The v1 installer-stub update path has no server-side kill switch, revocation
feed, staged rollout, or channel promotion. If a published release is bad, the
primary technical recovery is a higher-version hotfix release.

Deleting a GitHub Release is only a containment attempt. It does not remove
cached `latest*.yml` responses or payloads from clients that already checked for
or downloaded the update. The app therefore keeps `autoDownload` and
`autoInstallOnAppQuit` disabled in v1: users must explicitly download, then
explicitly click "Restart and install."

When `beta` and `stable` channels land, use channel promotion or staged rollout
metadata to limit initial blast radius before moving a release to all users.

## Future Re-evaluation

Re-enable differential updates only if signing happens before blockmap
generation, or if the pipeline regenerates and uploads blockmaps after signing.
