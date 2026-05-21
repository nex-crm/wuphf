# @wuphf/desktop

WUPHF v1 desktop shell. Electron 42 application boundary: main process +
sandboxed preload + React renderer + utility-process broker spawn.

This package is the **OS-level security boundary** for the rewrite. Everything app-related (receipts, projections, broker state, OAuth tokens) lives behind a separate process the renderer reaches over loopback HTTP. The shell only exposes OS verbs (open external URL, show file in folder, app version, broker liveness).

## Run it

```bash
bun install                    # at repo root, once
bun run desktop:dev            # boots Electron window
```

The window boots the React renderer shell. The first route shows broker
liveness, the app version, and the loopback broker URL once bootstrap has
completed. App data is fetched from the broker over loopback HTTP/SSE after
the renderer exchanges `getBrokerStatus().brokerUrl` for an API bearer at
`/api-token`.

Quit with `Cmd+Q` / `Ctrl+Q`. Broker shutdown is cooperative: the supervisor
sends a parentPort shutdown message, waits a 5s grace window, uses
`UtilityProcess.kill()` for handle-bound cleanup on POSIX, and uses
`taskkill /pid <pid> /T` with `/F` escalation on Windows.

## Build it

```bash
cd apps/desktop && bun run build
# Outputs to apps/desktop/out/{main,preload,renderer}
```

The packaged installer (.dmg / .exe / .AppImage) is produced by `feat/installer-pipeline`, not this package.

## Test it

```bash
cd apps/desktop && bun run test                # vitest
cd apps/desktop && bun run test:coverage       # one-way ratchet
cd apps/desktop && bun run check:ipc-allowlist # CI grep gate
cd apps/desktop && bun run check:invariants    # structural deny-list
bun audit --audit-level high                   # repo root workspace lockfile
```

Main-process logs are local only. The app writes rotated JSONL files under
Electron's standard logs directory (`app.getPath("logs")`) as `main.log`,
`main.1.log`, and `main.2.log`; nothing is uploaded.

## Architecture

```mermaid
flowchart LR
    Main["Electron main process<br/>BrowserWindow · lifecycle · broker spawn"]
    Broker["Broker utility process<br/>loopback HTTP/SSE listener"]
    Preload["Preload (sandbox)<br/>contextBridge: 5 OS verbs<br/>src/shared/api-contract.ts"]
    Renderer["Renderer (untrusted)<br/>React app shell + routes"]

    Main -->|utilityProcess.fork| Broker
    Broker -.->|liveness pings| Main
    Main -->|BrowserWindow webPreferences.preload| Preload
    Preload -->|contextBridge.exposeInMainWorld("wuphf")| Renderer
    Renderer -->|window.wuphf.<verb>()| Preload
    Preload -->|ipcRenderer.invoke| Main
    Renderer -->|/api-token · /api/* · /api/events| Broker
```

The renderer **never** touches `~/.wuphf/` or any file under it. Anything
app-data-shaped travels over loopback HTTP/SSE, not IPC.

## Read more

- [`AGENTS.md`](./AGENTS.md) — 16 hard rules every contributor (human or AI) must follow.
- [`docs/modules/preload.md`](./docs/modules/preload.md) — the contextBridge allowlist contract.
- [`docs/modules/broker-spawn.md`](./docs/modules/broker-spawn.md) — utility-process lifecycle, restart policy.
- [`docs/modules/security-model.md`](./docs/modules/security-model.md) — threat model, sandbox guarantees, what each layer trusts.
- [`docs/runbooks/electron-stack-maintenance.md`](./docs/runbooks/electron-stack-maintenance.md) — Electron/toolchain bump procedure.

## RFC anchors

Architecture: §7.1, §7.3. Branch: §15 row 8 (`feat/r1-renderer-foundation`).
