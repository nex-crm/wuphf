# Embedded Claude Code Terminals with Gossip Bus — Design Spec

> Real Claude Code terminal panes per agent with cross-agent context sharing. No agent responds in a vacuum.

**Branch:** `nazz/experiment/multi-agent-cli`
**Date:** 2026-03-23
**Libraries:** `charmbracelet/x/vt`, `creack/pty`
**Reference:** TUIOS (github.com/Gaurav-Gosain/tuios)

---

## 1. Vision

Launch `wuphf` and see a real Claude Code terminal for each agent on your team. When the CEO thinks, the FE Engineer sees it. When FE runs a command, BE knows about it. They debate, brainstorm, correct each other — like a real team in a room. The user talks to everyone; the CEO orchestrates execution.

---

## 2. Architecture

### 2.1 Component Overview

```
┌─ wuphf TUI (Bubbletea) ──────────────────────────────────┐
│                                                          │
│  PaneManager                                             │
│  ├── TerminalPane (CEO)    ← creack/pty + x/vt          │
│  ├── TerminalPane (PM)     ← creack/pty + x/vt          │
│  ├── TerminalPane (FE)     ← creack/pty + x/vt          │
│  ├── TerminalPane (BE)     ← creack/pty + x/vt          │
│  ├── TerminalPane (Design) ← creack/pty + x/vt          │
│  ├── TerminalPane (CMO)    ← creack/pty + x/vt          │
│  └── TerminalPane (CRO)    ← creack/pty + x/vt          │
│                                                          │
│  GossipBus                                               │
│  ├── OutputObserver per pane (goroutine parsing stdout)  │
│  ├── Broadcasts GossipEvents to all other agents         │
│  └── Injects [TEAM CONTEXT] into each agent's PTY       │
│                                                          │
│  Roster sidebar (existing, enhanced)                     │
│  Input bar (routes to focused pane or broadcasts)        │
└──────────────────────────────────────────────────────────┘
```

### 2.2 Data Flow

```
User types: "Let's build a landing page for our SaaS product"
  ↓
Input bar sends to ALL agents via GossipBus broadcast
  ↓
Each agent's Claude session receives the message
  ↓
CEO starts thinking → Observer captures thinking block
  ↓
GossipBus broadcasts: [TEAM: @ceo thinking]: "We need to..."
  ↓
PM sees CEO's thinking, starts contributing requirements
  ↓
FE sees both, starts thinking about implementation
  ↓
CEO announces plan: "Here's how we'll execute..."
  ↓
All agents see the plan and begin their parts
```

---

## 3. Components

### 3.1 TerminalPane

Wraps a single Claude Code PTY session with VT terminal emulation.

```go
// internal/tui/terminal_pane.go

type TerminalPane struct {
    slug      string
    name      string
    emulator  *vt.SafeEmulator  // charmbracelet/x/vt (thread-safe)
    ptmx      *os.File          // master PTY fd from creack/pty
    cmd       *exec.Cmd         // claude process
    focused   bool
    width     int
    height    int
    alive     bool              // process still running
}
```

**Methods:**
- `Spawn(slug, name, systemPrompt, cwd string) error` — starts `claude -p` with PTY
- `View() string` — returns `emulator.Render()`
- `SendKey(key tea.KeyMsg)` — forwards keystroke to PTY
- `SendText(text string)` — writes raw text to PTY stdin
- `Resize(w, h int)` — updates PTY window size + emulator
- `Close()` — kills process, closes PTY

**Claude Command:**
```bash
claude -p "<system_prompt>\n\n<initial_message>" \
  --output-format stream-json \
  --verbose \
  --max-turns 50 \
  --no-session-persistence
```

**PTY → Emulator goroutine:**
```go
go func() {
    buf := make([]byte, 4096)
    for {
        n, err := p.ptmx.Read(buf)
        if err != nil { return }
        p.emulator.Write(buf[:n])  // feed raw terminal output to VT parser
        // Also feed to OutputObserver for gossip
        p.observer.Feed(buf[:n])
    }
}()
```

### 3.2 OutputObserver

Parses Claude Code's stream-json output from the PTY to extract meaningful events for the gossip bus.

```go
// internal/tui/output_observer.go

type OutputObserver struct {
    slug     string
    bus      *GossipBus
    scanner  *bufio.Scanner  // reads from a pipe tee'd from PTY
}

type GossipEvent struct {
    FromSlug  string
    Type      string    // "thinking", "text", "tool_use", "tool_result", "plan"
    Content   string
    Timestamp time.Time
}
```

**Parsing strategy:**
- Tap the PTY output stream (tee to both VT emulator AND observer)
- Observer scans for Claude's NDJSON lines (type: "assistant", "user", "result")
- Extracts thinking blocks, text responses, tool use, tool results
- Emits GossipEvents to the bus
- Truncates content to 500 chars for injection (keeps context manageable)

### 3.3 GossipBus

The shared nervous system. Captures every agent's output and shares it with all others.

```go
// internal/tui/gossip_bus.go

type GossipBus struct {
    agents     map[string]*TerminalPane
    observers  map[string]*OutputObserver
    eventLog   []GossipEvent    // rolling buffer, max 100 events
    mu         sync.Mutex
}
```

**Methods:**
- `Register(pane *TerminalPane)` — adds agent + starts observer goroutine
- `Unregister(slug string)` — removes agent
- `Broadcast(event GossipEvent)` — sends to all agents except sender
- `InjectContext(targetSlug string, event GossipEvent)` — writes to agent's PTY

**Context injection format:**
```
[TEAM @ceo thinking]: I think we should start with the hero section...
[TEAM @fe text]: I'll build the hero with a gradient background and CTA
[TEAM @be tool_use]: Bash: mkdir -p api/routes
```

**Throttling:**
- Max 1 injection per agent per 2 seconds (prevents flooding)
- Batch multiple events into single injection
- Skip injecting tool_result if it's >200 chars (just inject "[TEAM @slug ran Bash]")

### 3.4 PaneManager

Manages all panes, layout, focus routing.

```go
// internal/tui/pane_manager.go

type LayoutMode string
const (
    LayoutLeaderFocused LayoutMode = "leader"
    LayoutGrid          LayoutMode = "grid"
    LayoutTabs          LayoutMode = "tabs"
)

type PaneManager struct {
    panes       map[string]*TerminalPane
    focusOrder  []string          // ordered list of slugs
    focusedSlug string
    layout      LayoutMode
    gossipBus   *GossipBus
    roster      RosterModel       // existing component
    width       int
    height      int
}
```

**Layout: Leader-Focused (default)**
```
┌────────────────────────────────────┬──────────────┐
│                                    │ AGENTS       │
│   CEO (70% width, 60% height)     │ ● CEO  talk  │
│   Full Claude Code terminal        │ ● PM   think │
│                                    │ ● FE   code  │
│                                    │ ○ BE   idle  │
├─────────┬─────────┬────────────────┤ ○ Design     │
│ PM      │ FE      │ BE             │ ○ CMO        │
│ (small) │ (small) │ (small)        │ ○ CRO        │
└─────────┴─────────┴────────────────┴──────────────┘
```

- CEO pane: `width * 0.7`, `height * 0.6`
- Active specialist panes split remaining bottom area equally
- Idle agents: no pane, just roster entry
- Agent becomes "active" when GossipBus detects output

**Focus routing:**
- Focused pane receives all keyboard input
- Unfocused panes only receive GossipBus injections
- `Ctrl+1-7` switches focus
- `Ctrl+N/P` cycles focus
- `Tab` cycles through active panes only

### 3.5 Agent System Prompts

**CEO (Team-Lead):**
```
You are the CEO of the Founding Team. Your team is in this session with you.

Messages prefixed [TEAM @slug] are from your teammates — they can see everything you say.

Your role:
1. Listen to the user's directive
2. Think through the approach
3. Invite input from relevant team members
4. Make the final execution decision
5. Announce the plan clearly so everyone can act

You can address teammates: "@fe can you handle the frontend?"
Always explain your reasoning so the team can follow.
```

**Specialists:**
```
You are the FE Engineer on a collaborative team. You are in a shared session with your CEO and teammates.

Messages prefixed [TEAM @slug] are from your teammates — they can see everything you say.

Your role:
1. Listen to the user's request and your team's discussion
2. Contribute your frontend expertise proactively
3. Debate ideas when you disagree — explain why
4. When CEO announces a plan, execute your part
5. Share progress so others can build on your work

Address teammates by @slug. Be concise but thorough.
```

---

## 4. Integration with Existing Code

### What We Keep
- `internal/agent/packs.go` — pack definitions (agent slugs, names, expertise)
- `internal/agent/prompts.go` — prompt generation (updated for collaborative prompts)
- `internal/tui/roster.go` — roster sidebar (enhanced with activity status)
- `internal/tui/styles.go` — lipgloss styles
- `internal/tui/keybindings.go` — key mapping
- `internal/tui/autocomplete.go` — slash command autocomplete
- `internal/commands/*` — all 55+ commands
- `internal/tui/render/*` — table, timeline, taskboard, insights renders
- `internal/config/*` — configuration
- `internal/api/*` — WUPHF API client

### What We Replace
- `internal/tui/stream.go` → `internal/tui/pane_manager.go` (multi-pane replaces single stream)
- `internal/provider/claude.go` → `internal/tui/terminal_pane.go` (direct PTY replaces NDJSON wrapper)
- `internal/tui/model.go` → updated to use PaneManager instead of StreamModel

### What We Add
- `internal/tui/terminal_pane.go` — TerminalPane model
- `internal/tui/pane_manager.go` — multi-pane layout + focus
- `internal/tui/gossip_bus.go` — cross-agent context sharing
- `internal/tui/output_observer.go` — stdout parser

---

## 5. File Structure

```
internal/tui/
├── terminal_pane.go        # NEW: PTY + VT emulator wrapper
├── terminal_pane_test.go   # NEW
├── pane_manager.go         # NEW: multi-pane layout + focus
├── pane_manager_test.go    # NEW
├── gossip_bus.go           # NEW: cross-agent context sharing
├── gossip_bus_test.go      # NEW
├── output_observer.go      # NEW: stdout parser for gossip
├── output_observer_test.go # NEW
├── model.go                # MODIFIED: uses PaneManager
├── roster.go               # MODIFIED: activity indicators
├── keybindings.go          # MODIFIED: pane focus keys
├── styles.go               # EXISTING
├── autocomplete.go         # EXISTING
├── mention.go              # EXISTING
├── picker.go               # EXISTING
├── spinner.go              # EXISTING
├── messages.go             # MODIFIED: new pane/gossip messages
├── init_flow.go            # EXISTING
├── stream.go               # KEPT (fallback for non-terminal mode)
├── generative.go           # EXISTING
├── generative_registry.go  # EXISTING
└── runtime.go              # MODIFIED: spawns PaneManager
```

---

## 6. Dependencies

```
go get github.com/charmbracelet/x/vt
go get github.com/creack/pty
```

Both are well-maintained Go libraries. `x/vt` is Charm's official VT emulator (MIT). `creack/pty` is the standard Go PTY library (MIT).

---

## 7. Success Criteria

- [ ] All 7 agents boot with real Claude Code PTY terminals
- [ ] User message broadcasts to all agents via GossipBus
- [ ] CEO's thinking/text visible to all specialists in real-time
- [ ] Specialists can see each other's output and respond
- [ ] CEO makes final decision, team executes collaboratively
- [ ] Native Claude Code TUI rendered correctly in each pane (colors, cursor, scrollback)
- [ ] Roster shows real-time activity (thinking/talking/coding/idle)
- [ ] Ctrl+1-7 switches focus between panes
- [ ] Focused pane receives keyboard input
- [ ] Existing /commands work in focused pane
- [ ] GossipBus throttles to prevent context flooding
- [ ] Clean shutdown: all PTY processes killed on Ctrl+C

---

## 8. Implementation Order

1. **TerminalPane** — single pane that spawns and renders a Claude Code PTY
2. **PaneManager** — multi-pane layout with focus routing
3. **GossipBus + OutputObserver** — cross-agent context sharing
4. **Model integration** — wire PaneManager into the root model
5. **Roster enhancement** — activity indicators from gossip events
6. **Testing** — unit tests + termwright E2E
