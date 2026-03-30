# One to Composio Migration Plan

## Goal

Keep the current WUPHF agent UX that feels good:

- search for an action
- inspect the schema / usage
- execute it
- create a workflow
- execute a workflow
- register triggers / relays

But remove `One` as a required dependency before WUPHF becomes useful.

The product contract should belong to WUPHF, not to One or Composio.

## Decision

WUPHF should keep a `One-style` agent experience, but migrate the backend substrate toward `Composio`.

That means:

- agents continue using WUPHF-owned tools like:
  - `team_action_connections`
  - `team_action_search`
  - `team_action_knowledge`
  - `team_action_execute`
  - `team_action_workflow_create`
  - `team_action_workflow_execute`
  - `team_action_trigger_create`
  - `team_action_triggers`
- WUPHF decides which provider satisfies those tools:
  - `one`
  - `composio`
  - later, anything else

## Why

### Why keep the current UX

One has the better current agent mental model.

Its public surface maps well to how agents think:

- discover capability
- inspect action
- dry-run
- execute
- create a reusable flow
- connect a trigger / relay

That UX is worth preserving.

### Why move off One dependency

We do not want WUPHF usefulness to depend on:

- a separate One provisioning step
- One-specific setup before integrations work
- One-specific workflow artifacts being our source of truth

### Why Composio is the stronger backend substrate

Composio has the better SDK story and the better-documented trigger surface.

For the things we actually need:

- actions across integrations
- trigger registration
- agent-created workflows

Composio looks like the better long-term substrate, even though its raw UX is less elegant than One's.

## Non-goals

- Do not rewrite WUPHF around a vendor SDK.
- Do not expose vendor-native terms directly to users or agents.
- Do not make `.one` or Composio-specific artifacts the business logic source of truth.
- Do not block current users on a hard migration before parity exists.

## Product rule

WUPHF owns the user-facing and agent-facing contract.

Vendors only implement the backend.

So:

- `One UX`, `WUPHF contract`, `Composio backend`

## Target architecture

### 1. WUPHF provider-neutral action plane

Create and keep a provider-neutral action interface under `internal/action`.

It should own these concepts:

- `ListConnections`
- `SearchActions`
- `ActionKnowledge`
- `ExecuteAction`
- `CreateWorkflow`
- `ExecuteWorkflow`
- `ListTriggers`
- `CreateTrigger`
- `DeleteTrigger`
- `GetTriggerEvent`

WUPHF tools call this interface, never vendor code directly.

### 2. WUPHF-native workflow spec

WUPHF should define its own workflow spec.

That spec should include:

- workflow key
- title
- purpose
- inputs
- steps
- provider bindings
- approval policy
- schedule
- trigger definition

The source of truth is WUPHF.

Providers get compiled artifacts:

- One flow JSON
- Composio workflow / run config
- or plain WUPHF execution if needed

### 3. WUPHF-native trigger spec

WUPHF should define its own trigger contract:

- source app
- event type
- filters
- delivery mode
- resulting office action
- associated workflow / skill

Again, providers implement it. WUPHF owns it.

## Migration phases

### Phase 0. Freeze the UX contract

Do not change the agent-facing tool contract while migrating.

The current One-style flow becomes the stable WUPHF contract:

1. list connections
2. search actions
3. inspect schema / knowledge
4. dry-run if risky
5. execute
6. create / execute a workflow
7. register / inspect triggers

This is what we preserve.

### Phase 1. Normalize provider interfaces

Refactor `internal/action` so One is just one provider implementation.

Add:

- `Provider`
- `WorkflowProvider`
- `TriggerProvider`
- provider capability flags

Capabilities should at least include:

- `action_execute`
- `workflow_create`
- `workflow_execute`
- `trigger_create`
- `trigger_receive`
- `dry_run`

This lets WUPHF degrade gracefully when a provider is missing a feature.

### Phase 2. Add Composio provider

Implement a `composio` provider behind `internal/action`.

Important constraint:

WUPHF is Go. Composio's public SDK story is not native Go-first, so this should be implemented as a sidecar / adapter boundary, not by polluting the Go app with vendor-specific runtime assumptions.

That provider should first support:

- list connections / linked accounts
- search tools / actions
- inspect tool schema
- execute tool
- create triggers
- list / delete triggers

Do not make workflows the first migration dependency.

### Phase 3. Move workflows to WUPHF-owned specs

Before replacing One completely, stop depending on One-native flows as the source of truth.

Do this:

- WUPHF skill builder generates WUPHF workflow specs
- One or Composio providers compile those specs for execution
- Skills, Calendar, and Insights stay WUPHF-native

This is the most important anti-lock-in step.

Without it, switching providers later will be painful again.

### Phase 4. Dual-run and parity test

Run both providers behind the same WUPHF contract and compare them on real scenarios.

Required scenarios:

1. Gmail send email
2. HubSpot create/update note or task
3. Slack post message
4. Scheduled daily digest workflow
5. Incoming email trigger -> office action
6. CRM trigger -> workflow execution

For each scenario, measure:

- setup friction
- connection reliability
- schema quality
- action success
- dry-run support
- trigger reliability
- visibility / observability in WUPHF

### Phase 5. Default switch

Switch WUPHF default action provider to Composio only when:

- actions are at parity or better
- triggers are clearly better
- WUPHF-native workflow specs are in place
- user setup no longer depends on One for common cases

One can remain as an optional provider during transition.

### Phase 6. Remove One as a required dependency

At this point:

- new setups should not need One
- existing One-backed skills continue to run until migrated
- WUPHF can provide a migration path for legacy One workflows / relays

Then One becomes optional or can be removed later.

## What we keep from One

We should explicitly keep these ideas:

- action discovery by natural language
- schema / knowledge lookup before execution
- dry-run before risky writes
- workflow objects as reusable automation units
- very simple agent mental model

These are product wins, even if the backend changes.

## What we borrow from Composio

We should explicitly borrow these strengths:

- stronger SDK-oriented integration model
- better trigger documentation and trigger lifecycle
- better long-term backend posture for cross-system execution

## Acceptance criteria

The migration is successful only if all of these are true:

1. Agents still use the same simple WUPHF action UX.
2. New users do not need One setup before integrations become useful.
3. WUPHF workflows are WUPHF-owned, not One-owned.
4. Trigger registration and event handling are first-class in WUPHF.
5. Composio can handle the three core jobs:
   - direct action execution
   - scheduled workflows
   - event-triggered workflows
6. Office visibility is preserved:
   - actions
   - planned actions
   - workflow runs
   - trigger registrations
   - trigger events
7. One can be removed later without redesigning the agent UX again.

## Immediate next step

Do not rip out One first.

The next implementation step should be:

1. freeze the current WUPHF action tool contract
2. add capability-based provider interfaces
3. implement a Composio adapter for actions and triggers
4. move workflow source-of-truth into WUPHF
5. dual-run both providers against the same scenarios

That gets us to the end state with the least regret.

## Sources

- One CLI: https://github.com/withoneai/cli
- One MCP: https://github.com/withoneai/mcp
- One website: https://www.withone.ai/
- Composio docs: https://docs.composio.dev/
- Composio triggers docs: https://docs.composio.dev/docs/triggers
- Composio trigger creation docs: https://docs.composio.dev/docs/setting-up-triggers/creating-triggers
- Composio repository: https://github.com/ComposioHQ/composio
