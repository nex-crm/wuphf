# WUPHF Execution Plan

One goal: Paperclip users switch to us immediately.
One method: write the content first, build only what makes it true.

---

## The ICP (one sentence)

Claude Pro/Max users running 3+ agents who are hitting rate limits and
can't see what their agents are doing.

## The pain (from Paperclip's issue tracker, filed by real users)

1. **Token burn** — 10x consumption vs alternatives (#544). Root causes:
   session resume accumulation, global MCP inheritance, LLM polling.
2. **Workspace corruption** — 8 agents, 222 workspaces, 1 cwd (#3335).
3. **No visibility** — can't see agents working, can't steer mid-task.
4. **No cost tracking** — dollar budgets, no per-task token visibility (#1756).

## The switch trigger (what makes them move)

A Reddit post with real benchmark numbers showing the same workload
running on dramatically fewer tokens, with visible agents and zero
workspace corruption. Plus a one-command import of their existing setup.

---

## 10 things to build (nothing else)

| # | What | Work | Unblocks |
|---|------|------|----------|
| 1 | CEO on Sonnet by default | 1-line change | Claude Pro compatibility |
| 2 | Delegation mode as default | Flip in launcher | Familiar feel for switchers |
| 3 | `/collab` command | Add inverse of `/focus` | The upgrade path |
| 4 | Per-agent MCP scoping | ~30 lines Go | The token claim |
| 5 | Cost tracking (per-task, per-agent, tokens) | Hook budget infra to headless turns | The "I can see what it costs" claim |
| 6 | Workspace isolation as default | Wire existing worktree code | The "nothing corrupts" claim |
| 7 | Live agent streaming in web view | Streaming panel on agent click | The product aha |
| 8 | Lightweight DM (no /1o1 shutdown) | Sidebar DM while office runs | The "steer mid-flight" claim |
| 9 | `wuphf import --from <path>` | ~400 lines Go | Zero switch cost |
| 10 | Prebuilt binary (curl install) | goreleaser | No Go required |

Plus: LICENSE file, verify no --resume, verify Claude Pro works, delete A2UI,
3-agent minimal default pack.

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
