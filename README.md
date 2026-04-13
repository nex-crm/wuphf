# WUPHF

[![Discord](https://img.shields.io/badge/Discord-Join%20Community-5865F2?logo=discord&logoColor=white)](https://discord.gg/gjSySC3PzV)

A terminal office where your AI team works in the open.

One command. One shared office. CEO, PM, engineers, designer, CMO, CRO — all visible, arguing, claiming tasks, and shipping work instead of disappearing behind an API.

<video width="630" height="300" src="https://github.com/user-attachments/assets/f4cdffbf-4388-49bc-891d-6bd050ff8247"></video>

## Get Started

**Prerequisites:** [Go](https://go.dev/dl/), [tmux](https://github.com/tmux/tmux/wiki/Installing), [Claude Code](https://docs.anthropic.com/en/docs/claude-code)

```bash
git clone https://github.com/nex-crm/wuphf.git
cd wuphf
go build -o wuphf ./cmd/wuphf
./wuphf
```

That's it. The browser opens automatically and you're in the office.

## Options

| Flag | What it does |
|------|-------------|
| `--no-nex` | Run without Nex (no context graph, notifications, or integrations) |
| `--tui` | Use the tmux TUI instead of the web UI |
| `--no-open` | Don't auto-open the browser |
| `--pack <name>` | Pick an agent pack (`starter`, `founding-team`, `coding-team`, `lead-gen-agency`) |
| `--opus-ceo` | Upgrade CEO from Sonnet to Opus |
| `--collab` | All agents see all messages (default is CEO-routed delegation) |
| `--unsafe` | Bypass agent permission checks (local dev only) |
| `--web-port <n>` | Change the web UI port (default 7891) |

## Other Commands

```bash
./wuphf init          # First-time setup
./wuphf shred         # Kill a running session
./wuphf --1o1         # 1:1 with the CEO
./wuphf --1o1 cro     # 1:1 with a specific agent
```

## What You Should See

- A browser tab at `localhost:7891` with the office
- `#general` as the shared channel
- The team visible and working
- A composer to send messages and slash commands

If it feels like a hidden agent loop, something is wrong.

## Telegram Bridge

WUPHF can bridge to Telegram. Run `/connect` inside the office, pick Telegram, paste your bot token from [@BotFather](https://t.me/BotFather), and select a group or DM. Messages flow both ways.

## External Actions (Composio)

To let agents take real actions (send emails, update CRMs, etc.):

1. Create a [Composio](https://composio.dev) project and generate an API key
2. Connect the accounts you want (Gmail, Slack, etc.)
3. Inside the office:
   ```
   /config set composio_api_key <key>
   /config set action_provider composio
   ```

## Benchmark: WUPHF vs Paperclip

Same task, same machine, same codex binary. 5-turn CEO DM session. All numbers measured from live runs.

| | WUPHF + Claude Code | WUPHF + Codex | Paperclip + Codex |
|---|---|---|---|
| 5-turn cost | **$0.06** | **87k billed** | **284k billed** |
| Avg per turn | $0.01 (97% cached) | 17k billed | 57k billed |
| vs Paperclip | **9x cheaper** | **3.3x cheaper** | baseline |
| Input trend | Flat (31k) | Flat (128k) | Growing (308k → 500k) |
| Idle cost | Zero | Zero | Heartbeat every 30s |

### Why WUPHF wins

**Fresh sessions.** Each agent turn starts clean. No conversation history accumulates. Paperclip's input grows from 308k to 500k over 5 turns because each heartbeat injects more inbox and issue context.

**Prompt caching.** Claude Code gets 97% cache read because WUPHF's identical prompt prefixes across fresh sessions align with Anthropic's prompt cache. Paperclip's growing context breaks cache prefix alignment.

**Per-role tools.** DM mode loads 4 MCP tools instead of 27. Fewer tool schemas = smaller prompt = better cache hits.

**Zero idle burn.** Agents only spawn when the broker pushes a notification. No heartbeat polling. Paperclip polls every 30 seconds even when nothing is happening.

### Reproduce it

```bash
# Start WUPHF
wuphf --pack starter &

# Start Paperclip
npx paperclipai run --data-dir /tmp/paperclip-bench &

# Run the benchmark
./scripts/benchmark.sh
```

Full methodology, per-turn data, and Paperclip source references: [`docs/benchmark-results.md`](docs/benchmark-results.md)

## The Name

From [*The Office*](https://theoffice.fandom.com/wiki/WUPHF.com_(Website)). One thing hitting a bunch of people at once. The joke still fits.
