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

- `electron`: `42.0.1`
- `electron-builder`: `25.1.8`
- `electron-updater`: `6.3.9`

The runtime was lifted from `33.0.0` to `42.0.1` in the desktop-shell PR
(`feat(deps): bump installer-stub electron to 42 + tar override for clean
audit`). Reasoning: Electron 33 reached end-of-life on 2025-04-29 and was
flagged by `bun audit` for four high-severity advisories (use-after-free in
offscreen child windows / WebContents permission callbacks / PowerMonitor;
renderer command-line switch injection). Bumping to `42.0.1` aligns the stub
with `apps/desktop`'s pin and eliminates those advisories from the workspace
lockfile.

PR #780 (issue #771) bumped the full electron stack:

- `electron-builder` 25.1.8 → 26.8.1
- `electron-updater` 6.3.9 → 6.8.3 (and moved into `dependencies` per
  the new APPROVED_RUNTIME_DEPS allowlist)
- Schema migration: top-level `win.publisherName` → `win.signtoolOptions.publisherName`
  (the v25 array form `win.publisherName: [foo]` is rejected outright in v26).
  CI workflow CLI override updated to `--config.win.signtoolOptions.publisherName=…`.

The previous deferral was driven by an attempt that broke `bun run
build:dry-run` on the schema change; #780 worked through the migration
in a dedicated PR and landed it cleanly with the new packaged-runtime-deps
gate verifying app.asar contents end-to-end.

The Windows-symlink follow-up tracked at #781 is separate (electron-builder
25 didn't follow bun symlinks on Windows; v26 does, which is what made #780
viable in the first place).

## Bump Procedure

1. Pick an Electron major that is currently supported on the official schedule.
2. Check electron-builder release notes for signing, notarization, artifact
   naming, NSIS, and updater metadata changes.
3. Check electron-updater release notes for Windows publisher verification,
   GitHub provider, and `latest*.yml` compatibility changes.
4. Update the three package versions in `apps/installer-stub/package.json`
   together.
5. Run `bun install` from repo root so `bun.lock` records the new versions.
6. If tag-to-package version normalization changes, update
   `apps/installer-stub/scripts/normalize-package-version.js`; the release
   workflow calls that shared script from every platform build job.
7. Run:

   ```bash
   (cd apps/installer-stub && bun run lint && bun run build:dry-run)
   bash apps/installer-stub/scripts/check-invariants.sh
   bash apps/installer-stub/scripts/verify-latest-yml.sh
   ```

8. Let PR CI build all three platform artifacts.
9. Before merge, run a draft-only signed rewrite smoke tag and verify:
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
