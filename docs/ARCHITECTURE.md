# WUPHF CLI — Architecture

> Zero Humans Company in a CLI — autonomous multi-agent team.

**Branch:** `nazz/experiment/multi-agent-cli`
**Last Updated:** 2026-03-24

---

## How It Works

```bash
./wuphf                    # Launch team (default: founding team, 7 agents)
./wuphf --pack coding-team # Launch coding team (4 agents)
./wuphf --cmd "/help"      # Non-interactive command
./wuphf kill               # Stop the team
```

`./wuphf` creates a tmux session with:
- **Window 0 "channel"**: Go TUI showing the team conversation feed. Human types here.
- **Window 1 "agents"**: All agents in tiled panes (7 Claude Code sessions).

```
Window 0 (channel)              Window 1 (agents — tiled)
┌────────────────────────┐      ┌───────────┬───────────┐
│ wuphf team channel       │      │ 🤖 CEO    │ 🤖 PM     │
│                        │      │ claude>   │ claude>   │
│ @ceo: Let's build...   │      ├───────────┼───────────┤
│ @pm: Requirements:...  │      │ 🤖 FE     │ 🤖 BE     │
│ @fe: I'll use React... │      │ claude>   │ claude>   │
│                        │      ├───────────┼───────────┤
│ ╭──────────────────╮   │      │ 🤖 Design │ 🤖 CMO    │
│ │ Type here...     │   │      │ claude>   │ claude>   │
│ ╰──────────────────╯   │      └───────────┴───────────┘
└────────────────────────┘      (Ctrl+B z to zoom any pane)
```

Navigation:
- `Ctrl+B 0` — channel view
- `Ctrl+B 1` — agent panes
- `Ctrl+B arrow` — switch between panes
- `Ctrl+B z` — zoom a pane full-screen
- `/quit` or `Ctrl+C` in channel — kill everything

## Communication Stack

### Ephemeral: Broker (localhost:7890)
- In-memory HTTP message store, started by `./wuphf`
- Agents post via `team_broadcast` MCP tool → broker stores message
- Channel TUI polls broker every 1s → displays messages
- Notification loop pushes new messages to agent panes via `tmux send-keys`
- Dies with the session. That's intentional.

### Durable: WUPHF Knowledge Graph
- Agents use `add_context` MCP tool to persist decisions/facts
- `query_context` retrieves across sessions
- WUPHF hooks (SessionStart, UserPromptSubmit) provide automatic context

### Office Tools
- `team_broadcast` — post to channel
- `team_poll` — read recent messages
- `team_status` — share current activity
- `team_members` — see who's active
- `human_interview` — block on a human answer when the team cannot proceed responsibly

These tools now run from the main Go binary via the hidden `wuphf mcp-team` subcommand.
Generic Nex tools come from the installed `nex-mcp` binary when Nex is enabled.

## Agent Packs

| Pack | Agents | Leader |
|------|--------|--------|
| founding-team (default) | CEO, PM, FE, BE, Designer, CMO, CRO | CEO |
| coding-team | Tech Lead, FE, BE, QA | Tech Lead |
| lead-gen-agency | AE, SDR, Research, Content | AE |

Each agent gets `--append-system-prompt` with:
- Their role and team roster
- Instructions to use `team_broadcast`/`team_poll` for communication
- `@slug` tagging convention
- Leader gets "final decision authority"
- Specialists get "contribute proactively, respond when tagged"

## File Structure

```
cmd/wuphf/
├── main.go              # Entry: ./wuphf, ./wuphf kill, ./wuphf --cmd
└── channel.go           # Channel TUI (polls broker, renders, human input)

internal/
├── team/
│   ├── launcher.go      # tmux session mgmt, agent spawning, notification loop
│   └── broker.go        # HTTP message broker (localhost:7890)
├── agent/
│   ├── packs.go         # 3 pack definitions
│   ├── prompts.go       # System prompt generation
│   ├── loop.go          # Agent state machine
│   ├── service.go       # Agent lifecycle
│   ├── tools.go         # 7 WUPHF API tools
│   ├── session.go       # Session store
│   ├── gossip.go        # Knowledge propagation
│   └── adoption.go      # Credibility scoring
├── commands/
│   ├── slash.go          # 21 canonical commands
│   ├── helpers.go        # parseFlags, formatTable
│   └── cmd_*.go          # Command groups (objects, records, etc.)
├── orchestration/
│   ├── message_router.go # Skill-based routing
│   ├── delegator.go      # Team-lead delegation parser
│   └── executor.go       # Concurrent execution
├── provider/
│   ├── claude.go         # Claude Code subprocess provider
│   ├── gemini.go         # Gemini provider
│   └── resolver.go       # Provider selection
├── tui/                  # Bubbletea TUI (stream, roster, panes, gossip)
├── tui/render/           # Table, timeline, taskboard, insights, graph
├── chat/                 # Chat channels + messages
├── calendar/             # Cron scheduling
├── config/               # Configuration
├── api/                  # WUPHF HTTP client
└── teammcp/              # Go MCP server for office/team tools
```

## What Works (Verified)
- `./wuphf` launches tmux with channel + 7 agent panes
- Broker starts and serves messages
- Channel TUI displays messages, accepts human input
- `/quit` kills entire session
- `./wuphf kill` stops from outside
- `./wuphf --cmd` non-interactive dispatch
- 340+ unit tests pass

## Known Issues
- Agent panes are small when terminal is narrow (<200 cols)
- `notifications/claude/channel` push not yet verified end-to-end
- `tmux send-keys` notification can interrupt agent mid-thought
- Agent pane titles show "✳ Claude Code" (Claude overrides tmux pane title)
- No automatic agent response to channel messages without notification push
