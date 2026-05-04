# Agent Workbench ICP Live QA Tutorial

This tutorial frames the three ICP examples as live QA steps for the Phase 2
agent workbench. The browser surface should keep task context, evidence, and
live terminal output together so a reviewer can inspect what an agent did
without jumping across apps.

## Before You Start

Build and start WUPHF from the repo root:

```bash
go build -o wuphf ./cmd/wuphf
./wuphf
```

In another terminal, run the web app:

```bash
cd web
bun run dev
```

Open the WUPHF web UI. These checks assume the workbench route is available at
`#/apps/workbench`, with optional agent and task segments:

```text
#/apps/workbench/<agent-slug>/tasks/<task-id>
```

## ICP Example 1: Alex Reviews A Long-Running Task

Scenario: Alex opens the workbench from an agent profile to understand what the
agent is doing now.

Live QA steps:

1. Open an agent profile for an agent with recent task activity.
2. Click `Open workbench`.
3. Confirm the workbench header shows the agent name and `@agent-slug`.
4. Confirm `Context` shows the current task title, owner, status, channel, and
   latest run.
5. Confirm `Evidence and artifacts` shows the memory workflow state, including
   lookup, capture, and promote progress when those steps exist.
6. Confirm the live terminal is scoped to the selected agent.
7. Confirm `Recent runs` only lists runs for that agent.

Expected result: Alex can review task context, evidence, recent runs, and live
terminal output in one place.

## ICP Example 2: Jordan Opens A Specific Task

Scenario: Jordan starts from a task card and needs the workbench locked to that
task, not just the owning agent.

Live QA steps:

1. Open the Tasks app.
2. Choose a task owned by an agent rather than the human user.
3. Click `Workbench` on that task card.
4. Confirm the hash route includes both the agent and task:
   `#/apps/workbench/<agent-slug>/tasks/<task-id>`.
5. Confirm the workbench header includes `#<task-id>`.
6. Confirm `Tasks` lists only the selected task when a task id is present.
7. Confirm `Recent runs` is filtered to the same agent and task.

Expected result: Jordan lands directly on the selected task's workbench context
and does not have to reselect it.

## ICP Example 3: Marcus Deep-Links From A Run

Scenario: Marcus receives or opens a workbench URL with a task id and expects
the view to resolve the agent from run or task data.

Live QA steps:

1. Open `#/apps/workbench/<agent-slug>/tasks/<task-id>` for a task with a known
   run.
2. Refresh the browser.
3. Confirm the workbench still resolves the selected agent and task.
4. Confirm the run list highlights or includes the target task id.
5. Remove the agent segment in a test harness and render with only `taskId`.
6. Confirm the workbench can derive the agent from the matching run first, or
   from the task owner when no run exists.

Expected result: Marcus can share and reopen a task-specific workbench URL
without losing the agent context.

## Focused Test Coverage

Workbench coverage lives in `web/src/components/workbench/AgentWorkbench.test.tsx`.
Run it with:

```bash
bash scripts/test-web.sh web/src/components/workbench/AgentWorkbench.test.tsx
```

The focused tests should cover:

- Rendering task context, memory evidence, recent runs, and live terminal output.
- Switching context from the recent-runs list.
- Resolving the selected agent from task/run data for task-specific deep links.
- Showing the empty state when there is no agent, task, or run data.

If the workbench component is absent in a future workspace, keep this tutorial
as scaffolding and add the tests when the component export exists. Do not add
production component code from the docs/test ownership lane.
