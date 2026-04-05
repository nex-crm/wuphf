# CC-agent Lessons: Phase 3 Execution Plan

## Purpose

This is the implementation-ready plan for Phase 3 of the CC-agent-inspired WUPHF improvements.

It corresponds to the `Deep Architectural Polish` portion of:

- [cc-agent-implementation-roadmap.md](/Users/najmuzzaman/Documents/nex/WUPHF/docs/cc-agent-implementation-roadmap.md)

This doc answers:

- what the exact Phase 3 branches should be
- which real WUPHF files are likely to change
- what each branch should prove
- what termwright and runtime scenarios should validate

## Current WUPHF Code Map for Phase 3

Phase 3 is where the durable substrate work happens.

### Runtime, office, and retained state

- [broker.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/broker.go)
- [ledger.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/ledger.go)
- [launcher.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/launcher.go)
- [scheduler_runtime.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/scheduler_runtime.go)
- [session_mode.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/session_mode.go)
- [broker_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/broker_test.go)
- [launcher_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/launcher_test.go)

What lives there today:

- office state persistence
- actions/signals/decisions/watchdogs
- scheduler execution
- agent launch/runtime bridging
- office vs `1:1` session mode

### Channel rendering and state consumption

- [channel.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel.go)
- [channel_messages.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_messages.go)
- [channel_render.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_render.go)
- [channel_layout.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_layout.go)
- [channel_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_test.go)

What lives there today:

- current UI-facing state assembly
- message/event rendering
- runtime strip and header consumption
- cached rendering

### Action/workflow/provider substrate

- [types.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/action/types.go)
- [registry.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/action/registry.go)
- [composio.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/action/composio.go)
- [composio_workflows.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/action/composio_workflows.go)
- [workflow_store.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/action/workflow_store.go)
- [actions.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/teammcp/actions.go)

What lives there today:

- provider routing
- external action execution
- generic workflow storage/execution
- skill mirroring and scheduler wiring

### Existing UAT coverage

- [office-channel-e2e.sh](/Users/najmuzzaman/Documents/nex/WUPHF/tests/uat/office-channel-e2e.sh)
- [one-on-one-channel-e2e.sh](/Users/najmuzzaman/Documents/nex/WUPHF/tests/uat/one-on-one-channel-e2e.sh)
- [autonomy-acceptance.sh](/Users/najmuzzaman/Documents/nex/WUPHF/tests/uat/autonomy-acceptance.sh)
- [comprehensive-uat.sh](/Users/najmuzzaman/Documents/nex/WUPHF/tests/uat/comprehensive-uat.sh)

## Recommended Phase 3 Branch Order

Recommended order:

1. `feat/runtime-state-model`
2. `feat/per-agent-transcript-inbox`
3. `feat/execution-artifacts`
4. `feat/session-memory`
5. `feat/history-virtualization`
6. `feat/tmux-capability-layer`
7. `feat/capability-registry`

That order is deliberate:

- branch 1 creates the UI/runtime contract
- branch 2 fixes multi-agent transcript and notification scoping
- branch 3 turns work into retained objects
- branch 4 makes long-lived sessions survivable
- branch 5 scales the channel/direct transcript experience
- branch 6 hardens terminal behavior in tmux/screen environments
- branch 7 centralizes capability exposure after the runtime model is clearer

## Branch 1: `feat/runtime-state-model`

### Goal

Create one normalized UI-facing runtime state model for agents, jobs, readiness, and session mode.

### What Should Change

- explicit runtime state object that unifies:
  - office vs `1:1`
  - active work
  - blocked/waiting-human state
  - readiness/integration state
  - active execution context
- channel and direct UI consume that same model
- less implicit derivation spread across broker, launcher, and render code

### Likely Files

- [broker.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/broker.go)
- [launcher.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/launcher.go)
- [channel.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel.go)
- [channel_render.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_render.go)
- likely new helper/module:
  - `[internal/tui/runtime_state.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/tui/runtime_state.go)` or similar
- [broker_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/broker_test.go)
- [channel_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_test.go)

### Termwright Scenarios

1. Same blocked state appears consistently in office and `1:1`
2. Active work summary matches underlying broker/runtime state
3. Readiness and integration state do not drift between views

### Done Means

- UI surfaces pull from one coherent runtime model

## Branch 2: `feat/per-agent-transcript-inbox`

### Goal

Give each agent/task its own transcript scope and first-class inbox/outbox model.

### What Should Change

- transcript ownership per agent/task
- inbox/outbox model for directed notifications and human messages
- “viewed transcript” distinct from office feed
- suppression of unrelated queue noise while zoomed into one agent/task

### Likely Files

- [broker.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/broker.go)
- [launcher.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/launcher.go)
- [channel_messages.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_messages.go)
- [channel_thread.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_thread.go)
- [channel_sidebar.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_sidebar.go)
- [broker_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/broker_test.go)
- [launcher_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/launcher_test.go)

### Termwright Scenarios

1. User zooms into one agent/task:
   - unrelated office noise is suppressed
2. Directed human message:
   - lands in the intended agent/task scope
3. Completed agent/task transcript:
   - remains reviewable

### Done Means

- multi-agent work no longer depends on one flattened feed

## Branch 3: `feat/execution-artifacts`

### Goal

Turn tasks, workflows, approvals, and external actions into retained execution artifacts.

### What Should Change

- explicit lifecycle:
  - started
  - running
  - blocked
  - completed
  - failed
  - interrupted
- retained partial outputs and summaries
- richer progress snapshots
- review/resume metadata

### Likely Files

- [broker.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/broker.go)
- [ledger.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/ledger.go)
- [scheduler_runtime.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/scheduler_runtime.go)
- [actions.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/teammcp/actions.go)
- [channel_render.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_render.go)
- [broker_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/broker_test.go)
- [actions_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/teammcp/actions_test.go)

### Termwright Scenarios

1. External action dry-run then execute:
   - artifact retains both planned and executed states
2. Interrupted workflow:
   - partial progress remains visible
3. Completed task/workflow:
   - review metadata survives the run

### Done Means

- work is represented as retained runtime objects, not only transcript text

## Branch 4: `feat/session-memory`

### Goal

Add session-operational memory and transcript compaction distinct from Nex organizational memory.

### What Should Change

- session summaries
- transcript compaction points
- recovery summaries
- continuity for long-running office and direct sessions

### Likely Files

- [broker.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/broker.go)
- [ledger.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/ledger.go)
- [channel_messages.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_messages.go)
- likely new package/module:
  - `[internal/team/session_memory.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/session_memory.go)` or similar
- [broker_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/broker_test.go)

### Termwright Scenarios

1. Long office session compacts cleanly
2. Returning user sees recovery summary
3. Direct session preserves operational continuity across restart

### Done Means

- WUPHF gains session memory that complements Nex instead of pretending Nex replaces it

## Branch 5: `feat/history-virtualization`

### Goal

Scale long transcript rendering beyond cache-only optimization.

### What Should Change

- visible-row windowing
- lower first-paint work
- incremental row generation
- reduced markdown/render work outside the visible range

### Likely Files

- [channel_render.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_render.go)
- [channel_layout.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_layout.go)
- [channel_messages.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_messages.go)
- [channel.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel.go)
- [channel_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_test.go)

### Termwright Scenarios

1. Very long office history:
   - first paint remains usable
2. Scroll around old content:
   - navigation stays responsive
3. Live updates while scrolled:
   - unread semantics remain correct

### Done Means

- long-running channels and direct sessions remain performant in practice

## Branch 6: `feat/tmux-capability-layer`

### Goal

Centralize tmux/screen-specific clipboard, notification, status, and redraw behavior.

### What Should Change

- one tmux-aware capability layer for:
  - clipboard/copy
  - bell/notification
  - status redraw semantics
  - terminal capability detection
- less ad hoc behavior split across launcher and UI

### Likely Files

- [launcher.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/launcher.go)
- [tmux_manager.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/tui/tmux_manager.go)
- [statusbar.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/tui/statusbar.go)
- likely new helper/module:
  - `[internal/tui/termcaps.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/tui/termcaps.go)` or similar
- [tmux_manager_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/tui/tmux_manager_test.go)
- [launcher_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/launcher_test.go)

### Termwright Scenarios

1. tmux office boot:
   - status redraw remains clean
2. Copy/selection flow:
   - expected path is visible and consistent
3. Notification path under tmux:
   - no corrupted redraws

### Done Means

- tmux is treated as a supported runtime contract, not just a shell wrapper

## Branch 7: `feat/capability-registry`

### Goal

Centralize capability assembly for tools, actions, workflows, skills, and future plugins.

### What Should Change

- one registry for:
  - office tools
  - `1:1` tools
  - Nex capabilities
  - action providers
  - workflows
  - future plugins/skills
- clearer mode-aware capability exposure
- safer evolution of permissions and UI surfacing

### Likely Files

- [server.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/teammcp/server.go)
- [actions.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/teammcp/actions.go)
- [registry.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/action/registry.go)
- [types.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/action/types.go)
- likely new helper/module:
  - `[internal/capabilities](/Users/najmuzzaman/Documents/nex/WUPHF/internal/capabilities)` or similar
- [server_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/teammcp/server_test.go)
- [actions_test.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/teammcp/actions_test.go)

### Termwright Scenarios

1. Office vs `1:1` capability surfaces:
   - expected commands/tools are available
2. Provider switch:
   - capability exposure changes cleanly
3. Missing provider or integration:
   - UI/readiness remains truthful

### Done Means

- capability exposure is deliberate, centralized, and mode-aware

## Phase 3 Success Criteria

- UI surfaces draw from one coherent runtime model
- agent/task transcripts and notifications have explicit ownership
- work is retained as artifacts, not only transcript traces
- long sessions remain understandable and performant
- tmux behavior is intentional and reliable
- capability exposure becomes easier to reason about and evolve safely
