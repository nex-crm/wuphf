# Electron Stack Maintenance

Last updated: 2026-05-09 / Owner: @FranDias

`electron`, `electron-builder`, and `electron-updater` are a release stack. Bump
them together unless this runbook records a specific reason to split the change.

Official reference:

- Electron release schedule: <https://releases.electronjs.org/schedule>
- Electron support policy: <https://www.electronjs.org/docs/latest/tutorial/electron-timelines>

## Current State

As of 2026-05-10 (post-PR #780):

- Latest stable Electron: `42.x` (`42.0.0`, May 2026)
- Supported Electron window: latest 3 stable majors, not a traditional LTS
  branch (currently `42.x`, `41.x`, and `40.x`)
- Currently pinned installer stub stack:

- `electron`: `42.0.1`
- `electron-builder`: `26.8.1`
- `electron-updater`: `6.8.3` (declared in `dependencies`, allowlisted by
  `wuphfRuntimeDependenciesAllowlist`)

The runtime was lifted from `33.0.0` to `42.0.1` in the desktop-shell PR
(`feat(deps): bump installer-stub electron to 42 + tar override for clean
audit`). Reasoning: Electron 33 reached end-of-life on 2025-04-29 and was
flagged by `bun audit` for four high-severity advisories (use-after-free in
offscreen child windows / WebContents permission callbacks / PowerMonitor;
renderer command-line switch injection). Bumping to `42.0.1` aligns the stub
with `apps/desktop`'s pin and eliminates those advisories from the workspace
lockfile.

PR #780 (issue #771) bumped the full electron stack and migrated the v26
schema:

- `electron-builder` 25.1.8 → 26.8.1
- `electron-updater` 6.3.9 → 6.8.3 (moved into `dependencies` per the new
  APPROVED_RUNTIME_DEPS allowlist)
- Schema migration: top-level `win.publisherName` → `win.signtoolOptions.publisherName`
  (the v25 array form `win.publisherName: [foo]` is rejected outright in v26).
  CI workflow CLI override updated to `--config.win.signtoolOptions.publisherName=…`.

## Windows packaging gap (release-blocking) — issue #781

The `check-packaged-runtime-deps.js` post-build gate fails on **Windows**
even on electron-builder 26: bun's per-workspace symlinks are created
on Windows, but electron-builder's app-builder doesn't follow them, so
`electron-updater` plus its 9-entry transitive closure is pruned out of
`app.asar`. Linux + macOS bundle correctly.

`release-rewrite.yml` resolves this by gating the Windows gate's
`continue-on-error` on build mode:

- **PR builds**: gate is `continue-on-error: true` (diagnostic only).
  Windows pipeline runs to completion + uploads an installer with the
  known pruning gap; downstream consumers can still pull artifacts.
- **Production tag releases**: gate is `continue-on-error: false`.
  A failing gate hard-fails `build-win`, blocking the publish job from
  uploading a Windows installer that would crash on first launch.

Until #781 closes (likely path: a bun config to materialize prod-dep
real files on Windows, or upstream electron-builder collector fix),
**signed Windows release tags will not publish.** That is intentional —
shipping a notarized installer that crashes on launch is worse than
shipping no Windows installer.

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
