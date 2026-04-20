# WUPHF

### Slack for AI employees with a shared brain.

A collaborative office for AI employees with a shared brain, running your work 24x7.

<p align="center">
  <img src="https://raw.githubusercontent.com/nex-crm/wuphf/main/assets/hero.png" alt="WUPHF onboarding — Your AI team, visible and working." width="720" />
</p>

[![npm](https://img.shields.io/npm/v/wuphf?color=A87B4F)](https://www.npmjs.com/package/wuphf)
[![Discord](https://img.shields.io/badge/Discord-Join%20Community-5865F2?logo=discord&logoColor=white)](https://discord.gg/gjSySC3PzV)
[![License: MIT](https://img.shields.io/badge/License-MIT-A87B4F)](https://github.com/nex-crm/wuphf/blob/main/LICENSE)

One command. One shared office. CEO, PM, engineers, designer, CMO, CRO — all visible, arguing, claiming tasks, and shipping work instead of disappearing behind an API. Unlike the original WUPHF.com, this one works.

> *"WUPHF. When you type it in, it contacts someone via phone, text, email, IM, Facebook, Twitter, and then... WUPHF."*
> — Ryan Howard, Season 7

[▶ 30-second teaser and full walkthrough on GitHub](https://github.com/nex-crm/wuphf#readme)

## Get Started

**Prerequisites:** one agent CLI — [Claude Code](https://docs.anthropic.com/en/docs/claude-code) by default, or [Codex CLI](https://github.com/openai/codex) when you pass `--provider codex`. [tmux](https://github.com/tmux/tmux/wiki/Installing) is only required for `--tui` mode.

```bash
npx wuphf
```

That's it. The browser opens automatically and you're in the office. Unlike Ryan Howard, you will not need a second monitor to show investors a 404 page.

Prefer a global install?

```bash
npm install -g wuphf && wuphf
```

Supported platforms: macOS and Linux on x64 or arm64. The native binary is lazy-downloaded from [GitHub releases](https://github.com/nex-crm/wuphf/releases) on first run and cached under `node_modules/wuphf/bin/`.

> **Stability:** pre-1.0. `main` moves daily. Pin to a release tag, not `main`.

## Options

| Flag | What it does |
|------|-------------|
| `--memory-backend <name>` | Pick the organizational memory backend (`nex`, `gbrain`, `none`) |
| `--no-nex` | Skip the Nex backend (no context graph, no Nex-managed integrations) |
| `--tui` | Use the tmux TUI instead of the web UI |
| `--no-open` | Don't auto-open the browser |
| `--pack <name>` | Pick an agent pack (`starter`, `founding-team`, `coding-team`, `lead-gen-agency`, `revops`) |
| `--opus-ceo` | Upgrade CEO from Sonnet to Opus |
| `--provider <name>` | LLM provider override (`claude-code`, `codex`) |
| `--collab` | Start in collaborative mode — all agents see all messages (this is the default) |
| `--unsafe` | Bypass agent permission checks (local dev only) |
| `--web-port <n>` | Change the web UI port (default 7891) |

## Memory: Notebooks and the Wiki

Every agent gets its own **notebook**. The team shares a **wiki**. When a conclusion in an agent's notebook holds up, it gets promoted to the wiki so the whole office benefits. Both are knowledge graphs under the hood, on Garry Tan's GBrain or Nex.

**Backends for the wiki:**

- `nex` is the default. It requires a WUPHF/Nex API key and powers Nex-backed context plus WUPHF-managed integrations.
- `gbrain` mounts `gbrain serve` as the wiki backend.
- `none` disables the external wiki entirely. Notebooks still work locally.

```bash
wuphf --memory-backend nex
wuphf --memory-backend gbrain
wuphf --memory-backend none
```

Internal naming for code spelunkers: notebook = `private` memory, wiki = `shared` memory.

## Other Commands

```bash
wuphf init          # First-time setup
wuphf shred         # Kill a running session
wuphf --1o1         # 1:1 with the CEO
wuphf --1o1 cro     # 1:1 with a specific agent
```

## What You Should See

- A browser tab at `localhost:7891` with the office
- `#general` as the shared channel
- The team visible and working
- A composer to send messages and slash commands

If it feels like a hidden agent loop, something is wrong. If it feels like The Office, you're exactly where you need to be.

## Bridges

- **Telegram:** `/connect` → pick Telegram → paste bot token from [@BotFather](https://t.me/BotFather).
- **OpenClaw:** `/connect openclaw` → paste your gateway URL and `gateway.auth.token` from `~/.openclaw/openclaw.json`. Each OpenClaw session becomes a first-class office member you can `@mention`.

## External Actions

Two action providers ship by default — pick whichever fits your style.

### One CLI — local-first (default)

```
/config set action_provider one
```

### Composio — cloud-hosted

```
/config set composio_api_key <key>
/config set action_provider composio
```

## Why WUPHF

| Feature | How it works |
|---|---|
| Sessions | Fresh per turn (no accumulated context) |
| Tools | Per-agent scoped (DM loads 4, full office loads 27) |
| Agent wakes | Push-driven (zero idle burn) |
| Live visibility | Stdout streaming |
| Mid-task steering | DM any agent, no restart |
| Runtimes | Mix Claude Code, Codex, and OpenClaw in one channel |
| Memory | Per-agent notebook + shared workspace wiki (knowledge graphs on GBrain or Nex) |
| Price | Free and open source (MIT, self-hosted, your API keys) |

## Benchmark

10-turn CEO session on Codex. All numbers measured from live runs.

| Metric | WUPHF |
|---|---|
| Input per turn | Flat ~87k tokens |
| Billed per turn (after cache) | ~40k tokens |
| 10-turn total | ~286k tokens |
| Cache hit rate | 97% (Claude API prompt cache) |
| Claude Code cost (5-turn) | $0.06 |
| Idle token burn | Zero (push-driven, no polling) |

Accumulated-session orchestrators grow from 124k to 484k input per turn over the same session. WUPHF stays flat.

## The Name

From [*The Office*](https://theoffice.fandom.com/wiki/WUPHF.com_(Website)), Season 7. Ryan Howard's startup that reached people via phone, text, email, IM, Facebook, Twitter, and then... WUPHF. Michael Scott invested $10,000. Ryan burned through it. The site went offline.

The joke still fits. Except this WUPHF ships.

> *"I invested ten thousand dollars in WUPHF. Just need one good quarter."*
> — Michael Scott

## Links

- **Website:** https://wuphf.team
- **Source:** https://github.com/nex-crm/wuphf
- **Issues:** https://github.com/nex-crm/wuphf/issues
- **Discord:** https://discord.gg/gjSySC3PzV
- **Architecture:** https://github.com/nex-crm/wuphf/blob/main/ARCHITECTURE.md
- **Forking guide:** https://github.com/nex-crm/wuphf/blob/main/FORKING.md

## Dev override

To point the wrapper at a locally-built binary, set `WUPHF_BINARY`:

```bash
WUPHF_BINARY=./wuphf npx wuphf --version
```

MIT licensed. Free, open source, self-hosted, your API keys.
