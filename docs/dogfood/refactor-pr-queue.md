# Refactor PR Queue Fixture

## Queue

| PR | Domain | Goal | Owner | Status | Required Evidence |
|---|---|---|---|---|---|
| API-001 | platform | Add route registry proof for platform health/version/usage/upgrade/queue/token routes | backend + Web | done | route registry test, typed health/upgrade responses, protected-route auth test, `web/src/api/platform.ts`, `web/src/api/upgrade.ts` |
| TEST-001 | platform | Standardize local Web unit/component test entry point | docs + tooling | done | `scripts/test-web.sh`, docs guard for agent-facing runner guidance, full Web Vitest suite |
| TASK-000 | tasks | Name task HTTP request/response envelopes and register task routes | backend | done | `internal/team/broker_tasks_contracts.go`, task route registry test, focused `internal/team` route/task tests |
| TASK-WEB-001 | tasks | Move Web task contract out of catch-all API client | frontend | done | `web/src/api/tasks.ts`, `web/src/api/tasks.test.ts`, updated task consumers |
| TASK-001A | tasks | Extract typed task list and ack services behind task routes | backend | done | `TaskListRequest`, `Broker.ListTasks`, `Broker.AckTask`, `internal/team/broker_tasks_service_test.go`, route contract update |
| TASK-001 | tasks | Extract task POST mutation service | backend | done | `Broker.MutateTask`, `TaskMutationError`, service tests, existing task route tests |
| TASK-002 | tasks | Split `MutateTask` internals into narrower create/update helpers | backend | planned | broker task tests, mutation service tests |
| REVIEW-001 | reviews | Define review queue/list/detail/action contract | backend + TUI | planned | review tests, Web parity check, TUI renderer test |
| WIKI-001 | wiki | Name wiki search/read/write contract | backend + Web | planned | wiki route tests, Web API tests, search modal check |
| WS-001 | workspaces | Document route-family ownership and service boundary | backend + CLI | planned | workspace broker tests, CLI workspace tests |
| WEB-ROUTER-001 | web | Evaluate TanStack Router route-module migration after contract slices | frontend | deferred | migration plan, no behavior rewrite, route/state ownership map |

## Coordination Notes

- Each PR updates `docs/surfaces.md`.
- Shared API/type files need one coordinating owner.
- TUI and Web agents should not invent behavior that is not visible in the
  contract row.
