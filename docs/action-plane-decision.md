# WUPHF Action Plane Decision: Composio vs One

## Scope

This decision is intentionally narrow.

We are **not** trying to replace the current Nex/Nango-backed context path.
We are **not** trying to redesign all integrations.

We only need to solve three missing abilities in WUPHF:

1. **Direct action execution**
   - While working, an agent can realize "I should send that email" or "I should update HubSpot" and actually do it.
2. **Workflow creation**
   - While working, an agent can create a reusable multi-step automation that acts across integrations.
3. **Trigger registration**
   - While working, an agent can register a trigger like:
     - "when a Gmail message arrives"
     - "when a HubSpot deal changes"
     - "then do something in Slack / CRM / email / WUPHF"

That is the whole objective.

## Current WUPHF reality

WUPHF already has the coordination side:

- visible office runtime
- persisted signals / decisions / actions / watchdogs
- tasks / requests / calendar
- skills
- scheduler
- CEO-led routing and cross-channel bridging
- Claude Code agent sessions

What it does **not** have is a first-class **external action plane**.

Today an agent can decide:

- "we should send an email"
- "we should update HubSpot"
- "we should register a trigger"
- "we should run this every day at 9am"

but it cannot reliably do those things through a real cross-system execution layer.

## The architecture simplification we should adopt

We should stop expecting one provider to own everything.

For WUPHF, the correct split is:

- **Context plane**
  - current Nex/Nango-backed read/sync/insight path
  - untouched in this phase
- **Action plane**
  - direct external actions
  - workflow execution
  - trigger registration and inbound trigger events

And the ownership model should be:

- **WUPHF owns**
  - why something ran
  - which signal / decision / task caused it
  - whether approval was required
  - visible ledger and office surfaces
  - schedule
- **Provider owns**
  - action catalog / search
  - action execution
  - workflow runtime
  - trigger registration
  - inbound event delivery

That removes most of the confusion.

## What One appears to be good at

Official sources:

- One CLI: <https://github.com/withoneai/cli>
- One MCP: <https://github.com/withoneai/mcp>
- Older Pica repo: <https://github.com/withoneai/pica>

What the public material supports:

- clear CLI-first action flow
  - `one list`
  - `one actions search`
  - `one actions knowledge`
  - `one actions execute`
- workflow artifact model
  - `one flow create`
  - `one flow execute`
  - flow files under `.one/flows/...`
- small MCP surface
- good fit for existing terminal agents

Important caveats:

- the execution path is proxied through One's hosted API
- the old `pica` repo is community/open but explicitly no longer actively maintained
- the public trigger/event story is not nearly as explicit as the action and flow story

### Practical reading on One

If our only goal were:

- "give agents the ability to directly take actions"
- and "give them a simple workflow artifact model"

One would be a very strong first choice.

## What Composio appears to be good at

Official sources:

- Repo: <https://github.com/ComposioHQ/composio>
- Docs: <https://docs.composio.dev/docs>
- Triggers: <https://docs.composio.dev/docs/triggers>
- CLI: <https://docs.composio.dev/docs/cli>

What the public material supports:

- broad connector and toolkit coverage
- direct tool execution model
- explicit trigger model
  - webhook triggers
  - polling triggers
  - app-specific trigger docs
- CLI and SDK support
- workbench/runtime support for multi-step execution

Important caveats:

- it is a bigger platform surface than we strictly need
- it can pull architecture toward "provider runtime as the center" if we are not disciplined
- some of the richer workbench/runtime ideas are more than WUPHF needs for phase 1

### Practical reading on Composio

Composio is a better fit when the requirement is not just actions, but:

- actions
- triggers
- workflow-ish execution

all together in one provider.

## Side-by-side against our three actual requirements

### 1. Direct action execution

**One**
- Strong fit.
- Simpler, cleaner CLI model for terminal agents.

**Composio**
- Also strong.
- Probably broader coverage.

### 2. Workflow creation

**One**
- Strong fit.
- The explicit `flow create` / `flow execute` model maps nicely to WUPHF skills.

**Composio**
- Good enough, though the public product shape is less "workflow artifact" and more "tool execution + runtime/workbench."
- Still usable for this requirement.

### 3. Trigger registration

**One**
- This is the weak point in the public evidence.
- There are hints of relays/events, but the public docs are not as explicit or mature here.

**Composio**
- Stronger fit.
- Trigger support is clearly documented and feels first-class rather than incidental.

## Decision

There are actually two decisions here:

1. **Which provider should back WUPHF's first action plane?**
2. **What execution surface should WUPHF agents see?**

Those should not be the same decision.

### Provider decision

If we want **one provider now** to solve all three missing abilities, WUPHF should start with **Composio**, not One.

Reason:

- both can help on direct actions
- One looks cleaner for action + workflow alone
- but triggers are one of the three explicit requirements, not an optional nice-to-have
- Composio's trigger support is much more clearly documented and productized today
- Composio's broader tool and runtime surface makes it less likely that we hit a hard coverage wall and need to swap providers quickly

### Agent-facing surface decision

WUPHF agents should **not** talk directly to a vendor CLI as their long-term interface.

Instead, WUPHF should expose a **small One-like action surface** of its own:

- search
- knowledge
- execute
- workflow create / execute
- trigger create / list / delete

That gives us the best part of One:

- clean, agent-native ergonomics
- simple mental model
- workflow-friendly shape

without forcing WUPHF to bet on One's weaker public trigger story.

So the real recommendation is:

- **Use Composio as the backend provider for phase 1**
- **Expose a One-like WUPHF-native tool surface to agents**
- **Do not integrate One and Composio at the same time**
- **Do not touch the Nex/Nango context plane yet**

## What that means strategically

The real decision is not "Composio is better than One in general."

It is:

> For WUPHF's narrowly scoped missing capability set, Composio is the safer provider backend, while One is the better inspiration for the agent-facing interface.

If later we find that:

- Composio is too heavy,
- the CLI/operator ergonomics are poor,
- or One's trigger story is actually stronger in practice than its public docs suggest,

then we can re-run the comparison with real pilot results.

But phase 1 should not start with two providers.

The important part is:

- do **not** confuse provider backend choice with agent UX choice
- do **not** let a vendor CLI become the permanent system contract

## Implementation plan

### Phase 1: Add a provider-agnostic action plane inside WUPHF

Add a new package:

- `internal/action`

Core types:

- `Provider`
- `Connection`
- `ActionSearchResult`
- `ActionKnowledge`
- `ExecutionRequest`
- `ExecutionResult`
- `WorkflowSpec`
- `WorkflowResult`
- `TriggerSpec`
- `TriggerResult`
- `ApprovalMode`

This layer must stay provider-agnostic even though phase 1 only implements Composio.

### Phase 2: Implement a Composio provider wrapper

Add:

- `internal/action/composio.go`

Responsibilities:

- list connected accounts / connections
- search actions or tools
- fetch action/tool knowledge
- execute actions
- materialize workflow execution
- register / list / delete triggers
- normalize provider output into WUPHF types

Default posture:

- reads can run autonomously
- external writes default to approval in phase 1
- destructive or customer-facing actions always require approval in phase 1

### Phase 3: Expose a WUPHF-native One-like tool surface to agents through team MCP

Add provider-agnostic tools in:

- `internal/teammcp/server.go`

Tool surface:

- `team_action_connections`
- `team_action_search`
- `team_action_knowledge`
- `team_action_execute`
- `team_action_workflow_create`
- `team_action_workflow_execute`
- `team_action_triggers`
- `team_action_trigger_create`
- `team_action_trigger_delete`

These should describe capabilities in office/WUPHF terms, not provider marketing terms.

The mental model for agents should mirror the simpler One-style flow:

1. search
2. inspect knowledge/schema
3. execute
4. save as workflow if repeated
5. register trigger if event-driven

That keeps the agent experience clean even if Composio is the backend.

### Phase 4: Make actions visible in the office ledger

Extend broker ledger records with:

- `external_action_planned`
- `external_action_executed`
- `external_action_failed`
- `external_workflow_created`
- `external_workflow_executed`
- `external_trigger_registered`
- `external_trigger_received`

Every action record should include:

- provider
- platform
- action key
- task / signal / decision linkage
- approval mode
- result summary
- retryability

WUPHF should remain the visible source of truth.

### Phase 5: Use WUPHF skills as the human-readable wrapper

Generated skills should be able to store:

- provider action references
- workflow definitions
- trigger specs
- approval policy
- expected inputs / outputs

This lets an agent do things like:

- discover a repeated pattern
- create a skill for it
- schedule it in WUPHF
- or bind it to an external trigger

without making provider artifacts the top-level business logic source of truth.

### Phase 6: Keep schedule inside WUPHF

This is important.

For schedules like:

- "run every day at 9am"
- "send me a daily digest"

WUPHF should keep using its own scheduler and then invoke the provider execution path.

Do **not** move scheduling into the provider just because the provider can execute workflows.

That preserves:

- office visibility
- CEO/policy intervention
- auditability
- one timing model

### Phase 7: Trigger ingress

For external events like:

- Gmail received
- HubSpot deal change

use provider trigger registration, but feed the resulting event into WUPHF as an `office_signal`.

That way:

- the event is visible in `Insights`
- policy can decide what to do
- CEO can still triage when needed
- the system remains a visible operating environment rather than a hidden automation mesh

## Acceptance criteria

We should consider phase 1 successful only if all of these are true:

1. An agent can discover and execute a real external action while working.
2. An agent can create a reusable workflow artifact through WUPHF.
3. WUPHF can schedule that workflow natively at a given time.
4. An agent can register a real external trigger on a connected account.
5. A fired trigger becomes a visible `office_signal`.
6. Executed actions are visible in WUPHF ledger/UI with approval state and result.
7. Customer-facing or destructive actions require approval in phase 1.
8. The current Nex/Nango read/context path remains untouched.

## Final recommendation

For this specific problem, the best next move is:

1. stop evaluating multiple providers at once
2. keep context ingestion separate
3. implement a provider-agnostic `internal/action` layer
4. use **Composio first as the backend**
5. expose a **One-like WUPHF-native action UX** to agents
6. keep One on the bench as a later alternative if we discover that:
   - Composio is too heavyweight,
   - or One's real trigger story is better than the current public docs suggest

That is the least confusing path and the one most likely to close the actual product gap without creating a vendor-shaped architecture we regret later.
