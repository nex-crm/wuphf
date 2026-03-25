# Embedded Claude Code Terminals + Gossip Bus — Implementation Plan

> **For agentic workers:** Use TeamCreate for parallel execution of independent tasks.

**Goal:** Replace the single-stream chat view with real Claude Code PTY terminals per agent, connected via a GossipBus so every agent sees what others are thinking and saying.

**Architecture:** Each agent spawns a real `claude -p` process via `creack/pty`. Terminal output is rendered via `charmbracelet/x/vt` emulator. A GossipBus captures output from each agent and broadcasts it to all others via PTY stdin injection.

**Tech Stack:** Go 1.24+, `charmbracelet/x/vt`, `creack/pty/v2`, Bubbletea, Lipgloss

**Spec:** `docs/superpowers/specs/2026-03-23-embedded-terminals-gossip.md`

---

## Task 1: TerminalPane — Single PTY + VT Emulator

**Files:**
- Create: `internal/tui/terminal_pane.go`
- Create: `internal/tui/terminal_pane_test.go`

Implement a TerminalPane that spawns a real process via PTY and renders its output via VT emulator.

```go
type TerminalPane struct {
    slug     string
    name     string
    emulator *vt.SafeEmulator
    ptmx     *os.File        // master PTY from creack/pty
    cmd      *exec.Cmd
    focused  bool
    width    int
    height   int
    alive    bool
    mu       sync.Mutex
}
```

Methods:
- `NewTerminalPane(slug, name string, width, height int) *TerminalPane`
- `Spawn(command string, args []string, env []string, cwd string) error` — starts process with PTY, launches reader goroutine that feeds PTY output to emulator
- `View() string` — returns `emulator.Render()` (ANSI string for Bubbletea)
- `SendKey(key string)` — writes keystroke bytes to `ptmx`
- `SendText(text string)` — writes raw text to `ptmx`
- `Resize(w, h int)` — resizes PTY + emulator
- `IsAlive() bool`
- `Close()` — sends SIGTERM, waits, closes PTY

Test with a simple process (e.g., `echo hello`) — verify View() contains "hello".

Commit: `"feat: add TerminalPane with PTY + VT emulator"`

---

## Task 2: PaneManager — Multi-Pane Layout + Focus

**Files:**
- Create: `internal/tui/pane_manager.go`
- Create: `internal/tui/pane_manager_test.go`

Manages multiple TerminalPanes with layout and focus routing.

```go
type PaneManager struct {
    panes       []*TerminalPane       // ordered list
    paneMap     map[string]*TerminalPane // slug → pane
    focusedIdx  int
    layout      string                // "leader" | "grid" | "tabs"
    roster      RosterModel
    width       int
    height      int
}
```

Methods:
- `NewPaneManager(roster RosterModel) *PaneManager`
- `AddPane(pane *TerminalPane)` — adds pane to manager
- `RemovePane(slug string)`
- `FocusPane(slug string)` — set focus by slug
- `FocusNext()` / `FocusPrev()` — cycle focus
- `Focused() *TerminalPane` — returns currently focused pane
- `Update(msg tea.Msg) tea.Cmd` — routes key events to focused pane
- `View(width, height int) string` — renders all panes in layout

Layout rendering (leader-focused):
- First pane (leader): `width * 0.7`, `height * 0.6`
- Active specialist panes: split remaining bottom area equally
- Roster sidebar: `rosterWidth` on right
- Use lipgloss.JoinHorizontal/Vertical for composition

Key bindings:
- `Ctrl+1` through `Ctrl+7`: focus specific pane
- `Ctrl+N`: next pane
- `Ctrl+P`: previous pane

Commit: `"feat: add PaneManager with leader-focused layout"`

---

## Task 3: GossipBus + OutputObserver — Cross-Agent Context

**Files:**
- Create: `internal/tui/gossip_bus.go`
- Create: `internal/tui/output_observer.go`
- Create: `internal/tui/gossip_bus_test.go`

### OutputObserver

Taps the PTY output stream, parses Claude's stream-json NDJSON, emits GossipEvents.

```go
type OutputObserver struct {
    slug    string
    bus     *GossipBus
    buffer  bytes.Buffer  // accumulates partial lines
}

// Feed is called with raw PTY output bytes
func (o *OutputObserver) Feed(data []byte)
```

Parsing: scan for complete NDJSON lines, extract:
- `type: "assistant"` with `content[].type: "thinking"` → GossipEvent{Type: "thinking"}
- `type: "assistant"` with `content[].type: "text"` → GossipEvent{Type: "text"}
- `type: "assistant"` with `content[].type: "tool_use"` → GossipEvent{Type: "tool_use"}
- `type: "user"` with tool_result → GossipEvent{Type: "tool_result"}

### GossipBus

```go
type GossipBus struct {
    panes      map[string]*TerminalPane
    observers  map[string]*OutputObserver
    eventLog   []GossipEvent
    throttle   map[string]time.Time  // last injection per target
    mu         sync.Mutex
}
```

Methods:
- `NewGossipBus() *GossipBus`
- `Register(pane *TerminalPane)` — creates observer, starts feed
- `Unregister(slug string)`
- `Emit(event GossipEvent)` — called by observers, broadcasts to all others
- `InjectContext(targetSlug string, event GossipEvent)` — writes formatted context to target's PTY

Injection format:
```
[TEAM @ceo thinking]: We need a hero section with a strong CTA...
```

Throttling: max 1 injection per target per 2 seconds. Batch events.
Truncation: content capped at 300 chars. tool_result capped at 100.

Tests: verify event routing (sender doesn't receive own events), throttling, truncation.

Commit: `"feat: add GossipBus for cross-agent context sharing"`

---

## Task 4: Wire Into Root Model — Replace StreamModel

**Files:**
- Modify: `internal/tui/model.go` — use PaneManager instead of StreamModel
- Modify: `internal/tui/runtime.go` — spawn PTY panes instead of agent service loops
- Modify: `internal/tui/messages.go` — add pane-related messages

Update `NewModel()`:
1. Create PaneManager
2. Create GossipBus
3. Load agent pack from config
4. For each agent in pack:
   - Build system prompt (collaborative version from spec)
   - Create TerminalPane
   - Spawn `claude -p "<prompt>" --output-format stream-json --verbose --max-turns 50`
   - Register with GossipBus
   - Add to PaneManager
5. Focus CEO pane

Update `Update()`:
- Route key events to PaneManager
- Handle pane focus switching (Ctrl+1-7, Ctrl+N/P)
- Forward GossipBus events to roster for activity indicators
- Handle Ctrl+C double-press for clean shutdown

Update `View()`:
- Return PaneManager.View() for the stream view
- Keep help/agents/chat views as alternatives

Update `Init()`:
- Start a ticker for periodic re-render (VT emulators update async)

Keep StreamModel as fallback (when `--no-terminal-panes` flag or no claude in PATH).

Commit: `"feat: wire PaneManager + GossipBus into root model"`

---

## Task 5: Enhanced Roster + Activity Indicators

**Files:**
- Modify: `internal/tui/roster.go`

The roster now shows real-time activity from GossipBus events:

Activity states:
- `●` green + "talking" — agent produced text output
- `◐` yellow + "thinking" — agent is in thinking phase
- `⚡` purple + "coding" — agent is using tools (Bash, Edit, etc.)
- `○` gray + "idle" — no recent output (>10s)
- `◆` blue + "listening" — agent received gossip context

The GossipBus emits events to a channel that the roster subscribes to.

Commit: `"feat: enhance roster with real-time activity from gossip bus"`

---

## Task 6: Update Agent Prompts for Collaboration

**Files:**
- Modify: `internal/agent/prompts.go`

Update `BuildTeamLeadPrompt` and `BuildSpecialistPrompt` with collaborative instructions:

Team-Lead prompt includes:
- "Messages prefixed [TEAM @slug] are from your teammates"
- "Your team can see everything you say"
- "Make final decisions but listen to input first"
- Team roster with @slugs

Specialist prompt includes:
- "You are in a shared session with your team"
- "Contribute proactively, debate, correct mistakes"
- "When CEO announces a plan, execute your part"

Commit: `"feat: update agent prompts for collaborative team communication"`

---

## Task 7: Input Bar + User Message Broadcasting

**Files:**
- Modify: `internal/tui/pane_manager.go`

The input bar at the bottom of the screen works in two modes:

1. **Focused mode** (default): keystrokes go to the focused pane's PTY
2. **Broadcast mode** (Ctrl+B to toggle): types a message that gets sent to ALL agent PTYs simultaneously

When in broadcast mode:
- Input bar shows "[BROADCAST]" prefix
- On Enter: message is written to every agent's PTY as user input
- Format: `User: <message>`

When in focused mode:
- Input goes directly to the focused pane's PTY (raw keystrokes)
- The pane renders exactly as if the user were in Claude Code directly

Commit: `"feat: add broadcast input mode for team-wide messages"`

---

## Task 8: Clean Shutdown + Process Management

**Files:**
- Modify: `internal/tui/terminal_pane.go`
- Modify: `internal/tui/pane_manager.go`

Ensure clean shutdown:
- Ctrl+C double-press sends SIGTERM to all Claude processes
- Wait up to 5s for graceful exit
- SIGKILL any remaining processes
- Close all PTY file descriptors
- Clean exit from Bubbletea

Process monitoring:
- Goroutine per pane watches cmd.Wait()
- When a process exits, mark pane as dead
- Roster shows ✕ for dead agents
- PaneManager auto-removes dead panes from layout

Commit: `"feat: clean shutdown and process lifecycle management"`

---

## Task 9: Testing + Termwright E2E

**Files:**
- Create/update tests
- Create: `tests/uat/terminal-panes-e2e.sh`

Unit tests:
- TerminalPane: spawn `echo hello`, verify View contains "hello"
- PaneManager: add 3 panes, verify focus cycling
- GossipBus: emit event, verify other agents receive it, sender doesn't
- OutputObserver: feed NDJSON lines, verify correct GossipEvent types

Termwright E2E:
- Boot wuphf → verify roster shows all 7 agents
- Verify CEO pane is visible and focused
- Type a message → verify it appears in CEO's terminal
- Wait for agent output → verify roster shows activity
- Ctrl+N → verify focus switches to next pane
- Ctrl+C twice → clean exit

Commit: `"test: add terminal pane unit tests and termwright E2E"`

---

## Task 10: Final Verification + Push

- [ ] `go test ./...` — all pass
- [ ] `go build -o wuphf ./cmd/wuphf` — clean build
- [ ] `./wuphf --version` — prints version
- [ ] Manual test: launch `./wuphf`, see CEO terminal, type message, see response
- [ ] `bash tests/uat/terminal-panes-e2e.sh` — E2E passes
- [ ] `git push origin nazz/experiment/multi-agent-cli`
