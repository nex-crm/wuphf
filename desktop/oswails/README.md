# WUPHF desktop shell (Wails)

A single Go binary that boots the **existing** WUPHF broker + web UI
**in-process** (no sidecar) and attaches a native [Wails](https://wails.io) v2
window to it. The desktop app *is* the `wuphf` process with a window bolted on.

## Why Wails (and not Tauri/Electron/Pake)

`team.Launcher.LaunchWeb(port)` is non-blocking and returns a live loopback URL,
so a Go host can start the broker in-process and attach a window — deleting the
entire sidecar lifecycle (spawn / port-handshake / crash-restart / orphan-kill)
that a Rust (Tauri) or Node (Electron) host would have to build. Full rationale
and the cross-platform de-risk live in
[`docs/specs/desktop-pake-feasibility.md`](../../docs/specs/desktop-pake-feasibility.md).

## OS boundary

This is the **only** Go package allowed to import
`github.com/wailsapp/wails/v2/...` (enforced by depguard + `scripts/check-wails-boundary.sh`).
All app data stays on the existing HTTP/SSE/WebSocket loopback transport
(`internal/team/broker_web_proxy.go`). Wails is reserved for OS verbs only:
native notifications, tray, dock badge, deep-link, autostart, file pickers, and
the single-instance lock.

## How it works

1. `init()` calls `runtime.LockOSThread()` — Cocoa needs the run loop on the
   main thread; the broker boot (below) spawns goroutines that would otherwise
   migrate the main goroutine off it and the window would never appear.
2. `main()` picks a free loopback port and an OS app-data runtime home, then
   boots the broker in a goroutine: `NewLauncher("") → SetNoOpen(true) →
   PreflightWeb() → LaunchWeb(port)`.
3. The Wails window loads an embedded bootstrap page. `bootstrap.go`'s
   asset-server middleware templates the live port into it; the page polls the
   loopback origin and `location.replace`s to `http://127.0.0.1:<port>/` once
   the broker answers. Landing on a real http origin gives the SPA native
   SSE / WebSocket / WebAuthn — the `wails://` custom scheme cannot carry a
   WebSocket.

## Build

The shell is behind the `desktop` build tag so `go build ./...` / CI don't pull
in the Wails CGO webview deps (a non-tagged `stub.go` keeps the package valid).

```bash
# macOS / Windows
cd desktop/oswails && wails build -s -skipbindings -tags desktop

# Linux (Ubuntu 24.04 ships WebKitGTK 4.1; the production tag is what wails
# build injects — a plain `go build` without it makes Wails refuse to run)
cd desktop/oswails && wails build -s -skipbindings -tags "desktop webkit2_41"
```

`wails build` produces a GUI-subsystem binary (no console window). A plain
`GOOS=windows go build` would need `-ldflags -H=windowsgui`.

## Single-instance & attach

`SingleInstanceLock` ensures one desktop instance per machine — a second launch
focuses the running window instead of spawning a competing process.

**Shared office, one broker per workspace.** The shell opens the user's active
workspace (the same office an unqualified `wuphf web` uses). Before booting it
calls `team.RunningOfficeURL()`, which reads the `office.json` sidecar (written
next to `office.pid` with the running broker's web URL) and, if a loopback,
WUPHF-identity-verified peer is live, **attaches** the window to it instead of
booting a second broker. The CLI's `wuphf web` does the same — opens the running
office and exits rather than `killStaleBroker`-ing a peer. Clean shutdown clears
the sidecar; a stale one self-heals via the reachability probe. This makes one
broker per workspace the invariant, so the broker port is no longer a concern.

## Known follow-ups

- **Desktop+CLI simultaneous cold-start** has no lock between the attach-check
  and boot, so two front-ends starting within ~ms could both boot
  (`SingleInstanceLock` only covers desktop↔desktop). An advisory boot lock is
  the fix.
- **Identity probe** confirms a WUPHF office via a marker in the served `/`; a
  dedicated broker endpoint on the web port would be more robust.
- **Multi-workspace:** the desktop opens the default `~` office, not
  `cli_current`.
- **Cross-platform:** macOS + Linux WebKitGTK validated by hand; Windows
  WebView2 via `.github/workflows/desktop-webview-probe.yml`.
