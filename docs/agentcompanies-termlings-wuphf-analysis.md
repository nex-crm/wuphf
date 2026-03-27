# Agent Companies vs Termlings vs WUPHF

Date: 2026-03-26

## Executive Summary

These three things are not direct substitutes.

- **Agent Companies** is a **protocol** for describing portable AI companies in markdown.
- **Termlings** is a **runtime/product** for actually operating a local autonomous agent company.
- **WUPHF** is strongest today as a **context/integration substrate** that can make agents smarter across tools and systems.

That matters because the right question is not "which one wins?"
The right question is "which layer should WUPHF own, and which ideas should it borrow?"

My conclusion:

1. **Agent Companies is not better than WUPHF or Termlings as a product.** It is a specification layer. But it is very good at one thing we need: portable, vendor-neutral desired state for companies, teams, agents, projects, tasks, and skills.
2. **Termlings is still better than us today at the runtime/product layer** for an autonomous local AI company. It has a tighter operating loop, clearer shared state, better scheduler/operator primitives, and a more coherent autonomy story.
3. **WUPHF is better than both of them at external context and real-world intelligence**: knowledge graph, integrations, typed business data, cross-tool memory, insights, and context propagation.
4. **The right future for WUPHF is not to become Termlings or Paperclip.** It is to combine:
   - Agent Companies for portable desired-state manifests
   - Termlings-like runtime discipline for operating the company
   - WUPHF/Nex as the context-change and insight engine that tells the company when to act

If we execute that well, WUPHF could become the first system that is:

- company-aware
- skill-aware
- context-aware across tools
- and able to act continuously on important changes in Nex

That is a much stronger position than "terminal Slack clone" or "context graph only."

## Sources

### Agent Companies

- Website: <https://agentcompanies.io/>
- Specification: <https://agentcompanies.io/specification>
- Repo: <https://github.com/agentcompanies/agentcompanies>
- Agent Skills home: <https://agentskills.io/home>
- Specific sources reviewed:
  - <https://github.com/agentcompanies/agentcompanies/blob/main/README.md>
  - <https://github.com/agentcompanies/agentcompanies/blob/main/specification.mdx>
  - <https://github.com/agentcompanies/agentcompanies/blob/main/what-are-agent-companies.mdx>
  - <https://github.com/agentcompanies/agentcompanies/blob/main/client-implementation/adding-skills-support.mdx>

### Termlings

- Website: <https://termlings.com/>
- Docs: <https://termlings.com/docs>
- Repo: <https://github.com/tomtev/termlings>
- Specific sources reviewed:
  - <https://github.com/tomtev/termlings/blob/main/README.md>
  - <https://github.com/tomtev/termlings/blob/main/docs/APPS.md>
  - <https://github.com/tomtev/termlings/blob/main/docs/LIFECYCLE.md>
  - <https://github.com/tomtev/termlings/blob/main/docs/MESSAGING.md>
  - <https://github.com/tomtev/termlings/blob/main/docs/REQUESTS.md>
  - <https://github.com/tomtev/termlings/blob/main/docs/SCHEDULER.md>
  - <https://github.com/tomtev/termlings/blob/main/docs/SECURITY.md>
  - <https://github.com/tomtev/termlings/blob/main/docs/SPAWN.md>

### WUPHF / this repo

- [README.md](../README.md)
- [cli/README.md](../cli/README.md)
- [cli/docs/context.md](../cli/docs/context.md)
- [cli/docs/requirements.md](../cli/docs/requirements.md)
- [cli/docs/slack-tui-architecture.md](../cli/docs/slack-tui-architecture.md)
- [mcp/src/server.ts](../mcp/src/server.ts)
- [docs/termlings-analysis.md](./termlings-analysis.md)

## What Agent Companies Actually Is

Agent Companies is a **portable company package format**.

It is not:

- a TUI
- a runtime
- a scheduler
- a message broker
- a browser layer
- a coordination engine

It is a markdown-first package model for describing:

- company boundaries
- teams
- agents
- projects
- starter tasks
- skill attachments

with progressive disclosure and clean separation between:

- **base protocol**
- **skill protocol**
- **vendor-specific extensions**

That last point is important.

The spec is very explicit that:

- `SKILL.md` stays owned by Agent Skills
- runtime-specific fidelity should stay out of the base package
- vendor-specific config should live in sidecars like `.paperclip.yaml`

That is good protocol hygiene.

## What Termlings Actually Is

Termlings is a runtime and operator product.

It answers questions like:

- how do agents message each other?
- where do tasks live?
- how do humans answer requests?
- how do scheduled nudges happen?
- how do agents spawn?
- what is the shared workspace state?
- how do apps get enabled/disabled per agent?

That is why it feels more "finished" as a company OS.

## What WUPHF Actually Is

WUPHF is strongest today as a context engine and integration substrate.

It answers questions like:

- what does the system know across tools?
- what changed in Slack, meetings, CRM, notes, files, and agent sessions?
- what entities/records/relationships/insights exist?
- how does context get injected into many different agent surfaces?
- how do plugins/hooks/MCP/CLI share the same memory layer?

That is a different center of gravity.

## The Core Comparison

| Dimension | Agent Companies | Termlings | WUPHF |
| --- | --- | --- | --- |
| What it is | Protocol/spec | Runtime/product | Context/integration platform |
| Main artifact | Markdown manifests | Local workspace + runtime | API-backed graph + multi-surface tooling |
| Primary value | Portability | Operability | Intelligence/context |
| State model | Desired state | Local file-backed runtime state | Remote graph + local client/plugin state |
| Team execution | Not provided | Yes | Partial / emerging |
| Skill model | Core | Supports app/skill model | Supports tools/plugins/hooks/MCP |
| Human workflow | Not provided directly | Built-in requests and messaging | Commands + integrations + some TUI flows |
| Autonomy loop | Not provided directly | Local autonomous agent operation | Potentially much stronger if tied to Nex changes |
| External context | Minimal by design | Limited/local | Strong |
| Portability | Strongest | Moderate | Moderate |

## Is Agent Companies Better?

### Better than us at these things

#### 1. Separating desired state from runtime state

This is one of the cleanest ideas in the whole ecosystem.

The spec says:

- `COMPANY.md`, `TEAM.md`, `AGENTS.md`, `PROJECT.md`, `TASK.md` describe intent
- runtime-specific fidelity does not belong there
- current runs, spend, approvals, and machine details are not the base protocol

This is exactly right.

We should copy that discipline.

#### 2. Progressive disclosure as a first-class rule

Agent Companies makes context-budget management part of the protocol:

1. catalog
2. activation
3. resources on demand

That is much more mature than just "load whatever seems relevant."

For WUPHF, this matters because we want:

- lots of organizational structure
- lots of context from Nex
- lots of skills

without blowing out context windows.

#### 3. Portable company packaging

Agent Companies gives us a clean way to represent:

- the company
- the org
- roles
- default tasks
- reusable skill attachments

without tying that description to one runtime or one vendor.

That is valuable if WUPHF should eventually:

- import companies
- export companies
- share company templates
- sync desired-state org design across environments

### Not better than us at these things

#### 1. Runtime behavior

Agent Companies does not tell you:

- how to route messages
- how to schedule work
- how to manage human interview flows
- how to spawn/retire agents
- how to persist active runtime state
- how to react to new external events

It explicitly leaves that open.

So it is not a substitute for building the runtime.

#### 2. External intelligence

Agent Companies has nothing like Nex/WUPHF context graph semantics:

- no records
- no relationships
- no meeting transcripts
- no CRM context
- no integrations
- no insight feed

That is not a flaw. It is just out of scope.

## Is Termlings Better?

### Yes, for runtime/product coherence

Termlings still wins on:

- local operating model
- scheduler
- requests
- messaging
- app model
- spawn/runtime/security clarity
- "this actually feels like one product"

### No, for real external business intelligence

Termlings is weaker than WUPHF on:

- knowledge graph depth
- typed business semantics
- CRM/integration depth
- cross-tool context continuity
- insight generation from external systems

Termlings is much more of a self-contained company runtime.
WUPHF is much more of a company intelligence layer.

## Where WUPHF Is Better Than Both

### 1. Cross-tool context and memory

This is still our clearest advantage.

WUPHF can become the system that notices:

- a new sales blocker in Slack
- a changed deal stage in Salesforce
- a new objection in meeting transcripts
- a pattern in support tickets
- a risky shift in project discussions

and then turns that into company action.

Neither Agent Companies nor Termlings is naturally built for that.

### 2. Insight-driven autonomy

This is the key part of your instruction, and it changes the whole framing.

You do **not** just want a static team of agents.
You want an autonomous system that **acts on context change across tools**.

That means WUPHF’s long-term loop should be:

1. ingest changes from Nex and connected systems
2. detect important changes / insights
3. map those changes into the current company structure
4. decide whether to:
   - ignore
   - notify
   - create/update a task
   - wake an agent
   - open a channel
   - ask the human
   - execute a workflow
5. let the team work with the updated context
6. persist new decisions back into Nex

That is where WUPHF can become something neither of the other two are.

### 3. Real-world company intelligence instead of self-contained simulation

Termlings is good at a synthetic/local company.

WUPHF can be better at a real company because it can be driven by:

- actual meetings
- actual customers
- actual CRM changes
- actual Slack changes
- actual calendar events
- actual emerging insights

That is strategically stronger.

## What We Should Learn From Agent Companies

### 1. Add a desired-state company layer

WUPHF should probably support something very close to Agent Companies.

Not because the spec is trendy.
Because we actually need a clean place to define:

- company defaults
- teams
- channels
- agents
- role instructions
- starter workflows/tasks
- attached skills

without mixing that with live runtime state.

### 2. Use vendor extensions instead of polluting the base model

If we support Agent Companies, WUPHF-specific fields should not leak into the base protocol.

Do this instead:

- base manifests remain portable
- WUPHF adds a sidecar, for example:
  - `.wuphf.yaml`
  - or `metadata.wuphf`

That sidecar can hold:

- Nex mapping rules
- insight routing defaults
- budget/cost controls
- runtime adapter preferences
- notification policies
- action thresholds

### 3. Make graph resolution visible

The client implementation guide is right: activation should operate on a graph, not isolated files.

WUPHF should make it visible:

- what company subtree is active
- what roles/skills are attached
- what projects/tasks were pulled in
- what came from external references

That would help both users and agents.

### 4. Preserve active context deliberately

This is a great rule from the protocol docs:

Once a company/team/agent/skill is active, do not let it fall out of context accidentally during compaction.

That is directly relevant to the office runtime.

## What We Should Learn From Termlings

### 1. Runtime discipline

Termlings is better at turning "agent company" into boring, reliable operating loops.

We should copy:

- explicit operator primitives
- explicit request flows
- explicit scheduler model
- explicit local shared state
- explicit app gating

### 2. App model

We should define real office apps, not just feature piles.

For example:

- messaging
- requests
- tasks
- channels
- browser
- insights
- memory
- integrations
- workflows

Each app should control:

- tool visibility
- prompt visibility
- UI visibility
- allowed agents
- scheduling hooks

### 3. Security posture

We need a clearer and more honest runtime security story.

## UX Comparison

Agent Companies does not really compete on UX because it is a protocol, not a product.
So the real UX comparison here is Termlings vs the WUPHF direction.

### What Termlings gets right beyond visual style

This is the part most worth stealing.

Termlings has much better **terminal product coherence** than we do today.

Not just cleaner screens. Better interaction contracts.

The repo/test surface makes that pretty clear:

- view-specific state is protected explicitly
- request lists preserve selection visibility in bounded viewports
- composer behavior has real product design behind it
- slash-form flows are tightly integrated with the active thread/view
- animation is gated so it only runs when something meaningful is happening
- render caching exists where repeated redraws would otherwise create churn

That means the product is spending effort on:

- legibility
- continuity
- trust
- operator orientation

instead of just "terminal styling."

### Termlings UX strengths

#### 1. Stronger identity system

The avatars and DNA-driven identity do real product work:

- agents feel like recurring characters
- users remember who is who
- the product feels playful and alive
- the company metaphor lands faster

This is not superficial.
Identity is part of navigation in an agent team.

#### 2. Better immediate delight

Termlings understands something important:

if the product is about running a little AI company, then delight is functional.

The cute avatar animations and character cues make:

- the system easier to trust emotionally
- the workspace easier to remember
- the agents easier to distinguish

#### 3. More coherent product feel

Even when the implementation is local-first and rough in places, the UX feels like one world.

#### 4. Better affordances around real work

The best UX lesson is that Termlings makes work primitives feel first-class:

- messages
- requests
- tasks
- calendar
- scheduling

That is stronger than having powerful commands that still feel like separate subsystems.

#### 5. Better keyboard-first terminal design

Its interaction model looks more intentionally terminal-native:

- search/select flows
- segmented editing
- stable composer treatment
- explicit hints
- less accidental redraw chaos

This kind of coherence matters a lot more in terminals than in browsers.

### WUPHF UX opportunity

WUPHF should not copy Termlings’ avatar style.
But we should absolutely learn from what the avatars are doing.

The right WUPHF move is probably:

- Slack-like office structure
- stronger teammate personalities
- funny, memorable Office-style identities
- avatars that feel like weird corporate coworkers
- presence and mood states that make the office feel alive

That means:

- funny but not clownish
- strong role silhouettes
- subtle motion or status cues
- more visible emotional range
- a recognizable WUPHF aesthetic instead of generic agent UI

We should also learn from how Termlings makes a terminal app feel like one system:

- fewer conceptual seams
- stronger first-class work surfaces
- clearer state-specific affordances
- more stable editing and navigation behavior

### Where WUPHF can surpass Termlings on UX

If we combine:

- a stronger office UI
- real channel/thread/task/request structure
- visible Nex-driven notifications
- memorable Office-style avatars and personalities
- real action on external context changes

then WUPHF can become much more compelling than Termlings visually and behaviorally.

Termlings shows that character matters.
WUPHF can go further by making that character live inside a richer office metaphor tied to real-world company signals.

The key is this:

Termlings makes the terminal feel coherent.
WUPHF can make the terminal feel coherent **and consequential**.

That means:

- better terminal affordances
- stronger character and identity
- and actions grounded in real Nex-driven changes across tools

That combination is where our UX can become genuinely better, not just different.

## The Best Combined Architecture For WUPHF

This is the model I think makes the most sense.

### Layer 1: Agent Companies as portable desired state

Use Agent Companies (or a compatible subset) for:

- company
- teams
- roles
- starter projects/tasks
- skill attachments

This gives us:

- portability
- version-controlled company design
- reusable company packages
- compatibility with a growing ecosystem

### Layer 2: WUPHF office runtime as the live operating system

WUPHF should own:

- channels
- message routing
- human requests/interviews
- task execution state
- scheduling
- live membership/presence
- channel management
- runtime budgets and cost
- active company graph resolution

This is the "Termlings runtime discipline" layer.

### Layer 3: Nex as the company intelligence engine

Nex should remain the thing that knows what changed across reality.

That means:

- context graph
- entity changes
- insights
- transcripts
- CRM changes
- cross-tool memory
- proactive signals

This is what makes WUPHF more than a local toy runtime.

### Layer 4: Autonomy policy engine

This is the part we should build much more aggressively.

WUPHF should decide, based on change type and current company graph:

- whether the CEO should summarize to channel
- whether a task should be created
- whether an existing task should be updated
- whether a specialist should be nudged
- whether a human request is required
- whether the system should stay quiet

This is the missing "action on context change" layer.

## What WUPHF Should Build Next

### 1. A real change-to-action pipeline

Not just "poll insights and summarize."

We need:

- change ingestion
- prioritization
- action classification
- routing
- task creation/update
- human escalation
- channel selection
- follow-up scheduling
- audit trail

### 2. Desired state vs live state split

Adopt Agent Companies-compatible manifests for desired state.
Keep office runtime state separate.

Examples:

- desired state:
  - company structure
  - teams
  - roles
  - skills
  - templates
- live state:
  - open channels
  - active members in channels
  - tasks in progress
  - pending human requests
  - schedules
  - costs
  - live runtime sessions

### 3. Stronger autonomy gating

Because WUPHF is fed by real external systems, it can go wrong in more consequential ways than a local toy runtime.

So actions should be gated by explicit policy:

- notify only
- task only
- human approval required
- safe auto-act
- do not act

### 4. Request system, not just interviews

Turn human interview into a full request/approval subsystem.

### 5. Skill attachment model

If we adopt Agent Companies, roles should attach skills cleanly.

Then WUPHF can decide:

- which skills stay active
- which skills to activate on demand
- which external references to pull in

## Bottom Line

If the question is:

### "What should WUPHF become?"

My answer is:

WUPHF should become an **autonomous company intelligence runtime**.

That means:

- **Agent Companies** provides portable company structure
- **Termlings-like runtime discipline** provides clean local operation
- **Nex/WUPHF context graph** provides real-world situational awareness

So the product should not just be:

- "a team of agents chatting"

It should be:

- a company that knows what changed across tools
- decides whether that change matters
- routes it through the right people/channels/tasks
- asks humans only when needed
- and keeps learning from what happened

That is the most defensible direction of the three.

## Plain Answer To "Which One Is Best?"

- **Best protocol/design layer:** Agent Companies
- **Best current local runtime/product:** Termlings
- **Best current context/integration intelligence layer:** WUPHF

The winning WUPHF strategy is not to copy one of them.

It is to combine:

- Agent Companies for portability
- Termlings for runtime discipline
- WUPHF/Nex for intelligence and action on context change

That combination is the most ambitious thing here, and it is also the one that could actually matter the most.
