# Surface Parity Contracts

WUPHF has two human-facing renderers: the TUI (`cmd/wuphf/`) and the web UI
(`web/`). Product behavior should be contract-first. A capability can still be
terminal-shaped, browser-shaped, or intentionally asymmetric, but the contract,
state ownership, gaps, and tests must be visible before either surface grows new
semantics.

This doc is an implementation gate. Every feature PR that adds, removes, or
changes a user-facing capability must update the matrix row or add one.

## Status Words

- `contracted` - both surfaces consume the same capability contract.
- `partial` - the surface intentionally renders a narrower view of the same
  contract.
- `planned` - the capability has a target contract but one surface is not wired.
- `not-surfaced` - the capability exists but this surface has no renderer.
- `web-only` - browser-only by product choice; explain the reason in gaps.
- `tui-only` - terminal-only by product choice; explain the reason in gaps.

## Capability Parity Matrix

| Capability | Domain | Shared API | Web state | TUI state | Missing gaps | Test coverage |
|---|---|---|---|---|---|---|
| Office channels, roster, DMs | office | `/channels`, `/channels/dm`, `/channel-members`, `/members`, `/office-members` | `contracted`: `web/src/hooks/useChannels.ts`, `web/src/hooks/useMembers.ts`, sidebar and shell components | `contracted`: `cmd/wuphf/channel_*`, `cmd/wuphf/channelui/*` | Extract channel service methods before moving handlers out of `internal/team` | `internal/team/broker_office_channels_test.go`, `internal/team/broker_providers_test.go`, TUI channel tests, web sidebar tests |
| Messages, inbox, outbox, threads | messages | `/messages`, `/reactions`, `/events` | `contracted`: message hooks, composer, feeds, thread panel | `contracted`: channel mailbox, thread, composer, unread files | SSE event union is still implicit in `broker_sse.go`; route contract is not centralized | `internal/team/broker_messages_test.go`, `internal/team/broker_streams_test.go`, `cmd/wuphf/channel_*_test.go`, web message tests |
| Tasks and work evidence | tasks | `/tasks`, `/tasks/ack`, `/task-plan`, `/agent-logs`, `/tasks/memory-workflow`, `/tasks/memory-workflow/reconcile` | `contracted`: `web/src/api/tasks.ts`, `TasksApp`, `TaskDetailModal`, receipts and activity task views | `partial`: task cards and workflow summaries in `cmd/wuphf/channelui/task_workflow_builders.go` and policy/task line builders | `BrokerRouteContracts()` covers task routes and typed HTTP envelopes; `ListTasks` and `AckTask` are typed service boundaries, while create/update service extraction remains pending | `internal/team/broker_route_contracts_test.go`, `internal/team/broker_tasks_service_test.go`, `internal/team/broker_tasks_test.go`, `internal/team/task_pipeline_test.go`, `web/src/api/tasks.test.ts`, `web/src/components/apps/TaskDetailModal.test.tsx` |
| Requests, approvals, interviews | requests | `/requests`, `/requests/answer`, `/interview`, `/interview/answer` | `contracted`: `RequestsApp`, `InterviewBar`, `HumanInterviewOverlay` | `contracted`: `cmd/wuphf/channel_interview.go`, `cmd/wuphf/channelui/interview*` | Request action input/output structs are still handler-local | `internal/team/broker_requests_interviews_test.go`, TUI interview tests, web request/interview tests |
| Co-founder sharing | requests | `/humans`, `/humans/me`, `/humans/invites`, `/humans/invites/accept`, `/humans/sessions`, `wuphf share` | `partial`: shared web office consumes invite sessions after acceptance | `contracted`: `cmd/wuphf/share.go` mints the private-network invite URL | Host broker tokens stay out of shared browser sessions; richer sharing settings should extend the same humans contract | `cmd/wuphf/share_test.go`, `internal/team/broker_human_share_test.go`, sharing settings/web session tests pending |
| Review queue and promotion actions | reviews | `/review/list`, `/review/`, `/notebook/promote`, `review:state_change` SSE | `contracted`: `web/src/components/review/*` | `not-surfaced`: no terminal review queue yet | Need a terminal review list/detail/action renderer or an explicit web-only decision | `internal/team/broker_review_test.go`, `internal/team/promotion_state_test.go`, web review tests |
| Wiki search, read, write, audit | wiki | `/wiki/write`, `/wiki/write-human`, `/wiki/read`, `/wiki/search`, `/wiki/lookup`, `/wiki/list`, `/wiki/article`, `/wiki/catalog`, `/wiki/audit`, `/wiki/sections`, `/wiki/lint/*`, `/wiki/archive/sweep`, `/wiki/extract/replay`, `/wiki/dlq` | `contracted`: wiki shell, article, search, audit, lint, sources, edit log | `planned`: terminal text search/read/write is not wired | TUI should consume the same search/read/write contract and render text-first output | `internal/team/wiki_*_test.go`, `internal/team/broker_wiki_dlq_test.go`, web wiki tests |
| Agent notebook and promotion source | notebook | `/notebook/write`, `/notebook/read`, `/notebook/list`, `/notebook/catalog`, `/notebook/search`, `/notebook/promote`, `notebook:write` SSE | `contracted`: `web/src/components/notebook/*`, `web/src/api/notebook.ts` | `planned`: no terminal notebook catalog/detail yet | TUI needs entry list/read/promote affordances if notebook remains structured text | `internal/team/broker_notebook_test.go`, `internal/team/notebook_worker_test.go`, web notebook tests |
| Workspaces and destructive workspace actions | workspaces | `/workspaces/list`, `/workspaces/create`, `/workspaces/switch`, `/workspaces/pause`, `/workspaces/resume`, `/workspaces/shred`, `/workspaces/restore`, `/workspaces/trash`, `/workspaces/onboarding`, `/workspace/reset`, `/workspace/shred`, `/admin/pause` | `contracted`: workspace rail, create/shred/restore modals, settings danger zone | `contracted`: `cmd/wuphf/workspace*.go`, workspace adapter | Two workspace route families exist; document which one each new action extends | `internal/team/broker_workspaces_test.go`, `cmd/wuphf/workspace*_test.go`, web workspace tests |
| Providers, settings, integrations | providers | `/config`, `/status/local-providers`, `/image-providers`, `/nex/register`, `/telegram/*`, `/bridges` | `contracted`: settings app and integration modals | `partial`: doctor, onboarding probes, integration helpers | Provider status and config should move behind typed service methods before new settings panels expand | `internal/team/broker_config_provider_endpoints_test.go`, `internal/team/local_providers_status_test.go`, onboarding and web settings tests |
| Calendar and scheduled work | operations | scheduler, task, request, and action projections over the shared calendar view state | `contracted`: `CalendarApp`, schedules panel, activity views | `contracted`: `/calendar`, `/queue`, day/week/filter/all calendar controls | A standalone calendar HTTP contract is still pending; renderers derive from scheduler/task/request state today | `internal/calendar/*_test.go`, `internal/team/broker_scheduler_test.go`, calendar TUI tests, web calendar tests pending |
| Policies, scheduler, operations | operations | `/policies`, `/scheduler`, `/scheduler/`, `/operations/bootstrap-package`, `/studio/*` | `contracted`: policies app, system schedules panel, artifact/activity views | `partial`: policy/task cards and operational summaries in channel UI | Operation bootstrap and scheduler route types are split across handler files; central contract registry is pending | `internal/team/broker_policies_test.go`, `internal/team/broker_scheduler_test.go`, `internal/team/operation_*_test.go`, web scheduler tests |
| Skills and playbooks | skills | `/skills`, `/skills/`, `/skills/compile`, `/skills/compile/stats`, `/playbook/*` | `contracted`: skills app, playbook badges and execution log | `partial`: command and channel summaries | Skill CRUD, compile, invoke, and playbook actions need a named domain service before further UI growth | `internal/team/broker_skills_test.go`, `internal/team/skill_*_test.go`, web skills tests |
| Entity graph, signals, decisions, actions | integrations | `/entity/*`, `/learning/*`, `/signals`, `/decisions`, `/watchdogs`, `/actions` | `partial`: graph app, activity/artifact views, wiki entity panels | `partial`: message and task summaries only | Graph visualization is browser-shaped, but text summaries should still share entity/query contracts | `internal/team/broker_entity*_test.go`, `internal/team/entity_*_test.go`, web graph/entity tests |
| Health, usage, version, upgrade | platform | `/health`, `/version`, `/usage`, `/upgrade-check`, `/upgrade-changelog`, `/upgrade/run`, `/queue`, `/web-token` | `contracted`: runtime strip, health check app, `web/src/api/platform.ts`, `web/src/api/upgrade.ts`, upgrade/settings surfaces | `contracted`: doctor, upgrade command, status strips | `BrokerRouteContracts()` covers the platform route set; generated TypeScript remains pending | `internal/team/broker_route_contracts_test.go`, `internal/team/broker_misc_handlers_test.go`, `internal/team/broker_upgrade_test.go`, `cmd/wuphf/channel_doctor_test.go`, `web/src/api/platform.test.ts`, `web/src/api/upgrade.test.ts` |
| Receipts and rich artifacts | artifacts | `/agent-logs`, scheduler/artifact projections, receipt views | `web-only`: receipts and rich artifact panels | `partial`: text summaries and task evidence lines | Rich visual receipts stay browser-shaped; terminal must still show evidence summaries through task/log contracts | `web/src/components/apps/TaskDetailModal.test.tsx`, `cmd/wuphf/channel_artifacts_test.go`, receipt/artifact app tests pending |

## Implementation Rules

Every new capability starts with a shared contract row:

1. Name the domain and route set before adding UI-specific state.
2. Add or identify service-level methods with typed inputs and outputs.
3. Keep HTTP handlers as adapters over those service methods.
4. Wire Web and TUI renderers to the same contract, or state why one surface is
   intentionally absent.
5. Add service tests for business behavior and route tests for HTTP behavior.
6. Update this matrix in the same PR.

If Web needs richer visual state than TUI, keep the richer state derived from
the shared contract. Do not add hidden semantics that only one renderer can
observe.

## Contract Debt

- Most HTTP contracts still live across `internal/team/broker*.go` handlers and
  handwritten TypeScript types in `web/src/api/client.ts`, `web/src/api/wiki.ts`,
  `web/src/api/notebook.ts`, and `web/src/api/workspaces.ts`.
- The target direction remains a route registry or generated contract package
  that maps route, method, request type, response type, events, and domain.
- SSE is intentionally listed as a shared API where relevant, but the event union
  is not centralized yet.

## What This Doc Is Not

- A backlog. Use issues and PRs for scheduling.
- A promise of identical UI. Renderers can differ when the contract and missing
  gaps are explicit.
- A substitute for architecture docs. Domain ownership and change guides live in
  [`docs/architecture/agent-navigability.md`](architecture/agent-navigability.md).
