# CC-agent Plan Status

## Scope

This document replaces the earlier gap audit that was written after PR `#16`.
That older audit is no longer accurate: the later follow-up work on `main`
landed the remaining major CC-agent-inspired roadmap slices.

Reference planning docs:

- [cc-agent-deep-analysis.md](./cc-agent-deep-analysis.md)
- [cc-agent-implementation-roadmap.md](./cc-agent-implementation-roadmap.md)
- [cc-agent-phase1-execution-plan.md](./cc-agent-phase1-execution-plan.md)
- [cc-agent-phase2-execution-plan.md](./cc-agent-phase2-execution-plan.md)
- [cc-agent-phase3-execution-plan.md](./cc-agent-phase3-execution-plan.md)

## Current Read

`main` now contains the roadmap themes that previously showed up as gaps:

- context-aware interaction routing and contextual footer hints
- draft-safe composer recall and review-before-submit flows
- reusable confirmation and interview primitives
- canonical workspace switching across office, recovery, tasks, requests, direct sessions, inbox, and outbox
- unread anchors, away summaries, recovery surfaces, and transcript rewind helpers
- runtime state snapshots, capability registry/readiness checks, and session memory summaries
- retained execution artifacts for tasks, workflows, requests, and actions
- per-agent inbox/outbox transcript lanes
- large-history viewport virtualization for the main transcript path

The product is therefore no longer missing a major planned branch from the
CC-agent analysis. The remaining work is maintenance and polish, not a roadmap
gap.

## Status Matrix

| Phase | Planned branch | Status | Evidence |
| --- | --- | --- | --- |
| 1 | `feat/context-aware-keybindings` | Implemented | [interaction.go](../internal/tui/interaction.go), [channel.go](../cmd/wuphf/channel.go), [channel_context.go](../cmd/wuphf/channel_context.go) |
| 1 | `feat/contextual-footer-hints` | Implemented | [channel_composer.go](../cmd/wuphf/channel_composer.go), [channel_context.go](../cmd/wuphf/channel_context.go) |
| 1 | `feat/draft-safe-history` | Implemented | [channel_history.go](../cmd/wuphf/channel_history.go) |
| 1 | `feat/interaction-primitives` | Implemented | [channel_confirm.go](../cmd/wuphf/channel_confirm.go), [interaction.go](../internal/tui/interaction.go) |
| 1 | `feat/runtime-change-confirmations` | Implemented | [channel_confirm.go](../cmd/wuphf/channel_confirm.go), [channel.go](../cmd/wuphf/channel.go) |
| 1 | `feat/safety-dialogs` | Implemented | [channel_confirm.go](../cmd/wuphf/channel_confirm.go) |
| 2 | `feat/structured-human-interviews` | Implemented | [channel_interview.go](../cmd/wuphf/channel_interview.go), [broker.go](../internal/team/broker.go) |
| 2 | `feat/approval-steering` | Implemented | [broker.go](../internal/team/broker.go), [server.go](../internal/teammcp/server.go) |
| 2 | `feat/agent-office-switcher` | Implemented | [channel_switcher.go](../cmd/wuphf/channel_switcher.go) |
| 2 | `feat/unread-navigation-semantics` | Implemented | [channel_unread.go](../cmd/wuphf/channel_unread.go), [channel_window.go](../cmd/wuphf/channel_window.go) |
| 2 | `feat/transcript-recovery` | Implemented | [channel_recovery.go](../cmd/wuphf/channel_recovery.go) |
| 2 | `feat/away-summaries` | Implemented | [channel_recovery.go](../cmd/wuphf/channel_recovery.go), [channel_unread.go](../cmd/wuphf/channel_unread.go) |
| 2 | `feat/in-channel-readiness` | Implemented | [channel_doctor.go](../cmd/wuphf/channel_doctor.go), [channel_workspace_state.go](../cmd/wuphf/channel_workspace_state.go) |
| 2 | `feat/insert-search-surfaces` | Implemented | [channel_insert_search.go](../cmd/wuphf/channel_insert_search.go) |
| 3 | `feat/runtime-state-model` | Implemented | [runtime_state.go](../internal/team/runtime_state.go), [channel_workspace_state.go](../cmd/wuphf/channel_workspace_state.go) |
| 3 | `feat/per-agent-transcript-inbox` | Implemented | [channel_mailboxes.go](../cmd/wuphf/channel_mailboxes.go), [broker.go](../internal/team/broker.go), [server.go](../internal/teammcp/server.go) |
| 3 | `feat/execution-artifacts` | Implemented | [runtime_artifacts.go](../internal/team/runtime_artifacts.go), [channel_artifacts.go](../cmd/wuphf/channel_artifacts.go) |
| 3 | `feat/session-memory` | Implemented | [session_memory.go](../internal/team/session_memory.go), [session_memory_snapshot.go](../internal/team/session_memory_snapshot.go) |
| 3 | `feat/history-virtualization` | Implemented | [channel_viewport_virtual.go](../cmd/wuphf/channel_viewport_virtual.go), [channel_window.go](../cmd/wuphf/channel_window.go) |
| 3 | `feat/tmux-capability-layer` | Implemented | [capabilities.go](../internal/team/capabilities.go), [channel_doctor.go](../cmd/wuphf/channel_doctor.go) |
| 3 | `feat/capability-registry` | Implemented | [capability_registry.go](../internal/team/capability_registry.go) |

## What Remains

What remains is operational follow-through, not another roadmap phase:

- benchmark and profile large transcript workloads
- tune cache sizes and rendering hot paths based on benchmark results
- continue copy and layout polish on artifact, recovery, and blocked-state surfaces
- prune or update historical docs that still describe pre-merge gaps

## Bench Target

Use the viewport benchmark when profiling transcript rendering work:

```sh
go test ./cmd/wuphf -run '^$' -bench 'BenchmarkOfficeViewport' -benchmem
```

That benchmark compares:

- hot cached virtualized viewport rendering
- cold virtualized viewport rendering
- the older full-render plus slice path
