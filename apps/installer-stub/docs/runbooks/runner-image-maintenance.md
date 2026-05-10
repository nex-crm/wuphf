# Runner Image Maintenance

Last updated: 2026-05-09 / Owner: @FranDias

Release runner labels are part of the signing surface. Treat image changes like
release-infrastructure changes, not routine CI cleanup.

Official references:

- GitHub runner images repository: <https://github.com/actions/runner-images>
- Runner image releases/announcements:
  <https://github.com/actions/runner-images/releases>

## Current Labels

| Job | Current label | Review by | Notes |
|---|---|---|---|
| `build-mac` | `macos-15` | 2026-12-01 | Bumped from `macos-14` in PR #780: dmg-builder@1.2.0 + hdiutil on the macos-14 image consistently failed with `Device not configured` even after multiple workflow reruns. macos-15 (Sequoia) ships a newer diskarbitrationd that handles concurrent dmgbuild calls cleanly |
| `build-win` | `windows-2022` | 2026-09-01 | Azure Trusted Signing action and PowerShell Authenticode check run here |
| `build-linux` | `ubuntu-24.04` | 2026-09-01 | AppImage/deb build and manifest generation |
| `publish` / `detect-secrets` | `ubuntu-24.04` | 2026-09-01 | GitHub release asset verification and checksum generation |

## Upgrade Procedure

1. Read the runner-images announcement for the retiring label and the successor
   label.
2. Create a branch that changes only the relevant `runs-on` label and inline
   review-by comment in `.github/workflows/release-rewrite.yml`.
3. Run normal PR checks and confirm unsigned artifacts still build.
4. For macOS image changes, push a draft-only rewrite smoke tag and verify:
   - codesign uses the expected Developer ID identity
   - notarytool submission succeeds
   - `.app` and `.dmg` stapling validate
   - `latest-mac.yml` sha512 matches the final updater `.zip`
5. For Windows image changes, verify Azure signing still finds the Trusted
   Signing tooling, Authenticode status is `Valid`, and signer CN matches
   `AZURE_EXPECTED_PUBLISHER_NAME`.
6. For Linux image changes, install the produced AppImage/deb on a clean Linux
   machine or VM and run the first-launch smoke test.
7. Update `docs/CALENDAR.md` with the next review date.

Do not wait until GitHub brownouts begin. Move the macOS label before the first
announced deprecation date so release-day fixes are not coupled to Xcode,
notarytool, or stapler behavior changes.
