# CC-agent Plan Gap Audit

## Scope

This audit compares the CC-agent analysis and phase plans against the code currently on `main` after PR `#16`.

Source planning docs:

- [cc-agent-deep-analysis.md](/Users/najmuzzaman/Documents/nex/WUPHF/docs/cc-agent-deep-analysis.md)
- [cc-agent-implementation-roadmap.md](/Users/najmuzzaman/Documents/nex/WUPHF/docs/cc-agent-implementation-roadmap.md)
- [cc-agent-phase1-execution-plan.md](/Users/najmuzzaman/Documents/nex/WUPHF/docs/cc-agent-phase1-execution-plan.md)
- [cc-agent-phase2-execution-plan.md](/Users/najmuzzaman/Documents/nex/WUPHF/docs/cc-agent-phase2-execution-plan.md)
- [cc-agent-phase3-execution-plan.md](/Users/najmuzzaman/Documents/nex/WUPHF/docs/cc-agent-phase3-execution-plan.md)

Status meanings:

- `Implemented`: the planned branch intent is substantially present in the repo.
- `Partial`: some useful slice landed, but the branch intent is not fully realized.
- `Missing`: little or none of the intended branch shape is present.

## Summary

The repo now contains a meaningful subset of the roadmap:

- picker-based navigation and `/switch`
- improved slash autocomplete and composer ergonomics
- doctor/readiness surface
- interview rendering
- local tool execution
- task logs and worktree-backed task execution
- agent compaction with `Office Insight`

The repo does **not** yet contain most of the broader CC-agent-inspired overhaul described in the plans:

- context-aware keyboard architecture
- draft-safe history and recall
- reusable confirmation and continue primitives
- structured interview workflow with review-before-submit
- approval steering
- one canonical office/agent switcher
- unread and away-state navigation semantics
- transcript recovery
- session memory
- history virtualization
- capability and tmux abstraction layers

## Matrix

| Phase | Planned branch | Status | Evidence in repo | Gap |
| --- | --- | --- | --- | --- |
| 1 | `feat/context-aware-keybindings` | Partial | [keybindings.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/tui/keybindings.go#L9) has mode-based mappings and [channel.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel.go#L1030) routes active picker behavior before normal input handling. | No explicit interaction-context model, no centralized overlay/help/transcript key ownership layer, and no dedicated keymap module such as the planned `internal/tui/keymap.go`. |
| 1 | `feat/contextual-footer-hints` | Partial | [channel_composer.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_composer.go#L39) changes hint copy for normal compose, interviews, and `1:1`. Slash commands are categorized in [channel.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel.go#L439). | Hints are still mostly static strings. The plan called for state-driven footer truthfulness across blocked, reply, autocomplete, and other interaction states. |
| 1 | `feat/draft-safe-history` | Missing | There is no `channel_history.go`, no recall/search subsystem, and no code evidence of draft snapshot/restore behavior for main vs thread composers. | The current product still lacks safe history navigation, draft restoration, and cursor restoration. |
| 1 | `feat/interaction-primitives` | Partial | A reusable double-press helper exists in [keybindings.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/tui/keybindings.go#L101). | The broader primitive set from the plan is absent: no shared continue component, no common pending-state treatment, and no standard confirm/cancel byline primitives reused across surfaces. |
| 1 | `feat/runtime-change-confirmations` | Missing | I found no dedicated confirmation flow for provider/autonomy/runtime setting changes. | High-impact runtime changes still do not appear to route through a deliberate confirmation UX. |
| 1 | `feat/safety-dialogs` | Missing | There is no reusable dialog subsystem and no new safety-dialog helper module. | Disruptive actions are not covered by the bounded-detail confirmation model described in the plan. |
| 2 | `feat/structured-human-interviews` | Partial | Interview state and rendering exist in [broker.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/broker.go#L50), [channel.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel.go#L204), and [channel.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel.go#L3656). | The planned guided mini-flow is incomplete: no review-before-submit, no explicit skip/continue workflow, and no extracted interview-specific interaction layer. |
| 2 | `feat/approval-steering` | Missing | I found interview and approval kinds, but no concrete `approve with note` or `reject with steer` flow in channel or broker code. | Approval UX still looks binary compared with the planned operator-steering model. |
| 2 | `feat/agent-office-switcher` | Partial | Pickers exist for channels, direct sessions, tasks, requests, agents, and threads in [channel.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel.go#L4015) and nearby picker callsites. | The planned canonical switcher does not exist. Navigation is still spread across separate commands and pickers rather than one unified office/direct/task transcript surface. |
| 2 | `feat/unread-navigation-semantics` | Missing | I found no unread divider, jump-to-latest affordance, or first-class new-message semantics in the current channel rendering paths. | Long sessions still lack the explicit “new since you looked” model called for in the plan. |
| 2 | `feat/transcript-recovery` | Missing | I found no transcript recovery or rewind flow aligned with the planned branch. | Recovery and transcript surgery remain absent as a deliberate user flow. |
| 2 | `feat/away-summaries` | Missing | I found no away summary or return-summary implementation in channel or team runtime code. | Return moments are still not treated as a first-class UX concept. |
| 2 | `feat/in-channel-readiness` | Partial | The doctor/readiness surface is real in [channel_doctor.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_doctor.go#L33), [channel.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel.go#L441), and [channel.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel.go#L4223). | This is narrower than the roadmap branch. It covers setup and runtime readiness, but not a richer, always-legible in-channel readiness model woven through office flows. |
| 2 | `feat/insert-search-surfaces` | Missing | I found no insert-mode search surface or recall/search authoring tool beyond slash autocomplete and pickers. | The authoring/search ergonomics described in the plan were not implemented. |
| 3 | `feat/runtime-state-model` | Partial | Runtime state is surfaced in multiple places, and worktree/readiness/task state is exposed through [broker.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/broker.go#L103), [channel_render.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_render.go#L578), and [channel_doctor.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_doctor.go#L33). | The planned normalized UI-facing runtime state model is still missing. There is no dedicated module like the proposed `internal/tui/runtime_state.go`, and state derivation remains spread across broker, launcher, and UI code. |
| 3 | `feat/per-agent-transcript-inbox` | Missing | There is `1:1` mode and directed human messaging, but no explicit inbox/outbox transcript model per agent/task in the office UI. | The plan’s transcript ownership, directed inbox/outbox semantics, and zoomed transcript isolation were not landed. |
| 3 | `feat/execution-artifacts` | Partial | Task/worktree execution metadata exists in [broker.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/broker.go#L103), task logging lands in [task_runtime.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/agent/task_runtime.go#L19), and output logs are written from [loop.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/agent/loop.go#L653). | This is still lighter than the planned retained artifact model. There is no broader artifact lifecycle spanning workflows, approvals, external actions, and resume/review metadata. |
| 3 | `feat/session-memory` | Partial | Agent-side compaction and `Office Insight` exist in [task_runtime.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/agent/task_runtime.go#L68), [prompts.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/agent/prompts.go#L91), and [router.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/chat/router.go#L56). | The roadmap called for session-operational memory across office and direct sessions. What landed is mainly agent runtime compaction, not a fuller office/direct session memory subsystem like the proposed `internal/team/session_memory.go`. |
| 3 | `feat/history-virtualization` | Missing | I found no visible-row windowing, transcript virtualization, or dedicated rendering-window subsystem. | Long transcript scaling is still mostly handled by current rendering and caching, not virtualization. |
| 3 | `feat/tmux-capability-layer` | Missing | [channel_doctor.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_doctor.go#L79) checks that `tmux` exists, but there is no capability abstraction layer for terminal features. | The plan’s tmux/screen hardening layer was not implemented. |
| 3 | `feat/capability-registry` | Missing | The repo has an action registry in [registry.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/action/registry.go), but not the broader runtime capability registry described in the phase plan. | Provider/action registration exists, but a central runtime capability exposure layer does not. |

## What Actually Landed

These roadmap-adjacent areas are clearly present and should not be discounted:

- `/switch` and fuzzy picker-based navigation in [channel.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel.go#L447) and [picker.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/tui/picker.go)
- stronger slash command categorization and visibility rules in [channel.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel.go#L439) and [autocomplete.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/tui/autocomplete.go)
- composer motion and input ergonomics in [channel.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel.go) and [channel_composer.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_composer.go#L39)
- doctor/readiness checks in [channel_doctor.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel_doctor.go#L33)
- interview rendering and answer plumbing in [channel.go](/Users/najmuzzaman/Documents/nex/WUPHF/cmd/wuphf/channel.go#L3656) and [broker.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/broker.go#L437)
- local coding tools, working directory support, and outbox writes in [tools.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/agent/tools.go#L112)
- task output logs and compaction in [task_runtime.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/agent/task_runtime.go#L19) and [loop.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/agent/loop.go#L653)
- local worktree task isolation in [worktree.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/worktree.go#L15) and [broker.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/team/broker.go#L3555)
- task status exposure for agents via [server.go](/Users/najmuzzaman/Documents/nex/WUPHF/internal/teammcp/server.go#L394)

## Recommended Next Pass

If the goal is to continue the actual roadmap rather than celebrate PR `#16` as complete, the highest-leverage next sequence is:

1. `feat/runtime-state-model`
2. `feat/context-aware-keybindings`
3. `feat/draft-safe-history`
4. `feat/structured-human-interviews`
5. `feat/agent-office-switcher`
6. `feat/session-memory`

That order fixes the biggest architectural and operator-trust gaps first.
