# Electron Stack Maintenance

Last updated: 2026-05-09 / Owner: @FranDias

`electron`, `electron-builder`, and `electron-updater` are a release stack. Bump
them together unless this runbook records a specific reason to split the change.

Official reference:

- Electron release schedule: <https://releases.electronjs.org/schedule>
- Electron support policy: <https://www.electronjs.org/docs/latest/tutorial/electron-timelines>

## Current State

As of 2026-05-09:

- Latest stable Electron: `42.x` (`42.0.0`, May 2026)
- Supported Electron window: latest 3 stable majors, not a traditional LTS
  branch (currently `42.x`, `41.x`, and `40.x`)
- Currently pinned installer stub stack:

- `electron`: `33.0.0`
- `electron-builder`: `25.1.8`
- `electron-updater`: `6.3.9`

Electron's official schedule lists Electron 33 end of life as 2025-04-29. Do not
bump the runtime in this PR; use this runbook for the dedicated stack upgrade.
Action: bump the pinned stack to the latest stable supported Electron major
(currently `42.x`) in a follow-up PR before enabling signed production release
traffic from this installer stub.

## Bump Procedure

1. Pick an Electron major that is currently supported on the official schedule.
2. Check electron-builder release notes for signing, notarization, artifact
   naming, NSIS, and updater metadata changes.
3. Check electron-updater release notes for Windows publisher verification,
   GitHub provider, and `latest*.yml` compatibility changes.
4. Update the three package versions in `apps/installer-stub/package.json`
   together.
5. Run `bun install` from repo root so `bun.lock` records the new versions.
6. Run:

   ```bash
   (cd apps/installer-stub && bun run lint && bun run build:dry-run)
   bash apps/installer-stub/scripts/check-invariants.sh
   bash apps/installer-stub/scripts/verify-latest-yml.sh
   ```

7. Let PR CI build all three platform artifacts.
8. Before merge, run a draft-only signed rewrite smoke tag and verify:
   - macOS codesign/notary/staple still pass
   - Windows Azure signing and signer CN assertion still pass
   - Linux AppImage/deb artifacts launch
   - all three updater manifests verify after final signing/stapling

## Automation

Dependabot is configured for `/apps/installer-stub` and groups `electron`,
`electron-builder`, and `electron-updater` into one update group. Dependabot
updates `package.json`; the PR owner must run `bun install` and commit the
matching `bun.lock` changes before merge.

If Renovate replaces Dependabot later, carry forward the same group rule for the
three package names and keep Bun lockfile updates in the same PR.
