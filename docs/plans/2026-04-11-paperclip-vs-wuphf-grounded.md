# Paperclip vs WUPHF — Grounded Analysis

Generated 2026-04-11 on branch `nazz/feat/web-view`. This supersedes an earlier shallow analysis that relied on WebFetch summaries instead of reading the actual source. Paperclip was cloned to `/tmp/paperclip-analysis/paperclip` (HEAD `b00d52c`) and the claims below cite real files and line numbers.

## Executive summary

Paperclip and WUPHF are optimizing for different products. Paperclip is a **task-execution control plane** with strong audit, retry, and budget semantics. WUPHF is a **conversational office runtime** with A2UI, Nex integration, and a terminal-first TUI. The earlier "steal from Paperclip" framing understated both how much Paperclip has built (it's much larger than I implied) and what WUPHF already has that Paperclip does not. The grounded answer: lift three specific patterns, avoid four others that look attractive but don't serve WUPHF's current shape.

## Corrections to the earlier analysis

| Earlier claim | Reality |
|---|---|
| "Adapter interface is a 3-method contract (`invoke/status/cancel`)." | Real interface is `ServerAdapterModule` with ~15 methods, defined in `packages/adapter-utils/src/types.ts:292-331`. It includes `execute`, `testEnvironment`, `listSkills`, `syncSkills`, `sessionCodec`, `sessionManagement`, `models`, `listModels`, `supportsLocalAgentJwt`, `agentConfigurationDoc`, `onHireApproved`, `getQuotaWindows`, `detectModel`, `getConfigSchema`. |
| "Paperclip ships with ~5 adapters." | 10 built-in adapters: `claude_local`, `codex_local`, `opencode_local`, `pi_local`, `cursor`, `gemini_local`, `openclaw_gateway`, `hermes_local`, `process`, `http`. Each is its own npm package. External adapters can override built-ins via `plugin-loader.ts` with pause/resume semantics. (`server/src/adapters/registry.ts:207-222`) |
| "The scheduler is ~150 lines." | `server/src/services/heartbeat.ts` is **4,533 lines**. It's the engine: run claim, concurrency control, budget guard, orphan reaping, retry tracking, session compaction, workspace isolation, live event publishing. |
| "One `PropertiesPanel` reused with 3 modes." | It's a 29-line render-slot (`ui/src/components/PropertiesPanel.tsx`) backed by React Context (`ui/src/context/PanelContext.tsx`). Any page calls `openPanel(<jsx/>)` to push its own content. Completely different (and better) pattern than what I described. Visibility persisted to localStorage. |
| "IssueChatThread has a clean chain-of-thought fold primitive." | It's a 2,007-line component built on top of `@assistant-ui/react` primitives (`MessagePrimitive`, `ActionBarPrimitive`, `ThreadPrimitive`, `useMessage`, `useAuiState`). Not a small lift. The fold logic itself (`resolveAssistantMessageFoldedState` + synchronous render-time state derivation, `IssueChatThread.tsx:1001-1016`) is clean, but it depends on the assistant-ui runtime. |
| "Billing codes as a separate concept." | Cost is tracked in `cost_events` (5-dimensional: company, agent, issue, project, goal, heartbeat_run) with `billingCode` as one field among many. The adapter returns `costUsd` directly in `AdapterExecutionResult` (`packages/adapter-utils/src/types.ts:78-83`), so billing is a consequence of the adapter reporting cost per run, not a separate subsystem. |
| "Budgets are a single monthly limit." | Layered system: `budget_policies` table (scope = company/agent/project, metric = billed_cents, hard-stop flag, soft-alert threshold), `budget_incidents` table (audit when a policy fires), `agents.{budgetMonthlyCents, spentMonthlyCents}` convenience fields, plus `companies.pauseReason = 'budget'` for auto-pause. `getInvocationBlock(companyId, agentId, ctx)` in `server/src/services/budgets.ts:716` checks all layers before dispatch. |
| "Task-centric communication, no side channel." | Confirmed and stronger than I described. `SPEC-implementation.md:38` says: `"Communication | Tasks + comments only (no separate chat system)"`. They explicitly rejected chat. |

## Grounded comparison

| Dimension | Paperclip | WUPHF (current) |
|---|---|---|
| **Runtime surface** | `heartbeat_runs` table, queued→running→succeeded/failed/cancelled/timed_out lifecycle, content-addressed log storage (`logSha256`, `logCompressed`), retry tracking (`retryOfRunId`, `processLossRetryCount`), orphan reaping via `reapOrphanedRuns` in `heartbeat.ts:2333` | Implicit — `runHeadlessClaudeTurn(ctx, slug, notification)` (`internal/team/headless_claude.go:21`) and `runHeadlessCodexTurn` (`internal/team/headless_codex.go:185`). No queue, no run table, no retry, no orphan detection. Each turn is fire-and-forget. |
| **Agent dispatch** | `startNextQueuedRunForAgent` uses `withAgentStartLock` (`heartbeat.ts:309`) to serialize, checks `parseHeartbeatPolicy(agent).{enabled, intervalSec, wakeOnDemand, maxConcurrentRuns}`, atomic DB claim via `heartbeatRuns UPDATE WHERE status = 'queued'` | `notifyAgentsLoop` (`launcher.go:310`) polls message stream, calls `deliverMessageNotification` → launches headless turn. No locks, no policy, no concurrency cap per agent. |
| **Adapter per agent** | `agents.adapterType` + `agents.adapterConfig` jsonb (`packages/db/src/schema/agents.ts:25-26`), so CEO can run Opus Claude and dev can run Codex | Global `--provider claude|codex` flag at startup. All agents share one provider. Model per agent is hardcoded: CEO gets Opus, specialists get Sonnet (`headless_claude.go:152-157`). |
| **Cost tracking** | `cost_events` table indexed 5 ways (`packages/db/src/schema/cost_events.ts`), populated from `AdapterExecutionResult.{inputTokens, outputTokens, costUsd, provider, biller, model, billingType}` | None. Latency is logged (`appendHeadlessClaudeLatency`) but token counts and dollars are not captured. |
| **Budget enforcement** | `getInvocationBlock` called at run claim time, checks company/agent/project in layers, auto-pauses scopes that exceed hard-stop, logs `budget_incidents` | None. Turns run until the underlying CLI runs out of quota. |
| **Activity log** | One 94-line service, `logActivity(db, input)` in `server/src/services/activity-log.ts`. Invariant: every mutation calls it. Writes to table, publishes live event, emits to plugin event bus, redacts sensitive values. | Scattered across `office-ledger`, `watchdog-ledger`, individual service handlers. No single invariant, no redaction, no live event fan-out. |
| **Approvals** | `approvals` table with polymorphic `type` ("hire_agent", "approve_ceo_strategy", etc.), payload jsonb, state machine (pending→approved/rejected/cancelled). Separate `approval_comments` table for discussion. (`packages/db/src/schema/approvals.ts`) | `requests` table serves a similar purpose but is ad-hoc (kind/summary/blocking/required) with no typed payloads or state machine. |
| **Task model** | `issues` table with `checkoutRunId`, `executionRunId`, `executionLockedAt`, single-assignee atomic checkout, parent/sub-issue hierarchy, `originKind`/`originId`/`originRunId`, `billingCode`, per-issue `assigneeAdapterOverrides`, execution workspace isolation via git worktrees (`packages/db/src/schema/issues.ts`, 94 lines) | `tasks` on broker have `{id, channel, owner, status, title, ...}`. No atomic claim, no parent/child, no per-task adapter override, no workspace isolation. |
| **Web UI shell** | One `Layout.tsx` (486 lines) for mobile + desktop, `CompanyRail` + `Sidebar` + `BreadcrumbBar` + `<Outlet/>` + `PropertiesPanel` + `MobileBottomNav`, swipe gestures at 30px edge zone with 50px minimum, body overflow management, skip link for a11y, 2 keyboard shortcuts hooks. | Hand-rolled vanilla JS in `web/index.html` (7,274 lines including themes). No mobile story. Agent-specific bespoke `#agent-panel`. |
| **Chat surface** | `IssueChatThread.tsx` (2,007 lines) on `@assistant-ui/react`, chain-of-thought fold with synchronous render-time state derivation, rolling reasoning line with CSS enter/exit animations (`cot-line-enter`, `cot-line-exit`), rolling tool display, user/assistant/system message primitives, feedback buttons with optional reason popover | Chat feed in `web/index.html` (`appendMessageToContainer`), hand-rolled. No fold, no rolling display, no feedback capture, no error boundary. The recent-actions panel was just removed for being space-wasteful. |
| **Generative UI** | None. Chat is text + tool traces. | **A2UI exists** (`internal/tui/generative.go`, 269 lines): `A2UIComponent` struct with type/children/props/dataRef/action, RFC 6901 JSON Pointer resolution, component registry, inline table and progress rendering. This is an architectural advantage Paperclip does not have. |
| **Workflow runtime** | None. Agents execute prompts, emit text + tool calls. | Partial: `internal/action/workflow_store.go` (131 lines) as scaffolding. Approved April 1 design targets a JSON workflow runtime on top of A2UI — not yet implemented. |
| **Integration** | Terminal-native via adapters only. No CRM, email, or calendar integration beyond `openclaw_gateway`. | Nex integration for insights/notifications/memory. One CLI for Gmail/Calendar/CRM. |

## What's actually worth stealing

Ranked by ROI-for-WUPHF's-current-state, not by how interesting the idea is.

### 1. `logActivity` as an invariant — 1 day, high ROI

The 94-line `activity-log.ts` is the cleanest pattern in Paperclip. One function, three jobs: write to a table, publish a live event, emit to a plugin event bus. Every mutation in Paperclip calls it. The `runId` foreign key enables per-run audit replay, which is how you debug agent misbehavior after the fact.

For WUPHF, the equivalent is `func (b *Broker) logActivity(ActivityInput) error` that:
- Appends to an `activity_log` slice on `brokerState` (persisted via `saveLocked`)
- Broadcasts to any SSE/websocket subscribers (for real-time web view updates)
- Takes `{actorType, actorId, action, entityType, entityId, runId, details}` — same shape

WUPHF has partial audit today (office-ledger, watchdog ledger, request lifecycle logs). Unifying would be ~50 lines of Go + ~30 lines of Go to migrate existing call sites. Enables the rest: replay, inline system messages in chat, cleaner `/doctor` output.

### 2. PropertiesPanel render-slot pattern — 0.5 day, high ROI

The 29-line `PropertiesPanel.tsx` is a dumb container. `PanelContext` holds `panelContent: ReactNode | null`. Pages call `openPanel(<jsx/>)` on mount to push their detail. The panel's visibility is persisted to localStorage.

For WUPHF's vanilla JS web view, the equivalent is:
- One `#detail-panel` element in `web/index.html`
- One `WuphfDetail.show(contentEl)` / `WuphfDetail.hide()` API
- `localStorage` for the visible preference
- `openAgentPanel(slug)` becomes `WuphfDetail.show(buildAgentDetail(slug))`
- Tasks and requests gain their own `buildTaskDetail(id)` / `buildRequestDetail(id)` that push into the same slot

Replaces the bespoke `#agent-panel` with one slot. About 150 lines of JS added, 200 lines removed. Biggest ergonomic win in the web view.

### 3. Chain-of-thought fold inline in chat — 1-2 days, medium-high ROI

The fold pattern is strictly better than the "Recent Actions" panel that was just removed. Agent messages collapse their work by default, expand on click, auto-expand while running. The rolling reasoning line during active runs (`IssueChatReasoningPart`, animates via CSS `cot-line-enter`/`cot-line-exit`) is a small detail that makes active runs feel alive.

For WUPHF's web view, **don't depend on `@assistant-ui/react`** — that's a big library for a vanilla JS project. Build a minimal version:
- Agent messages get an optional `_cot` array of `{type: 'reasoning' | 'tool_call', ...}` entries
- When present and the message is not actively running, wrap in a fold button with chevron
- When running, auto-expand and show the latest tool call with a spinner
- Use CSS keyframe animations for the rolling text
- Click expands/collapses; preserves fold state per message ID

WUPHF already emits tool_use/tool_result events during `runHeadlessClaudeTurn` (`headless_claude.go:90-99`) — they just need to be attached to the message in broker state. ~300 lines of JS + ~50 lines of Go to plumb the events through.

### Bonus (if/when the workflow runtime lands)

**Adopt `heartbeat_runs`-style run table** once WUPHF has a queue worth tracking (e.g., workflow steps running async). The schema fields to copy: `{id, companyId, agentId, invocationSource, triggerDetail, status, startedAt, finishedAt, error, errorCode, usageJson, resultJson, contextSnapshot, retryOfRunId, processLossRetryCount}`. Don't copy `logStore`/`logRef`/`logSha256` — that's premature.

## What WUPHF has that Paperclip doesn't

These are real structural advantages. Paperclip is a control plane for task execution; WUPHF is (becoming) a conversational office with generative UI.

1. **A2UI (`internal/tui/generative.go`).** Agents can emit JSON schemas that render as real UI components — tables, cards, progress bars. RFC 6901 JSON Pointer resolution for data binding. This is the foundation the April 1 approved workflow runtime sits on. Paperclip has nothing here — chat is text + collapsible tool traces.

2. **Office metaphor.** Channels, threads, tasks, requests, calendar as a unified conversational surface. Paperclip explicitly rejected this ("Tasks + comments only (no separate chat system)"). Whether Paperclip is right depends on user type: for task-execution power users, tasks+comments is cleaner. For founders running a small AI company who want the legibility of a Slack office, WUPHF's model reads better.

3. **Nex integration.** Real-time context from the outside world (CRM changes, email, calendar, meetings). Paperclip is a closed system with `process` and `http` adapters but no first-class knowledge integration.

4. **Terminal-first TUI + web view parity.** Two surfaces for the same broker. Paperclip is web-only. For a terminal-native builder audience, the TUI is a moat.

5. **Telegram, Gmail, calendar, slash commands, MCP integration.** WUPHF's daily operator surface is further along than Paperclip's for actual work. Paperclip has routines (cron) and MCP, but the integration set is thinner.

6. **The `channel_email.go` and email-triage code path** — this is where the workflow runtime was supposed to replace hardcoded Go but didn't. It's still an asset — a working email assistant — even if the generalization didn't land.

## What NOT to adopt

These patterns look attractive but don't serve WUPHF's current shape.

1. **Full `ServerAdapterModule` interface.** The 15-method shape is sized for a plugin ecosystem with 10+ external adapters. WUPHF needs 2 (claude, codex) plus a third (process/http) if/when it wants to call external services. Lift a minimal `HeadlessAdapter` interface with just `Execute(ctx, slug, notification) error` and `Cancel(ctx, slug) error`. Store provider per agent in `officeMember.adapterType`. That's the MVP of the idea without the complexity.

2. **Full budget policy system.** `budget_policies` + `budget_incidents` + multi-scope enforcement is appropriate for ops teams with real dollars on the line. WUPHF has one user. Add `cost_events` as passive logging now — when each headless run finishes, write `{agent, model, input_tokens, output_tokens, cost_cents, occurred_at}` to a file or broker state. That's enough to answer "how much did today cost." Policies can come later.

3. **Agent Companies protocol / Clipmart.** Markdown-based portable company packages are only relevant once you have multiple packs worth sharing. WUPHF has one pack (Founding Team), one user.

4. **Plugin system.** Paperclip has 20+ files under `plugin-*` (plugin-host-services, plugin-job-coordinator, plugin-job-scheduler, plugin-job-store, plugin-lifecycle, plugin-loader, plugin-registry, plugin-runtime-sandbox, plugin-secrets-handler, plugin-state-store, plugin-stream-bus, plugin-tool-dispatcher, plugin-tool-registry, plugin-worker-manager, plugin-dev-watcher, plugin-event-bus, plugin-config-validator, plugin-capability-validator, plugin-manifest-validator, plugin-log-retention, plugin-ui-static). This is where Paperclip got expensive. Only worth it when third parties want to build against you, which WUPHF does not need.

5. **Organizational hierarchy (goals → projects → milestones → issues → sub-issues).** WUPHF's current flat task model is appropriate for its scope. Adding a single `project_id` field on tasks if needed is fine. Don't copy the full hierarchy.

6. **`@assistant-ui/react` dependency.** A big library with its own runtime primitives. The chain-of-thought fold pattern is worth stealing; the library is not. Implement a minimal equivalent in vanilla JS.

7. **`reapOrphanedRuns` / `processLossRetryCount`.** These matter when runs are long-lived and can be orphaned by process death. WUPHF's turns are short (a few seconds to a few minutes). Don't need it until the workflow runtime has async steps.

## The real 3-item shortlist

Not 15. Not 7. **Three.**

1. **Unify audit via `logActivity`** — ~1 day — unblocks replay, inline system messages in chat, real-time web view updates
2. **Render-slot `#detail-panel` replacing bespoke `#agent-panel`** — ~0.5 day — biggest ergonomic improvement in the web view
3. **Chain-of-thought fold inline in chat messages** — ~1-2 days — the right answer to "where should agent tool traces live visually"

Everything else waits. In particular:
- The adapter interface wait until there's a concrete reason to run mixed providers in one office
- Cost tracking waits until the first bill gets uncomfortable
- Budgets wait until that bill has stakeholders other than the founder
- The workflow runtime is on a different branch with a different plan (see `najmuzzaman-nazz-email-postmortem-design-20260401-153916.md`) and should not be re-scoped through a Paperclip lens

## What I still do not know

Things worth verifying before implementation, not before deciding:

1. Whether WUPHF's activity log should write to broker state JSON or a SQLite file (Paperclip uses Postgres; WUPHF uses JSON state files today)
2. Whether the fold pattern should live inside `appendMessageToContainer` or as a separate `renderAgentMessage` helper
3. Whether the detail panel should be responsive (hide on mobile?) — Paperclip's is `hidden md:flex`, desktop-only
4. Whether to integrate with the existing `publishLiveEvent`-equivalent or introduce a new broadcast mechanism in the broker
5. Whether the chain-of-thought fold should also be available in the TUI (`cmd/wuphf/channel.go`) or web-only for now

## Sources (verified reads, not WebFetch)

Paperclip clone: `/tmp/paperclip-analysis/paperclip` at HEAD `b00d52c`.

- `packages/adapter-utils/src/types.ts:13-331` (AdapterRuntime, AdapterExecutionContext, ServerAdapterModule, AdapterExecutionResult, AdapterBillingType)
- `server/src/adapters/registry.ts:1-416` (10 built-in adapters, override/pause)
- `server/src/adapters/process/execute.ts:14-86` (process adapter implementation)
- `server/src/services/heartbeat.ts:2182-2192` (parseHeartbeatPolicy), `:2202-2278` (claimQueuedRun with budget check), `:2333-2358` (reapOrphanedRuns)
- `server/src/services/budgets.ts:716-830` (getInvocationBlock layered check)
- `server/src/services/activity-log.ts:1-95` (the entire file)
- `packages/db/src/schema/heartbeat_runs.ts:1-55` (full schema with content-addressed logs)
- `packages/db/src/schema/cost_events.ts:1-54` (5-dimensional cost tracking)
- `packages/db/src/schema/activity_log.ts:1-26` (runId linkage)
- `packages/db/src/schema/approvals.ts:1-28`
- `packages/db/src/schema/agents.ts:1-42` (adapter_type, adapter_config, runtime_config, budget_monthly_cents)
- `packages/db/src/schema/issues.ts:1-94` (single-assignee, atomic checkout, per-issue adapter overrides)
- `ui/src/components/Layout.tsx:58-486` (responsive single layout, swipe gestures)
- `ui/src/components/PropertiesPanel.tsx:1-29` (render-slot pattern)
- `ui/src/context/PanelContext.tsx:1-75` (the context that powers PropertiesPanel)
- `ui/src/components/IssueChatThread.tsx:513-628` (ChainOfThought component), `:677-733` (RollingToolPart), `:967-1172` (AssistantMessage with fold)
- `doc/SPEC-implementation.md:38-52` (V1 product decisions, including "Tasks + comments only")

WUPHF cross-reference reads:

- `internal/team/headless_claude.go:1-190` (current runtime model)
- `internal/team/headless_codex.go:1-50` (codex worker queue)
- `internal/team/broker.go:264-306` (brokerState, Broker structs)
- `internal/team/launcher.go:113-175` (Launcher lifecycle)
- `internal/tui/generative.go:1-269` (A2UI component system, RFC 6901 pointer resolution)
- `internal/action/workflow_store.go` (131 lines of workflow scaffolding)
- `docs/competitive-analysis-multi-agent-projects.md:1-120` (WUPHF's own positioning doc)
- `~/.gstack/projects/nex-crm-wuphf/najmuzzaman-nazz-email-postmortem-design-20260401-153916.md` (approved April 1 workflow runtime design)
