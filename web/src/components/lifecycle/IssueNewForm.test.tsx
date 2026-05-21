import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { IssueNewForm } from "./IssueNewForm";

const tasksApi = vi.hoisted(() => ({
  createTasks: vi.fn(),
}));

const routerApi = vi.hoisted(() => ({
  navigate: vi.fn(),
}));

vi.mock("../../api/tasks", () => tasksApi);
vi.mock("../../lib/router", () => ({ router: routerApi }));

function renderForm() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <IssueNewForm />
    </QueryClientProvider>,
  );
}

describe("<IssueNewForm>", () => {
  it("creates manual issues as spec-level issue tasks", async () => {
    tasksApi.createTasks.mockResolvedValueOnce({
      tasks: [{ id: "task-issue-1", title: "Spec onboarding", status: "open" }],
    });

    renderForm();

    fireEvent.change(screen.getByTestId("issue-new-title"), {
      target: { value: "Spec onboarding handoff" },
    });
    fireEvent.change(screen.getByTestId("issue-new-details"), {
      target: { value: "Goal, context, and acceptance criteria." },
    });
    const form = screen.getByTestId("issue-new-submit").closest("form");
    expect(form).not.toBeNull();
    if (!form) return;
    fireEvent.submit(form);

    await waitFor(() => expect(tasksApi.createTasks).toHaveBeenCalled());
    expect(tasksApi.createTasks).toHaveBeenCalledWith(
      [
        {
          title: "Spec onboarding handoff",
          assignee: "human",
          details: "Goal, context, and acceptance criteria.",
          task_type: "issue",
        },
      ],
      { channel: "general", createdBy: "human" },
    );
  });
});
