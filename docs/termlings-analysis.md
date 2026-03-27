# Termlings Analysis

Date: 2026-03-26

## Executive Summary

Termlings is not just "our thing with a cleaner UI."

It is a more opinionated, more internally coherent product in one specific direction: a local, file-backed, autonomous company OS for agents running in Claude/Codex. It treats messaging, tasks, requests, browser, calendar, CRM, CMS, social, analytics, finance, and memory as first-class local apps inside one workspace model.

This repo is stronger in a different direction: external context infrastructure. We are better at cross-tool memory, context graph/querying, CRM-style entity operations, integrations, plugin surfaces, proactive recall/capture, and turning many different agent surfaces into one knowledge substrate.

So the short answer is:

- If the question is "which project is the cleaner autonomous terminal company product today?", Termlings is ahead.
- If the question is "which project is the stronger context/integration layer for many external agent surfaces?", we are ahead.
- If the question is "what should we learn?", the answer is: a lot, mostly around product coherence, runtime architecture, and making apps first-class instead of prompt theater.

## Sources

### Termlings

- Website: <https://termlings.com/>
- Docs: <https://termlings.com/docs>
- Repo: <https://github.com/tomtev/termlings>
- README: <https://github.com/tomtev/termlings/blob/main/README.md>
- Docs folder: <https://github.com/tomtev/termlings/tree/main/docs>
- Specific docs reviewed:
  - <https://github.com/tomtev/termlings/blob/main/docs/APPS.md>
  - <https://github.com/tomtev/termlings/blob/main/docs/LIFECYCLE.md>
  - <https://github.com/tomtev/termlings/blob/main/docs/MESSAGING.md>
  - <https://github.com/tomtev/termlings/blob/main/docs/REQUESTS.md>
  - <https://github.com/tomtev/termlings/blob/main/docs/SCHEDULER.md>
  - <https://github.com/tomtev/termlings/blob/main/docs/SECURITY.md>
  - <https://github.com/tomtev/termlings/blob/main/docs/SPAWN.md>
  - <https://github.com/tomtev/termlings/blob/main/docs/TERMLINGS.md>

### This repo

- [README.md](../README.md)
- [cli/README.md](../cli/README.md)
- [cli/docs/context.md](../cli/docs/context.md)
- [cli/docs/requirements.md](../cli/docs/requirements.md)
- [cli/docs/slack-tui-architecture.md](../cli/docs/slack-tui-architecture.md)
- [mcp/src/server.ts](../mcp/src/server.ts)

## What Termlings Actually Is

Termlings is a local workspace operating system for agent teams.

Its core design choices are:

- local-first and file-backed
- one workspace root, one `.termlings/` state tree
- one runtime mental model for CLI, TUI, server, and agents
- first-class agent apps instead of a thin "chat + tools" wrapper
- explicit operator/agent coordination primitives: messaging, requests, tasks, scheduler, spawn, browser

The important thing is not the pretty TUI. The important thing is that the product model is internally consistent.

It is very clear what the source of truth is:

- `.termlings/store/messages/*` for communication
- `.termlings/store/tasks/tasks.json` for tasks
- `.termlings/store/requests/*.json` for operator requests
- `.termlings/store/calendar/calendar.json` for time
- `.termlings/store/...` for each app domain

That clarity matters a lot.

## What This Repo Actually Is

This repo is stronger as a context/memory/integration substrate than as a single coherent autonomous company OS.

Its core strengths today are:

- cross-tool knowledge graph
- API-backed context querying and ingestion
- integrations with Slack, Gmail, calendars, Salesforce, HubSpot, Attio, etc.
- MCP surface for many structured operations
- CLI + plugin + hook surfaces for multiple agent tools
- proactive recall/capture
- graph/entity/task/record/list/note/relationship semantics

The multi-agent office idea is here in design and partial implementation, but compared to Termlings it is still less unified as a single runtime/product story.

Put bluntly:

- Termlings feels like one product with many apps.
- This repo feels like one powerful context platform with several partially merged product directions on top.

## High-Level Differences

| Area | Termlings | This repo |
| --- | --- | --- |
| Product center | Autonomous local team workspace | Knowledge/context graph across tools |
| State model | Local file-backed workspace store | Remote/API-backed graph + local CLI/plugin state |
| Agent coordination | First-class, built-in | Present, but less product-coherent |
| Human input | Requests app, DM/chat model | Commands + integrations + graph ops |
| Tasks | Core local app | API-backed task model |
| Memory | Local file memory app | Central knowledge graph + ingestion |
| Security story | Explicit and operator-friendly | Stronger on integrations, weaker on runtime-story coherence |
| Runtime story | Spawn presets, scheduler, apps, Docker modes | Many surfaces; less single-path operator story |
| External systems | Lighter and more webhook/file oriented | Much stronger |
| MCP/tool breadth | Narrower but product-coherent | Much broader |

## Is Termlings Better?

### Yes, in these ways

#### 1. It has a much more coherent runtime model

Termlings has a very legible loop:

1. initialize workspace
2. spawn agents
3. agents get consistent injected context
4. all shared state lands in `.termlings/store/*`
5. TUI, CLI, and server all read the same store

That is better than having multiple partially overlapping state/control planes.

#### 2. It treats "apps" as a real architecture boundary

This is one of the best ideas in the project.

Apps are:

- structured
- enable/disable-able
- agent-scoped
- reflected in prompt injection
- reflected in help/TUI/runtime access

That is much better than sprinkling capabilities across prompts, slash commands, and tools without one explicit availability model.

#### 3. It is more honest about autonomy and safety

Their security docs are unusually grounded.

They are explicit that:

- YOLO host-native spawn is convenience, not security
- Docker is safer but still not a perfect exfiltration boundary
- the real boundary is runtime sandbox + OS/container/VM

That honesty builds trust.

#### 4. It has better operator primitives

The combination of:

- `message`
- `conversation`
- `request`
- `task`
- `scheduler`
- `spawn`

is a much tighter operator experience than "chat plus a huge tool catalog."

#### 5. It is stronger on "running a team" than "querying a graph"

Termlings feels built around:

- assigning work
- nudging teammates
- scheduled followups
- asking the human for secrets or decisions
- claiming ownership

That is the core loop of an autonomous team product.

This repo is less mature there.

## Where We Are Better

### 1. We are much stronger on real external context

This is the biggest difference.

We have a real context graph story:

- ask/query context
- ingest notes/transcripts/files
- insights
- records/lists/notes/tasks/relationships
- graph visualization
- external integrations

Termlings has local memory and local app stores. That is useful. But it is not the same thing as a real cross-tool context graph with external business systems behind it.

If the user wants:

- "what do I know about this customer across Slack, meetings, CRM, and prior agent work?"

we are much better positioned.

### 2. We are stronger as infrastructure for many agent surfaces

This repo is designed to work across:

- Claude Code
- Codex
- OpenClaw
- Cursor / other editors
- MCP clients
- hooks/plugins/skills

Termlings is much more vertically integrated around its own workspace/runtime model.

That makes them more coherent, but us more extensible as a platform layer.

### 3. We have richer typed business operations

The MCP surface here includes:

- schema
- records
- lists
- notes
- tasks
- relationships
- insights
- integrations

That is much closer to a real operational memory system than Termlings’ local JSON app records.

### 4. We are better at "memory follows you across tools"

That is still a real differentiator.

Termlings is great if the team lives inside Termlings.
We are better if the user’s workflow spans multiple tools and they need durable, queryable shared context across them.

## Where Termlings Is Better Today

These are the real gaps.

### 1. Product coherence

Termlings has a cleaner product center.

This repo still carries multiple identities:

- knowledge graph platform
- CLI utility surface
- plugin ecosystem
- experimental Slack-style multi-agent office

Termlings feels like it decided what it is.

### 2. First-class local shared state

Their `.termlings/store/*` design is simple and powerful.

It gives them:

- predictable debugging
- easy operator inspection
- low-dependency runtime continuity
- a shared truth for TUI/CLI/server

We have more power, but also more indirection.

### 3. Requests as a product feature, not an afterthought

Their `request` app is excellent product design:

- env secrets
- confirm
- choice
- poll/check semantics
- clear operator workflow

This is a stronger pattern than loosely scattered "human interview" or ad hoc blocking prompts.

### 4. Scheduler as a real subsystem

They made time a first-class concern:

- scheduled DMs
- task reminders
- app syncs
- social publishing
- CMS publishing
- calendar events

That is the kind of boring infrastructure that makes an agent company feel alive.

### 5. Better runtime discipline

Spawn routes, presets, Docker modes, and per-agent app allowlists are not just docs. They are part of the architecture.

This matters. It reduces ambiguity.

### 6. Better docs discipline

Termlings does a good job of separating:

- high-level operator docs
- internal lifecycle/runtime notes
- app-by-app docs
- security posture

This repo has strong ideas in docs, but the product narrative is less tightly edited.

## Where We Should Not Copy Them

Not everything in Termlings is better.

### 1. Do not replace the knowledge graph with local JSON stores

That would be a strategic downgrade for us.

Their local-first store is good for coordination apps. It is not a substitute for our graph/integrations layer.

### 2. Do not collapse everything into one closed workspace model

One of our strengths is that context can follow the user across tools, editors, CLIs, and hooks. We should not trade that away for a more self-contained terminal experience.

### 3. Do not narrow the data model to "startup workspace apps"

Our richer entity/relationship/list/note/task/integration model is a real moat if we execute it well.

## What We Should Learn From Termlings

### 1. Create a real app model

This is the highest-value lesson.

We should define first-class office apps, for example:

- messaging
- requests / human interview
- tasks
- channels
- browser
- insights
- integrations
- memory/context

Then each app should control:

- tool exposure
- prompt/context exposure
- UI panels
- slash commands
- agent availability

Right now we often do this implicitly. Termlings does it explicitly.

### 2. Make shared state painfully legible

Even if the graph stays remote/API-backed, the office layer should still have a very legible local state model for:

- channels
- tasks
- requests
- schedules
- presence
- session status

The current office vision needs this.

### 3. Treat operator requests as a core subsystem

We should upgrade "human interview" into a full requests system with:

- confirm
- choice
- secret/env request
- status tracking
- resolution history
- queue/pending list

This is one of the most reusable pieces in Termlings.

### 4. Make scheduling boring and powerful

We should copy the scheduler mindset, not necessarily the exact implementation.

That means:

- recurring followups
- due-date task reminders
- periodic insight sweeps
- scheduled operator prompts
- channel nudges

### 5. Separate product surfaces more cleanly

Termlings is easier to understand because it draws firmer lines:

- operator commands
- agent apps
- runtime docs
- lifecycle docs
- security docs

We should do the same.

### 6. Be more explicit about security and autonomy

We should explain, in plain English:

- what happens when agents run with high autonomy
- what the trust boundary is
- what is local vs remote
- what credentials agents can touch
- what `--no-nex` really changes

### 7. Tighten the "team operating system" loop

The office should feel like:

- message
- task
- request
- schedule
- act

not:

- giant tool catalog
- some chat
- some memory
- maybe agents

## UX Comparison

This is not just "their UI is clean."
Their UX is better in ways that affect comprehension, trust, and delight.

### What they are doing well beyond avatars

This is the more important UX lesson.

Termlings has a lot of terminal coherence:

- the UI primitives match the operating model
- the motion is restrained and purposeful
- the keyboard affordances are legible
- viewports preserve context instead of yanking the user around
- composer behavior is treated as serious product design, not an afterthought

Even from the repo/tests/render helpers, several things are clear:

- animation only runs while the UI is actually active
- speaking avatars animate only when relevant
- request selection stays visible in its viewport
- slash-command forms keep visual continuity with the composer
- schedule flows have inline hints and target-aware defaults
- message layout caching avoids unnecessary redraw churn

That is all terminal UX discipline, not just aesthetics.

### Where Termlings is better

#### 1. Stronger visual identity

Termlings has a clear product personality.

The avatar/DNA system does a lot of work:

- agents feel like persistent characters
- the workspace feels playful instead of generic
- identity is visible at a glance
- the system feels like a world, not just a tool palette

That matters more than people think. When users are managing a team of agents, memorable identity helps them track who is who.

#### 2. Better "toy that wants to become a real product" energy

Termlings looks like someone cared about delight:

- cute avatar rendering
- a recognizable brand shape
- a more opinionated visual mood
- stronger feeling of "these are little coworkers"

That makes the system easier to emotionally parse.

#### 3. More visible affordances around the runtime

Their UI and docs together make the operating model legible:

- spawn
- schedule
- message
- request
- task

Even when the design is not literally Slack, the system feels like it knows what its primitives are.

#### 4. Better onboarding into the mental model

The combination of avatars, org-chart language, SOUL files, and direct commands makes the "company" concept easier to understand for a new user.

#### 5. Better terminal affordances and interaction coherence

This is the part we should study most carefully.

Termlings does a better job making the terminal feel like a designed application instead of a pile of text regions.

Examples:

- **First-class views**: messages, requests, tasks, and calendar are explicit app-level views, not just commands or secondary panels.
- **Stable viewports**: tests explicitly protect selected request visibility and render behavior inside bounded viewports.
- **Composer as a product surface**: the composer has ghost hints, token highlighting, schedule-aware defaults, segmented time editing, searchable timezone selection, and consistent background treatment even through ANSI resets.
- **Meaningful motion**: animation only occurs while there is real activity. That prevents terminal churn and keeps motion informative.
- **Explicit state cues**: typing, status, recurrence, selected fields, and thread targeting are all visually surfaced.
- **Keyboard-first ergonomics**: many flows are designed around arrow-key navigation, segmented editing, and slash-command expansion in place.

This adds up to something important:

The UI feels internally reliable.

### Where our current UX is weaker

#### 1. We still have more concept drift between product story and interface reality

We talk about:

- office
- channels
- autonomous company
- context-driven action

But the UI/runtime contract is still not as tight as Termlings’ contract between:

- apps
- views
- commands
- stored state

#### 2. Our affordances are not always backed by one crisp operating model

We often have:

- command surface
- MCP/tool surface
- Slack-style UI ideas
- graph operations
- agent runtime ideas

without one obvious, stable hierarchy of:

- what is a view
- what is a workflow
- what is a command
- what is a background system

#### 3. We under-invest in terminal micro-coherence

Termlings seems to care about:

- cursor placement
- scroll stability
- animation discipline
- hint text
- field editing semantics
- keeping panels visually continuous

Those are not glamorous, but they are the difference between:

- "this feels like a product"
- and
- "this feels like a prototype"

### Where we are better or can be better

#### 1. We have a stronger eventual UX direction if we finish it

A Slack-like office with:

- channels
- threads
- human interview/request cards
- agent presence
- system messages from Nex
- contextual tasking driven by real-world signals

could become a much more compelling UX than Termlings' current local-OS feel.

The issue is not that the idea is weak.
The issue is that it is not fully coherent yet.

#### 2. We have a better source of "real reasons to act"

A beautiful UI is much more convincing when the agents are reacting to real company signals:

- meetings
- CRM changes
- Slack changes
- insights
- tasks

That can create a richer UX than local-only team chatter.

#### 3. We can develop a much stronger thematic identity

Termlings has cute abstract creatures.

We do not need to copy that.
We should develop our own identity system that is unmistakably WUPHF.

The obvious direction is The Office-inspired teammate identity:

- funny but dry
- slightly chaotic
- memorable role archetypes
- avatars that feel like "paper-company coworkers turned AI team"

That could include:

- Office-style avatar archetypes by role
- subtle idle animations or mood states
- more characterful status text
- reaction-level humor without turning the product into a joke

The point is not fandom cosplay. The point is recognizability.

### UX lessons we should steal from Termlings

#### 1. Persistent, characterful identity

Every teammate should feel visually distinct.

We should add:

- stronger avatar system
- motion/animation where terminal constraints allow
- memorable presence states
- role-specific styling

#### 2. Delight is not optional

If the product is "run a company with agents," then joy and personality are part of usability.

Users need:

- clear identity
- emotional legibility
- a sense of momentum
- occasional humor

#### 3. Visual hierarchy should reinforce the operating model

If messaging, tasks, requests, and schedules are the main primitives, the UI should make those primitives obvious immediately.

#### 4. The system should feel alive even before it is useful

Termlings’ avatars and character cues help it feel alive early.
We should do the same, but in WUPHF’s own voice.

#### 5. Treat terminal UX as interaction design, not just rendering

The main thing to steal is not the visuals.

It is the way Termlings seems to think in terms of:

- focused views
- stable editing surfaces
- bounded motion
- state-specific affordances
- explicit keyboard mechanics
- UI behavior protected by tests

That is real product craft.

### Recommended WUPHF UX direction

Do not imitate Termlings visually.

Instead:

- keep the Slack-like office structure
- make teammates much more characterful
- build a distinctive Office-inspired avatar/personality system
- make Nex/system messages feel like a real office automation layer
- make presence, tasks, requests, and insights visually first-class

Termlings proves that cute matters.
For WUPHF, the right answer is not "cute aliens."
It is "funny, memorable coworkers in an Office-style operating room."

And beyond that, Termlings proves that **terminal coherence** matters:

- fewer ambiguous surfaces
- stronger view/state contracts
- better composer behavior
- more reliable keyboard affordances
- more disciplined motion

That is the deeper UX lesson we should take.

## Concrete Opportunities For This Repo

### Short term

- Add a first-class app/capability model instead of scattered enablement.
- Upgrade human interview into a full requests subsystem.
- Make task ownership, channel membership, and scheduling more explicit in the office layer.
- Write a clear security/runtime model document.

### Medium term

- Create one local office state layer for channels, presence, requests, schedules, and assignments.
- Keep graph/integrations remote, but make the office state local and inspectable.
- Give every office app a consistent UI/tool/prompt contract.

### Long term

- Combine our stronger graph/integration substrate with a Termlings-level coherent local operator runtime.
- That would be better than either project on its own:
  - their runtime coherence
  - our memory/integration depth

## Bottom Line

Termlings is not "better overall."
It is better at being a terminal-native agent company product.

We are better at being an AI memory/context platform with real integrations and typed business operations.

The uncomfortable part is this:

If someone asked today, "which one feels more like a finished product for running a local AI team in the terminal?", I would point them to Termlings.

If they asked, "which one has the better long-term foundation for cross-tool memory, CRM context, meeting transcripts, and real external business data?", I would point them here.

The real lesson is not to imitate their aesthetics.

The real lesson is to steal their product discipline:

- one coherent runtime story
- first-class apps
- explicit state
- explicit requests
- explicit scheduling
- explicit security posture

That is the part we should learn from immediately.
