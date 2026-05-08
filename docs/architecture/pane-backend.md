# Pane Backend

## Summary

The pane code is not the WUPHF TUI. The files under
`internal/team/pane_*.go`, `internal/team/pane_lifecycle*.go`,
`internal/team/tmux_runner.go`, and `internal/team/broker_pane.go` are a
process backend that uses tmux as a subprocess/session manager for pane-eligible
per-agent CLI sessions, currently the interactive Claude path, while the same
broker stream surface also carries headless Claude, Codex, and Opencode output.
Web mode can consume pane output through `startPaneCaptureLoops` when
`paneBackedAgents` is true; when panes are unavailable or not enabled, web mode
routes work through headless per-turn dispatch such as `claude --print`, so
agents keep running without the live xterm stream.

## Architectural Separation

The codebase has two tmux-adjacent systems. One is a user interface slated for
removal. The other is process infrastructure that stays.

### Deprecated UI Surface

The deprecated UI is the Bubble Tea "channel" interface under `cmd/wuphf`.
It is user-facing terminal UI code. It renders office state, messages, tasks,
requests, composer state, sidebars, and app panels.

Current main wires this path as `--tui` in `cmd/wuphf/main.go`. The desktop
platform plan renames that entry point to `--legacy-tui`; this document uses
"legacy TUI" for the surface regardless of which flag name is present in a
given branch.

The removable surface is:

| Path | Role | Size on `origin/main` |
|---|---|---|
| `cmd/wuphf/channel*.go` | Bubble Tea program, input, render, routing, and tests in package `main` | about 17k LOC |
| `cmd/wuphf/channelui/*` | Pure render/data-projection helpers for the channel TUI | about 9k LOC |

That code is allowed to disappear when the desktop/web replacement has parity.
Removing it means removing the user-facing terminal program, not removing tmux
from the broker's process-management toolbox.

### Process Backend

The process backend lives under `internal/team`. It exists to manage agent
processes and capture their stdout/stderr-shaped terminal output for broker
consumers. It is not a renderer, not a Bubble Tea model, and not a screen.

The kept surface is:

| Path | Role |
|---|---|
| `internal/team/tmux_runner.go` | Narrow test seam around the local `tmux` binary |
| `internal/team/pane_lifecycle.go` | Single-call tmux operations and pane/session helpers |
| `internal/team/pane_lifecycle_spawn.go` | Visible and overflow agent pane spawn orchestration |
| `internal/team/pane_capture.go` | Polls `tmux capture-pane`, strips ANSI/control bytes, diffs lines, and pushes agent-stream data |
| `internal/team/pane_dispatch.go` | Serializes agent wake packets into live tmux panes with `send-keys` |
| `internal/team/broker_pane.go` | Captures pane activity snapshots for broker-side liveness/activity checks |

These files are infrastructure. The important dependency direction is:

1. The launcher or broker decides whether a turn should be pane-backed or
   headless.
2. The pane backend starts or talks to subprocesses when a live pane target
   exists.
3. The broker publishes per-agent stream data.
4. A UI may choose to display that stream.

No file in the pane backend owns layout, keyboard focus, cards, sidebars,
message rendering, or Bubble Tea state. Those are legacy TUI concerns.

The backend also is not "tmux UI" in the product sense. tmux is used as a local
session/window/pane manager, comparable to a process supervisor with scrollback.
The user may never attach to those panes in web mode; the web UI sees only the
captured stream that the broker exposes.

## Web Mode Usage

Web mode starts in `internal/team/launcher_web.go`. `LaunchWeb` starts the
broker, serves the web UI, initializes the headless worker context, resumes
in-flight work, and calls `startPaneCaptureLoops`.

`startPaneCaptureLoops` is intentionally guarded. It returns immediately unless
`l.paneBackedAgents` is true and a broker is present. When the guard passes, it
starts one goroutine per pane-backed agent. Each goroutine polls the current
tmux pane target and pushes new lines into `Broker.AgentStream(slug)`.

The terminal view in the web UI consumes the same stream:

| Path | Role |
|---|---|
| `internal/team/broker_terminal.go` | Handles `/terminal/agents/{slug}` and upgrades to WebSocket |
| `web/src/lib/agentTerminalSocket.ts` | Builds the terminal WebSocket URL and client protocol |
| `web/src/components/agents/AgentTerminal.tsx` | Renders the xterm view and writes incoming data |

The WebSocket path is broker app data, not a Wails/native shell shortcut. It is
registered on the broker HTTP mux and guarded by the same auth middleware as
other protected broker routes.

Headless mode is the normal fallback and, on current main, the default startup
path for web mode. `internal/team/headless_claude.go` runs `claude --print` per
turn and tees stdout into the same agent stream buffer. Codex and Opencode are
modeled as non-pane, one-shot providers by provider capability and route
through the headless queue rather than tmux panes.

Pane spawn failures do not make the office unusable. `TrySpawnWebAgentPanes`
checks `TmuxAvailable`, attempts to create the session and agent panes, and
records failed pane slugs. Failed or skipped targets route through headless
dispatch. The user loses live pane capture for those agents, but the agent turn
still runs through the broker.

## Cross-Platform Reality

tmux is a POSIX tool. The pane backend should be treated as a POSIX-only process
backend, even though the surrounding broker and web UI are cross-platform.

Current main has two runtime gates:

| Gate | Effect |
|---|---|
| `officeTargeter.UsesPaneRuntime()` | Reads provider capabilities; pane-ineligible providers go headless |
| `paneLifecycle.TmuxAvailable()` | Checks whether `tmux` is on `PATH` before spawning panes |

The desktop platform plan adds the Windows product rule: native Windows desktop
launches should not enter the tmux pane path. They should report no usable pane
runtime and use the headless fallback until WUPHF has a Windows-specific
pseudo-terminal backend.

That is the trade-off for the first desktop shell. The Wails desktop app on
Windows can still run the broker, web UI, agent dispatch, and headless provider
turns. It just will not have tmux-backed live pane capture.

If users need equivalent live terminal capture on Windows, the future option is
a separate backend based on Windows-native process and terminal primitives, for
example ConPTY/winpty or a PowerShell-hosted equivalent. That would be a sibling
backend behind the same agent-stream contract, not a reason to keep or revive
the Bubble Tea channel UI.

## Contributor Rules

Do not delete `internal/team/pane_*.go` when removing the TUI. The TUI removal
target is `cmd/wuphf/channel*.go` plus `cmd/wuphf/channelui/*`. The pane backend
is independent infrastructure.

Do not move UI responsibilities into the pane backend. It should continue to
own subprocess lifecycle, tmux command execution, pane dispatch, capture, and
agent-stream writes. Rendering belongs to web components or, while it exists,
the legacy TUI.

Do not use "tmux" as shorthand for "the TUI." In this repository:

| Term | Meaning |
|---|---|
| Legacy TUI | Bubble Tea user interface under `cmd/wuphf/channel*.go` and `cmd/wuphf/channelui/*` |
| Pane backend | `internal/team` process backend that may use tmux to host agent CLI processes |
| Agent terminal | Web xterm consumer of broker agent streams |

Do not conflate `internal/team/notifier_*` with native OS notifications either.
Those files own agent wake/routing: deciding which agent should receive a
message or task packet and whether delivery goes through a headless queue or a
pane target.

Native OS notifications are part of the desktop shell boundary described in
`docs/architecture/desktop-platform.md` on `origin/docs/desktop-plan`. That plan
reserves Wails/native OS verbs for `desktop/oswails/`; current main does not
contain that package yet. When that package lands, it should stay separate from
`internal/team/notifier_*`.

A safe TUI-removal patch should therefore look like this:

1. Delete or disconnect the Bubble Tea entry point and render helpers.
2. Preserve pane lifecycle, dispatch, capture, and broker terminal routes.
3. Preserve headless fallback dispatch.
4. Keep web `AgentTerminal` pointed at the broker WebSocket stream.
5. Keep native desktop notifications outside `internal/team/notifier_*`.
