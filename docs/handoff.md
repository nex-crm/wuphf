# Handoff Document — WUPHF Multi-Agent CLI

> Complete context for any agent continuing this work. Read this before touching code.

**Date:** 2026-03-24
**Branch:** `nazz/experiment/multi-agent-cli`
**Location:** `/Users/najmuzzaman/Documents/wuphf/wuphf/cli/.worktrees/go-bubbletea-port`

---

## 1. The Vision

**One window where you type thoughts as they come — like a Slack team channel.** Different AI agent "team members" self-select which messages to handle. Agents work concurrently. Agents can debate each other. User never manages instances, branches, or routing. Just types.

The system must be MULTI-THREADED, not sequential. Each user message is an independent work item. Multiple agents work concurrently. User input is NEVER blocked. This is fundamentally different from "Team-Lead → dispatch → wait → respond."

### Who is the user?

Nazz — startup founder who runs 10+ concurrent Claude Code instances daily for feature work, demos, marketing, browser automation, debugging, PR reviews, and multi-agent coding teams. The core pain is context switching between instances. Wants a single window that acts like having a real team in Slack.

### What does success look like?

1. `./wuphf` launches a team of AI agents that collaborate organically
2. Human types in a channel, agents respond naturally — not prompted, not polled, just participating like real teammates
3. CEO makes final decisions but everyone contributes
4. When an agent is tagged, they respond. When a topic matches expertise, they chime in
5. Conversations are organic — no talking over each other, natural turn-taking
6. Decisions get stored permanently in the WUPHF knowledge graph
7. Chat is ephemeral — like Slack messages, gone when session ends

---

## 2. Architecture (Current State)

```
./wuphf launches:
├── Broker (localhost:7890) — ephemeral in-memory message store
├── tmux session "wuphf-team" (-L wuphf server to avoid nesting):
│   ├── Window 0 "channel" — Go TUI (Bubbletea) polling broker
│   │   └── Human types here, sees all team messages
│   └── Window 1 "agents" — 7 Claude Code sessions in tiled panes
│       └── Each agent has --append-system-prompt with role + team tools
├── Notification loop — pushes new broker messages to agent panes via tmux send-keys
├── WUPHF MCP — team_broadcast/poll/status/members tools on each agent
└── notifications/claude/channel — MCP push (built but not verified)
```

### Two-Layer Context (Critical Design Decision)

**Ephemeral (Broker):** Real-time team chat. In-memory. Dies with session. Agents use `team_broadcast` to post, `team_poll` to read. Like Slack messages.

**Durable (WUPHF Knowledge Graph):** Decisions, facts, outcomes. Persists forever. Agents use `add_context` to store, `query_context` to retrieve. Like decisions in Notion.

These are DIFFERENT. Don't conflate them. Team chatter is NOT stored in the knowledge graph. Only explicit decisions are.

---

## 3. User Preferences & Learnings

### Hard Rules
- **One command per action.** No aliases. `/agent list` not `/agents`. 21 canonical commands.
- **Always test with termwright** before declaring anything done. Never trust unit tests alone for TUI changes.
- **Team agents get the same effort** as direct work. Full context, exact patterns, edge cases.
- **Use TeamCreate** for parallel work, not individual Agent calls.
- **Never hand-edit generated files.** Update source, regenerate.
- **Ship incrementally** with E2E verification at each stage.
- **NEVER factor in sunk cost.** If the right path means redoing work, say so.

### Things That Failed
- **Multi-pane terminal embedding** (charmbracelet/x/vt) — 7 panes in 80 cols = 11 cols each. Claude can't render. Abandoned.
- **Custom NDJSON provider** (claude -p subprocess) — 30-40s hook delay, loses thinking visibility, agent echo bug. Replaced with real Claude Code sessions.
- **Slack-style TUI** (sidebar, channels, DMs, threads) — navigating channels in a terminal is cumbersome. Abandoned for single chat stream.
- **Agent Teams experimental feature** — only tmux/iTerm2, no custom UI. Can't integrate WUPHF context.
- **claude-peers-mcp** — good broker pattern but separate from WUPHF. Built the same thing into WUPHF MCP instead.

### Things That Worked
- **tmux with separate server** (`-L wuphf`) — avoids nesting issues when running inside Claude Code's tmux.
- **Shared broker** (localhost:7890) — simple, ephemeral, all agents share one channel.
- **Channel TUI** (Go Bubbletea) — clean conversation feed, human input, no clutter.
- **Agent packs** — pre-built teams (founding/coding/lead-gen) replace single-agent selection.
- **`tmux send-keys` for notification** — interim push mechanism that works today.

---

## 4. Current State (What Works)

| Feature | Status | Notes |
|---------|--------|-------|
| `./wuphf` launches team | ✅ | Default: founding team, 7 agents |
| Broker on :7890 | ✅ | Ephemeral message store |
| Channel TUI | ✅ | Full-screen, polls broker, human input |
| Human posts to channel | ✅ | Type + Enter in channel window |
| Agent panes in tmux | ✅ | Window 1 "agents", tiled layout |
| Notification push | ✅ | tmux send-keys (interim) |
| `/quit` kills everything | ✅ | Channel, agents, broker, tmux |
| `./wuphf kill` from outside | ✅ | Graceful cleanup |
| `./wuphf --solo` TUI | ✅ | Single-agent with 55+ commands |
| `./wuphf --cmd` dispatch | ✅ | Non-interactive |
| Pane border labels | ⚠️ | Set but Claude overrides with "✳ Claude Code" |
| Agent auto-response | ⚠️ | tmux send-keys works but interrupts mid-thought |
| MCP channel push | ⚠️ | Built but `--dangerously-load-development-channels` not added |
| 340+ unit tests | ✅ | `go test ./...` |

## 5. Known Issues (Prioritized)

### P0 — Must Fix

1. **Agents don't respond organically.** The `tmux send-keys` notification injects text into the agent's prompt, but it's a blunt instrument — it interrupts whatever the agent was doing. The proper fix is `notifications/claude/channel` via MCP, which requires adding `--dangerously-load-development-channels wuphf` to each Claude session's launch command.

2. **Agent pane titles show "✳ Claude Code".** Claude Code overrides `select-pane -T`. The pane border format works (`pane-border-status top`) but the content is wrong. Users can't tell which agent is which.

3. **Agent panes too small on narrow terminals.** 7 tiled panes in 80 cols. Claude needs 60+ cols. Only usable on wide terminals (200+ cols). Need to limit visible panes or use a different layout.

### P1 — Should Fix

4. **No automatic turn-taking.** When all agents get notified, they all try to respond at once. Need cooldown/floor mechanism (the GossipBus turn-taking protocol exists but isn't wired to the tmux-based flow).

5. **Channel TUI has no scroll indicator.** Can't tell if older messages exist above.

6. **Channel TUI has no agent status bar.** Was planned but not implemented. Should show `● CEO talking ◐ PM thinking ○ FE idle` at bottom.

7. **E2E test timing fragile.** Uses fixed `sleep` values instead of retry loops.

### P2 — Nice to Have

8. **Platform plugins (P2E)** and **integration management (P2F)** — not needed for core product, TS CLI handles these.

---

## 6. File Map

### Core (what you'll touch most)
```
cmd/wuphf/main.go              # Entry point: ./wuphf, ./wuphf kill, --solo, --cmd
cmd/wuphf/channel.go           # Channel TUI: polls broker, renders, input
internal/team/launcher.go    # tmux mgmt, agent spawning, notification loop
internal/team/broker.go      # HTTP message broker localhost:7890
internal/agent/packs.go      # 3 pack definitions
internal/agent/prompts.go    # System prompt generation
mcp/src/tools/team.ts        # team_broadcast/poll/status/members + channel push
mcp/src/server.ts            # MCP server with team tool registration
```

### Supporting (stable, rarely changed)
```
internal/commands/slash.go     # 21 commands
internal/commands/cmd_*.go     # Command implementations
internal/tui/stream.go         # Classic single-agent TUI
internal/tui/model.go          # Root Bubbletea model
internal/orchestration/        # Message router, delegator
internal/provider/claude.go    # Claude Code subprocess provider
```

---

## 7. How to Continue

### To fix P0-1 (MCP channel push):
1. In `launcher.go claudeCommand()`, add `--dangerously-load-development-channels wuphf` to the claude args
2. Verify `mcp/src/tools/team.ts startChannelPush()` actually delivers notifications
3. Test: post to broker via curl, verify agent's Claude session shows the message without tmux send-keys
4. Remove `notifyAgentsLoop` once MCP push works

### To fix P0-2 (pane titles):
- Claude Code uses OSC escape sequences to set the terminal title. tmux `pane-border-format` can use `#{pane_index}` with a lookup table instead of `#{pane_title}`.
- Or: accept it and rely on window name (agents window shows all panes, user learns positions).

### To fix P0-3 (pane sizing):
- Detect terminal width via tmux `display-message -p '#{client_width}'`
- If < 200 cols, show max 3 panes instead of 7
- Or use vertical splits (stacked) instead of tiled for narrow terminals

### To run E2E tests:
```bash
cd /Users/najmuzzaman/Documents/wuphf/wuphf/cli/.worktrees/go-bubbletea-port
go build -o wuphf ./cmd/wuphf
bash tests/uat/notetaker-e2e.sh    # Full team flow test
bash tests/uat/run-e2e.sh          # Solo TUI test
bash tests/uat/persona-tests.sh    # Multi-persona solo TUI test
```

### To test manually:
```bash
./wuphf                    # Launch team
# In channel: type "Let's build an AI notetaker company"
# Ctrl+B 1 → see agent panes
# Ctrl+B z → zoom a pane
# /quit → kill everything
```

---

## 8. Build & Dependencies

```bash
# Go CLI
go build -o wuphf ./cmd/wuphf
go test ./...

# MCP server
cd /Users/najmuzzaman/Documents/wuphf/wuphf/mcp
bun run build

# Required on PATH
tmux, claude
```

Go deps: `charmbracelet/bubbletea`, `charmbracelet/lipgloss`, `charmbracelet/x/vt`, `creack/pty/v2`
MCP deps: `@modelcontextprotocol/sdk`, `zod`
