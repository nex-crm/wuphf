# WUPHF Execution Plan

One goal: Paperclip users switch to us immediately.
One method: write the content first, build only what makes it true.

---

## Context (why this plan exists)

Paperclip (github.com/paperclipai/paperclip) is the leading multi-agent orchestrator.
52k GitHub stars. Node.js + React. Created by @cryppadotta. Launched March 2026. It
orchestrates AI agents (Claude Code, Codex, OpenClaw) as a "company" with org charts,
tasks, budgets.

Paperclip got famous because of timing (multi-agent pain was acute) and framing
("zero-human company"). But its GitHub issue tracker reveals massive production pain:

**We cloned Paperclip's source (94k lines TS, 68k lines TSX) and read their issues.**
The research is at `docs/plans/2026-04-11-paperclip-vs-wuphf-grounded.md`. The full
strategy context is at `docs/plans/2026-04-12-wuphf-strategy-vs-paperclip.md`. A
10-persona ICP test is at `docs/plans/2026-04-12-product-experience-test.md`.

This execution plan is the distilled output. Everything else is research context.

### Why people use Paperclip
- They have too many Claude Code terminals open and can't track what's happening
- They want a dashboard to see agent status, costs, tasks
- They want a "company" structure (CEO delegates, specialists execute)
- @dotta's origin story: "running an automated hedge fund, 20+ Claude tabs, no shared
  context, no cost tracking, no state recovery"

### Why people are frustrated with Paperclip (from their issue tracker)
- **Issue #544:** 10x token consumption vs running Claude Code directly. User debugged
  and found 3 root causes: (1) `--resume` accumulates full session history across runs,
  ~70% of waste, (2) every agent inherits ALL 12 MCP servers globally, ~24k tokens/turn
  overhead, (3) heartbeat polls LLM to learn "nothing to do"
- **Issue #3335:** 222 workspaces across 8 agents, ALL pointing at the same working
  directory. Agents corrupt each other's work. Fix exists in code but gated behind 3
  experimental flags. Default is still broken.
- **Issue #3401:** Heartbeat burns tokens on empty inbox. Maintainer confirmed:
  "This is a legitimate architectural concern."
- **Issue #1756:** Budget tracks dollars, not tokens. Claude Enterprise/Pro users
  track tokens. Budget UI is useless to them.

### What WUPHF already has that Paperclip doesn't
- **Shared office with channels** — agents work in a Slack-like channel, see each
  other's output. Paperclip explicitly rejected chat ("Tasks + comments only").
- **Push-driven wakes** — agents only fire when broker pushes a notification. No polling.
- **Fresh sessions per turn** — no `--resume` accumulation. Each turn starts clean.
- **CEO delegation mode** (PR #25, merged) — CEO routes, specialists only wake when
  assigned. Same model as Paperclip but without the token overhead.
- **Per-agent model selection** — CEO gets Opus, specialists get Sonnet (configurable).
- **Nex integration** — knowledge graph for CRM, email, calendar context. Paperclip
  has no equivalent world model.
- **Terminal-first TUI + web view** — both surfaces on one broker.
- **Go single binary** — vs Paperclip's Node.js server + React app.

### What's missing (the 10 items in this plan)
The architectural advantages exist but some aren't fully wired or visible to users.
This plan builds only the 10 things needed to make every claim in our Reddit post true.

### How the content strategy works
We wrote the exact Reddit post, Show HN post, X thread, Product Hunt launch, and
landing page FIRST. Each piece makes specific claims about the product. The claims
audit table under each piece lists what must be true before publishing. The features
we build are reverse-engineered from those claims. If a claim isn't in the content,
we don't build the feature. If a feature doesn't support a claim, we don't build it.

---

## The ICP (one sentence)

Claude Pro/Max users running 3+ agents who are hitting rate limits and
can't see what their agents are doing.

## The pain (from real Paperclip users, real issue numbers)

1. **Token burn** — 10x consumption vs alternatives (issue #544). Root causes:
   session resume accumulation, global MCP inheritance, LLM polling.
2. **Workspace corruption** — 8 agents, 222 workspaces, 1 cwd (issue #3335).
3. **No visibility** — can't see agents working, can't steer mid-task.
4. **No cost tracking** — dollar budgets, no per-task token visibility (issue #1756).

## The switch trigger (what makes them move)

A Reddit post with real benchmark numbers showing the same workload running on
dramatically fewer tokens, with visible agents and zero workspace corruption.
Plus a one-command import of their existing setup. The discovery hook is TOKEN
COST (what makes them click). The product aha is VISIBILITY (what makes them stay).
These are different stages of the funnel — cost draws them in, visibility converts.

---

## 10 things to build (nothing else)

### Item 1: CEO on Sonnet by default
**File:** `internal/team/headless_claude.go:152-157`
**Current:** `headlessClaudeModel()` returns `claude-opus-4-6` for `officeLeadSlug()`, `claude-sonnet-4-6` for everyone else.
**Change:** Return `claude-sonnet-4-6` for ALL agents by default. Add `--opus-ceo` CLI flag or config option to upgrade the lead to Opus when the user explicitly wants it.
**Why:** Paperclip's ICP is Claude Pro/Max users. Opus burns Pro quota fast. Sonnet for routing decisions is sufficient.

### Item 2: Delegation mode as default
**File:** `internal/team/launcher.go` — the focus mode logic from PR #25 (`codex/focus-mode`).
**Current:** Office mode (all agents see all messages) is default. Focus mode is opt-in via `--focus` flag.
**Change:** Flip. Delegation mode (focus) is default. Agents only wake when CEO delegates or human tags directly. Add `--collab` flag to start in collaborative mode.
**Also:** `cmd/wuphf/channel.go:843,2015,4679` — the `/focus` slash command handlers. Keep them but also add `/collab` as the inverse toggle.
**Also:** `web/index.html` — add `/collab` to the slash command list.

### Item 3: `/collab` command
**Files:** `cmd/wuphf/channel.go` (TUI handler), `web/index.html` (web handler), `internal/team/session_mode.go` (mode constants).
**Current:** `/focus` toggles focus mode on. No inverse command exists.
**Change:** Add `/collab` that disables focus mode (enables collaborative mode). Both `/focus` and `/collab` are toggles to their respective modes.

### Item 4: Per-agent MCP scoping
**File:** `internal/team/launcher.go:2634` — `ensureMCPConfig()` generates ONE mcp config used by ALL agents.
**File:** `internal/team/headless_claude.go:38` — `"--mcp-config", l.mcpConfig` passes the global config.
**Change:** Generate a per-agent mcp config at turn start. Each agent's config includes only the MCP servers relevant to its role/expertise. Write to `/tmp/wuphf-mcp-<slug>.json`. Pass that per-agent path in `--mcp-config`.
**Impact:** Saves ~24k tokens/turn for agents with unnecessary tool definitions. This is Paperclip's #2 token killer (issue #544).

### Item 5: Cost tracking (per-task, per-agent, tokens)
**File:** `internal/team/broker.go:283-301` — `teamUsageState` and `usageTotals` structs already exist.
**File:** `internal/team/headless_claude.go:73-104` — `ReadClaudeJSONStream` callback already receives token usage in the result.
**Change:** After each headless turn completes, write `{agent, provider, model, input_tokens, output_tokens, cached_tokens, cost_cents, task_id, occurred_at}` to broker state via the existing `teamUsageState`. Expose via broker HTTP API for web view.
**Web:** Add a cost panel to `web/index.html` showing per-agent, per-task token usage.

### Item 6: Workspace isolation as default
**File:** `internal/team/worktree.go` (100 lines) — `defaultPrepareTaskWorktree()` and `defaultCleanupTaskWorktree()` already exist.
**Change:** When a task is assigned to an agent and the task involves code changes, auto-create a git worktree via `prepareTaskWorktree(taskID)`. Branch: `wuphf-<taskID>`, path: `/tmp/wuphf-task-<taskID>/`. Pass the worktree path as the agent's working directory.
**Default:** ON for any task assigned to agents with coding-related expertise (fe, be, ai, tech-lead, qa). OFF for non-coding agents (cmo, cro, designer) unless explicitly requested.

### Item 7: Live agent streaming in web view
**File:** `web/index.html` — current agent panel shows static info (name, role, skills, toggle, edit).
**File:** `internal/team/headless_claude.go:66-103` — `updateHeadlessProgress()` already broadcasts progress events (thinking, text, tool_use, tool_result) to broker state.
**Change:** When user clicks an agent in the web sidebar while the agent is active, open a streaming panel that shows the real-time progress events. Use SSE or polling against the broker's `/health` or a new `/agent-stream/<slug>` endpoint. Show: current phase (thinking/tool_use/text), tool name, truncated input/output.

### Item 8: Lightweight DM (no /1o1 shutdown)
**File:** `web/index.html` — current 1:1 mode (`enterDMMode` at line ~5496) hides the main messages div, shows a separate dm-messages div, and POSTs to `/session-mode` changing the broker to 1o1 mode. This shuts down the office.
**File:** `internal/team/broker.go:2346` — `handleSessionMode` changes `b.sessionMode` which affects ALL message routing.
**Change:** Add a lightweight DM that does NOT change session mode. Instead: user clicks agent → a DM input appears in the detail panel or a sidebar overlay → message is posted to broker as a direct message to that agent (tagged with `to: <slug>`) → agent receives it as a steer/follow-up → office keeps running in current mode. The `/1o1` full-mode-switch stays for deep sessions.

### Item 9: `wuphf import --from <path>`
**New file:** `cmd/wuphf/import.go` (~400 lines)
**What it does:**
1. Reads Paperclip's PGlite DB or JSON state from the given path
2. Maps: companies → packs, agents → officeMembers (slug, name, role, adapterType), issues → tasks (title, status, owner)
3. Rewrites adapter configs (claude_local → claude, codex_local → codex)
4. Imports cost history if available
5. Writes to `~/.wuphf/` state files
6. Prints: "Imported N agents, M tasks. Office running at :7891"

### Item 10: Prebuilt binary (curl install)
**New file:** `.goreleaser.yml`
**New file:** `scripts/install.sh` — curl-downloadable installer
**What it does:** goreleaser builds binaries for darwin-arm64, darwin-amd64, linux-amd64, linux-arm64. The install script detects platform, downloads the right binary, puts it in PATH.
**Enables:** `curl -sSL https://wuphf.dev/install | sh`

### Housekeeping (do on Day 1)
- **LICENSE file:** Add MIT LICENSE to repo root.
- **Verify no --resume:** Confirm `headless_claude.go:29-40` args list does not include `--resume`. Document as invariant.
- **Verify Claude Pro:** Test that `claude --print` with Pro subscription works in headless mode.
- **Delete A2UI:** Remove `internal/tui/generative.go` (269 lines), `generative_registry.go` (294), `generative_test.go` (465). Remove `renderA2UIBlocks` + `isA2UIType` from `cmd/wuphf/channel.go:6321-6431`. Remove `/generative` case from `internal/tui/stream.go:750-792`. Remove 57 A2UI references from `web/index.html` (CSS classes `.a2ui-*` at lines 768-784, rendering code at lines 3359-3365 and 6301+).
- **3-agent minimal pack:** Add a "solo" or "starter" pack to `internal/agent/packs.go` with CEO + 2 specialists (e.g., CEO + Frontend + Backend). Make it the default when agent count is not specified.

---

## Content (this is the spec)

Each piece below is a promise to real people. Every claim must be true
before that piece publishes. The claims audit after each piece IS the
feature requirement.

### Reddit post — the discovery hook

**Where:** r/ClaudeAI, r/LocalLLaMA, r/selfhosted

**Title:**
"I built an open-source multi-agent office that fixes the 3 things
that make Paperclip burn 10x tokens"

**Body:**
```
If you're running 3+ Claude Code agents through Paperclip (or any
orchestrator), you've probably noticed the token bill climbing fast.

I dug into why. Three root causes:

1. Session resume. Paperclip uses --resume to continue sessions across
   runs. Every wake carries the full conversation history. 13 sessions
   deep = millions of cached tokens re-read every turn. (~70% of waste
   per user debugging in issue #544)

2. Global MCP inheritance. Every agent loads ALL your MCP servers.
   12 servers = 240 tool definitions = ~24,000 tokens of overhead on
   every single turn of every agent. Your backend agent is paying for
   your Google search MCP.

3. LLM polling. The heartbeat wakes agents on a timer. When there's
   nothing to do, the agent burns tokens learning "nothing to do."
   (Confirmed as legitimate by a Paperclip maintainer in issue #3401)

I built an alternative that fixes all three by design:
- Fresh sessions per turn (no accumulation)
- Per-agent tool scoping (each agent loads only its tools)
- Push-driven wakes (no polling, no empty-inbox token burn)

Plus: agents work in a shared channel (like Slack) so they see each
other's output. You can click any running agent and watch their tool
calls stream in real-time. DM them mid-task to steer — no restart,
the office keeps running.

Default is delegation mode (CEO routes work, specialists execute —
same model as Paperclip, just cheaper). Type /collab and agents start
coordinating with each other.

One command to import your existing Paperclip setup. Go binary,
self-hosted, MIT. Runs on Claude Pro (CEO on Sonnet, not Opus).

Benchmark: [same workload, real token numbers, side by side]

[link to repo]
```

**What must be true before this publishes:**

| Claim | Status |
|-------|--------|
| Fresh sessions per turn | VERIFY (no --resume in args) |
| Per-agent tool scoping | BUILD (~30 lines) |
| Push-driven wakes | TRUE |
| Agents in shared channel | TRUE |
| Click agent, see streaming | BUILD (web view panel) |
| DM mid-task, no restart | BUILD (lightweight DM) |
| Delegation mode default | FLIP (1 line) |
| /collab command | BUILD |
| Import existing setup | BUILD (~400 lines) |
| CEO on Sonnet | CHANGE (1 line) |
| Runs on Claude Pro | VERIFY |
| Benchmark numbers | RUN THE BENCHMARK |
| Go binary, curl install | BUILD (goreleaser) |

---

### Show HN — the technical credibility

**Title:** "Show HN: Open-source multi-agent office — agents share a channel,
you watch them work and steer mid-flight"

**First paragraph:**
```
I had too many Claude Code terminals open and couldn't tell which one
was doing what. Built an office where agents work in a shared channel,
delegate through a CEO, and you can see every tool call in real-time.
Click any agent, DM them mid-task. No restart.

Default: delegation mode (CEO routes, specialists execute — quiet).
/collab: agents coordinate with each other.

Go binary. Self-hosted. MIT. Fresh sessions per turn, per-agent tool
scoping. Runs on Claude Pro.
```

Same claims, same requirements.

---

### X thread — the viral version (6 tweets)

```
1/ Too many Claude tabs open. Can't tell which agent is doing what.

Built a shared office. Here's what changed.

2/ Three things burning your multi-agent tokens:
- Session resume (history accumulates across runs — 70% of waste)
- Global MCP (every agent loads all 240 tool definitions — 24k tokens/turn)
- LLM polling (agent burns tokens learning "nothing to do")

3/ Fix: fresh sessions, per-agent tools, push-driven wakes.

But the real thing: agents share a channel. They see each other's work.
Click any agent → watch tool calls live → DM them mid-task.
No restart. Office keeps running.

4/ Default: delegation mode. CEO routes. Quiet.
/collab: agents talk to each other. CEO suggests it when tasks overlap.

5/ Same workload as [comparable setup]. [X]% fewer tokens.
Because the architecture doesn't waste tokens on confusion.

6/ Open source. Go binary. Self-hosted. MIT.
Runs on Claude Pro (CEO on Sonnet).
One command to import your existing setup.

[link]
```

---

### Product Hunt — the polished launch

**Tagline:** "See your AI agents work. Steer them mid-flight."

**Description:** Same claims as Reddit, shorter format.

**Maker comment:** "Built this because I had too many Claude tabs.
The moment agents shared a channel, everything changed."

---

### Landing page — the conversion surface

**Hero:** "See your AI agents work. Steer them mid-flight."
**Subhead:** "A shared office. Fresh sessions. Per-agent tools. No token waste."
**CTA:** "Watch the 2-minute demo"
**Demo:** Video of clicking agent → streaming → DM mid-task → cost panel.

**Comparison table (no competitor name):**

```
                     Ticket queue    Shared office
See agents working   No              Yes, real-time
Steer mid-task       Kill & restart  DM, no restart
Session tokens       Accumulate      Fresh per turn
Tool definitions     All agents, all Per-agent scoped
Empty inbox cost     Burns tokens    Zero (push-only)
Import existing      —               One command
```

---

### Technical blog — the earned claim (publishes LAST)

**Title:** "Same 6-agent workload, [X]% fewer tokens — here's why"
**Requires:** The benchmark. Real numbers. No theory.

---

## Publishing sequence

| When | What publishes | What must be true by then |
|------|---------------|--------------------------|
| After Week 1 | X thread | Items 1-5 (CEO Sonnet, delegation default, /collab, per-agent MCP, cost tracking) |
| After Week 2 | Reddit post, Show HN | Items 6-8 (workspace isolation, streaming, lightweight DM) |
| After Week 3 | Landing page, YouTube demo | Items 9-10 (import command, curl install) |
| After Week 4 | Product Hunt, technical blog | Benchmark with real numbers |

---

## What we are NOT building (noise, explicitly cut)

- Dorsey's four-layer framework implementation
- AutoResearch-style improvement loops
- Routines / cron system (ericosiu's 48 crons)
- Self-healing cron doctor
- Personal agent model (one agent per team member)
- Save-as-skill
- Agent Companies protocol
- Plugin system
- Goals/projects/milestones hierarchy
- Chain-of-thought fold in web chat
- PropertiesPanel render-slot
- logActivity invariant / unified audit
- Approval state machines
- Org chart visualization
- Multi-tenant / white-label
- Nex as "strategic core" (it's an integration, not the pitch)
- A2UI or workflow runtime
- Email postmortem as separate direction

All of these are either future work (after people switch) or intellectual
context that helped us think but is not the execution plan.

---

## The research context (archived, not the plan)

The following docs contain the research that led to this execution plan.
They are reference material, not action items:

- `docs/plans/2026-04-11-paperclip-vs-wuphf-grounded.md` — source code comparison
- `docs/plans/2026-04-12-wuphf-strategy-vs-paperclip.md` — full strategy context
- `docs/plans/2026-04-12-product-experience-test.md` — 10-persona ICP panel

The strategy doc (718 lines + 467 lines of content plan) was valuable for
arriving at the 10-item execution list. The execution list is 10 items.
