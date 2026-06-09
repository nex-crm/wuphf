# ARCHITECTURE

How WUPHF works under the hood, anchored to files you can open. One page. Read it, then the code makes sense.

## The shape

```
          ┌──────────────┐         ┌──────────────┐
 human ──▶│   Web UI /   │────────▶│    Broker    │◀── Nex / Telegram / Composio
          │  TUI / 1:1   │         │  (pub/sub +  │    (optional integrations)
          └──────────────┘◀────────│    queue)    │
                                   └──────┬───────┘
                                          │ push on message
                                          ▼
                        ┌─────────────────────────────────┐
                        │  Per-agent headless runners     │
                        │  (Claude Code / Codex, fresh    │
                        │   session per turn, scoped MCP) │
                        └─────────────────────────────────┘
                                          │
                                          ▼
                            isolated git worktree per agent
```

## Core components

| File | Role |
|---|---|
| `cmd/wuphf/` | CLI entrypoint, slash commands, TUI, launcher |
| `internal/team/broker.go` | Message bus. Every message is a push event — agents are spawned on wake, not polled |
| `internal/team/launcher.go` | Decides which agents wake for a given message (focus/collab mode, tags) |
| `internal/team/headless_claude.go` | Spawns `claude` as a one-shot per turn; no `--resume` accumulation |
| `internal/team/headless_codex.go` | Same model for Codex |
| `internal/team/worktree.go` | Per-agent isolated git worktree so agents can't corrupt each other |
| `internal/team/resume.go` | On restart, replays unfinished tasks + unanswered messages to the right agents |
| `internal/teammcp/` | The per-agent MCP tool surface. DM mode loads ~4 tools; office mode loads more |
| `internal/agent/packs.go` | The team compositions (`starter`, `founding-team`, `coding-team`, `lead-gen-agency`, `revops`) — packs can also pre-seed default skills |
| `web/index.html` | The office UI — channels, composer, live streams |
| `mcp/` | MCP servers WUPHF ships for Nex context, human-in-the-loop approvals, etc. |

## Three load-bearing choices

With the file that implements each. The original rationale for all three was
token cost; as of the SOTA uplift (`docs/specs/sota-uplift.md`) the design
optimizes for outcome quality first — packets are sized for what the agent
needs to do the work well, and token spend is no longer a constraint.

1. **Fresh session per turn** (`headless_claude.go`). Every agent turn is `claude -p "<prompt>"` from scratch. No `--resume`, no growing history. Kept because it makes turns crash-safe, provider-agnostic, and replayable — continuity comes from rich work packets (full thread context, full task spec) and, per the uplift plan, a per-(agent,task) state ledger, not from a growing in-process history. Prompt caching still applies but is a side effect, not the goal.

2. **Per-agent scoped MCP** (`internal/teammcp/`). An agent in DM mode sees only the handful of tools that mode needs. Kept for tool-use accuracy: smaller, role-shaped tool surfaces measurably beat exhaustive ones. Each agent role gets exactly the tools it needs, nothing more.

3. **Push-driven broker** (`broker.go`). Agents sleep until the broker pushes them a message. No heartbeat polling an empty inbox. The push carries a generous work packet, and agents are free to pull more (thread history, wiki, learnings) whenever the packet isn't enough.

## Data flow of one message

1. Human types in web UI → POSTs to broker.
2. Broker decides who wakes (focus mode: CEO only unless tagged; collab mode: everyone).
3. `launcher.go` builds the per-agent prompt + scoped MCP manifest.
4. `headless_claude.go` shells out to `claude -p` in the agent's worktree.
5. stdout streams back through the broker → web UI.
6. Agent responses with `@other-agent` mentions re-enter step 2.
7. Tool calls are gated: mutating tools require human approval via the Requests panel unless `--unsafe`.

## Optional integrations

- **Nex** (`internal/action/nex_client.go` + external `nex-mcp` binary): context graph, notifications, email/CRM context. Opt out with `--no-nex`.
- **Telegram** (`internal/team/telegram.go`): bidirectional bridge via `/connect`.
- **Composio** (`--action provider`): lets agents take real-world actions (send email, update CRM).
- **OpenClaw** (`internal/team/openclaw.go` + `internal/openclaw/` WS client): bridge users' existing OpenClaw agents into the office. Connect via `/connect openclaw`.
- **OpenClaw Gateway HTTP** (`internal/provider/openclaw_http.go`): run WUPHF office members through OpenClaw Gateway's OpenAI-compatible API server. Select with `--provider openclaw-http`.
- **Hermes Agent** (`internal/provider/hermes_agent.go`): run WUPHF office members through a local Hermes gateway using its OpenAI-compatible API server. Select with `--provider hermes-agent`.

These integrations are load-time optional. Core WUPHF is just `broker + launcher + headless runners + worktrees`.

## What's intentionally not here

- No central LLM proxy, no "model router" layer. Each agent shells out directly.
- No conversation-persistent sessions. Persistence is in the channel log, not the model.
- No SaaS backend. Everything is local, single binary, local sqlite/files.

## Next stops

- [`FORKING.md`](FORKING.md) — how to cut Nex, swap branding, add packs.
- `scripts/benchmark.sh` — run the 9× benchmark yourself. Full methodology is inline in the script comments.
