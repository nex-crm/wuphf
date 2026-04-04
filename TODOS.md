# TODOS

## P2: Action Allowlist for Workflow Specs
**What:** Workflow specs declare which Composio actions they use. Runtime rejects undeclared actions.
**Why:** Security gap — agent-generated workflows can call ANY Composio action. Allowlist prevents malicious/accidental calls.
**Context:** Identified in CEO review Section 3 (Security). User approval gates v1, but allowlist is the proper long-term fix. Agent generation prompt should declare actions upfront.
**Effort:** S (CC: ~20 min) | **Priority:** P2 | **Depends on:** Workflow runtime

## P2: Background DataSource Polling
**What:** DataSources specify `poll` intervals. Runtime refreshes data in the background during workflow execution.
**Why:** v1 fetches data ONCE at workflow start. Real-time use cases (email triage, monitoring) need live data.
**Context:** Design doc explicitly deferred this to v2. Requires background goroutine management, stale-data consistency, TUI re-rendering on data update.
**Effort:** S (CC: ~45 min) | **Priority:** P2 | **Depends on:** Workflow runtime

## P3: channel.go Decomposition
**What:** Break `cmd/wuphf/channel.go` (5600+ lines) into focused modules: channel_workflow.go, channel_messages.go, channel_input.go, channel_state.go.
**Why:** Anti-pattern. Monolithic file grows with every feature. Workflow runtime will add more rendering/key-handling code.
**Context:** Identified in CEO review taste calibration. No functional change, pure refactor. Can be done anytime.
**Effort:** S (CC: ~45 min) | **Priority:** P3 | **Depends on:** Nothing

## P1: Stronger Channel Empty and Offline States
**What:** Make office and channel empty states feel alive even when there is no current live work.
**Why:** The current channel hierarchy and runtime strip work well once activity exists, but offline/preview states still feel flat and low-signal.
**Context:** Identified after the Hermes/OpenClaw-inspired channel UI pass. This is a legibility and product-feel gap, not a missing feature gap.
**Effort:** S (CC: ~30 min) | **Priority:** P1 | **Depends on:** Channel hierarchy pass

## P1: In-Channel "Needs Me Now" Treatment
**What:** Make blocking interviews, missing integrations, setup-required states, and provider disconnects visually unavoidable inside the channel.
**Why:** `/doctor` exists, but blocked states still rely too much on users discovering overlays instead of seeing urgency in the main workspace.
**Context:** Identified during channel UI review. Should complement `/doctor`, not replace it.
**Effort:** M (CC: ~60 min) | **Priority:** P1 | **Depends on:** Channel hierarchy pass, doctor surface

## P2: Expandable Execution Event Cards
**What:** Upgrade styled execution/status text blocks into truly structured, expandable/collapsible event cards.
**Why:** Current rendering improves hierarchy, but execution artifacts still behave like decorated transcript text instead of inspectable runtime objects.
**Context:** Identified after adding runtime strip and event-card styling. Best paired with richer execution artifact state.
**Effort:** M (CC: ~75 min) | **Priority:** P2 | **Depends on:** Structured execution artifacts

## P2: Long-History Virtualization
**What:** Add viewport virtualization, incremental rendering, and first-paint optimization for dense markdown/message histories.
**Why:** Render caching helped, but it is not enough for truly long-running channel histories.
**Context:** Validated by comparing WUPHF to CC-agent's `VirtualMessageList` architecture.
**Effort:** M (CC: ~90 min) | **Priority:** P2 | **Depends on:** Channel rendering stabilization

## P1: Workflow Authoring Reliability
**What:** Make agent-created generic workflows less prompt-sensitive and more reliably valid on the first try.
**Why:** Backend workflow execution is working, but the agent-mediated authoring path is still too fragile.
**Context:** Proven during Composio digest workflow testing. Execution path is good; authoring UX is not.
**Effort:** M (CC: ~60 min) | **Priority:** P1 | **Depends on:** Generic workflow runtime

## P1: Prove Composio Trigger Path End to End
**What:** Fully validate trigger registration, event ingestion, and office surfacing for Composio-backed triggers.
**Why:** Actions and scheduled workflows are proven live, but trigger ingestion is still not fully validated.
**Context:** Explicitly left open after the Composio migration/testing passes.
**Effort:** M (CC: ~75 min) | **Priority:** P1 | **Depends on:** Composio provider

## P2: Improve Generated Workflow Content Quality
**What:** Improve summary formatting, actionability, and output cleanliness for generated digests and similar workflows.
**Why:** The workflow engine is functionally correct, but output quality still feels rough.
**Context:** Seen in the daily digest live probe. Execution succeeded; content polish lagged.
**Effort:** S (CC: ~30 min) | **Priority:** P2 | **Depends on:** Workflow authoring reliability

## P2: Surface Doctor Findings in Core Flow
**What:** Surface readiness and blocked-state findings directly in setup and main channel flows, not just in `/doctor`.
**Why:** Important problems should be visible where the user is already working.
**Context:** Identified after the first doctor/readiness pass.
**Effort:** S (CC: ~30 min) | **Priority:** P2 | **Depends on:** Doctor surface

## P3: Simplify and Standardize Setup Guidance
**What:** Tighten README and setup guidance into a single recommended path for non-technical users.
**Why:** External action/integration readiness is now more capable, but setup still has too many potential branches.
**Context:** Identified during One/Composio onboarding work and doctor/readiness review.
**Effort:** S (CC: ~20 min) | **Priority:** P3 | **Depends on:** None

## P2: TUI PR Consolidation
**What:** Reconcile and sequence the runtime-clarity, channel-hierarchy, and later polish PRs into a coherent merge plan.
**Why:** Channel/UI work is now split across multiple branches and should land cleanly instead of fragmenting the UI architecture.
**Context:** Current open branches/PRs around runtime clarity and channel hierarchy.
**Effort:** S (CC: ~20 min) | **Priority:** P2 | **Depends on:** CI passing on active PRs

## P1: Context-Aware Keyboard Navigation Model
**What:** Replace scattered key handling with an explicit keybinding model organized by contexts like chat, autocomplete, confirmation, transcript, task switcher, and help.
**Why:** CC-agent’s keyboard UX feels coherent because key resolution, chord handling, and help hints all come from one context-aware model.
**Context:** Derived from `defaultBindings.ts`, `schema.ts`, and `useKeybinding.ts` in the CC-agent study.
**Effort:** M (CC: ~90 min) | **Priority:** P1 | **Depends on:** Channel/composer state cleanup

## P1: Canonical Agent/Office Switcher Surface
**What:** Add one primary switcher/navigation surface that always includes the main office plus agent/task transcripts, status, unread count, and jump actions.
**Why:** CC-agent stays legible in multi-agent mode because “main”, viewed transcript, foregrounded task, and selected footer state are distinct and navigable.
**Context:** Derived from `CoordinatorAgentStatus.tsx`, `teammateViewHelpers.ts`, and the subagent multi-agent review.
**Effort:** M (CC: ~90 min) | **Priority:** P1 | **Depends on:** Normalized runtime state model

## P1: Per-Agent Transcript and Inbox Model
**What:** Give each agent/task its own transcript scope plus a first-class inbox/outbox model for notifications and directed human messages.
**Why:** WUPHF’s office feed is strong, but CC-agent’s transcript-scoped multi-agent experience is stronger because execution and transcript routing are not flattened into one stream.
**Context:** Derived from `sessionStorage.ts`, `diskOutput.ts`, `selectors.ts`, and the subagent’s multi-agent analysis.
**Effort:** L (CC: ~2-3h) | **Priority:** P1 | **Depends on:** Normalized runtime state model

## P2: Overlay Focus and Escape Arbitration
**What:** Introduce one focus/overlay model that coordinates autocomplete, dialogs, thread view, picker surfaces, and cancel behavior.
**Why:** CC-agent prevents haunted-feeling terminal UX by explicitly distinguishing active overlays, modal overlays, and typing-safe overlays.
**Context:** Derived from `overlayContext.tsx` in the CC-agent study.
**Effort:** M (CC: ~60 min) | **Priority:** P2 | **Depends on:** Context-aware keyboard navigation model

## P2: Mouse-Safe Selection and Copy Policy
**What:** Design mouse support around selection quality, copy behavior, and selectable regions instead of only click handlers.
**Why:** CC-agent treats selection as infrastructure: clean diff copying, excluded gutters, and readable selection overlays. WUPHF still treats mouse mostly as a toggle problem.
**Context:** Derived from `screen.ts` and the tmux/clipboard study.
**Effort:** M (CC: ~75 min) | **Priority:** P2 | **Depends on:** Channel rendering stabilization

## P2: tmux-Aware Terminal Capability Layer
**What:** Centralize tmux/screen-specific handling for clipboard, OSC passthrough, notifications, bells, and status redraw semantics.
**Why:** CC-agent treats tmux as a supported runtime environment with explicit correctness rules. WUPHF currently relies too much on defaults and ad hoc fixes.
**Context:** Derived from `osc.ts`, `useTerminalNotification.ts`, and `bridgeUI.ts`.
**Effort:** M (CC: ~90 min) | **Priority:** P2 | **Depends on:** Startup and runtime profiling pass

## P2: Unread Divider and “New Messages” Channel Semantics
**What:** Add first-class unread dividers, jump-to-latest behavior, and a compact “new since you looked” affordance in channels and direct sessions.
**Why:** CC-agent’s sticky prompt and “N new messages” semantics make navigation feel grounded in long-running sessions.
**Context:** Derived from `FullscreenLayout.tsx` and the hierarchy/navigation pass.
**Effort:** M (CC: ~75 min) | **Priority:** P2 | **Depends on:** Long-history virtualization

## P1: Normalized Runtime State Model
**What:** Introduce a UI-facing runtime state model that unifies agent activity, execution state, readiness, integration status, and direct-session/office mode semantics.
**Why:** WUPHF still spreads meaningful runtime state across broker, launcher, tmux, and UI code. This is the biggest architectural gap surfaced by the CC-agent study.
**Context:** Derived from `docs/cc-agent-deep-analysis.md`, especially the review of `AppStateStore.ts`, REPL orchestration, and mode handling.
**Effort:** M (CC: ~90 min) | **Priority:** P1 | **Depends on:** None

## P1: Rich Execution Artifact Model
**What:** Turn tasks and workflows into retained execution artifacts with explicit started/running/blocked/completed timelines, output references, progress snapshots, and resume/review semantics.
**Why:** WUPHF currently has visible work, but not deep retained execution objects. CC-agent's task runtime is much richer and more recoverable.
**Context:** Derived from the CC-agent study of `LocalAgentTask`, `RemoteAgentTask`, and the broader query/task runtime split.
**Effort:** L (CC: ~2-3h) | **Priority:** P1 | **Depends on:** Normalized runtime state model

## P1: Session Memory and Context Compaction
**What:** Add a WUPHF session-memory layer for offices, direct sessions, and long-running tasks, including transcript compaction and recovery summaries.
**Why:** Nex gives organizational memory, but WUPHF still lacks a strong session-operational memory system.
**Context:** Derived from the CC-agent study of `sessionMemory.ts`, `contextCollapse`, and `autoCompact`.
**Effort:** L (CC: ~2-3h) | **Priority:** P1 | **Depends on:** Normalized runtime state model

## P2: Startup and Runtime Profiling Pass
**What:** Profile WUPHF startup and runtime render hot paths, then optimize cold boot, first paint, and heavy app switches intentionally.
**Why:** CC-agent treats startup speed as product quality. WUPHF has improved runtime rendering, but startup and first-paint discipline are still shallow.
**Context:** Derived from the CC-agent study of `src/entrypoints/cli.tsx` and transcript/runtime performance work.
**Effort:** M (CC: ~75 min) | **Priority:** P2 | **Depends on:** None

## P2: "While You Were Away" Summaries
**What:** Add per-channel and per-direct-session recaps for what happened while the user was away.
**Why:** Returning to an active office is currently too manual. CC-agent treats return moments as a first-class UX problem.
**Context:** Derived from the CC-agent study of `awaySummary.ts`.
**Effort:** M (CC: ~60 min) | **Priority:** P2 | **Depends on:** Rich execution artifact model, session memory

## P2: Capability Registry for Tools, Actions, and Skills
**What:** Centralize capability assembly for office tools, direct tools, Nex tools, action providers, workflows, and future plugins.
**Why:** WUPHF's capability surface is growing, but it is not yet governed by a single assembly and permission model.
**Context:** Derived from the CC-agent study of `tools.ts`, `loadSkillsDir.ts`, and plugin runtime handling.
**Effort:** M (CC: ~90 min) | **Priority:** P2 | **Depends on:** Normalized runtime state model

## P3: Delight and Teaching Layer
**What:** Add purposeful polish features for idle/waiting/empty states such as smarter tips, better welcome/return moments, and richer product guidance during latency.
**Why:** WUPHF is becoming more capable, but still underuses waiting time and empty time as product moments.
**Context:** Derived from the CC-agent study of `WelcomeV2`, `Spinner`, `tipScheduler`, and `CompanionSprite`.
**Effort:** M (CC: ~75 min) | **Priority:** P3 | **Depends on:** Channel empty/offline states, doctor surfacing

## P1: Structured Human Interview Flows
**What:** Upgrade blocking human interviews from highlighted transcript moments into structured mini-flows with progress, completeness, skip/continue semantics, review-before-submit, and optional notes.
**Why:** CC-agent's interview/elicitation flow is dramatically more legible and easier to complete than raw conversational back-and-forth.
**Context:** Derived from the micro-interaction analysis in `docs/cc-agent-deep-analysis.md`, especially `QuestionView`, `PreviewQuestionView`, `SubmitQuestionsView`, and `QuestionNavigationBar`.
**Effort:** M (CC: ~90 min) | **Priority:** P1 | **Depends on:** Normalized runtime state model

## P2: Approval Prompts with Inline Steering
**What:** Let human approvals include optional "yes, but..." or "no, instead..." guidance at the approval point for actions, workflows, and risky runtime transitions.
**Why:** Binary accept/deny gates throw away valuable operator steering. CC-agent's permission prompt flow captures this elegantly.
**Context:** Derived from the micro-interaction analysis of `PermissionPrompt.tsx`.
**Effort:** M (CC: ~60 min) | **Priority:** P2 | **Depends on:** Rich execution artifact model

## P2: Preserve Partial Output on Interrupt
**What:** When a human interrupts an in-progress response or task, preserve the partial artifact or summary before marking the run interrupted.
**Why:** Abrupt interruption should not make visible work disappear. This is a small trust-building behavior CC-agent handles well.
**Context:** Derived from the micro-interaction analysis of `REPL.tsx` cancellation behavior.
**Effort:** S (CC: ~30 min) | **Priority:** P2 | **Depends on:** Rich execution artifact model

## P3: Quick Tangent / Side Question Mode
**What:** Add a lightweight "quick tangent" interaction that lets the user ask a side question without disrupting the main office/direct task flow.
**Why:** CC-agent's `/btw` is a strong example of a tiny feature that materially improves operator flow.
**Context:** Derived from the micro-interaction analysis of `utils/sideQuestion.ts`.
**Effort:** M (CC: ~75 min) | **Priority:** P3 | **Depends on:** Session memory and context compaction

## P2: Contextual Footer and Shortcut Hints
**What:** Make footer/help hints mode-aware and resolve actual configured keybindings instead of static strings.
**Why:** CC-agent's footer/help system teaches only the actions that matter right now and stays truthful after customization.
**Context:** Derived from `PromptInputFooterLeftSide.tsx`, `PromptInputHelpMenu.tsx`, and `ConfigurableShortcutHint.tsx`.
**Effort:** S (CC: ~30 min) | **Priority:** P2 | **Depends on:** Channel/composer state cleanup

## P2: Draft-Preserving History and Recall
**What:** Preserve drafts, cursor position, mode, and pasted content across history navigation and search/recall flows.
**Why:** Recall should feel safe. CC-agent protects unfinished input very carefully during history and search.
**Context:** Derived from `useArrowKeyHistory.tsx`, `useHistorySearch.ts`, and `HistorySearchDialog.tsx`.
**Effort:** M (CC: ~60 min) | **Priority:** P2 | **Depends on:** Composer state cleanup

## P2: Transcript Recovery and Summarize UX
**What:** Add explicit transcript restore/rewind/summarize interactions instead of treating recovery as a raw message jump.
**Why:** CC-agent treats restore as "conversation surgery" with restore-both / restore-code / summarize-from-here semantics.
**Context:** Derived from `MessageSelector.tsx`.
**Effort:** M (CC: ~90 min) | **Priority:** P2 | **Depends on:** Session memory and context compaction

## P2: Safety Dialogs for Disruptive Operations
**What:** Add small, bounded-detail safety dialogs before disruptive operations such as reset, restore, branch-changing actions, or workflow resume/apply flows.
**Why:** CC-agent explains risky transitions clearly, checks state first, and avoids surprising the user after the fact.
**Context:** Derived from `TeleportStash.tsx`.
**Effort:** S (CC: ~45 min) | **Priority:** P2 | **Depends on:** Rich execution artifact model

## P3: Lightweight Insert/Search Surfaces
**What:** Add richer search/open/insert overlays for file paths, references, and transcript targets so composition is not purely raw text driven.
**Why:** CC-agent's quick-open and global-search dialogs are authoring tools, not just navigation tools.
**Context:** Derived from `QuickOpenDialog.tsx` and `GlobalSearchDialog.tsx`.
**Effort:** M (CC: ~75 min) | **Priority:** P3 | **Depends on:** Composer state cleanup

## P2: Confirm High-Impact Runtime Setting Changes
**What:** Add confirmation/education steps for mid-session changes that alter cost, autonomy, or execution behavior.
**Why:** CC-agent warns carefully before changing behaviorally meaningful toggles like thinking mode or auto mode.
**Context:** Derived from `ThinkingToggle.tsx` and `AutoModeOptInDialog.tsx`.
**Effort:** S (CC: ~30 min) | **Priority:** P2 | **Depends on:** Normalized runtime state model

## P3: Reusable Continue / Double-Press / Pending-State Primitives
**What:** Standardize repeated-confirm, continue, and pending-state interactions as shared UI primitives.
**Why:** Small consistency primitives like `useDoublePress.ts` and `PressEnterToContinue.tsx` keep the whole product feeling coherent.
**Context:** Derived from the micro-interaction analysis of small interaction helpers.
**Effort:** S (CC: ~25 min) | **Priority:** P3 | **Depends on:** None
