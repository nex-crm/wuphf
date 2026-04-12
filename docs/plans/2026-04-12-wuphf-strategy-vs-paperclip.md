# WUPHF Strategy: The Paperclip Playbook

Written 2026-04-12 on branch `nazz/feat/web-view`. This is the strategic plan for
positioning WUPHF as the better multi-agent office runtime, informed by a deep source
read of Paperclip (cloned at b00d52c, 94k lines server TS + 68k lines UI TSX) and a
review of its GitHub issue tracker, user community, and public reception.

## Decisions Made

### KILL: A2UI (generative UI component system)
- Files: `internal/tui/generative.go` (269), `generative_registry.go` (294),
  `generative_test.go` (465), `internal/action/workflow_store.go` (131)
- References: 14 in `channel.go`, 8 in `stream.go`, 57 in `web/index.html`
- Reason: not functional, never shipped a working user-facing feature
- The April 1 approved design doc (workflow runtime + A2UI hybrid) is dead in its
  current form. If workflow runtime comes back, it comes back without A2UI.

### KILL: Email Postmortem as a separate product direction
- Branch `nazz/email-postmortem` has 113 commits. Not deleting the branch.
- Decision: stop investing as a parallel direction. Fold email triage into WUPHF
  as a pre-built pack ("Inbox Triage" or "Customer Ops" pack).

### CHANGE: Focus mode (delegation) becomes the default
- PR #25 (`codex/focus-mode`) already shipped CEO-routed delegation mode.
- Decision: flip to default. Specialists only wake when CEO delegates or human tags directly.
- Rename: "focus mode" → "delegation mode" (the default, unnamed in UI)
- Add: `/team` command to enable collaborative mode (inverse of current `/focus`)
- CEO agent suggests `/team` when 2+ agents work on related tasks — the "aha moment"
- Rationale: cheaper (fewer agents firing), simpler (Paperclip-familiar), safer (smaller
  blast radius for month 1). Collab mode is the upgrade, not the baseline.

### ELEVATE: Nex is the strategic core, not a nice-to-have
- Dorsey's framework: "the value is in the world model, not the interfaces"
- ericosiu's evidence: "the data accumulates in ways that can't be fast-forwarded"
- Decision: Nex integration quality is a prerequisite for launch, not a post-launch item.
  The comparison post should show Nex making decisions Paperclip can't because Paperclip
  has no world model.

---

## Part 1: Why Paperclip Got Famous

### The growth story
- Created by [@cryppadotta / "dotta"](https://github.com/paperclipai)
- Launched March 4, 2026
- 30k stars in 3 weeks, 38k in 4 weeks, 43k+ as of April 2026
- Did NOT break out on Hacker News. Multiple submissions, max 6 points, 1 comment
  ("Love paperclip I've been using it for a few days now the UI is really nice.")
- Growth came from: YC network, Twitter/X, and a coordinated wave of 8+ Medium articles
  in a 2-week window (Towards AI, MindStudio, SOTAAZ, 4thPath, DealRoom, Flowtivity,
  UCStrategies, MrDelegate)

### The actual fame drivers
1. **"Zero-human company" framing is polarizing.** Every discourse camp reacts. Polarizing = shareable.
2. **Timing.** Multi-agent coordination pain became acute in early 2026 as more people ran
   multiple Claude Code / Codex / OpenClaw sessions. The coordination gap was felt.
3. **Company metaphor maps to how developers already think.** Reviewer quote: "The
   company/project/issue metaphor maps directly to how developers already think about work."
4. **Zero-friction onboarding.** 16 pre-built company templates, embedded PGlite (no Docker),
   MIT licensed, self-hosted, no account.
5. **Provider-agnostic.** "If it can receive a heartbeat, it's hired." Works with Claude,
   Codex, OpenClaw, Cursor, Gemini, Hermes, Pi, OpenCode, any subprocess, any HTTP webhook.
6. **Logan (OpenClaw adjacent):** "When I first started playing with OpenClaw this was the
   vision I had for where you could take Agents. Everyone should go try Paperclip right now."
7. **Opensoul** (6-agent marketing agency built on Paperclip) validated the stack in
   production — a concrete reference customer.

### What users actually love (smaller list than the hype)
- The dashboard. "Browser-based dashboard for oversight, without writing any UI code yourself."
- Persistence out of the box. "No Docker Compose, no Supabase setup."
- The control/execution plane split is "architecturally correct."
- Self-hosted, MIT, no account.

---

## Part 2: What's Actually Broken (from GitHub issues and user reports)

### Token burn is the #1 pain

**Issue #544** — "consumes 10x more tokens than alternatives for the same work."
User Neelkanthsahu02 identified the **3 hidden token killers** via 100+ bash debugging calls:

1. **Session Resume (~70% of waste).** `--resume` carries full conversation history from all
   previous runs. 13 accumulated sessions = millions of cached tokens re-read every turn.
   Fix: fresh sessions per task.
2. **12 MCP Servers inherited globally (~24k tokens/turn).** Every agent loads all MCP servers
   from the user's global Claude config. 240 tool definitions = 24k tokens overhead.
   Fix: per-agent MCP scoping.
3. **Unnecessary skills.** Agents inherit skills they don't use.
   Fix: per-agent skill selection.

Follow-up from garfieldcoked: "I like the concept of Paperclip but hitting my limits too fast."

**Issue #1183** — "Full conversation history + verbose tool outputs sent every heartbeat.
Leads to exponential token consumption, reaching model limits within minutes." Proposed:
instruction caching, tool output truncation (2k token limit), rolling context window,
metadata stripping after first turn.

**Issue #3401** (filed 2026-04-11) — "Heartbeat is architecturally heavyweight — burns LLM
tokens without delivering proportional value." Maintainer response: "This is a legitimate
architectural concern. The current heartbeat model is polling-over-push — it spends LLM
tokens to learn 'nothing to do.' The report is correct."

**Issue #2101** — Agents hit rate limits, retry on fixed 3600s schedule instead of honoring
the actual reset time from the rate-limit response.

**Issue #1348** — "Archived companies still running heartbeats and consuming tokens." Comment:
"This is urgently needed !!!"

**Issue #1756** — Budget is dollar-denominated. Claude Enterprise/Max users track tokens, not
dollars. Workaround: set budget to 0 (unlimited) and track externally — defeating the purpose.

### Workspace corruption is the #2 pain

**Issue #3335** — "222 active execution workspaces all pointing at the same cwd across 8
agents. Every agent was reading and writing the same files simultaneously." Git worktree
isolation exists in the codebase but gated behind 3 experimental flags, no UI, no docs.
The known-dangerous behavior is still the production default a month after being filed.

Combined with #3207 (workspace cleanup destroying unpushed work): agents can silently lose
each other's completed work.

### Other production bugs
- **#3338** — Session tokens logged in plaintext in server.log. Security.
- **#3352** — DELETE /api/agents fails with FK constraint violation. Can't delete agents.
- **#3407** — Routine dispatch crashes with duplicate key errors.
- **#3325** — gemini_local resumes stale session and hallucinates completion.
- **#3364** — Retry wakeups lose their issue context after failure.
- **#3377** — Org chart broken on mobile portrait.
- **Effloow (14 agents)** — Day one fabrication incident: "agents filled the site with
  invented content — plausible but entirely false data, statistics, and narratives."
- **Reviewer note** — "batch outreach hit 23 leads instead of 3 due to cascading mistakes."

### The pattern
Paperclip shipped the vision before they shipped the discipline. Token complaints dominate
their issue tracker. Workspace integrity is broken by default. These are the gaps.

---

## Part 3: The Collaboration Model Question

### The tension
Paperclip uses a Linear-style ticket system: agents check out tasks, do them, post comments.
No chat, no channels, no real-time conversation. Their SPEC explicitly says "Tasks + comments
only (no separate chat system)."

WUPHF uses a Slack-style office: agents live in channels, respond to messages, have threads
and DMs. Tasks exist but emerge from the conversation.

### Where Paperclip's ticket model wins
- **Batch work**: 50 independent issues, each assigned to an agent. No coordination needed.
- **Time-shifted work**: agents run at 3am, human reviews at 9am. No conversation needed.
- **Scale**: 20+ agents doing parallelizable work. Shared context = token cost.
- **Clean audit**: task → comments → cost → billing code. Easy to search and filter.
- **Fire-and-forget**: assign, wait, review. No supervision required.

### Where WUPHF's collaboration model wins
- **Ambiguity**: "figure out why users are churning" needs discussion, not a ticket.
- **Real-time human-in-the-loop**: typing in the channel > commenting on a ticket.
- **Quality through visibility**: Agent A sees Agent B's output in the shared channel,
  catches conflicts before shipping. In Paperclip, A never sees B's output.
- **Creative work**: brainstorming, design reviews, strategy — conversation, not tickets.
- **Small team feel**: the "founding team in a room" mental model. This is what the
  20x company looks like — 3-5 agents + 1 human, same Slack, same context.
- **Observability**: "what's happening right now" is a glance at the channel.
  In Paperclip, it's filtering the issue tracker by status.
- **Preventing fabrication**: agents seeing each other's output in real-time catches
  "this data looks invented" faster than post-hoc task review.

### The critical insight: WUPHF is the superset
WUPHF has channels AND tasks. Paperclip has tasks only.

WUPHF can offer:
- "Collaborative mode" — agents work in a shared channel, human watches and steers
- "Quiet mode" — agents work silently on assigned tasks, post results when done (Paperclip's model)

Paperclip CANNOT offer collaborative mode. They explicitly rejected chat. Adding it
requires rebuilding their data model — a multi-month rewrite.

### Evidence from multi-agent ecosystem
- AutoGen, CrewAI, OpenAI Swarm, LangGraph — all use conversation as the coordination
  primitive, not task queues. Paperclip's task-only model is the industry outlier.
- Opensoul (marketing agency on Paperclip) is forced into task structure for collaborative
  work that's naturally conversational. The friction is tolerated, not chosen.
- Garry Tan's "20x company" thesis: small teams + AI. These teams talk in Slack, not Linear.
- The Effloow fabrication incident is partly a visibility problem — no shared channel where
  agents could flag "this data looks invented."

### The risk
Token cost of shared context. In Paperclip, each agent sees only its task. In WUPHF, each
agent sees the whole channel. More visibility = more tokens.

Mitigation: the same token discipline patterns from Part 2, applied to WUPHF's model:
- Rolling context window (last N relevant messages)
- Per-agent channel filtering (frontend agent doesn't need CRO's sales discussion)
- Summary blocks (every 50 messages, CEO posts a summary that replaces raw messages)
- Task-scoped context option (narrow to task thread when assigned)

### Strategic recommendation
Don't dilute the collaboration model to match Paperclip. Double down as the positioning.

---

## Part 4: Final Positioning (Converged)

### One-liner (revised after ICP panel — lead with visibility, not cost)
"See your AI agents work. Steer them mid-flight."

### Why this changed
The 10-persona ICP test revealed: visibility (live streaming + mid-flight DM) is the
emotional hook that converts. Token savings is the rational justification AFTER the hook
lands. 5 of 9 active personas identified Scene 5 (streaming) as their aha moment. Only
Lena rated the 73% token claim — and scored it 4/10 on "marketing claim earned right now."
Lead with what IS proven, not what WILL BE proven after a benchmark.

### The pitch (revised after ICP panel)

> "Most multi-agent tools are ticket queues wearing a dashboard.
> WUPHF is a shared office. Watch your agents work. Steer them mid-flight.
> Start in delegation mode — familiar, clean, cheap. When you're ready, type /collab
> and watch your agents coordinate. Your Claude bill drops as a side effect."

(Lena's language, validated as the strongest single line by the DevRel persona who
would actually write about the product.)

### Four pillars (reordered after ICP panel — visibility leads, cost follows)

1. **"See and steer"** — live streaming of agent work + mid-flight DM without mode switch.
   This is the #1 aha moment across all personas. The hero demo. The conversion trigger.
   Alex: "a different way of working with agents." Lena: "the first AI tool that treats
   observability as a first-class feature." Sam: "I can screenshot these tool call traces
   for my VP. That's evidence, not vibes."

2. **"Two modes, one office"** — delegation mode (CEO-routed, quiet) is the default.
   /collab mode (agents coordinate in shared channels) is one command away. CEO suggests
   switching when appropriate. Paperclip can only do delegation. WUPHF does both. PR #25.

3. **"Your bill drops as a side effect"** — token discipline baked in. Fresh sessions,
   per-agent MCP scope, tool output truncation, no LLM polling. BUT: don't lead with
   "70% cheaper" until the benchmark exists. Lena scored the claim 4/10 on "earned right
   now." Lead with the architectural reasoning; earn the number in Week 5.

4. **"A world model that compounds"** — Nex as shared knowledge graph. Dorsey's Layer 2,
   ericosiu's Single Brain. The moat that can't be fast-forwarded.

### The switch progression (user journey)
- **Minute 0**: `wuphf import paperclip --from ~/.paperclip` → office loads with their agents
- **Day 1**: delegation mode. CEO receives tasks, delegates to specialists, clean and quiet.
  User notices: "my bill is way lower." Token savings visible in cost dashboard.
- **Week 1**: user gets comfortable. Adds Nex connection (Gmail, CRM). CEO starts referencing
  real customer data in its decisions. "Wait, it knows about that deal?"
- **Week 2**: CEO suggests: "PM and Frontend are both working on the pricing page. Want me to
  turn on team mode so they can coordinate?" User types `/team`. Agents start riffing in the
  shared channel. The "aha moment."
- **Month 1**: routines running (morning brief at 8am, lead scoring 3x/day, content scan 2x/day).
  System is compounding. The "flywheel" ericosiu described.
- **Month 3**: the world model (Nex) has 90 days of accumulated context. CEO makes connections
  no human noticed. "This is a different game entirely."

### What WUPHF is
"The AI company operating system — delegation mode for execution, team mode for collaboration,
Nex for memory. Starts quiet, gets smarter."

### What WUPHF is not
- Not a task queue (that's Paperclip — we're the superset)
- Not a session manager (that's Claude Squad)
- Not a control room (that's claude-code-by-agents)
- Not an autonomous OS without oversight (that's AI-company / AI Team OS)
- Not N isolated personal agents (that's NemoClaw — we're one shared office)

---

## Part 5: Technical Patterns to Adopt

### From Paperclip (verified in source)

**Steal directly:**
1. `logActivity` invariant (`server/src/services/activity-log.ts`, 94 lines) — one function,
   every mutation calls it, writes + publishes live event. WUPHF equivalent: ~50 lines Go.
2. PropertiesPanel render-slot (`ui/src/components/PropertiesPanel.tsx`, 29 lines +
   `ui/src/context/PanelContext.tsx`, 75 lines) — any page pushes JSX into one panel via context.
3. Chain-of-thought fold (`IssueChatThread.tsx:1001-1016`) — synchronous render-time state
   derivation, auto-expand while running, collapse when done. Build minimal vanilla JS version.
4. `cost_events` table shape (`packages/db/src/schema/cost_events.ts`) — 5-dimensional
   (company, agent, issue, project, goal, heartbeat_run) with billingCode, provider, biller,
   billingType, model, inputTokens, cachedInputTokens, outputTokens, costCents, occurredAt.
5. Atomic checkout via compare-and-swap (`heartbeat.ts:2225-2234`) — UPDATE WHERE status = 'queued'.

**Understand but defer:**
6. `WakeupOptions` envelope (`heartbeat.ts:326-336`) — structured wakes with provenance.
7. Wake coalescing (`mergeCoalescedContextSnapshot`, `heartbeat.ts:856`) — dedupe multiple
   wake triggers for the same agent.
8. Session compaction policies — per-agent context window tuning.
9. `requestDepth` on tasks — delegation hop counter, infinite loop prevention.
10. Budget policies as layered enforcement (company → agent → project).

**Skip entirely:**
11. Full `ServerAdapterModule` interface (15 methods, sized for 10+ adapter ecosystem)
12. Plugin system (20+ files)
13. Agent Companies protocol / Clipmart
14. Goals → projects → milestones → issues hierarchy
15. `@assistant-ui/react` dependency

### Things WUPHF already does better
- **Push-driven agent wake** (not timer-based LLM polling)
- **Per-agent system prompts** (not global system prompt inheritance)
- **Per-agent model selection** (CEO gets Opus, specialists get Sonnet)
- **Office metaphor with channels** (vs task-only communication)
- **CEO-routed focus mode** (PR #25, merged) — specialists stop cross-talking, take
  work only from CEO delegation or direct human tag, report back to CEO. This IS
  Paperclip's task-delegation model, shipped as a toggle. WUPHF can do BOTH
  collaborative (default) AND task-delegation (focus mode). Paperclip can only do one.
- **Nex integration** (real-world context from CRM, email, calendar) — validates
  Jack Dorsey's "World Model" layer and ericosiu's "Single Brain" pattern.
  See Part 9 for analysis.
- **Terminal-first TUI** (Paperclip is web-only)

### Validated by real production usage (ericosiu OpenClaw post, 2026-04-12)

ericosiu (founder, revenue agent company + marketing agency) runs 5 named agents
on OpenClaw with 48 daily crons, local inference on Mac Mini M4 + DGX Spark + Mac
Studio. Six months of compounding data. 6,862 Gong transcripts, 6k+ sales leads.

Key validations:
- The "Single Brain" (unified vector DB, 15-min ingestion) IS Nex
- The org chart (Alfred CEO + 4 specialists with lanes) IS WUPHF's office model
- Focus mode delegation IS how ericosiu's agents coordinate
- "Never instruct twice" IS save-as-skill
- "Flat files over databases" IS WUPHF's JSON state approach
- "The system compounds" — month 1 terrible, month 3 flywheel
- Local inference cut costs 70% — hardware pays for itself in weeks

Key patterns to adopt:
- **Broker-native routines** (48 daily crons, not LLM polling)
- **Self-healing cron doctor** (automated, runs 2x/daily, auto-repairs)
- **Security gates on all outbound actions** (confirmation for side effects)
- **LLMs for judgment, scripts for everything else** (explicit boundary)
- **Personal agents per team member** (multi-user WUPHF offices)
- **"The data accumulates in ways that can't be fast-forwarded"** — Nex as moat

### Token discipline — the wedge features
These are the specific engineering tasks that deliver claim #1 ("your bill dropped 70%"):

1. **Verify no `--resume` by default.** Audit `headless_claude.go:29-40`. Confirmed: args
   list does not include `--resume`. Good. Document this as a design invariant: "WUPHF
   agents start fresh sessions per turn by default."
2. **Per-agent MCP config.** Currently `--mcp-config` at `headless_claude.go:38` passes one
   global config. Change to: generate a per-agent `mcp.json` at turn start containing only
   the MCP servers that agent needs. ~30 lines of Go.
3. **Tool output truncation.** Wrap the Claude stream reader to cap tool_result content at
   2k tokens, showing head/tail with "[truncated]" in the middle.
4. **`cost_events` passive logging.** After each turn completes, write `{agent, provider,
   model, input_tokens, output_tokens, cost_cents, occurred_at}` to broker state. Read from
   `provider.ClaudeStreamResult` which already has token counts.
5. **No LLM polling.** Already correct. Document: "WUPHF agents wake on push. There is no
   timer-based heartbeat that burns tokens."
6. **Paused/archived state stops all dispatch.** Audit broker to confirm.
7. **Rolling context window.** Summarize older channel messages into short-term memory.
   Medium effort (~1-2 weeks). Per-agent configurable.

---

## Part 6: Final Roadmap (Converged)

Everything below serves ONE goal: ship a comparison post in 5 weeks that makes the
switch obvious. Every task earns its place by contributing to a specific pillar.

### Week 1: Clean slate + token foundation
**Goal: WUPHF starts clean, default mode is delegation, token savings begin.**

- [ ] Delete A2UI (generative.go, generative_registry.go, generative_test.go,
      renderA2UIBlocks in channel.go, /generative in stream.go, a2ui CSS+JS in web)
- [ ] Check workflow_store.go + composio_workflows.go — remove if orphaned by A2UI kill
- [ ] Flip focus mode to default in launcher.go — rename to "delegation mode" in UI/docs
- [ ] Add `/team` command to enable collaborative mode (inverse of current `/focus`)
- [ ] CEO system prompt addition: suggest `/team` when 2+ agents work on related tasks
- [ ] Per-agent MCP config generation in headless_claude.go:38 (~30 lines)
      → generates per-turn mcp.json with only the MCP servers that agent needs
- [ ] `cost_events` passive logging: after each turn, write {agent, provider, model,
      input_tokens, output_tokens, cost_cents, occurred_at} to broker state

**Pillar served:** "Your bill dropped 70%" (per-agent MCP = biggest single token savings)

### Week 2: Token discipline + audit
**Goal: every known Paperclip token-burn pattern is avoided by design.**

- [ ] Tool output truncation: wrap Claude stream reader, cap tool_result at 2k tokens,
      show head/tail with [truncated]. Configurable per tool type.
- [ ] Verify no --resume session accumulation (audit headless_claude.go args)
- [ ] Verify paused/archived state stops ALL dispatch (audit broker.isPaused paths)
- [ ] logActivity invariant: `func (b *Broker) logActivity(input ActivityInput) error`
      → append to activity_log slice in brokerState, persist via saveLocked
      → broadcast to SSE/websocket subscribers (live web view updates)
      → ~50 lines Go, migrate existing office-ledger + watchdog calls
- [ ] Rolling context window: configurable per agent, summarize older messages into
      short-term memory block, drop raw messages from next turn's context
- [ ] Document token discipline as design invariants in CLAUDE.md:
      "WUPHF agents start fresh sessions per turn. No --resume. No global MCP inheritance.
       No LLM polling. Tool outputs are truncated at 2k tokens."

**Pillar served:** "Your bill dropped 70%" (full token discipline pass)

### Week 3: Workspace integrity + web UX
**Goal: multi-agent workspace is safe by default, web view has the three Paperclip-lifted patterns.**

- [ ] Git worktree per agent per active task (default for multi-agent projects)
      → single-agent projects use project root
      → second agent assigned → worktree auto-created
      → cleanup preserves unpushed commits
- [ ] Atomic task checkout: `POST /tasks/:id/checkout {slug}` in broker
      → compare-and-swap under mutex, 409 if already owned
- [ ] PropertiesPanel render-slot in web view: replace bespoke #agent-panel with one
      generic #detail-panel via WuphfDetail.show(contentEl) / hide()
      → agent detail, task detail, request detail, cost detail all push into same slot
- [ ] Chain-of-thought fold in web chat: agent messages get expandable tool-call sections
      → collapsed when done, auto-expand while running
      → rolling reasoning line during active runs (CSS keyframe animations)
      → plumb tool_use/tool_result events from headless_claude.go into message in broker
- [ ] Inline system messages in chat: task state changes + run completions as thin
      divider lines in the message feed (fed by logActivity)

**Pillar served:** "Nothing corrupts" + web UX polish for the demo

### Week 4: Migration + routines + world model
**Goal: Paperclip users can import their setup, Nex integration is tight, routines work.**

- [ ] `wuphf import paperclip --from <path>` command (~400 lines Go)
      → read PGlite DB or JSON state from Paperclip's data directory
      → map: companies → packs, agents → officeMembers (with adapterType), issues → tasks
      → import cost_events history, activity_log
      → rewrite adapter configs (claude_local → claude, codex_local → codex)
      → print: "Imported N agents, M tasks, $X in cost history. Office running at :7891"
- [ ] Minimal HeadlessAdapter interface: Execute(ctx, slug, notification) error +
      Cancel(ctx, slug) error. Store adapterType per officeMember. Refactor
      headless_claude.go + headless_codex.go behind it. Enables mixed providers.
- [ ] Broker-native routines: cron schedule per agent stored in broker state
      → fires wake events on schedule (no LLM involved in scheduling)
      → routine = {id, agentSlug, schedule, prompt, enabled, lastRun, nextRun}
      → CEO morning brief at 8am, lead scoring 3x/day, etc.
      → self-healing: if routine fails, log to activity, surface in doctor panel
- [ ] Nex integration depth pass:
      → verify insights → CEO notification → delegation flow is tight
      → CEO's system prompt references Nex context for decisions
      → cost dashboard shows Nex-attributed value (e.g., "CEO used Nex context
        to prioritize lead X, which closed for $Y")
- [ ] Security gates: outbound action confirmation via requests system
      → any MCP tool call with external side effects (email, CRM update, deploy)
        creates a pending request unless explicitly approved in agent's permissions

**Pillar served:** "Migrate in 5 minutes" + "A world model that compounds"

### Week 5: Proof + launch prep
**Goal: the comparison post is written with real numbers.**

- [ ] Port Opensoul-style stack (6-agent marketing office) to WUPHF
      → agents: CEO, Strategist, Creative, Producer, Growth, Analyst
      → tasks: same workload as Opensoul case study
      → routines: content scan, lead scoring, competitor analysis
      → Nex: connect Gmail + Calendar + CRM
- [ ] Run head-to-head benchmark: same 24h workload on WUPHF vs Paperclip
      → measure: total tokens, cost in dollars, workspace corruptions,
        fabrication incidents, time-to-first-output
      → expected result: ~70% fewer tokens, 0 corruptions, 0 fabrication
- [ ] Write comparison post with real numbers, code, prompts
      → title: "We ported a Paperclip marketing office to WUPHF. 73% fewer tokens."
      → structure: setup → what we measured → results → why → try it yourself
      → include: `curl | sh && wuphf import paperclip` install path
- [ ] "Switching from Paperclip" doc: what's the same, what's different, what's better
- [ ] 3 starter packs:
      → Founding Team (coding): CEO, PM, FE, BE, Designer
      → Marketing Office (ericosiu-inspired): CEO, Strategist, Content, SEO, Sales
      → Customer Ops: CEO, Support, Sales, Account Manager
- [ ] Record 2-minute demo video: import → delegation mode → task completion → cost comparison

### Week 6: Ship
**Goal: the world knows WUPHF exists.**

- [ ] Publish comparison post
- [ ] Submit to: HN, Lobsters, r/LocalLLaMA, r/ClaudeAI, r/AI_Agents, r/selfhosted
- [ ] Post in OpenClaw Discord, Paperclip Discord, relevant Telegram/Slack communities
- [ ] DM Paperclip users who filed the loudest issues:
      → #544 (garfieldcoked, Neelkanthsahu02): "we built per-agent MCP scoping"
      → #3335 (rudyjellis): "workspace isolation is our default, not behind 3 flags"
      → #3401 (ttomiczek): "we don't burn tokens on empty inboxes"
      → #1183 (ed2ti): "we cap tool output at 2k tokens + rolling context window"
- [ ] X/Twitter thread: the ericosiu framework adapted for WUPHF
      → show: same 5-agent structure, same Single Brain (Nex), but shared office
- [ ] Hacker News "Show HN" with a different angle than Paperclip used:
      → not "zero-human company" (polarizing, already used)
      → try: "Show HN: I built an AI office that costs 70% less to run than Paperclip"
      → or: "Show HN: Multi-agent Slack where agents collaborate (and your bill drops 70%)"

---

## Part 7: Hard Truths (Updated)

1. **The window is 4-8 weeks.** Paperclip's token optimization and workspace isolation are
   in their Phase 4 backlog. If they ship before our comparison post, the wedge weakens.
   Speed > polish.
2. **"80%" is the ambition, 10-20% is realistic.** ~500-1000 active users switching is still
   a massive outcome for a solo-founder project. Don't optimize for the number — optimize
   for making the switch obvious for anyone who hits the pain.
3. **People switch because of the bill. They stay because of the office.** Delegation mode
   gets them in the door. Team mode + Nex compounding is why they don't go back.
4. **The comparison post is the single highest-leverage artifact.** Without it, nobody knows
   WUPHF exists. Everything in weeks 1-4 exists to make week 5 credible.
5. **Token discipline is hard.** Paperclip burns tokens because the engineering is genuinely
   difficult. WUPHF will hit the same problems. The advantage is knowing WHERE the problems
   are (from reading their issue tracker) before building.
6. **Nex is the moat, but it's also the dependency.** If Nex integration is buggy or slow,
   the "world model that compounds" pillar collapses. Nex stability is a prerequisite.
7. **Month 1 will be terrible for WUPHF users too.** ericosiu was honest about this. Don't
   overpromise. The messaging should be: "The first week is setup. The second week it starts
   working. The third week you can't live without it."
8. **Local inference is the real cost play long-term.** ericosiu cut costs 70% via hardware,
   not prompt optimization. If WUPHF supports local models via Ollama/llama.cpp (through the
   adapter interface), that's a second cost-reduction vector beyond token discipline. Defer
   to post-launch but keep the adapter interface open for it.

---

## Part 9: The ericosiu Signal — Real Production Multi-Agent

Source: ericosiu post "How I built a real marketing team on OpenClaw" (2026-04-12).
References Jack Dorsey "From Hierarchy to Intelligence" + Karpathy AutoResearch.

### Architecture
- Mac Mini M4 (32GB) + DGX Spark GB10 (128GB) x2 + Mac Studio Ultra
- Local inference cut costs ~70%. Hardware pays for itself in weeks.
- 5 agents: Alfred (CEO/ops), Oracle (SEO), Arrow (Sales), Cyborg (Recruiting), Flash (Content)
- World Agent above all — organizational brain that coordinates across agents
- Single Brain: unified vector DB, ingests all company data every 15 minutes
  (Slack, CRM, Gong, GA4, GSC, docs, meeting notes, financials)
- 48 daily cron jobs
- Self-healing cron doctor runs 2x/daily

### Dorsey's four layers mapped
| Layer | ericosiu | WUPHF | Paperclip |
|---|---|---|---|
| Capabilities | OpenClaw + MCP + scripts | MCP + One CLI | Adapters (10 types) |
| World Model | Single Brain (vector DB) | Nex (knowledge graph) | None |
| Intelligence | 5 named agents with lanes | Office members with roles | Agents in org tree |
| Surfaces | Slack cards, email, dashboards | TUI + web + Telegram | React dashboard |

### Design principles (verbatim from post)
1. "LLMs handle judgment, scripts handle everything else"
2. "Never instruct twice" — automate on second occurrence
3. "Security gates on everything" — inbound and outbound scanners
4. "Self-healing over monitoring" — auto-repair before human notices
5. "Flat files over databases" — markdown/JSON, no abstraction layer
6. "The system compounds" — month 1 terrible, month 3 flywheel

### What this means for WUPHF strategy
1. **Nex is the moat, not the orchestration.** The Single Brain is the competitive
   advantage that "can't be fast-forwarded." WUPHF + Nex already has this.
   Paperclip does not.
2. **Cron-based routines are how real operators run multi-agent.** Not heartbeats,
   not chat. 48 scheduled jobs. WUPHF needs broker-native routines.
3. **The target user invests months.** "Month 1 was terrible. Month 3, the flywheel
   kicked in." WUPHF should shorten month 1, not promise to skip it.
4. **The product angle**: "Your internal implementation becomes your product."
   WUPHF packs are this — save your office config, sell it as a template.
5. **Local inference is the real cost play.** ericosiu cut costs 70% via local
   hardware, not via prompt optimization. Worth considering for WUPHF's roadmap
   whether local model support (via Ollama, llama.cpp, etc.) is a priority.

## Part 10: Dorsey's "From Hierarchy to Intelligence" — WUPHF's Blueprint

Source: [Sequoia article](https://sequoiacap.com/article/from-hierarchy-to-intelligence/),
[Block essay](https://block.xyz/inside/from-hierarchy-to-intelligence). Published 2026-03-31.
Context: Block cut ~4,000 positions (~40-50% headcount) to implement this.

### The four layers
1. **Capabilities** — atomic primitives with no UI of their own
2. **World Model** — dual-sided: company self-understanding + per-customer understanding
3. **Intelligence Layer** — composes capabilities into proactive solutions
4. **Interfaces** — delivery surfaces ("not where the value is created")

### The 1:1 mapping to WUPHF

| Dorsey layer | WUPHF implementation |
|---|---|
| Capabilities | MCP tools + One CLI + Nex actions (`capability_registry.go`) |
| Company World Model | Nex knowledge graph + office channel history + task/request state |
| Customer World Model | Nex entities (contacts, companies, deals) enriched with CRM/email/calendar |
| Intelligence Layer | CEO agent + broker notification routing + Nex insights |
| Interfaces | TUI + web view + Telegram bridge |
| ICs | Specialist agents (FE, BE, Designer, PM, etc.) |
| DRIs | Task owners — agents assigned to cross-cutting outcomes |
| Player-Coaches | CEO agent — builds + coordinates |

### Key strategic implications from Dorsey
- **"The value is in the model and the intelligence, not the interfaces."** — Reorders
  WUPHF priorities: Nex depth (Layer 2) > CEO routing intelligence (Layer 3) > web polish
  (Layer 4). Most of this session was spent on Layer 4. Should shift.
- **"Customer reality generates the backlog directly."** — When the CEO agent can't handle
  a Nex notification because no specialist has the right capability, that failure IS the
  roadmap. Log these capability gaps; they tell you what agents/skills to build next.
- **"AI doesn't augment your company. It reveals what your company actually is."** — WUPHF's
  role is to make the intelligence visible, not to pretty up the dashboard.

## Part 11: Karpathy's AutoResearch — The Loop Pattern for WUPHF

Source: [github.com/karpathy/autoresearch](https://github.com/karpathy/autoresearch). 21k
stars, 8.6M views. Released 2026-03-07.

### The core loop
```
human writes program.md (research direction)
→ agent modifies code
→ train 5 min (fixed time budget)
→ evaluate val_bpb (single metric)
→ improved? → commit and keep
→ not improved? → revert
→ repeat
```

### WUPHF equivalent
```
founder writes pack config (office direction)
→ routine fires (scheduled)
→ agent executes (MCP tools, emails, CRM updates)
→ evaluate outcome (Nex insights: pipeline value, response rate, etc.)
→ worked? → save-as-skill, repeat
→ didn't work? → log failure, try different approach
→ repeat
```

Karpathy's vision: "not to emulate a single PhD student, it's to emulate a research
community." That's WUPHF's shared office — agents share findings in channels, build on
each other's discoveries. Paperclip's isolated task model can't do this.

ericosiu's "AutoGrowth" is this pattern applied to marketing: "Arrow tests different
subject lines. After four weeks, questions outperformed statements by 2.3x. That insight
got applied automatically." WUPHF routines should be AutoResearch loops.

## Part 12: The Personal Agent Model — Why WUPHF's Shared Office Wins

ericosiu's NemoClaw rollout: each team member gets their own isolated agent. N people =
N agents. They share data through the Single Brain but don't see each other's work.

WUPHF's model: the whole team shares ONE office. Shared channels for team context. Private
DMs (1:1 mode) for personal conversations. Any team member can DM any agent directly.

### Why shared office beats isolated personal agents
1. **Cross-pollination** — Alice's sales question in #sales gets seen by Oracle (SEO), who
   surfaces a relevant content piece. In ericosiu's model, that only happens through the
   Single Brain if someone wrote it down. In WUPHF, it happens in real-time.
2. **Onboarding** — new person scrolls back, sees how the team works. Isolated agents start
   blank.
3. **No routing bottleneck** — coordination happens via shared channels, not through a
   central World Agent. No single point of failure.
4. **Privacy when needed** — 1:1 DM mode (PR #25, `/1o1`). Personal stays private, team
   stays shared.
5. **One world model** — all agents share the same Nex knowledge graph. Not N copies.
6. **Fewer agents** — one Arrow serves 5 salespeople. Cheaper and smarter than 5 isolated
   Arrows that can't see each other's work.

### The pitch
"ericosiu gives each person a private agent. WUPHF gives the whole team a shared office
where every agent works for everyone — and you can DM any of them privately. One Arrow
serving 5 people is cheaper and smarter than 5 Arrows that can't see each other."

## FINAL PLAN: Content-Driven Development

**Principle:** Write the content first. Build only what makes the claims true.
The content IS the spec. If we can't truthfully say it, we don't ship it.

Updated 2026-04-12 after ICP panel (10 personas), stargazer analysis (52k stars,
2k open issues), confusion tax research, Reddit engagement pattern study.

### ICP (corrected — aligned to Paperclip's actual users)
- Claude Pro/Max users running 3+ agents, hitting rate limits daily
- Technical operators with "too many Claude tabs open" (@dotta's origin story)
- Cost-aware, frustrated by token waste, terminal-native
- NOT limited to API-key users — most Paperclip users are on subscriptions

---

## Content Plan: Every Piece, Exact Copy

### 1. REDDIT POST (r/ClaudeAI, r/LocalLLaMA, r/selfhosted)

**Title:**
"I cut multi-agent token waste from 44% to under 5% by making agents share
a channel — here's the architecture (open source)"

**Body (draft):**
```
If you're running 3+ AI agents, you're probably burning 22-44% of your tokens
on what researchers call the "confusion tax":

- Orientation tax (5-10%): agents waste tokens figuring out what's happening
- Duplication tax (10-20%): agents redo work another agent already completed
- Amnesia tax (5-10%): agents repeat approaches already ruled out
- Context pollution (5-8%): noise in the context window

(Source: Sandelin, "Your AI Agent Teams Are Burning Money — Here's the Math")

I built an open-source multi-agent office that eliminates these by design:

1. Agents share a visible channel (like Slack). They SEE each other's work.
   No orientation tax — the channel IS the state.
2. CEO agent delegates with atomic task checkout. No two agents claim the
   same work. No duplication.
3. Built-in knowledge graph (optional) persists learnings across sessions.
   No amnesia.
4. Delegation mode by default — agents only see their assigned task + CEO
   context. No context pollution.

Fresh sessions per turn (no --resume accumulation). Per-agent tool scoping
(specialists don't load 240 tool definitions). No LLM polling for "is there
work" — push-driven only.

The interesting part: you can click any running agent and see their tool
calls streaming in real-time. DM them mid-task to steer. The channel keeps
running. No mode switch. No restart.

Default is delegation mode (quiet, CEO routes everything — familiar if you've
used Paperclip). Type /collab and agents start coordinating in the shared
channel.

Go binary, self-hosted, MIT. Works with Claude Code and Codex. Runs on
Claude Pro — CEO defaults to Sonnet, not Opus.

[link] | Try: curl -sSL https://wuphf.dev/install | sh
```

**Claims made → features required:**
| Claim | Feature needed | Status |
|-------|---------------|--------|
| "agents share a visible channel" | shared channel model | SHIPPED |
| "they SEE each other's work" | gossip bus / channel visibility | SHIPPED |
| "CEO delegates with atomic task checkout" | `/tasks/:id/checkout` endpoint | NEEDS BUILD |
| "knowledge graph persists learnings" | Nex integration | SHIPPED (needs polish) |
| "delegation mode by default" | flip focus mode to default | 1-LINE CHANGE |
| "fresh sessions per turn" | no --resume | SHIPPED (verify) |
| "per-agent tool scoping" | per-agent MCP config | NEEDS BUILD (~30 lines) |
| "no LLM polling" | push-driven wakes | SHIPPED |
| "click agent, see streaming" | live agent progress in web view | PARTIALLY SHIPPED |
| "DM mid-task, no mode switch" | lightweight DM without /1o1 shutdown | NEEDS BUILD |
| "type /collab" | rename /focus, add /collab inverse | NEEDS BUILD |
| "CEO defaults to Sonnet" | change headlessClaudeModel default | 1-LINE CHANGE |
| "works with Claude Pro" | verify Pro subscription compatibility | VERIFY |
| "curl install" | goreleaser prebuilt binary | NEEDS BUILD |

---

### 2. SHOW HN

**Title:**
"Show HN: Open-source multi-agent office — agents share a Slack-like channel,
you watch them work and steer mid-flight"

**First paragraph:**
```
I had 8 Claude Code terminals open and couldn't remember which one was doing
what. Built WUPHF — a shared office where AI agents work in channels, delegate
through a CEO, and you can see every tool call in real-time. Click any agent,
DM them mid-task, watch them adjust. No restart. The channel keeps running.

Default is delegation mode (CEO routes work, specialists execute — quiet and
cheap). Type /collab and they start coordinating with each other.

Go binary. Self-hosted. MIT. Fresh sessions per turn, per-agent tool scoping.
Runs on Claude Pro (CEO on Sonnet by default, not Opus).
```

**HN angle:** NOT "zero-human company" (already used by Paperclip, polarizing).
NOT "70% cheaper" (unearned until benchmarked). Instead: "I had too many
Claude tabs" — the @dotta origin story, but with our solution.

**Claims audit:** Same as Reddit post + "8 Claude Code terminals" (the ICP moment).

---

### 3. X/TWITTER THREAD (6 tweets)

```
1/ I had 8 AI agent sessions open and couldn't tell which one was doing what.

So I built a shared office where they all work in the same Slack-like channel.

Here's what changed. 🧵

2/ The problem with multi-agent setups:
Each agent is a separate session. Separate context. Separate memory.

They redo each other's work.
They forget what was tried.
They burn tokens figuring out "what's happening."

Researchers call it the "confusion tax." 22-44% of your tokens. Wasted.

3/ The fix: make agents share a channel.

- They SEE each other's output
- CEO delegates tasks (no duplication)
- Knowledge graph remembers across sessions (no amnesia)
- Delegation mode: agents only wake when assigned (no noise)

4/ The part that surprised me:

You can click any running agent and watch their tool calls stream in real-time.
Type a DM: "also handle the empty array case."
Agent adjusts. No restart. Channel keeps running.

That changed how I work with agents entirely.

5/ Default mode: delegation (CEO routes, specialists execute). Quiet. Cheap.

Type /collab → agents start talking to each other. SEO tells Content about
keyword gaps. Content incorporates them. CEO watches and steers.

You can do both. Most tools can only do one.

6/ Open source. Go binary. Self-hosted. MIT.

Works with Claude Pro (CEO on Sonnet, not Opus).
Fresh sessions per turn (no token accumulation).
Per-agent tool scoping (no 240-definition overhead).

github.com/[repo] | curl -sSL https://wuphf.dev/install | sh
```

**Claims audit:** Same set. Thread format = each tweet earns its own claim.

---

### 4. PRODUCT HUNT

**Tagline:** "A shared office for your AI agents — watch them work, steer mid-flight"

**Description:**
```
WUPHF is an open-source multi-agent office where your AI agents work together
in a Slack-like channel. Default: delegation mode (CEO routes, specialists
execute). One command: /collab (agents coordinate with each other).

What makes it different:
→ See every agent's tool calls in real-time
→ DM any agent mid-task without stopping the office
→ Fresh sessions per turn (no token accumulation)
→ Per-agent tool scoping (no global MCP overhead)
→ Built-in knowledge graph that compounds over time
→ Go binary, self-hosted, MIT. Runs on Claude Pro.

For technical founders running 3+ AI agents who are tired of too many tabs,
too much token waste, and agents that can't see each other's work.
```

**Maker comment:**
```
I built this because I had too many Claude Code sessions open and couldn't
track what any of them were doing. The moment I made agents share a channel —
so they could see each other's output and I could see all of them — everything
changed. The CEO delegates quietly by default. Type /collab when you want them
to brainstorm. The live streaming and mid-task DMs are the features I use
most. Cost tracking is built in so you know what every task actually costs.
```

---

### 5. LANDING PAGE

**Hero:**
```
See your AI agents work.
Steer them mid-flight.
```

**Subhead:**
```
A shared office where agents delegate, collaborate, and you watch everything
happen. Fresh sessions. Per-agent tool scoping. No confusion tax.
```

**CTA:** "Watch the 2-minute demo" → Scene 5 video (streaming + DM mid-task)

**3 value props (below hero):**

```
DELEGATION MODE (default)
CEO routes work. Specialists execute. Quiet and cheap.
Familiar if you've used ticket-based orchestration.

/COLLAB MODE (one command)
Agents see each other's work in shared channels.
CEO suggests it when tasks overlap. The upgrade.

WORLD MODEL (compounds over time)
Connect Gmail, CRM, calendar. Agents reference real business
context. Month 3 is when the flywheel kicks in.
```

**Social proof section:**
```
"Most multi-agent tools are ticket queues wearing a dashboard.
This is a shared office." — DevRel reviewer after reading 72k lines of source

"The first AI tool I can justify to my CFO." — Platform engineer

"Like having a tiny dev shop in your terminal." — Freelance developer
```
(These are from the ICP panel — Lena, Sam, Jordan)

**Comparison section (no competitor name — let readers figure it out):**
```
                        Ticket-based          Shared office
                        orchestration         (WUPHF)
Agents see each other   No                    Yes
Mid-task steering       Kill & restart        DM without stopping
Token accumulation      10x over time         Fresh per turn
Tool definition bloat   All agents get all    Per-agent scoped
Agent wake trigger      LLM poll (burns $)    Push (free)
Collaboration mode      Not possible          /collab toggle
Cost tracking           Post-hoc dashboard    Per-task, per-agent
```

---

### 6. YOUTUBE / DEMO VIDEO (2 minutes)

**Script:**
```
0:00 — "I have 5 AI agents running. Let me show you what that looks like."
       Screen: WUPHF office, channel view, agents in sidebar with status dots.

0:10 — "I type a task. The CEO reads it and delegates."
       Screen: type "Write a blog post about our new pricing"
       CEO responds: "@content, draft the pricing blog. Pull context from
       the announcement notes."
       Content agent status changes to "working."

0:25 — "Here's what most tools can't do. I click on Content."
       Screen: click Content in sidebar. Streaming panel opens.
       Tool calls scrolling: "Reading pricing-announcement.md..."
       "Drafting intro paragraph..."

0:40 — "I notice something. I DM them without stopping anything."
       Screen: type in DM: "Make sure to mention the free tier."
       Content acknowledges. Channel keeps running behind.
       "No restart. No mode switch. The office keeps going."

0:55 — "After the task, I check the cost."
       Screen: cost panel showing "12k tokens, $0.04" for this task.
       "Every task, every agent, tracked."

1:10 — "Default is delegation mode. CEO routes everything.
       But watch this."
       Screen: type "/collab"
       "Now agents see each other."
       SEO posts: "Found 3 keyword gaps for the blog."
       Content: "Incorporating now."
       "That coordination happened without me."

1:30 — "Go binary. Self-hosted. MIT. Runs on Claude Pro.
       curl install, one command.
       See your agents work. Steer them mid-flight."
       Screen: terminal with curl | sh install.

1:45 — End card with GitHub link.
```

---

### 7. "SWITCHING" GUIDE (docs page)

```
# Switching from ticket-based orchestration

If you're running agents through a task queue — assign issue, wait, review —
and you want to see what they're actually doing:

## Step 1: Install
curl -sSL https://wuphf.dev/install | sh

## Step 2: Import (if you have an existing setup)
wuphf import --from ~/.paperclip/instances/default
# → "Imported 6 agents, 42 tasks, $38 cost history."

## Step 3: Start the office
wuphf

## What's different
- Agents work in a shared channel (you see everything)
- CEO delegates by default (quiet mode, like your ticket system)
- /collab switches to collaborative mode (agents coordinate)
- Click any agent → see their tool calls live → DM them mid-task
- Cost tracked per task, per agent
- Fresh sessions per turn (no token accumulation)
- Per-agent tool scoping (no bloated tool definitions)

## What stays the same
- Your agents, your tasks, your config
- Self-hosted, no account, MIT licensed
- Works with Claude Code, Codex
```

---

### 8. TECHNICAL BLOG POST

**Title:** "The confusion tax: why your multi-agent team wastes 22-44% of its tokens
and how a shared channel architecture fixes it"

**Structure:**
1. The confusion tax (cite Sandelin) — 5 categories of waste
2. Why ticket-based orchestration CAUSES each tax
3. How shared-channel architecture ELIMINATES each tax
4. The architecture: broker + CEO + gossip bus + delegation/collab modes
5. Real benchmark numbers (THIS REQUIRES THE BENCHMARK)
6. The one-line changes that matter most (Sonnet CEO, per-agent MCP)
7. "Try it yourself" — link + install command

**This is the LAST content piece to publish.** It requires the benchmark (Week 5).
Everything else can publish before the benchmark.

---

## Claims Audit → Feature Backlog

Every claim made in ANY content piece above. If it's not true, it blocks publishing.

| Claim | Current State | Work Needed | Priority |
|-------|--------------|-------------|----------|
| "CEO defaults to Sonnet" | CEO on Opus (`headless_claude.go:153`) | Change 1 line | P0 — DAY 1 |
| "delegation mode by default" | Focus mode exists but not default | Flip default in launcher | P0 — DAY 1 |
| "/collab to enable collaboration" | `/focus` exists as the toggle | Add `/collab` as inverse command | P0 — DAY 1 |
| "fresh sessions per turn" | No `--resume` in args | VERIFY and document as invariant | P0 — DAY 1 |
| "no LLM polling" | Push-driven wakes | SHIPPED — document | P0 — DAY 1 |
| "agents share a visible channel" | Channels + gossip bus | SHIPPED | — |
| "per-agent tool scoping" | Global MCP config | Build per-agent mcp.json generation | P1 — WEEK 1 |
| "cost tracked per task, per agent" | Budget infra exists, no data flows | Hook cost_events into headless turns | P1 — WEEK 1 |
| "click agent, see streaming live" | TUI has streaming, web has activity indicators | Web streaming panel for live tool calls | P1 — WEEK 1 |
| "DM mid-task without mode switch" | /1o1 shuts down office | Lightweight sidebar DM that doesn't switch modes | P1 — WEEK 2 |
| "atomic task checkout" | Tasks have owner but no compare-and-swap | `POST /tasks/:id/checkout` in broker | P2 — WEEK 2 |
| "curl install" | Go build from source | goreleaser + install script | P2 — WEEK 2 |
| "import from existing setup" | Doesn't exist | `wuphf import --from <path>` | P2 — WEEK 3 |
| "knowledge graph compounds" | Nex integration exists | Polish: verify insights → CEO flow is tight | P2 — WEEK 3 |
| "CEO suggests /collab" | Doesn't exist | System prompt addition | P2 — WEEK 3 |
| "22-44% → under 5% confusion tax" | Not benchmarked | Run the benchmark | P3 — WEEK 4 |
| "works with Claude Pro" | Probably works, unverified | Test on Pro subscription | P1 — WEEK 1 |
| "3-agent minimal default" | Default pack has 5-8 agents | Add a "solo" or "minimal" pack with 3 agents | P1 — WEEK 1 |

---

## Content-Driven Roadmap

### What can we publish TODAY (all claims already true)
- Nothing yet. CEO on Opus and delegation not default block everything.

### What we can publish after DAY 1 (4 one-line changes)
- **X/Twitter thread** — all claims are architectural, verified in source
- **Product Hunt tagline draft** — for feedback, not launch

### What we can publish after WEEK 1 (per-agent MCP + cost tracking + streaming)
- **Reddit post** — all claims become TRUE including per-agent scoping and cost tracking
- **Show HN** — same claim set
- **Landing page** (draft) — hero, subhead, value props, comparison table

### What we can publish after WEEK 2 (DM + install + checkout)
- **YouTube demo video** — Scene 5 (streaming + DM) is the hero scene
- **Landing page** (final) — with demo video embedded
- **"Switching" guide** — migration path documented

### What we can publish after WEEK 3 (import + Nex + /collab suggestion)
- **Product Hunt launch** — full launch with maker comment
- **Switching guide** (with import command)

### What we can publish after WEEK 4 (benchmark)
- **Technical blog post** — confusion tax with real numbers
- **Reddit comparison post** — "same 6-agent workload, real token numbers"
- **Landing page comparison table** — with verified numbers

---

## Revised Roadmap (content-sequenced)

### DAY 1 (unblock all content)
- [ ] CEO on Sonnet by default (`headless_claude.go:153`: change `claude-opus-4-6` to `claude-sonnet-4-6` for non-lead agents... wait, this is ALREADY done — lead gets Opus, others get Sonnet. The LEAD needs to be Sonnet too for Pro users.)
  → Actually: add a config option. CEO on Sonnet by default, `--opus-ceo` flag to upgrade.
- [ ] Flip delegation mode to default (launcher.go)
- [ ] Add `/collab` command (inverse of `/focus`)
- [ ] Verify no `--resume` in headless args (audit + document)
- [ ] Add LICENSE file (MIT — Chris won't recommend without it)
- [ ] 3-agent minimal pack: CEO + 2 specialists

### WEEK 1 (unblock Reddit + Show HN)
- [ ] Per-agent MCP config generation (~30 lines)
- [ ] cost_events: log {agent, tokens, cost} per turn to broker state
- [ ] Budget hard cap: stop agents when token/cost limit hit
- [ ] Web view: live streaming panel when clicking agent (tool calls, not just status)
- [ ] Verify Claude Pro subscription works with headless mode
- [ ] Delete A2UI code

### WEEK 2 (unblock YouTube demo + landing page)
- [ ] Lightweight DM: sidebar DM to any agent without /1o1 mode switch
- [ ] Prebuilt binary via goreleaser (curl install)
- [ ] Atomic task checkout endpoint
- [ ] Record 2-min demo video (Scene 5 hero)
- [ ] Build landing page

### WEEK 3 (unblock Product Hunt)
- [ ] `wuphf import --from <path>` command
- [ ] CEO suggests /collab when 2+ agents on related tasks
- [ ] Nex integration polish (insights → CEO → delegation flow)
- [ ] 3 starter packs: Founding Team, Marketing Office, Customer Ops
- [ ] Product Hunt launch prep

### WEEK 4 (unblock technical blog)
- [ ] Port benchmark workload (6-agent marketing office or coding team)
- [ ] Run side-by-side: same workload, WUPHF vs ticket-based, measure tokens
- [ ] Write technical blog post with real numbers
- [ ] Reddit comparison post with real numbers
- [ ] Update landing page with verified comparison table

### WEEK 5 (launch)
- [ ] Publish all content in sequence:
  - Day 1: X thread
  - Day 2: Reddit post (r/ClaudeAI)
  - Day 3: Show HN
  - Day 4: Product Hunt
  - Day 5: Technical blog + Reddit comparison
- [ ] DM Paperclip issue filers (#544, #3335, #3401, #1183)
- [ ] Post in OpenClaw Discord, relevant communities
- [ ] "Switching" guide live on docs site

---

## Part 8: Open Questions

1. Should WUPHF support Paperclip's company templates as an import format? (Would expand
   the "import your existing setup" pitch to template-only users too.)
2. Does the email-postmortem code path stay as-is, or get stripped to a minimal pack spec?
3. What's the right channel context budget per agent turn? (Need to benchmark.)
4. Should the comparison post target Opensoul (marketing) or a coding workflow? Marketing
   is more accessible; coding is where the founder's expertise is.
5. Is Windows binary support worth the effort for v1, or Mac/Linux only?
6. Does WUPHF need Docker, or is the Go binary + JSON state file sufficient?

---

## References

### Paperclip source (cloned locally)
- `packages/adapter-utils/src/types.ts:292-331` — ServerAdapterModule interface
- `server/src/adapters/registry.ts:207-222` — 10 built-in adapters
- `server/src/services/heartbeat.ts` — 4,533-line runtime engine
- `server/src/services/activity-log.ts` — 94-line logActivity invariant
- `server/src/services/budgets.ts:716-830` — layered budget enforcement
- `packages/db/src/schema/heartbeat_runs.ts` — run lifecycle schema
- `packages/db/src/schema/cost_events.ts` — 5-dimensional cost tracking
- `packages/db/src/schema/issues.ts` — 94-line issue schema with atomic checkout
- `ui/src/components/Layout.tsx` — 486-line responsive single layout
- `ui/src/components/IssueChatThread.tsx` — 2,007-line chat surface
- `ui/src/components/PropertiesPanel.tsx` — 29-line render slot
- `doc/SPEC-implementation.md:38` — "Tasks + comments only (no separate chat system)"

### GitHub issues cited
- [#544](https://github.com/paperclipai/paperclip/issues/544) — 10x token consumption, 3 hidden killers
- [#1183](https://github.com/paperclipai/paperclip/issues/1183) — context window management proposal
- [#1348](https://github.com/paperclipai/paperclip/issues/1348) — archived companies burn tokens
- [#1756](https://github.com/paperclipai/paperclip/issues/1756) — token-based budgets needed
- [#2101](https://github.com/paperclipai/paperclip/issues/2101) — retry after rate limits
- [#3335](https://github.com/paperclipai/paperclip/issues/3335) — workspace isolation not default (222 workspaces, 1 cwd)
- [#3338](https://github.com/paperclipai/paperclip/issues/3338) — session tokens in plaintext
- [#3352](https://github.com/paperclipai/paperclip/issues/3352) — can't delete agents (FK violations)
- [#3401](https://github.com/paperclipai/paperclip/issues/3401) — heartbeat burns tokens learning nothing
- [#3402](https://github.com/paperclipai/paperclip/issues/3402) — token burn despite best practices
- [#3406](https://github.com/paperclipai/paperclip/issues/3406) — subscription-aware budgets
- [#3407](https://github.com/paperclipai/paperclip/issues/3407) — routine dispatch duplicate key

### External reviews and articles
- [Towards AI explainer](https://pub.towardsai.net/paperclip-the-open-source-operating-system-for-zero-human-companies-2c16f3f22182)
- [4th Path review — 8.7/10](https://www.the4thpath.com/2026/03/paperclip-ai-review-if-agents-are.html)
- [vibecoding.app review](https://vibecoding.app/blog/paperclip-review)
- [Effloow 14-agent case study](https://dev.to/jangwook_kim_e31e7291ad98/how-we-built-a-company-powered-by-14-ai-agents-using-paperclip-4bg6)
- [HN: noduerme multi-agent wrangling](https://news.ycombinator.com/item?id=47422091)
- [HN: Opensoul marketing stack](https://news.ycombinator.com/item?id=47336615)

### WUPHF prior docs
- `docs/competitive-analysis-multi-agent-projects.md` — WUPHF's own positioning
- `docs/plans/2026-04-11-paperclip-vs-wuphf-grounded.md` — grounded technical comparison
- `~/.gstack/projects/nex-crm-wuphf/najmuzzaman-nazz-email-postmortem-design-20260401-153916.md` — April 1 approved design (now killed in hybrid form)
