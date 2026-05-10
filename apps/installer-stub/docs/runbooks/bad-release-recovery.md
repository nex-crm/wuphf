# Bad Release Recovery

Last updated: 2026-05-09 / Owner: @FranDias

Use this runbook after a rewrite release has been published and users may have
seen the installer assets or `latest*.yml` manifests.

## Triage

1. Assign one incident owner and stop any manual publication or tag
   re-issuance for the affected version.
2. Record the version, platform, install type, first bad symptom, and whether
   the app can still launch.
3. Classify severity:
   - Critical: launch crash, data loss, security regression, or signed malware
     concern.
   - High: updater cannot recover, install fails on a primary platform, or a
     large user group is blocked.
   - Medium: degraded feature with a manual workaround.
   - Low: cosmetic or documentation issue.
4. Preserve release evidence before editing anything:

   ```bash
   gh release view "$BAD_TAG" --json tagName,isDraft,isPrerelease,assets,url
   ```

## Hotfix Flow

The reliable recovery path is fix-forward with a higher version, for example
`v0.0.3-rewrite` after a bad `v0.0.2-rewrite`.

1. Revert or patch the bug on `feat/installer-pipeline`.
2. Run the installer gates locally.
3. Create and push a new higher rewrite tag.
4. Approve the `production-release` environment gates.
5. Wait for signed macOS, signed Windows, Linux, manifest verification, and the
   draft release upload to pass.
6. Manually install the new artifacts on affected platforms.
7. Publish the hotfix release.
8. Send emergency comms with the fixed version, affected versions, platform
   notes, and manual reinstall instructions for users whose app cannot launch.

Users whose app still launches can use the in-app update controls after the
hotfix is published. Users whose app crashes before the update UI loads need a
manual installer download.

## Yank Attempt

Deleting the bad GitHub Release is only a containment attempt. It does not
remove cached `latest*.yml` responses from GitHub, proxies, local updater
caches, or clients that already polled. Clients that already saw or downloaded
the bad version may still install it.

Use this only when the incident owner accepts that limitation:

1. Confirm the replacement hotfix is already in progress.
2. Delete the GitHub Release for the bad tag.
3. Update emergency comms to say the bad release was pulled from the Releases
   page, but users who already checked for updates may still need the hotfix or
   manual reinstall.

Do not delete or move the git tag for a published release as a rollback. Use a
higher version tag.

## Downloaded But Not Installed

The app sets `autoUpdater.autoDownload = false` and
`autoUpdater.autoInstallOnAppQuit = false`. That gives ops a recovery window:
after a user downloads an update, quitting the app does not silently install it.
The bad update installs only if the user explicitly clicks "Restart and
install."

Containment message for this group:

1. Do not click "Restart and install" for the bad version.
2. Keep the app open or quit normally; quit will not auto-install the downloaded
   update in v1.
3. Wait for the higher-version hotfix, then check for updates again.

This is not a full kill switch. There is no server-side advisory in v1 that can
revoke a payload already downloaded to disk.

## Worst Case Manual Recovery

If the hotfix cannot help because the app cannot launch, tell users to manually
download and install the last known-good release from GitHub Releases. For the
first rewrite line, that is `v0.0.1-rewrite`.

1. Open the WUPHF Releases page.
2. Download the installer for the user's platform from `v0.0.1-rewrite`.
3. Install over the bad version.
4. After the hotfix is published, update to the new higher version.

## Future Channels

When `beta` and `stable` channels land, publish first to `beta`, observe, then
promote to `stable`. Channel promotion or staged rollout can limit blast radius,
but recovery from users already on a bad version still requires a higher-version
fixed release.
