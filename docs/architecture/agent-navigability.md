# Agent Navigability

WUPHF should be easy for several agents to change without each agent rebuilding
the whole product model from scattered handlers. This guide defines domain
boundaries, file ownership, and the evidence expected when a task changes code.

## Domain Boundaries

Each domain should converge on the same shape:

- service methods with typed inputs and outputs
- HTTP handlers as thin adapters
- Web and TUI renderers consuming the same contract
- service-level tests for behavior and route-level tests for HTTP semantics

| Domain | Owns | State | Service and route files | Web consumers | TUI consumers | Required tests |
|---|---|---|---|---|---|---|
| office | channels, roster, channel membership, DMs, office member generation | `teamChannel`, `officeMember`, channel store, member index, channel access rules | `internal/team/broker_office_channels.go`, `internal/team/broker_office_members.go`, `internal/team/broker_channel_access.go`, `internal/channel/*` | sidebar channel/member hooks, shell channel state, agent and channel wizards | `cmd/wuphf/channel_*`, channel picker, sidebar, member draft views | office channel tests, provider/channel tests, Web sidebar tests, TUI channel tests |
| messages | channel messages, inbox/outbox views, threads, reactions, message SSE | `channelMessage`, message subscribers, reaction state, thread fields | `internal/team/broker_messages.go`, `internal/team/broker_streams.go`, `internal/team/broker_sse.go`, `internal/channel/*` | message feed, composer, thread panel, message hooks | channel mailbox, thread, composer, unread and render files | broker message tests, stream/SSE tests, Web message tests, TUI message/thread tests |
| tasks | task lifecycle, work evidence, worktrees, task memory workflow, task logs | `teamTask`, typed task HTTP envelopes, worktree fields, task memory workflow, agent log root | `internal/team/broker_tasks*.go`, `internal/team/broker_tasks_contracts.go`, `internal/team/broker_tasks_service.go`, `internal/team/broker_tasks_mutation_service.go`, `internal/team/task_pipeline.go`, `internal/team/worktree.go` | `web/src/api/tasks.ts`, `web/src/components/apps/TasksApp.tsx`, `TaskDetailModal.tsx`, receipts/activity task views | `cmd/wuphf/channelui/task_workflow_builders.go`, task/policy line builders, channel task views | `internal/team/broker_route_contracts_test.go`, `internal/team/broker_tasks_service_test.go`, `internal/team/broker_tasks_test.go`, `internal/team/task_pipeline_test.go`, `web/src/api/tasks.test.ts`, Web/TUI task rendering tests |
| requests | human gates, approvals, interviews, blocking request state | `humanInterview`, `pendingInterview`, scheduler request jobs | `internal/team/broker_requests_interviews.go`, `internal/team/broker_human.go` | `RequestsApp`, `InterviewBar`, `HumanInterviewOverlay` | `cmd/wuphf/channel_interview.go`, `cmd/wuphf/channelui/interview*` | request service tests, route method/status tests, Web/TUI interview tests |
| reviews | promotion review state, comments, state transitions | `ReviewLog`, `Promotion`, review subscribers | `internal/team/broker_review.go`, `internal/team/promotion_state.go`, `internal/team/promotion_log.go` | `web/src/components/review/*`, notebook promote controls | no renderer yet | review state tests, promote route tests, future TUI review tests |
| wiki | local markdown wiki, search, article read/write, audit, lint, archive, sections | wiki repo, `WikiWorker`, `WikiIndex`, `ReadLog`, `DLQ` | `internal/team/wiki_*.go`, `internal/team/broker_wiki_*.go`, `internal/team/broker_human.go` | `web/src/components/wiki/*`, `web/src/api/wiki.ts`, search modal | planned terminal search/read/write | wiki worker/index/service tests, route tests, Web article/search tests |
| notebook | agent notebook entries, catalog, search, promotion source | notebook files under wiki repo, notebook events | `internal/team/broker_notebook.go`, `internal/team/notebook_worker.go`, review promotion files | `web/src/components/notebook/*`, `web/src/api/notebook.ts` | planned terminal catalog/read/promote | notebook worker tests, broker notebook route tests, Web notebook tests |
| workspaces | workspace list/create/switch/pause/resume/shred/restore and admin pause | workspace orchestrator, launcher drain hooks, workspace state rows | `internal/team/broker_workspaces.go`, `cmd/wuphf/workspace*.go`, `internal/workspace/*` | workspace rail and modals, settings danger zone, `web/src/api/workspaces.ts` | workspace commands and adapter | broker workspace tests, CLI workspace tests, Web workspace tests |
| providers | runtime/provider config, local provider status, image providers, external integrations | config file, provider bindings, local provider probes, bridge state | `internal/team/broker_office_channels.go`, `internal/team/local_providers_status.go`, `internal/team/broker_image_providers.go`, integration handlers | settings app, onboarding provider picker, integration modals | doctor, onboarding probes, integration helpers | config/provider endpoint tests, local provider tests, onboarding tests |
| integrations | entity graph, facts, learning, signals, decisions, watchdogs, external events | `FactLog`, `EntityGraph`, learning log, signals/actions/decisions/watchdogs | `internal/team/broker_entity.go`, `internal/team/entity_*.go`, `internal/team/broker_learning.go`, misc handlers | graph app, entity panels, activity/artifacts views | text summaries in channel views | entity graph/fact tests, learning tests, route tests |
| operations | scheduler, policies, operation bootstrap, studio workflows | scheduler jobs, policies, operation bootstrap package state | `internal/team/broker_scheduler.go`, `internal/team/broker_policies.go`, `internal/team/operation_*.go`, `internal/team/broker_studio.go` | policies app, system schedules panel, artifacts/activity views | policy/task lines, operational summaries | scheduler tests, policy tests, operation bootstrap tests, Web scheduler tests |
| skills | skill CRUD, compile, synthesize, invoke, playbooks | skills slice, skill scanner/synthesizer metrics, playbook logs | `internal/team/broker_skills.go`, `internal/team/skill_*.go`, `internal/team/broker_playbook.go` | skills app, playbook badges, execution log | command summaries and channel output | skill CRUD/compile tests, playbook tests, Web skills tests |
| artifacts | task receipts, runtime artifacts, rich receipt views, evidence summaries | runtime artifact records, task logs, scheduler artifact projections | `internal/team/runtime_artifacts.go`, `internal/team/artifact_commit.go`, task log handlers, scheduler projections | `web/src/components/apps/ArtifactsApp.tsx`, `ReceiptsApp.tsx`, task detail evidence panels | `cmd/wuphf/channel_artifacts.go`, `cmd/wuphf/channel_artifact_snapshot.go`, `cmd/wuphf/channelui/artifact*` | task detail tests, TUI artifact tests, receipt/artifact app tests |
| platform | health, version, usage, upgrade, auth token, queue, SSE, streams | broker lifecycle, usage state, auth token, subscribers | `internal/team/broker_misc_handlers.go`, `broker_upgrade.go`, `broker_auth.go`, `broker_sse.go`, `broker_otlp_usage.go` | runtime strip, health check app, settings upgrade/status, `web/src/api/platform.ts`, `web/src/api/upgrade.ts` | doctor, status strips, upgrade command | middleware/auth/SSE/upgrade tests, `web/src/api/platform.test.ts`, `web/src/api/upgrade.test.ts` |

## How To Change A Domain

Use this sequence when adding or refactoring behavior:

1. Read the row in [`docs/surfaces.md`](../surfaces.md) and the domain row above.
2. Identify the service method that should own the behavior. If none exists,
   extract one before expanding the handler.
3. Add typed request/response structs next to the service boundary or in the
   current route contract location. Avoid new `map[string]any` response shapes.
4. Keep route handlers responsible for auth, method dispatch, decoding, status
   codes, and encoding only.
5. Update the Web API type or generated contract source before changing Web UI.
6. Update the TUI renderer from the same contract when the surface is contracted
   or partial.
7. Add or update service tests, route tests, and renderer tests named in the
   domain row.
8. Update [`docs/surfaces.md`](../surfaces.md) if the capability, shared API,
   renderer state, gaps, or tests changed.

## Ownership Rules

When several agents work in parallel, split ownership by domain and file class:

- Broker/domain service files: one agent owns the domain's `internal/team/*`
  service and route files for the PR.
- Shared API/type files: one agent owns the contract shape and coordinates all
  consumers before merge.
- Web component files: a Web agent owns `web/src/components/...`,
  `web/src/hooks/...`, and Web tests for that capability.
- TUI component files: a TUI agent owns `cmd/wuphf/...` and
  `cmd/wuphf/channelui/...` rendering and input tests.
- Docs/tests: each domain owner updates the docs and tests for their own
  changed behavior.

Agents should not edit another agent's owned files unless the owners coordinate
the handoff in the task or PR description.

## Canonical Repo Task Pipeline

Repo work should stay on one product pipeline:

`task -> local_worktree -> checks -> review -> draft_pr -> CI -> human gate`

Do not create separate state machines for "agent coding mode", "PR mode", and
"CI mode". Extend the task model with explicit fields only when the existing
task, review, worktree, scheduler, and receipt fields cannot represent the
state.

A repo task should not move to review or done without evidence:

- changed files summary
- tests/checks run
- log or command output summary
- worktree path and branch
- known risks
- next human action

## Test Runner Contract

Use one runner per test class so agent evidence is comparable:

- Go package tests: `bash scripts/test-go.sh` or
  `bash scripts/test-go.sh ./path/to/package`.
- Web unit/component tests: run `bash scripts/test-web.sh` for the full suite
  or `bash scripts/test-web.sh web/src/path/to/file.test.ts` for focused files.
- Web type/build checks: from `web/`, run `bunx tsc --noEmit` and
  `bun run build`.
- Web E2E checks: use `web/e2e/run-local.sh shell` unless a narrower E2E
  command is documented with the task.
- Bun-native package tests are package-local. For example, `npm/` uses
  `bun test`; do not use `bun test` inside `web/`, where it bypasses Vitest.

## Context Provenance

Nex context and integrations are additive to local-first state. A context item
shown to agents or humans should always expose its origin:

- local markdown wiki
- Nex context graph
- integration signal
- human note
- agent notebook

Do not hide provenance behind global behavior. Collaboration is debuggable only
when an agent can explain why it believes a fact and where that fact came from.

## Typed Contract Direction

Short term:

- declare typed Go request/response structs for every new route
- register migrated routes in `BrokerRouteContracts()` with domain, method,
  auth, request type, and response type
- keep TypeScript API types in one API module per domain
- update service and route tests with the contract shape

Target:

- a route registry that records domain, method, path, auth, request type,
  response type, event type, and renderer support
- generated TypeScript types or a centrally declared contract package
- CI that fails when generated contract files drift

Until the target exists, do not add new implicit response shapes to large
handlers or large Web API files without documenting the contract in the domain
row and tests.
