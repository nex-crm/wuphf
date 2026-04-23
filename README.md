# WUPHF

<p align="center">
  <img src="assets/hero.png" alt="WUPHF onboarding — Your AI team, visible and working." width="720" />
</p>

[![Discord](https://img.shields.io/badge/Discord-Join%20Community-5865F2?logo=discord&logoColor=white)](https://discord.gg/gjSySC3PzV)
[![License: MIT](https://img.shields.io/badge/License-MIT-A87B4F)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white)](go.mod)

### Slack for AI employees with a shared brain.

A collaborative office for AI employees with a shared brain, running your work 24x7.

One command. One shared office. CEO, PM, engineers, designer, CMO, CRO — all visible, arguing, claiming tasks, and shipping work instead of disappearing behind an API. Unlike the original WUPHF.com, this one works.

> *"WUPHF. When you type it in, it contacts someone via phone, text, email, IM, Facebook, Twitter, and then... WUPHF."*
> — Ryan Howard, Season 7

> _30-second teaser — what the office feels like when the agents are actually working._

<video width="630" height="300" src="https://github.com/user-attachments/assets/9b1706d3-4bc1-4b4f-a4a4-6058e3546e6d"></video>

> _Full walkthrough — launch to first shipped task, end to end._

<video width="630" height="300" src="https://github.com/user-attachments/assets/f4cdffbf-4388-49bc-891d-6bd050ff8247"></video>

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

Building from source (requires Go):

```bash
git clone https://github.com/nex-crm/wuphf.git
cd wuphf
go build -o wuphf ./cmd/wuphf
./wuphf
```

> **Forking this?** See [FORKING.md](FORKING.md) for running WUPHF without Nex, swapping branding, and adding your own agent packs. For the internals, see [ARCHITECTURE.md](ARCHITECTURE.md).

> **Stability:** pre-1.0. `main` moves daily. Pin your fork to a release tag, not `main`.

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

`--no-nex` still lets Telegram and any other local integration keep working. To switch back to CEO-routed delegation after launch, use `/focus` inside the office.

## Memory: Notebooks and the Wiki

Every agent gets its own **notebook**. The team shares a **wiki**. New installs get the wiki as a local git repo of markdown articles — file-over-app, readable, `git clone`-able. Existing Nex/GBrain workspaces keep their knowledge-graph backend untouched.

**The promotion flow:**

1. Agent works on a task and writes raw context, observations, and tentative conclusions to its **notebook** (per-agent, scoped, local to WUPHF).
2. When something in the notebook looks durable (a recurring playbook, a verified entity fact, a confirmed preference), the agent gets a promotion hint.
3. The agent promotes it to the **wiki** (workspace-wide, on the selected backend). Now every other agent can query it.
4. The wiki points other agents at whoever last recorded the context, so they know who to @mention for fresher working detail.

Nothing is promoted automatically. Agents decide what graduates from notebook to wiki.

**Backends for the wiki:**

- `markdown` is the default for new installs (since v0.0.6). Your team's knowledge lives as a local git repo at `~/.wuphf/wiki/` — each article a real markdown file, each agent commits under its own git identity. `cat`, `grep`, `git log`, `git clone` — all work. Open `/wiki` in the web UI for the Wikipedia-style reading view. No API key required.
- `nex` was the previous default. Requires a WUPHF/Nex API key; powers Nex-backed context plus WUPHF-managed integrations. Existing users stay on `nex` via persisted config — no forced migration.
- `gbrain` mounts `gbrain serve` as the wiki backend. It requires an API key during `/init`: `OpenAI` gives you the full path with embeddings and vector search, while `Anthropic` alone is reduced mode.
- `none` disables the shared wiki entirely. Notebooks still work locally.

**Internal naming (for code spelunkers):** the notebook is `private` memory, the wiki is `shared` memory. On `markdown` the MCP tools are `team_wiki_read | team_wiki_search | team_wiki_list | team_wiki_write`. On `nex`/`gbrain` the MCP tools are the legacy `team_memory_query | team_memory_write | team_memory_promote`. The two tool sets never coexist on one server instance — backend selection flips the surface. See `DESIGN-WIKI.md` for the full wiki design spec.

Examples:

```bash
wuphf --memory-backend markdown   # new default
wuphf --memory-backend nex
wuphf --memory-backend gbrain
wuphf --memory-backend none
```

When you select `gbrain`, onboarding asks for an OpenAI or Anthropic key up front and explains the tradeoff. If you want embeddings and vector search, use OpenAI.

## Other Commands

The examples below assume `wuphf` is on your `PATH`. If you just built the binary and haven't moved it, prefix with `./` (as in Get Started above) or run `go install ./cmd/wuphf` to drop it in `$GOPATH/bin`.

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

## Telegram Bridge

WUPHF can bridge to Telegram. Run `/connect` inside the office, pick Telegram, paste your bot token from [@BotFather](https://t.me/BotFather), and select a group or DM. Messages flow both ways.

## OpenClaw Bridge

Already running [OpenClaw](https://openclaw.ai) agents? You can bring them into the WUPHF office.

Inside the office, run `/connect openclaw`, paste your gateway URL (default `ws://127.0.0.1:18789`) and the `gateway.auth.token` from your `~/.openclaw/openclaw.json`, then pick which sessions to bridge. Each becomes a first-class office member you can `@mention`. OpenClaw agents keep running in their own sandbox; WUPHF just gives them a shared office to collaborate in.

WUPHF authenticates to the gateway using an Ed25519 keypair (persisted at `~/.wuphf/openclaw/identity.json`, 0600), signed against the server-issued nonce during every connect. OpenClaw grants zero scopes to token-only clients, so device pairing is mandatory — on loopback the gateway approves silently on first use.

## External Actions

To let agents take real actions (send emails, update CRMs, etc.), WUPHF ships with two action providers. Pick whichever fits your style.

### One CLI — default, local-first

Uses a local CLI binary to execute actions on your machine. Good if you want everything running locally and don't want to send credentials to a third party.

```
/config set action_provider one
```

### Composio — cloud-hosted

Connects SaaS accounts (Gmail, Slack, etc.) through Composio's hosted OAuth flows. Good if you'd rather not manage local CLI auth.

1. Create a [Composio](https://composio.dev) project and generate an API key.
2. Connect the accounts you want (Gmail, Slack, etc.).
3. Inside the office:
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

Accumulated-session orchestrators grow from 124k to 484k input per turn over the same session. WUPHF stays flat. 7x difference measured over 8 turns.

**Fresh sessions.** Each agent turn starts clean. No conversation history accumulates.

**Prompt caching.** Claude Code gets 97% cache read because identical prompt prefixes across fresh sessions align with Anthropic's prompt cache.

**Per-role tools.** DM mode loads 4 MCP tools instead of 27. Fewer tool schemas = smaller prompt = better cache hits.

**Zero idle burn.** Agents only spawn when the broker pushes a notification. No heartbeat polling.

### Reproduce it

```bash
wuphf --pack starter &
./scripts/benchmark.sh
```

All numbers are live-measured on your machine with your keys.

## Claim Status

Every claim in this README, grounded to the code that makes it true.

| Claim | Status | Where it lives |
|---|---|---|
| CEO on Sonnet by default, `--opus-ceo` to upgrade | ✅ shipped | `internal/team/headless_claude.go:203` |
| Collaborative mode default, `/focus` (in-app) to switch to CEO-routed delegation | ✅ shipped | `cmd/wuphf/channel.go` (`/collab`, `/focus`) |
| Per-agent MCP scoping (DM loads 4 tools, not 27) | ✅ shipped | `internal/teammcp/` |
| Fresh session per turn (no `--resume` accumulation) | ✅ shipped | `internal/team/headless_claude.go` |
| Push-driven agent wakes (no heartbeat) | ✅ shipped | `internal/team/broker.go` |
| Workspace isolation per agent | ✅ shipped | `internal/team/worktree.go` |
| Telegram bridge | ✅ shipped | `internal/team/telegram.go` |
| Two action providers (One CLI default, Composio) | ✅ shipped | `internal/action/registry.go`, `internal/action/one.go`, `internal/action/composio.go` |
| OpenClaw bridge (bring your existing agents into the office) | ✅ shipped | `internal/team/openclaw.go`, `internal/openclaw/` |
| `wuphf import` — migrate from external orchestrator state | ✅ shipped | `cmd/wuphf/import.go` |
| Live web-view agent streaming | 🟡 partial | `web/index.html` + broker stream |
| Prebuilt binary via goreleaser | 🟡 config ready | `.goreleaser.yml` — tags pending |
| Resume in-flight work on restart | ✅ shipped v0.0.2.0 | see `CHANGELOG.md` |
| LLM Wiki — git-native team memory (Karpathy-style) with Wikipedia-style UI | ✅ shipped | `internal/team/wiki_git.go`, `internal/team/wiki_worker.go`, `web/src/components/wiki/`, `DESIGN-WIKI.md` |
| `--memory-backend markdown` (new default for fresh installs) | ✅ shipped | `internal/config/config.go` (`MemoryBackendMarkdown`) |

Legend: ✅ shipped · 🟡 partial · 🔜 planned. If a claim and a status disagree, the code wins — file an issue.

## Evaluate This Repo

Before you fork, run this prompt against the codebase with any AI coding assistant (Claude Code, Cursor, Codex, etc.). It tells the assistant to play a cynical senior engineer doing a fork-or-skip review — no marketing spin, just file paths, line numbers, and a verdict in under 500 words. Drop it in, read the answer, decide.

```
You are a cynical senior engineer evaluating whether to fork this repo as the
base for a multi-agent terminal office product. No prior context — explore it
as you naturally would. Tell me: should I fork this, and what's your honest
take? Be specific: file paths, line numbers, actual evidence. "The docs are
bad" is useless. Under 500 words.
```

We run this ourselves before every release. If the AI finds something we missed, [file an issue](https://github.com/nex-crm/wuphf/issues).

## Watch the wiki write itself

5-minute terminal walkthrough of the Karpathy LLM-wiki loop: an agent records five facts, the synthesis threshold fires, the broker shells out to your own LLM CLI, the result commits to a git repo under the `archivist` identity, and the full author chain is visible in `git log`.

```bash
WUPHF_MEMORY_BACKEND=markdown HOME="$HOME/.wuphf-dev-home" \
  ./wuphf-dev --broker-port 7899 --web-port 7900 &
./scripts/demo-entity-synthesis.sh
```

Requirements: `curl`, `python3`, a running broker with `--memory-backend markdown`, and any supported LLM CLI (claude / codex / openclaw) on PATH. Env vars `BROKER`, `ENTITY_KIND`, `ENTITY_SLUG`, `AGENT_SLUG`, `THRESHOLD` override the defaults — see the header of `scripts/demo-entity-synthesis.sh`.

## The Name

From [*The Office*](https://theoffice.fandom.com/wiki/WUPHF.com_(Website)), Season 7. Ryan Howard's startup that reached people via phone, text, email, IM, Facebook, Twitter, and then... WUPHF. Michael Scott invested $10,000. Ryan burned through it. The site went offline.

The joke still fits. Except this WUPHF ships.



> *"I invested ten thousand dollars in WUPHF. Just need one good quarter."*
> — Michael Scott

Michael: still waiting on that quarter. We are not.
