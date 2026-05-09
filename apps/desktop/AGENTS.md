# @wuphf/desktop — agent instructions

This package is the WUPHF v1 Electron shell. It is **the security boundary** between the operating system, the renderer (untrusted web content), and the broker (utility process holding app data + secrets). Every change here can break the moat. Read this file before editing.

For the repo-wide base rules (lint, secrets, branch + PR discipline) see the root `CLAUDE.md` / `AGENTS.md`. The rules below are **additional, non-negotiable** for this package.

---

## What this package is

- **Main process** (`src/main/`) — Electron entry. Owns BrowserWindow, app lifecycle, broker spawn via `utilityProcess.fork()`. Has full Node + Electron API access.
- **Preload** (`src/preload/`) — Runs in a sandboxed renderer-adjacent context. Exposes a **typed allowlist of OS verbs only** to the renderer via `contextBridge.exposeInMainWorld`. Never exposes app data, broker state, file paths, or `ipcRenderer` directly.
- **Renderer** (`src/renderer/`) — Untrusted web content. Treats every `window.*` API as the surface of attack. Reaches the broker over **loopback HTTP/SSE** (not IPC) — that wiring lands in `feat/broker-loopback-listener`; in this package the renderer just shows broker liveness.
- **Shared** (`src/shared/`) — The single source of truth for the contextBridge contract. Imported by preload (to expose) and by renderer (as the `window.<api>` type). Never touches Node APIs.

The broker — when it lands in `feat/broker-loopback-listener` — is a separate process. This package only spawns/kills it and reports liveness. The broker's protocol surface is `@wuphf/protocol`, which this package re-exports nothing from at runtime.

---

## Hard rules (every PR is checked against these)

1. **Sandbox is always on.** Every `BrowserWindow` `webPreferences` must include `sandbox: true`, `contextIsolation: true`, `nodeIntegration: false`, `webSecurity: true`. There is one helper (`createSecureWindow`) that constructs the config; **all `new BrowserWindow(...)` callsites go through it**. CI greps for raw `new BrowserWindow` in `src/main/` and fails.
2. **No `remote` module, no `@electron/remote`.** Both are forbidden. Communication crosses the process boundary only via the contextBridge allowlist or loopback HTTP.
3. **Preload exposes OS verbs only.** The contextBridge surface is the type defined in `src/shared/api-contract.ts`. Allowed verbs today: `openExternal(url)`, `showItemInFolder(path)`, `getAppVersion()`, `getPlatform()`, `getBrokerStatus()`. **No `readFile`, no `writeFile`, no `getReceipts`, no `getProjections`, no `getBrokerToken`** — those are app data and must travel over loopback HTTP, not IPC. CI greps for forbidden verbs in `src/preload/` and fails.
4. **The contextBridge allowlist is closed.** Adding a new IPC channel requires (a) a new entry in `src/shared/api-contract.ts`, (b) a new test in `tests/preload-allowlist.spec.ts` asserting it's exposed, (c) an `IpcAllowlistEntry` justification with a one-line "why this is an OS verb, not app data" comment. Reviewers reject any new entry that smells like app data.
5. **`ipcMain.handle` channels mirror the contract.** Every channel name in `src/main/ipc/` must appear in `src/shared/api-contract.ts`. Every channel handler validates its payload at the boundary using a runtime guard (no implicit trust of arguments). CI greps for `ipcMain.handle` calls and asserts the channel name is on the allowlist.
6. **No `nodeIntegration` flag is ever set true, anywhere.** Same for `contextIsolation: false`. CI greps for both literal patterns and fails.
7. **CSP is strict.** The renderer HTML ships with a `<meta http-equiv="Content-Security-Policy">` containing `default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self' http://127.0.0.1:* http://localhost:*; img-src 'self' data:; base-uri 'none'; form-action 'none'; object-src 'none'; frame-ancestors 'none'; worker-src 'none'`. No `'unsafe-inline'`, no `'unsafe-eval'`, no remote script sources. The connect-src exception for loopback is the only network egress, remains wildcarded only until `feat/broker-loopback-listener` defines the broker port, and must tighten to that port in the same branch.
8. **No remote URLs.** `BrowserWindow.loadURL` only accepts `file://` (production) or `http://localhost:<vite-dev-port>` (dev). Any other origin is a bug. The `will-navigate` and `setWindowOpenHandler` events are wired to deny any non-allowlisted target.
9. **Broker spawn uses `utilityProcess.fork()` only.** Not `child_process.spawn`, not `child_process.fork`. The utility process gets `serviceName: "wuphf-broker"`, `stdio: "pipe"`, no inherited env vars beyond the explicit allowlist. On `app.before-quit`, the broker gets a cooperative parentPort shutdown request with a 5s grace; POSIX cleanup uses Electron's handle-bound `UtilityProcess.kill()`, while Windows also runs `taskkill /pid <pid> /T` and escalates to `/F` after the grace window. Crash-restart uses exponential backoff where the first wait is 250ms (capped at 60s, max 5 consecutive retries before surfacing a fatal-error window).
10. **No app data IPC.** The broker owns `~/.wuphf/`. The renderer reaches it over loopback HTTP. The main process **never** reads from `~/.wuphf/` and never proxies app data through IPC. CI greps for `~/.wuphf` and `homedir()` in `src/main/` (and bans both outside an explicit allowlist file).
11. **Date APIs are forbidden in main + preload.** Same rule as `@wuphf/protocol`: no `Date.now()`, no `new Date()` in `src/main/` or `src/preload/`. Timestamps come from event payloads (the broker stamps them). Broker supervision timers use the monotonic clock helper (`src/main/monotonic-clock.ts`); no other `performance.now()` callsites are allowed.
12. **No telemetry, no analytics, no crash reporters that exfiltrate.** No `app.setAsDefaultProtocolClient` for arbitrary schemes. No `crashReporter.start({ uploadToServer: true })`. Local crash dumps are fine; uploading is forbidden in this branch.
13. **Strict TypeScript.** No `any`, no `// @ts-ignore`, no `// biome-ignore`. The same `noUncheckedIndexedAccess`, `exactOptionalPropertyTypes`, `verbatimModuleSyntax` rules from `@wuphf/protocol` apply. All three TS projects (main / preload / renderer) typecheck independently.
14. **Tests are the contract.** Every contextBridge verb has (a) an exposure test (it's on the allowlist), (b) a security test (it does not read app data), (c) a behavior test (it does what its name claims). Coverage is a one-way ratchet at the measured floor; lowering it requires the same justification as `@wuphf/protocol`.
15. **The README + module docs match the code.** `docs/modules/{preload,broker-spawn,security-model}.md` are authoritative reading order for new contributors. Adding a contextBridge verb requires updating `preload.md`. Changing the broker spawn lifecycle requires updating `broker-spawn.md`.
16. **Lefthook + CI gates are mandatory.** `bash scripts/check-ipc-allowlist.sh` runs pre-push and in CI. `bash scripts/check-invariants.sh` runs the structural deny-list. Neither may be skipped (`--no-verify` is forbidden per repo CLAUDE.md). If a gate is wrong, fix the gate in the same PR — never bypass.

---

## Verification commands

From repo root unless otherwise noted.

```bash
# Typecheck all three TS projects
cd apps/desktop && bun run typecheck

# Lint
cd apps/desktop && bun run lint

# Unit + property tests
cd apps/desktop && bun run test

# Coverage (one-way ratchet)
cd apps/desktop && bun run test:coverage

# Structural invariants (IPC allowlist + main-process app-data ban)
cd apps/desktop && bun run check:ipc-allowlist
cd apps/desktop && bun run check:invariants

# Demo: opens the Electron window the user can click through
bun run desktop:dev
```

The demo is the verification deliverable — `bun run desktop:dev` must boot a window showing broker liveness + a click-to-test allowlisted IPC button. If you change the demo, capture a screenshot and attach it to the PR.

---

## What this package is NOT for

- **Not for the broker itself.** The broker process implementation lives in a future package (`@wuphf/broker` or `apps/broker`). This package only spawns/kills it.
- **Not for the renderer UI.** The full UI lands later — when it does, it imports from `web/` (the existing Vite app) or a new `apps/desktop-renderer/` and is loaded into the BrowserWindow this package owns. Until then, the renderer is a one-pane status view.
- **Not for auto-update.** Auto-update wiring lives in `feat/installer-pipeline` (Sparkle for Mac, electron-updater for Win). This package only knows about its own version string.
- **Not for code-signing.** Same — that's installer-pipeline.

If a change touches any of the above, it does not belong in this package.

---

## RFC anchors

Architecture: §7.1 (process topology), §7.3 (IPC discipline). Branch position: §15 row 2 (`feat/desktop-shell-skeleton`, week 0–2). Trust substrate this is protecting: §6 (receipt schema, audit chain). The renderer's loopback contract: §15 row 4 (`feat/broker-loopback-listener`).
