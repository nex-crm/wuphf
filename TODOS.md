# TODOS

## internal/team

### Pre-existing test failure: TestOperationBlueprintMatrixServesBootstrapPackageEndpoint

**Priority:** P0

Bootstrap package endpoint is returning blueprint id `"rsre"` instead of the expected blueprint slug for every fixture (bookkeeping-invoicing-service, local-business-ai-package, multi-agent-workflow-consulting, niche-crm, paid-discord-community, youtube-factory).

Reproduced against clean `origin/main` (not introduced by `feat/tasks-wont-do-column`).

```
--- FAIL: TestOperationBlueprintMatrixServesBootstrapPackageEndpoint (0.67s)
    operation_matrix_test.go:245: unexpected endpoint blueprint id: got "rsre" want "bookkeeping-invoicing-service"
    ... (6 subtests total)
FAIL  github.com/nex-crm/wuphf/internal/team
```

Noticed by: `/ship` on feat/tasks-wont-do-column, 2026-04-17.

Likely suspects:
- `internal/team/operation_bootstrap.go` — `operationStarterAgent` projection (see prior learning: added fields drop silently when not added to projection)
- `internal/operations/loader.go` — blueprint id parsing/validation

## Completed
