# Task Detail Live Agent ICP QA Tutorial

This tutorial turns the three ICP examples into live QA steps for task detail
pages. The browser surface should keep task context, ownership, memory evidence,
metadata, and live terminal output together so a reviewer can inspect what an
agent did without jumping across apps.

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

Open the WUPHF web UI. These checks use the Tasks app and task detail routes:

```text
#/tasks
#/tasks/<task-id>
```

## ICP Example 1: Alex Reviews A Long-Running Task

Scenario: Alex opens a task owned by an agent and needs to understand what is
happening now.

Live QA steps:

1. Open `#/tasks`.
2. Choose an in-progress task owned by an agent rather than the human user.
3. Open the task card.
4. Confirm the page route changes to `#/tasks/<task-id>`.
5. Confirm the page shows status, ownership, description/details, memory
   workflow state, and metadata for the selected task.
6. Confirm the live terminal appears only for agent-owned tasks.
7. Trigger or wait for agent output and confirm the terminal updates without
   leaving the task page.

Expected result: Alex can review task context, evidence, status, and live
terminal output in one place.

## ICP Example 2: Jordan Audits Tool-Heavy Work

Scenario: Jordan needs to inspect an agent task where most progress happens
through MCP tools, not visible shell output.

Live QA steps:

1. Open `#/tasks/<task-id>` for an active agent-owned task.
2. Have the agent invoke a visible tool such as a broadcast or wiki lookup.
3. Confirm the task terminal receives the tool-call line for that same task.
4. Open the owning agent panel and confirm the broader agent terminal still
   shows the same event in the all-agent stream.
5. Open a different task for the same agent and confirm the first task's tool
   event does not appear in the other task terminal.

Expected result: Jordan sees tool-heavy work in the task terminal while the
agent-level stream remains useful for broader agent debugging.

## ICP Example 3: Marcus Reopens A Shared Task Link

Scenario: Marcus receives a task link and expects it to reopen the same task
without reselecting it from the board.

Live QA steps:

1. Open `#/tasks/<task-id>` for an existing task.
2. Refresh the browser.
3. Confirm the app stays in Tasks and renders the same task detail page.
4. Remove the task id and open `#/tasks`.
5. Confirm the app renders the task board, not the default channel.
6. Open a missing task id and confirm the missing-task state offers a path back
   to the task board.

Expected result: Marcus can share and reopen task-specific URLs, and bare
Tasks URLs remain stable entry points.

## Focused Test Coverage

Run the focused browser-state tests with:

```bash
cd web
bun run test -- src/stores/app.test.ts src/hooks/useHashRouter.test.ts src/components/apps/TaskDetailModal.test.tsx src/lib/agentTerminalSocket.test.ts
```

The focused tests should cover:

- Routing `#/tasks` to the Tasks app and `#/tasks/<task-id>` to task detail.
- Rendering task detail page presentation without dialog chrome.
- Showing a task-scoped terminal for agent-owned tasks.
- Preserving websocket resize behavior before the socket opens.
- Keeping task detail state cleared when navigating away from Tasks.
