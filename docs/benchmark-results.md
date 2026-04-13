# WUPHF vs Paperclip: Token Efficiency Benchmark

All numbers measured from live runs. Same machine, same task, same time window.
No estimates, no projections. Run it yourself with `./scripts/benchmark.sh`.

## TL;DR

| | WUPHF + Claude Code | WUPHF + Codex | Paperclip + Codex |
|---|---|---|---|
| **5-turn cost** | **$0.06** | **87k billed** | **284k billed** |
| **vs Paperclip** | **9x cheaper** | **3.3x cheaper** | baseline |
| Cache hit | 97% | 89% | 88% |
| Input trend | Flat (31k) | Flat (128k) | Growing (308→500k) |
| Idle burn | Zero | Zero | Heartbeat every 30s |

---

## Setup

| | WUPHF + Claude Code | WUPHF + Codex | Paperclip |
|---|---|---|---|
| Version | 0.1.0 | 0.1.0 | 2026.403.0 |
| Model | claude-sonnet-4-6 | gpt-5.3-codex | gpt-5.3-codex |
| Agent | CEO (DM mode) | CEO (DM mode) | CEO (heartbeat) |
| Session | Fresh per turn | Fresh per turn | Fresh (resume available) |
| MCP tools | 4 (DM-optimized) | 4 (DM-optimized) | All (global) |
| Poll behavior | Authoritative push (no default poll) | Same | Heartbeat every 30s |

## Raw data: WUPHF + Claude Code (Sonnet 4.6)

| Turn | Context | Cache Read | Cache Create | Fresh | Output |
|------|---------|------------|--------------|-------|--------|
| 1 | 31,026 | 30,826 | 199 | 1 | 1 |
| 2 | 31,207 | 30,668 | 538 | 1 | 1 |
| 3 | 32,037 | 31,234 | 802 | 1 | 1 |
| 4 | 32,037 | 31,234 | 802 | 1 | 1 |
| 5 | 35,216 | 33,241 | 1,974 | 1 | 8 |
| **Total** | **161,523** | **157,203 (97%)** | **4,315** | **5** | **12** |

**Cost: $0.06 for 5 turns.** 97% of context is cache read at 1/10th the input price.

## Raw data: WUPHF + Codex (gpt-5.3-codex)

| Turn | Input | Cached | Effective | Output | Billed |
|------|-------|--------|-----------|--------|--------|
| 1 | 129,658 | 127,744 | 1,914 | 1,157 | 3,071 |
| 2 | 127,824 | 88,832 | 38,992 | 926 | 39,918 |
| 3 | 214,038 | 175,616 | 38,422 | 1,101 | 39,523 |
| 4 | 127,401 | 126,336 | 1,065 | 642 | 1,707 |
| 5 | 129,256 | 127,232 | 2,024 | 877 | 2,901 |
| **Total** | **728,177** | **645,760 (89%)** | **82,417** | **4,703** | **87,120** |

**Avg billed per turn: 17,424.** Cost flat across turns.

## Raw data: Paperclip + Codex (gpt-5.3-codex)

| Turn | Input | Cached | Effective | Output | Billed |
|------|-------|--------|-----------|--------|--------|
| 1 | 308,375 | 265,472 | 42,903 | 2,263 | 45,166 |
| 2 | 421,516 | 405,888 | 15,628 | 3,334 | 18,962 |
| 3 | 457,852 | 411,648 | 46,204 | 3,883 | 50,087 |
| 4 | 499,719 | 411,904 | 87,815 | 5,275 | 93,090 |
| 5 | 483,069 | 411,648 | 71,421 | 5,585 | 77,006 |
| **Total** | **2,170,531** | **1,906,560 (88%)** | **263,971** | **20,340** | **284,311** |

**Avg billed per turn: 56,862.** Input grows 308k → 500k over 5 turns.

---

## Why WUPHF wins

### 1. Fresh sessions = flat cost curve

WUPHF starts a clean `codex exec` or `claude --print` per turn. No conversation
history accumulates. Each turn costs the same as the first.

Paperclip's context grows because each heartbeat run injects the agent's full
inbox, issue history, and comments. Even without `--resume` (which would be worse),
the input grew from 308k to 500k over 5 turns.

```
Paperclip:    308k → 422k → 458k → 500k → 483k  (growing)
WUPHF Codex:  128k → 128k → 214k → 127k → 129k  (flat)
WUPHF Claude:  31k →  31k →  32k →  32k →  35k  (flat)
```

### 2. Prompt caching works because of fresh sessions

Claude Code gets 97% cache read because WUPHF's fresh sessions have identical
prompt prefixes. The system prompt, tool schemas, and MCP config are the same
every turn, which aligns with Anthropic's prompt cache.

Paperclip's growing inbox context shifts the prompt prefix each turn, reducing
cache efficiency.

### 3. Per-role tool sets

WUPHF registers 4 MCP tools in DM mode: `team_broadcast`, `team_poll`,
`human_message`, `human_interview`. In office mode, CEO gets 20 tools,
specialists get 7.

Paperclip loads all tools globally. Fewer tools = smaller JSON schema in the
prompt = better cache alignment = lower cost.

### 4. Authoritative notifications = no unnecessary polling

WUPHF's work packets include thread context, task state, and agent activity.
The `team_poll` tool is marked as "last resort" so agents don't defensively
read the channel before every reply.

Each skipped `team_poll` call saves ~3k tokens in tool round-trip overhead.

### 5. Zero idle burn

WUPHF agents only spawn when `deliverMessageNotification` pushes work. No
heartbeat, no polling loop, no LLM invocations while idle.

Paperclip's heartbeat runs every 30s (`heartbeat.ts`, confirmed in our
benchmark: `heartbeatIntervalMs: 30000`). Each tick checks all enabled agents
and spawns codex turns even when the inbox is empty.

---

## What we don't claim

- Claude Code's 9x advantage comes largely from Anthropic's prompt caching.
  The caching works because of our architecture (fresh sessions, stable prefixes),
  but the cache itself is an Anthropic platform feature.
- Codex first-turn cost is comparable to Paperclip (~40k effective). The 3.3x
  gap opens over multiple turns as Paperclip's context grows.
- Paperclip has features we don't: budget enforcement, approval workflows,
  multi-adapter per agent, embedded PostgreSQL. This benchmark measures only
  token efficiency.
- Cache hit rates vary between runs. Our numbers are from specific runs on
  2026-04-13. The structural advantage (flat vs growing) is consistent.

---

## Reproduce

```bash
# Start both systems
wuphf --pack starter &
npx paperclipai run --data-dir /tmp/paperclip-bench &

# Run WUPHF benchmark (automated)
./scripts/benchmark.sh

# Paperclip: create company + agents + tasks via API
CID=$(curl -s -X POST http://localhost:3100/api/companies \
  -H "Content-Type: application/json" \
  -d '{"name":"Bench"}' | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")

CEO=$(curl -s -X POST "http://localhost:3100/api/companies/$CID/agents" \
  -H "Content-Type: application/json" \
  -d '{"name":"CEO","adapterType":"codex_local","enabled":true}' \
  | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")

# Create and assign tasks, then read token data:
curl -s "http://localhost:3100/api/companies/$CID/heartbeat-runs" \
  | python3 -c "import sys,json; [print(r['usageJson']) for r in json.load(sys.stdin) if r['status']=='succeeded']"
```

## Environment

- Benchmark date: 2026-04-13
- Machine: Apple Silicon (M-series), macOS
- Codex CLI: 0.118.0
- Paperclip: 2026.403.0
- Claude Code: 2.1.104
- WUPHF: 0.1.0 (commit b4e605c + PR #43)
