# Electron Stack Maintenance

The desktop shell is a security boundary, so Electron and its build stack are
kept on supported lines with exact pins. Dependabot groups the stack weekly;
reviewers should treat each grouped bump as a runtime compatibility change, not
just a lockfile refresh.

## Current Target

As of 2026-05-09, `bun pm view electron version` reports `42.0.1`. This package
targets Electron `42.0.1` with exact pins for the direct desktop toolchain:

- `electron`
- `electron-vite`
- `vite`
- `vitest`
- `@vitest/coverage-v8`
- `@types/node`
- `typescript`

Keep Electron on one of the currently supported stable majors. Electron supports
the latest three stable majors; do not hold an EOL major without a dated
follow-up issue and a concrete blocker.

## Upgrade Procedure

1. Read the Electron release notes for every major crossed. Catalog breaking
   changes that touch:
   - `utilityProcess.fork`
   - `BrowserWindow` defaults and `webPreferences`
   - sandbox, context isolation, preload, and CSP behavior
   - renderer process crash and child process lifecycle events
2. Read electron-vite, Vite, and Vitest release notes for the target group.
   Keep peer ranges compatible; do not force a Vite major outside
   electron-vite's declared peer range.
3. Update `apps/desktop/package.json` with exact versions, then run
   `bun install` from the repo root to refresh `bun.lock`. CI and final
   verification use `bun install --frozen-lockfile` after the lockfile is
   committed.
4. Run the local gates:

   ```bash
   cd apps/desktop && bun run typecheck
   cd apps/desktop && bun run lint
   cd apps/desktop && bun run test
   cd apps/desktop && bun run test:coverage
   bun audit --audit-level high
   cd apps/desktop && bun run check:ipc-allowlist
   cd apps/desktop && bun run check:invariants
   cd apps/desktop && bun run build
   ```

5. Smoke the real Electron runtime:

   ```bash
   bun run desktop:dev
   ```

   Verify the window loads, the allowlisted IPC button works, broker status
   reaches `alive`, quitting shuts the broker down, and the local Electron logs
   directory contains `main.log`.
6. In the commit body or PR notes, list the Electron majors crossed and the
   release-note items checked. If a breaking change is irrelevant because the
   package does not use that API, say so directly.

## Failure Policy

If the latest supported Electron major fails a real runtime smoke because of a
confirmed upstream or tooling incompatibility, pin the newest supported major
that passes all gates. Add a dated issue for the blocked major with the failing
command, the exact error, and the release-note item being investigated.
