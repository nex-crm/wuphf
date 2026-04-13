# WUPHF vs Paperclip: Token Efficiency Benchmark

All numbers measured. Same machine, same task, same time window.

## Setup

| | WUPHF + Claude Code | WUPHF + Codex | Paperclip |
|---|---|---|---|
| Version | 0.1.0 | 0.1.0 | 2026.403.0 |
| Model | claude-sonnet-4-6 | gpt-5.3-codex | gpt-5.3-codex |
| Agents | CEO (DM mode) | CEO (DM mode) | CEO (heartbeat) |
| Session | Fresh per turn | Fresh per turn | Fresh (resume available) |
| Tools | 4 (DM-optimized) | 4 (DM-optimized) | All (global) |

## 5-turn CEO DM session

Task: "Tell me about priority N on our roadmap" for N=1..5

### WUPHF + Claude Code (Sonnet 4.6)

| Turn | Context | Cache Read | Cache Create | Fresh | Output |
|------|---------|------------|--------------|-------|--------|
| 1 | 31,017 | 30,421 | 595 | 1 | 1 |
| 2 | 31,136 | 30,552 | 583 | 1 | 1 |
| 3 | 31,293 | 30,746 | 546 | 1 | 1 |
| 4 | 31,382 | 30,766 | 615 | 1 | 1 |
| 5 | 35,835 | 32,953 | 2,881 | 1 | 8 |
| **Total** | **160,663** | **155,438 (97%)** | **5,220** | **5** | **12** |

**API cost: $0.07 for 5 turns.** 97% of context is cache read (1/10th price).

### WUPHF + Codex (gpt-5.3-codex)

| Turn | Input | Cached | Effective | Output | Billed |
|------|-------|--------|-----------|--------|--------|
| 1 | 129,658 | 127,744 | 1,914 | 1,157 | 3,071 |
| 2 | 127,824 | 88,832 | 38,992 | 926 | 39,918 |
| 3 | 214,038 | 175,616 | 38,422 | 1,101 | 39,523 |
| 4 | 127,401 | 126,336 | 1,065 | 642 | 1,707 |
| 5 | 129,256 | 127,232 | 2,024 | 877 | 2,901 |
| **Total** | **728,177** | **645,760 (89%)** | **82,417** | **4,703** | **87,120** |

**Avg billed per turn: 17,424**

### Paperclip + Codex (gpt-5.3-codex) — measured

| Turn | Input | Cached | Effective | Output | Billed |
|------|-------|--------|-----------|--------|--------|
| 1 | 308,375 | 265,472 | 42,903 | 2,263 | 45,166 |
| 2 | 421,516 | 405,888 | 15,628 | 3,334 | 18,962 |
| 3 | 457,852 | 411,648 | 46,204 | 3,883 | 50,087 |
| 4 | 499,719 | 411,904 | 87,815 | 5,275 | 93,090 |
| 5 | 483,069 | 411,648 | 71,421 | 5,585 | 77,006 |
| **Total** | **2,170,531** | **1,906,560 (88%)** | **263,971** | **20,340** | **284,311** |

**Avg billed per turn: 56,862**

## Head-to-head

| Metric | WUPHF Claude | WUPHF Codex | Paperclip |
|--------|-------------|-------------|-----------|
| 5-turn total | **$0.07** | **87,120** billed | **284,311** billed |
| Avg per turn | ~32k ctx (97% cached) | 17,424 billed | 56,862 billed |
| Input trend | **Flat** (31-36k) | **Flat** (127-214k) | **Growing** (308→500k) |
| Cache hit | 97% read | 89% | 88% |
| Idle burn | Zero | Zero | Heartbeat every 30s |
| vs Paperclip | — | **3.3x cheaper** | baseline |

## Why WUPHF wins

### 1. Fresh sessions beat accumulated context

WUPHF starts a clean `codex exec` or `claude --print` per turn. No conversation
history carries over. Paperclip's context grows because each heartbeat run injects
the agent's full inbox, issue history, and comments.

```
Paperclip input per turn: 308k → 422k → 458k → 500k → 483k  (growing)
WUPHF Codex per turn:     128k → 128k → 214k → 127k → 129k  (flat)
WUPHF Claude per turn:     31k →  31k →  31k →  31k →  36k  (flat)
```

### 2. Claude Code's prompt caching is a superpower

Anthropic's prompt cache stores the system prompt + tool definitions across turns.
97% of WUPHF's Claude context is cache read at 1/10th price. This works because
WUPHF's fresh sessions have identical prefixes. Paperclip's growing context breaks
cache prefix alignment.

### 3. Per-role tool sets cut schema overhead

WUPHF registers 4 tools in DM mode (broadcast, poll, human_message, human_interview).
Paperclip loads all tools globally per agent. Fewer tools = smaller schema = faster
cache alignment.

### 4. Zero idle burn

WUPHF agents only spawn when the broker pushes a notification. No heartbeat, no
polling, no LLM invocations while idle. Paperclip's heartbeat runs every 30s.

## The honest claim

> WUPHF + Claude Code: $0.07 for a 5-turn session. 97% prompt cache.
>
> WUPHF + Codex: 3.3x cheaper than Paperclip. Flat cost curve vs linear growth.
>
> Zero idle burn. Paperclip's heartbeat polls the LLM every 30 seconds even
> when nothing is happening.

## What we don't claim

- Claude Code's advantage comes from Anthropic's prompt caching, not our code alone.
- Codex first-turn cost is comparable to Paperclip (both ~40k effective). The gap
  opens on subsequent turns as Paperclip's context grows.
- Paperclip has features we don't: budget enforcement, approval workflows,
  multi-adapter per agent, PGlite database. This benchmark measures token efficiency.

## Reproduce

```bash
# WUPHF + Claude Code
wuphf --pack starter -provider claude-code &
# Send 5 DMs, capture SSE streams, parse message.usage

# WUPHF + Codex
wuphf --pack starter -provider codex &
# Send 5 DMs, capture SSE streams, parse turn.completed.usage

# Paperclip
npx paperclipai run --data-dir /tmp/paperclip-bench &
# Create tasks via API, read heartbeat-runs.usageJson

# Full script: ./scripts/benchmark.sh
```

## Data sources

- WUPHF Claude captures: `/tmp/wuphf-claude-turn{1..5}.txt`
- WUPHF Codex captures: `/tmp/wuphf-accum-turn{1..5}.txt`
- Paperclip: `http://localhost:3100/api/companies/$CID/heartbeat-runs` → usageJson
- Benchmark date: 2026-04-13
- Codex version: 0.118.0, Paperclip version: 2026.403.0, Claude Code version: 2.1.104
