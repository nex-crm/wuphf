# WUPHF Product Experience Test — ICP Panel Results

Date: 2026-04-12. Branch: nazz/strategy/multi-agent-positioning.
10 personas, deep immersion, 9-scene demo, all read the actual codebase.

## The Aha Moment Map

| Persona | Role | Aha Scene | What Made Them Say "Holy Shit" |
|---------|------|-----------|-------------------------------|
| Alex | Solo founder, Paperclip user | Scene 5 | Mid-stream DM to agent without restart — "a different way of working with agents" |
| Sam | Platform engineer | Scene 5+6 | Streaming (I can SHOW my VP) + cost dashboard (I can PROVE it) |
| Ryan | Agency founder | Scene 8 | /collab — "watch your AI team brainstorm" is the sentence that sells clients |
| Maya | Indie hacker | Scene 4 | CEO auto-delegation — but immediately followed by cost alarm |
| David | CTO, 30-person startup | Scene 4+5 | Worktree isolation + gossip awareness — "two agents literally cannot conflict" |
| Lena | DevRel, 50k followers | Scene 5 | "The first AI tool I've seen that treats observability as a first-class feature" |
| Jordan | Freelancer, $50/mo | Scene 4+5 | Parallelism potential — "dump 5 tasks and walk away" |
| Chris | OSS maintainer | Scene 4 | Delegator parsing @mentions — "not a chatbot pretending to delegate, a real routing system" |
| Nina | Non-technical marketer | N/A | Bounced at Scene 1 landing page — correct filtering |
| Marco | ML engineer, AutoResearch | Scene 8 | Credibility-weighted adoption scoring — "exactly what I've been building badly with bash" |

**CONVERGENCE: Scene 5 (live streaming + mid-flight DM) is the #1 aha.** 5 of 9 active
personas identified it. Scene 8 (/collab) is #2 with 2 personas.

## The Natural Language Library

### How each persona described WUPHF (their words, not ours)

| Persona | Their Slack Message | Their One-Sentence Definition |
|---------|--------------------|-----------------------------|
| Alex | "being able to DM an agent while it's working and have it adjust on the fly" | "a self-hosted AI agent workspace that replaces batch-style orchestration with a persistent, observable, interactive office" |
| Sam | "Finally, an AI dev tool I can justify to my CFO" | "Slack but your teammates are AI agents with access to your codebase" |
| Ryan | "I'm thinking about whether we could run one per client as a managed service" | "AI Slack with a built-in team" |
| Maya | "honestly really cool, but it burns through Claude Pro limits insanely fast" | (bounced on cost) |
| David | "when the backend agent starts working, the frontend agent sees it and can coordinate" | (gave detailed technical breakdown instead) |
| Lena | "Most multi-agent tools are ticket queues wearing a dashboard. WUPHF is a shared office" | "a shared office where agents see each other's work" |
| Jordan | "like having a tiny dev shop in your terminal" | "A tmux-based multi-agent office where a CEO agent delegates and you watch them all work" |
| Chris | "tmux but your panes are AI agents with a shared Slack-like channel between them" | (gave trust analysis instead) |
| Marco | "an agent observatory with a communication bus" | (gave technical deep-dive instead) |

### The Word-of-Mouth Sentences (most viral-potential)

1. **Lena:** "Most multi-agent tools are ticket queues wearing a dashboard. WUPHF is a shared office."
2. **Alex:** "actually lets you see and correct what your agents are doing mid-execution"
3. **Jordan:** "like having a tiny dev shop in your terminal"
4. **Chris:** "tmux but your panes are AI agents with a shared Slack-like channel"
5. **Marco:** "an agent observatory with a communication bus"
6. **Sam:** "the first AI tool where I can track cost per task"

## Convergence Analysis

### What ALL personas agree the product IS
Every active persona converged on two words: **visible** and **shared**.
- Visible: you can see what agents are doing in real-time
- Shared: agents see each other's work

The product is NOT "cheaper Paperclip." NOT "better orchestration." It IS:
**A visible, shared office for AI agents.**

### Where definitions diverge
- Alex/David/Chris frame it as **engineering infrastructure** (workspace, orchestration)
- Ryan frames it as **a productizable service** (white-label, per-client)
- Marco frames it as **a research tool** (observatory, communication bus)
- Sam frames it as **an ROI justification** (cost tracking, VP pitch)
- Jordan/Maya frame it as **overkill** (too many agents, too expensive for solo use)

### Which aha moment is most common?
**Scene 5: live streaming + mid-flight DM.** This should be the homepage hero demo/animation.
NOT a token savings chart. NOT a feature grid. A video of someone DMing an agent mid-task
and watching it adjust. That's the conversion moment.

### Which scene causes most drop-offs?
**Scene 6 (token cost) — for budget-constrained users.** Maya and Jordan both hit the wall
here. No cost visibility = can't trust it with a limited budget. Sam also flagged it as
critical for ROI justification.

**Scene 3 (first look) — for everyone.** If the first impression feels like a toy or
gimmick, Alex said he'd close the tab. The UI polish matters for first 5 seconds.

## ICP Segmentation (from persona reactions)

### PRIMARY ICP: Technical founders/CTOs running 3+ agents
**Who:** Alex, David, Chris. $100-200/mo budget. API keys, not Pro subscription.
**What they want:** Visibility, coordination, workspace isolation, audit trail.
**What converts them:** Scene 5 (streaming + DM).
**What retains them:** Compounding Nex context over time.

### SECONDARY ICP: Platform engineers at funded startups
**Who:** Sam. $500/mo pilot budget. Needs ROI proof for skeptical VP.
**What they want:** Per-task cost tracking, full observability, self-hosted.
**What converts them:** Scene 5+6 (streaming + cost dashboard).
**What retains them:** The cost dashboard data itself — it's the justification.

### EXPANSION ICP: Agency founders (white-label)
**Who:** Ryan. Would pay $500/mo base + per-client. $6k/year revenue per install.
**What they want:** Removable branding, multi-tenant, per-client cost reporting.
**What converts them:** Scene 8 (/collab) — "watch your AI team brainstorm" is client demo.
**What retains them:** Client lock-in through compounding Nex context.

### ASPIRATIONAL ICP: ML engineers / researchers
**Who:** Marco. Wants swarm topology, programmatic API, 10+ concurrent agents.
**What they want:** No CEO bottleneck, API control, dynamic packs.
**What converts them:** Scene 8 (adoption.go credibility scoring).
**What retains them:** Gossip bus as a research coordination primitive.

### NOT ICP (confirmed by panel):
- **Solo freelancers on Claude Pro** (Maya, Jordan) — budget too tight, product is overkill
- **Non-technical marketers** (Nina) — wrong product, correct filtering on landing page
- **Anyone without API keys** — the cost model doesn't work on flat-rate subscriptions

## Trust Ratings

| Persona | Trust (1-10) | Key Factor |
|---------|-------------|------------|
| Alex | 5 | Need to verify with own hands |
| Sam | 6 | Observability shows engineering maturity |
| Ryan | 7 | Architecture is close to what he needs |
| Lena | 7.5 (recommend) / 9 (honesty) / 4 (claim) | "Code is ahead of marketing, right order" |
| David | 8+ | All technical concerns addressed by architecture |
| Chris | 7 | Trust orchestration layer; distrust cloud integrations |
| Jordan | 5 | Trust architecture, distrust cost model |
| Maya | 4 | Wrong audience |
| Marco | 7 | "Genuinely novel mechanism" |
| Nina | N/A | Bounced |

## Willingness to Pay

| Persona | WTP | Condition |
|---------|-----|-----------|
| Alex | $0 binary + $50-100/mo Nex | 60 days of verified token savings |
| Sam | $40-60/seat/mo (12 devs) | SOC 2 timeline, self-hosted confirmed |
| Ryan | $500/mo base white-label | Multi-tenant isolation, removable branding |
| Maya | $0 | Product doesn't fit her budget |
| David | Not specified but implied significant | Works as described |
| Jordan | $0-15/mo | Hard budget cap, cost tracking, CEO on Sonnet |
| Chris | $50/mo | GitHub-native integration, LICENSE file added |
| Marco | $50-100/mo | Dynamic packs, swarm mode, API |

## Critical Feature Gaps (ranked by persona frequency)

| Gap | Flagged By | Impact |
|-----|-----------|--------|
| **Cost tracking (per-task, per-agent)** | Alex, Sam, Jordan, Maya, Lena | #1 blocker for adoption. "Plumbing without water." |
| **Budget hard cap** ("stop at $X") | Jordan, Maya, Sam | Safety net for budget-constrained users |
| **White-label branding** | Ryan | $150/mo tool vs $6k/yr revenue line |
| **CEO model configurable** (Sonnet option) | Jordan, Maya | CEO on Opus is too expensive for small setups |
| **Solo/minimal mode** (2 agents) | Maya, Jordan | Default 5-8 agents is overkill for solo builders |
| **Prebuilt binary / brew install** | Maya, Chris | Go build from source excludes non-Go developers |
| **LICENSE file** | Chris | "Will not recommend without explicit license" |
| **Programmatic API** | Marco | Needed for CI/CD integration and scripted orchestration |
| **Multi-tenant isolation** | Ryan | Per-client data isolation for agencies |
| **Dynamic pack loading** (YAML/JSON) | Marco | Custom agent topologies without recompiling Go |

## Derived Messaging

### Homepage headline (from natural language convergence)

**Option A (from Lena's tweet — strongest single line):**
> "Most multi-agent tools are ticket queues wearing a dashboard."
> "WUPHF is a shared office. Watch your agents work. Steer them mid-flight."

**Option B (from Alex's aha — most visceral):**
> "See what your AI agents are doing. DM them while they work."
> "One office. Delegation mode for focus. Collab mode for coordination. 70% fewer tokens."

**Option C (from Sam's VP pitch — most business):**
> "The first AI dev tool you can justify to your CFO."
> "Per-task cost tracking. Full observability. Self-hosted."

**Option D (from Chris's description — most technical):**
> "tmux but your panes are AI agents with a shared channel between them."

### CTA
"Watch the 2-minute demo" → Scene 5 (streaming + DM) as the hero video.
NOT "Sign up" (there's nothing to sign up for — it's self-hosted).
NOT "Read the docs" (too passive).

### Channel-specific messaging

**Show HN:** Option D — "tmux but your panes are AI agents with a shared channel"
**Reddit (r/ClaudeAI, r/LocalLLaMA):** Option A — "Most multi-agent tools are ticket queues"
**X/Twitter:** Option B — "See what your AI agents are doing. DM them while they work."
**Product Hunt:** Option C — "The first AI dev tool you can justify to your CFO"

### Persona-specific landing pages (future)

- `/for-founders` — Alex's language: "observable, interactive office"
- `/for-agencies` — Ryan's language: "run one per client as a managed service"
- `/for-platform-teams` — Sam's language: "justify to your CFO with per-task costs"
- `/for-researchers` — Marco's language: "agent observatory with a communication bus"

## What This Changes in the Strategy

### Pitch reorder (before vs after this test)

**Before:** Lead with "70% fewer tokens" (rational, cost-focused)
**After:** Lead with "see and steer your agents mid-flight" (emotional, visibility-focused)

The token savings is the rational justification AFTER the emotional hook lands.
Lena confirmed: the 73% claim is not earned yet (4/10 on marketing claim earned).
Visibility is earned RIGHT NOW and is the aha for 5/9 personas.

### Feature priority reorder

**Before:** Per-agent MCP scoping → cost_events → worktree isolation → migration tool
**After:**
1. **Cost tracking** (per-task, per-agent) — #1 blocker, flagged by 5 personas
2. **Budget hard cap** — safety net, flagged by 3 personas
3. **CEO model configurable** — let users set Sonnet for routing, reduces cost
4. **Per-agent MCP scoping** — still important, enables the token claim
5. **Prebuilt binary** (goreleaser) — removes Go install barrier
6. **LICENSE file** — trivial, do it today

### ICP confirmation

The product is FOR:
- Technical founders/CTOs running 3+ AI agents with API keys ($100-200/mo)
- Platform engineers justifying AI spend to skeptical leadership ($500/mo pilot)
- Agency founders who want to white-label ($500/mo + per-client)

The product is NOT FOR:
- Solo freelancers on Claude Pro ($50/mo max) — too expensive
- Non-technical users — correctly filtered by landing page
- Anyone without API-based billing — flat-rate subscriptions don't work

### The hero demo is Scene 5

The homepage should show a 30-second video of:
1. Agent working in a tmux pane (tool calls scrolling)
2. User clicking agent, streaming panel opens
3. User types a DM: "also handle the edge case for empty arrays"
4. Agent acknowledges and adjusts mid-task
5. Channel keeps running, other agents unaffected

That's the conversion moment. Everything else supports it.
