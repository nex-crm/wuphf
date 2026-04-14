# WUPHF

[![Discord](https://img.shields.io/badge/Discord-Join%20Community-5865F2?logo=discord&logoColor=white)](https://discord.gg/gjSySC3PzV)

A terminal office where your AI team works in the open.

> *"WUPHF. When you type it in, it contacts someone via phone, text, email, IM, Facebook, Twitter, and then... WUPHF."*
> — Ryan Howard, Season 7

One command. One shared office. CEO, PM, engineers, designer, CMO, CRO — all visible, arguing, claiming tasks, and shipping work instead of disappearing behind an API. Unlike the original WUPHF.com, this one works.

<video width="630" height="300" src="https://github.com/user-attachments/assets/71a29608-04e9-4d8f-8a8c-07889ce41070"></video>

<video width="630" height="300" src="https://github.com/user-attachments/assets/f4cdffbf-4388-49bc-891d-6bd050ff8247"></video>

## Get Started

**Prerequisites:** [Go](https://go.dev/dl/), [tmux](https://github.com/tmux/tmux/wiki/Installing), [Claude Code](https://docs.anthropic.com/en/docs/claude-code)

```bash
git clone https://github.com/nex-crm/wuphf.git
cd wuphf
go build -o wuphf ./cmd/wuphf
./wuphf
```

That's it. The browser opens automatically and you're in the office. Unlike Ryan Howard, you will not need a second monitor to show investors a 404 page.

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

If it feels like a hidden agent loop, something is wrong. If it feels like The Office, you're exactly where you need to be.

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

## Why WUPHF over Paperclip or Naive

| | WUPHF | Paperclip | Naive |
|---|---|---|---|
| Sessions | Fresh per turn | --resume (accumulates) | Same as Paperclip |
| Tools | Per-agent scoped | Global (all agents) | Same as Paperclip |
| Agent wakes | Push-driven | Heartbeat polling | Same as Paperclip |
| Live visibility | Stdout streaming | No | No |
| Mid-task steering | DM, no restart | Kill & restart | Kill & restart |
| Price | Free (self-hosted) | Free (self-hosted) | $49-149/mo + credits |
| Your API keys | Yes | Yes | No (buy credits) |
| License | MIT | MIT | Proprietary |

Naive is a [hosted fork of Paperclip](https://not-so-naive.vercel.app/) (YC S25). Same architecture underneath, same token waste, plus a billing markup. WUPHF is built from scratch with a different architecture.

## Benchmark

Same task, same machine, same codex binary. 5-turn CEO DM session. All numbers measured from live runs.

| | WUPHF + Claude Code | WUPHF + Codex | Paperclip + Codex |
|---|---|---|---|
| 5-turn cost | **$0.06** | **87k billed** | **284k billed** |
| Avg per turn | $0.01 (97% cached) | 17k billed | 57k billed |
| vs Paperclip | **9x cheaper** | **3.3x cheaper** | baseline |
| Input trend | Flat (31k) | Flat (128k) | Growing (308k → 500k) |
| Idle cost | Zero | Zero | Heartbeat every 30s |

**Fresh sessions.** Each agent turn starts clean. No conversation history accumulates.

**Prompt caching.** Claude Code gets 97% cache read because identical prompt prefixes across fresh sessions align with Anthropic's prompt cache.

**Per-role tools.** DM mode loads 4 MCP tools instead of 27. Fewer tool schemas = smaller prompt = better cache hits.

**Zero idle burn.** Agents only spawn when the broker pushes a notification. No heartbeat polling.

### Reproduce it

```bash
# Start WUPHF
wuphf --pack starter &

# Start Paperclip
npx paperclipai run --data-dir /tmp/paperclip-bench &

# Run the benchmark
./scripts/benchmark.sh
```

Full methodology, per-turn data, and Paperclip source references available in the benchmark results docs.

## The Name

From [*The Office*](https://theoffice.fandom.com/wiki/WUPHF.com_(Website)), Season 7. Ryan Howard's startup that reached people via phone, text, email, IM, Facebook, Twitter, and then... WUPHF. Michael Scott invested $10,000. Ryan burned through it. The site went offline.

The joke still fits. Except this WUPHF ships.



> *"I invested ten thousand dollars in WUPHF. Just need one good quarter."*
> — Michael Scott

Michael: still waiting on that quarter. We are not.
