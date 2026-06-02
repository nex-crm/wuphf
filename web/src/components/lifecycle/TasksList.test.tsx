import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Task } from "../../api/tasks";
import { TasksList } from "./TasksList";

function makeTask(overrides: Partial<Task>): Task {
  return {
    id: "task-1",
    title: "Task",
    status: "open",
    ...overrides,
  };
}

function renderList(tasks: Task[]) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <TasksList initialTasks={tasks} />
    </QueryClientProvider>,
  );
}

describe("<TasksList>", () => {
  it("shows spec-level issue tasks and hides ordinary execution tasks", () => {
    renderList([
      makeTask({
        id: "task-issue",
        title: "Spec the agent issue app",
        task_type: "issue",
      }),
      makeTask({
        id: "task-follow-up",
        title: "Fix button spacing",
        task_type: "follow_up",
      }),
    ]);

    expect(screen.getByText("Spec the agent issue app")).toBeInTheDocument();
    expect(screen.queryByText("Fix button spacing")).not.toBeInTheDocument();
  });

  it("treats drafted issue specs as issues even when legacy task type is absent", () => {
    renderList([
      makeTask({
        id: "task-draft",
        title: "Draft Stripe webhook spec",
        issue_draft_spec: {
          goal: "Receive Stripe webhook events.",
          drafted_at: "2026-05-20T12:00:00Z",
        },
      }),
    ]);

    expect(screen.getByText("Draft Stripe webhook spec")).toBeInTheDocument();
  });

  it("shows the issue-spec empty state when only small tasks exist", () => {
    renderList([
      makeTask({
        id: "task-small",
        title: "Reply with status",
        task_type: "follow_up",
      }),
    ]);

    expect(screen.getByTestId("issues-list-empty")).toHaveTextContent(
      "No task specs yet.",
    );
    expect(screen.queryByText("Reply with status")).not.toBeInTheDocument();
  });
});
