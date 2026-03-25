# Channel View Pivot — Design Spec

> Replace multi-pane terminal clutter with a clean Slack-style channel. Individual agent terminals accessible via tmux -CC when needed.

**Date:** 2026-03-23
**Replaces:** Embedded terminal pane layout (too cluttered, unreadable, no scroll)

---

## 1. The Problem

Showing 7 terminal panes in one screen doesn't work:
- Each pane is too narrow to read
- Raw Claude Code output is noisy (NDJSON, escape sequences, system messages)
- Can't scroll within panes
- Overwhelming — user doesn't know where to look

## 2. The Solution: Channel + Background Agents

```
┌─ wuphf (Primary Channel) ────────────────────────────────┐
│                                                         │
│  CEO: I think we should build the landing page first.   │
│  CEO: @fe can you handle the hero section?              │
│  CEO: @be set up the REST endpoints.                    │
│                                                         │
│  PM: Agreed. Here are the requirements:                 │
│    - Hero with gradient CTA                             │
│    - Features grid below fold                           │
│                                                         │
│  FE: On it. Starting with the hero component.           │
│    ⚡ Bash: mkdir -p src/components/hero                │
│    ↳ Created directory                                  │
│                                                         │
│  BE: Setting up Express routes for /api/auth.           │
│    ⚡ Edit: src/routes/auth.ts                          │
│                                                         │
│  You: ___                                               │
├─────────────────────────────────────────────────────────┤
│ ● CEO talking  ◐ PM thinking  ⚡ FE coding  ○ BE idle  │
└─────────────────────────────────────────────────────────┘
```

- **Primary view:** Single scrollable channel showing formatted agent messages
- **Status bar:** Compact agent activity indicators at bottom
- **Background:** Each agent runs in a tmux window (created via tmux -CC if in iTerm2)
- **Deep dive:** User can switch to any agent's raw terminal via tmux

## 3. Architecture

```
User's Terminal
├── Primary: wuphf TUI (Bubbletea) — the Channel
│   ├── GossipBus captures all agent output
│   ├── Filters out noise (NDJSON system msgs, rate limits, hooks)
│   ├── Renders clean formatted messages
│   └── Status bar shows all agent activity
│
└── Background: tmux sessions (one per agent)
    ├── tmux new-window -d "claude -p <prompt> ..."
    ├── Output captured via tmux capture-pane
    ├── GossipBus observer taps captured output
    └── User can attach via tmux select-window
```

## 4. What Changes

### model.go
- Default mode: Channel view (StreamModel with GossipBus integration)
- tmux -CC mode: detected via $TERM_PROGRAM=iTerm2, creates native tmux windows
- No more embedded pane rendering — PaneManager becomes optional debug tool

### GossipBus → Channel Adapter
- GossipBus events are rendered as StreamMessages in the channel
- Filter: only "text", "tool_use" (with name only), and "tool_result" (truncated)
- Skip: "thinking" (too noisy for channel), system/init/rate_limit messages
- Format: `AgentName: content` with agent-specific color

### tmux Integration
- On boot: `tmux new-session -d -s wuphf-agents` (if not already in tmux)
- For each agent: `tmux new-window -d -t wuphf-agents -n <slug> "claude -p <prompt>..."`
- Capture: `tmux capture-pane -p -t wuphf-agents:<slug>` every 500ms
- Observer parses captured text for GossipBus events
- If iTerm2: use tmux -CC for native tab integration

### Status Bar (replaces roster sidebar)
- Compact one-line activity indicator at bottom
- `● CEO talking  ◐ PM thinking  ⚡ FE coding  ○ BE idle  ○ Design idle  ○ CMO idle  ○ CRO idle`
- Updates in real-time from GossipBus

## 5. Human Judgment Tests

Termwright tests that check USABILITY, not just functionality:

### Readability Checks
- Message lines are at least 40 chars wide (readable sentences)
- No more than 3 consecutive blank lines (not wasting space)
- Agent name prefix is clearly separated from content
- No raw JSON or NDJSON visible in the channel

### Information Density
- At least 60% of visible lines contain meaningful content (not borders/chrome)
- Status bar is exactly 1 line (not overflowing)
- No overlapping text

### Flow Checks
- After user submits message, response appears within visible scroll area
- Messages appear in chronological order
- Agent color coding is consistent per agent

### Polish Checks
- Input field has clear border
- Status bar is always visible at bottom
- Mode indicator (INSERT) visible
- No flickering on re-render (stable between ticks)
