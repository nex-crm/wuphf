# WUPHF (pronounced "woof")

<p align="center">
  <img src="assets/hero.png" alt="WUPHF onboarding — Your AI team, visible and working." width="720" />
</p>

[![Discord](https://img.shields.io/badge/Discord-Join%20Community-5865F2?logo=discord&logoColor=white)](https://discord.gg/gjSySC3PzV)
[![License: MIT](https://img.shields.io/badge/License-MIT-A87B4F)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](go.mod)

<p align="left">
  <a href="https://news.ycombinator.com/item?id=47899844">
    <img src="website/hn-badge.svg" alt="WUPHF — Hacker News Life of Product Week's #1" width="223" height="48" />
  </a>
</p>

### Slack for AI employees with a shared brain.

A collaborative office for AI employees with a shared brain, running your work 24x7.

One command. One shared office. CEO, PM, engineers, designer, CMO, CRO — all visible, arguing, claiming tasks, and shipping work instead of disappearing behind an API. Unlike the original WUPHF.com, this one works.

> *"WUPHF. When you type it in, it contacts someone via phone, text, email, IM, Facebook, Twitter, and then... WUPHF."*
> — Ryan Howard, Season 7

> _30-second teaser — what the office feels like when the agents are actually working._

<video width="630" height="300" src="https://github.com/user-attachments/assets/36661391-a0ee-43d6-80d9-177776a53bc9"></video>

> _Full walkthrough — launch to first shipped task, end to end._

<video width="630" height="300" src="https://github.com/user-attachments/assets/f4cdffbf-4388-49bc-891d-6bd050ff8247"></video>

## Get Started

**Prerequisites:** one agent CLI — [Claude Code](https://docs.anthropic.com/en/docs/claude-code) by default, or [Codex CLI](https://github.com/openai/codex) when you pass `--provider codex`. [tmux](https://github.com/tmux/tmux/wiki/Installing) is required for `--legacy-tui` mode (the web UI runs agents headlessly by default; tmux-backed dispatch remains as an internal fallback).

```bash
npx wuphf
```

That's it. The browser opens automatically and you're in the office. Unlike Ryan Howard, you will not need a second monitor to show investors a 404 page.

Prefer a global install?

```bash
npm install -g wuphf && wuphf
```

Building from source (requires Go and Bun):

```bash
git clone https://github.com/nex-crm/wuphf.git
cd wuphf
cd web
bun install
bun run build
cd ..
go build -o wuphf ./cmd/wuphf
./wuphf
```

> **Forking this?** See [FORKING.md](FORKING.md) for running WUPHF without Nex, swapping branding, and adding your own agent packs. For the internals, see [ARCHITECTURE.md](ARCHITECTURE.md).

> **Stability:** pre-1.0. `main` moves daily. Pin your fork to a release tag, not `main`.

## Setup prompt (for AI agents)

Paste this into Claude Code, Codex, or Cursor and let your agent drive the install:

```text
Set up https://github.com/nex-crm/wuphf for me. Read `README.md` first, then run
`npx wuphf` to install and launch the office — the web UI opens at
http://localhost:7891 automatically.

Once it is running, confirm you can see #general, the team of agents, and the
composer. If anything looks off, re-read the "What You Should See" and "Other
Commands" sections of README.md before retrying — do not skip to flags.

For agent conventions and the MCP tools each teammate gets, read `AGENTS.md`.
For internals and how the broker, workspaces, and memory fit together, read
`ARCHITECTURE.md`. If I mention forking or rebranding, read `FORKING.md`.

After it is installed and running, open https://github.com/nex-crm/wuphf in my
browser. If I am logged in to GitHub, ask me whether you should star the repo
for me as a quick demo that the browser interaction works — only click the star
if I say yes. If I am not logged in, just open https://wuphf.team.
```

## Options

| Flag | What it does |
|------|-------------|
| `--memory-backend <name>` | Override the built-in markdown wiki (`nex`, `gbrain`, `none`) — legacy installs only |
| `--no-nex` | Skip the Nex backend (no context graph, no Nex-managed integrations) |
| `--legacy-tui` | Use the legacy tmux TUI instead of the web UI |
| `--no-open` | Don't auto-open the browser |
| `--pack <name>` | Pick an agent pack (`starter`, `founding-team`, `coding-team`, `lead-gen-agency`, `revops`) |
| `--opus-ceo` | Upgrade CEO from Sonnet to Opus |
| `--provider <name>` | LLM provider override (`claude-code`, `codex`, `opencode`, `ollama`, `hermes-agent`, `openclaw-http`) |
| `--collab` | Start in collaborative mode — all agents see all messages (this is the default) |
| `--unsafe` | Bypass agent permission checks (local dev only) |
| `--web-port <n>` | Change the web UI port (default 7891) |
| `--workspace <name>` | Use a specific workspace for one command (does not change the active workspace) |

`--legacy-tui` is deprecated, slated for removal, and retained only while the desktop replacement lands.

### Opencode and custom endpoints

`--provider opencode` shells out to the `opencode` CLI binary. WUPHF does not
own that provider's HTTP path, and `provider_endpoints.opencode.base_url` is not
consulted.

For custom OpenAI-compatible endpoints such as LiteLLM, OmniRoute, or local
proxies, use `--provider ollama` and set `WUPHF_OLLAMA_BASE_URL` or
`provider_endpoints.ollama.base_url`:

```bash
WUPHF_OLLAMA_BASE_URL="http://127.0.0.1:20128/v1" \
WUPHF_OLLAMA_MODEL="openai/gpt-5.4-mini" \
wuphf --provider ollama --memory-backend none --no-open
```

`--no-nex` still lets Telegram and any other local integration keep working. To switch back to CEO-routed delegation after launch, use `/focus` inside the office.

## Memory: Notebooks and the Wiki

WUPHF ships with built-in memory. No backend choice, no API key, no setup step in the wizard. Every agent gets its own **notebook**, and the team shares a **wiki** — a local git repo of markdown articles at `~/.wuphf/wiki/`. `cat`, `grep`, `git log`, and `git clone` all work.

**The promotion flow:**

1. An agent works on a task and writes raw context, observations, and tentative conclusions to its **notebook** (per-agent, scoped, local to WUPHF).
2. When something in the notebook looks durable (a recurring playbook, a verified entity fact, a confirmed preference), the agent gets a promotion hint.
3. The agent promotes it to the **wiki**. Now every other agent can query it.
4. The wiki points other agents at whoever last recorded the context, so they know who to @mention for fresher working detail.

Nothing is promoted automatically. Agents decide what graduates from notebook to wiki.

The wiki is not just a markdown folder. It is a living knowledge graph: typed facts with triplets, per-entity append-only fact logs, LLM-synthesized briefs committed under the `archivist` identity, `/lookup` cited-answer retrieval, and a `/lint` suite that flags contradictions, orphans, stale claims, and broken cross-references. The web UI gives you a Wikipedia-style reading view, a rich editor with WUPHF-specific inserts, and an AI-assisted maintenance assistant. See [DESIGN-WIKI.md](DESIGN-WIKI.md) for the reading view and [docs/specs/WIKI-SCHEMA.md](docs/specs/WIKI-SCHEMA.md) for the operational contract.

**Onboarding seeds the wiki for you.** The wizard optionally scans your website and any files you point it at, then writes a starter set of company-context articles (about, owner, products) before the first agent turn fires. Your team starts already knowing who you are and what you ship.

**Legacy backends.** Existing installs on Nex or GBrain keep working — backend selection is sticky in `config.json` and there is no forced migration. The CLI flag stays available for power users and for moving off legacy backends:

```bash
wuphf --memory-backend nex      # hosted Nex graph + WUPHF-managed integrations
wuphf --memory-backend gbrain   # local Postgres-backed graph
wuphf --memory-backend none     # no shared wiki; notebooks still work
```

The web wizard no longer surfaces this as a choice. Markdown is the default and the only path for fresh installs.

**Internal naming (for code spelunkers):** the notebook is `private` memory, the wiki is `shared` memory. On the built-in markdown backend the MCP tools are `notebook_write | notebook_read | notebook_list | notebook_search | notebook_promote | team_wiki_read | team_wiki_search | team_wiki_list | team_wiki_write | wuphf_wiki_lookup | run_lint | resolve_contradiction`. On `nex`/`gbrain` the MCP tools are the legacy `team_memory_query | team_memory_write | team_memory_promote`. The two tool sets never coexist on one server instance — backend selection flips the surface.

## Other Commands

The examples below assume `wuphf` is on your `PATH`. If you just built the binary and haven't moved it, prefix with `./` (as in Get Started above) or run `go install ./cmd/wuphf` to drop it in `$GOPATH/bin`.

```bash
wuphf init                    # First-time setup
wuphf share                   # Invite one team member over Tailscale/WireGuard
wuphf shred                   # Delete workspace state and reopen onboarding
wuphf workspace list          # Run multiple isolated offices side by side
wuphf workspace switch <name> # Flip the active workspace
wuphf --1o1                   # 1:1 with the CEO
wuphf --1o1 cro               # 1:1 with a specific agent
```

## Share With a Team Member

Two ways to invite a teammate. Pick the one that fits your network.

**Private network — Tailscale or WireGuard.** Both machines on the same private mesh. The invite never leaves the network and no public interface is exposed:

```bash
wuphf share
```

Or click "Create invite" on the Health Check tile inside the office to mint one without leaving the browser. Send the printed `/join` URL to your teammate. The invite is one use, expires after 24 hours, and the shared web listener only binds to a private-network address by default.

**Public tunnel — no shared network needed.** Click "Start tunnel" on the Health Check tile and WUPHF spins up a Cloudflare quick tunnel. The trycloudflare URL is paired with a 6-digit passcode the joiner has to type before they can land in the office; the join handler is rate-limited per source IP so a leaked URL alone cannot be brute-forced. `cloudflared` is bundled with the npm install (verified against a pinned SHA256 per platform) so the button works on first launch with zero extra setup.

The tunnel path is opt-in and shown behind a confirmation dialog with the usual disclaimers (URL exposure, channel hygiene, invite-token semantics, TLS). Public LAN binds on the network-share path remain blocked unless you pass `--unsafe-lan`.

For the full walkthrough, see [Share WUPHF With a Team Member](docs/tutorials/share-with-team-member.md).

## Publishing skills

Once a team-authored skill exists at `team/skills/<slug>.md`, you can publish it to the public agent-skill commons or pull a community skill back into your wiki. Publish opens a real PR via `gh`; install fetches a public raw `SKILL.md` and installs it as an active skill in the local team wiki.

```bash
# Publish your team's deploy skill to the Anthropic skills marketplace
wuphf skills publish deploy-frontend --to anthropics

# Dry-run the same publish to inspect the manifest + PR body without opening the PR
wuphf skills publish deploy-frontend --to anthropics --dry-run

# Publish to a custom GitHub repo (optionally pinning a non-main branch)
wuphf skills publish deploy-frontend --to github:nex-crm/wuphf-skills
wuphf skills publish deploy-frontend --to github:nex-crm/wuphf-skills@master

# Pull a community skill into your team's wiki
wuphf skills install web-research --from anthropics
```

Supported hubs: `anthropics`, `lobehub`, or any `github:owner/repo[@branch]`. Custom GitHub hubs default to `main` unless a branch is specified. Publish requires `gh auth login` first; install only needs network access since it fetches public raw URLs.

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

If you want WUPHF-created office members to run through OpenClaw instead of bridging pre-existing OpenClaw sessions, enable OpenClaw Gateway's OpenAI-compatible Chat Completions endpoint (`gateway.http.endpoints.chatCompletions.enabled = true`) and use `--provider openclaw-http`. The default endpoint is `http://127.0.0.1:18789/v1` and the default model target is `openclaw/default`; override them with `WUPHF_OPENCLAW_HTTP_BASE_URL` / `WUPHF_OPENCLAW_HTTP_MODEL` or `provider_endpoints.openclaw-http`.

For token-authenticated gateways, WUPHF sends `Authorization: Bearer ...` using `WUPHF_OPENCLAW_HTTP_API_KEY`, `OPENCLAW_GATEWAY_TOKEN`, `WUPHF_OPENCLAW_TOKEN`, or the saved OpenClaw token from Settings, in that order. Requests include a stable OpenAI `user` value derived from the WUPHF agent slug so OpenClaw can reuse the same per-agent session across turns.

## Hermes Agent Runtime

Already running [Hermes Agent](https://github.com/NousResearch/hermes-agent)? Point WUPHF agents at its local OpenAI-compatible API server with `--provider hermes-agent` or set `llm_provider` to `hermes-agent` in config. The default endpoint is `http://127.0.0.1:8642/v1` and the default model name is `hermes-agent`; override them with `WUPHF_HERMES_AGENT_BASE_URL` / `WUPHF_HERMES_AGENT_MODEL` or `provider_endpoints.hermes-agent`.

If your Hermes API server uses `API_SERVER_KEY`, export the same value as `WUPHF_HERMES_AGENT_API_KEY` before starting WUPHF. Authenticated requests get stable `X-Hermes-Session-*` headers per WUPHF agent slug so each office member keeps its own Hermes-side session.

Want to add a new integration? See [docs/ADD-A-TRANSPORT.md](docs/ADD-A-TRANSPORT.md).

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
| Runtimes | Mix Claude Code, Codex, Hermes Agent, and OpenClaw in one channel |
| Memory | Per-agent notebook + shared workspace wiki, git-native markdown by default (no API key needed) |
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
| Markdown wiki is the default for fresh installs (web wizard hides the choice) | ✅ shipped | `internal/config/config.go` (`MemoryBackendMarkdown`), `web/src/components/onboarding/Wizard.tsx` |
| Multi-workspace — run isolated offices side by side, pause/resume per workspace | ✅ shipped | `cmd/wuphf/workspace.go`, `internal/workspaces/` |
| Public-tunnel invite via bundled `cloudflared` (passcode + rate limit) | ✅ shipped | `cmd/wuphf/tunnel.go`, `cmd/wuphf/share_join_guard.go`, `npm/scripts/cloudflared.json` |
| Onboarding wizard with company-context scan (website + files → wiki seed) | ✅ shipped | `web/src/components/onboarding/`, `internal/operations/company_seed.go`, `internal/team/broker_company_seed.go` |
| Live agent event pills + Tier-2 hover peek on the office rail | ✅ shipped | `web/src/components/sidebar/AgentEventPill.tsx`, `internal/team/headless_activity_classifier.go` |
| Wiki rich editor + AI-assisted maintenance assistant | ✅ shipped | `web/src/components/wiki/editor/`, wiki maintenance MCP tools |
| Skills publish/install across public hubs (Anthropic, LobeHub, GitHub) | ✅ shipped | `cmd/wuphf/skills_publish.go` |

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

## Star History

<a href="https://www.star-history.com/?repos=nex-crm%2Fwuphf&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=nex-crm/wuphf&type=date&theme=dark&legend=top-left" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=nex-crm/wuphf&type=date&legend=top-left" />
   <img alt="Star History Chart" src="https://api.star-history.com/chart?repos=nex-crm/wuphf&type=date&legend=top-left" />
 </picture>
</a>
