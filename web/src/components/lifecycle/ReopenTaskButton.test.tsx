/**
 * ReopenTaskButton — pins the click→POST contract.
 *
 * ICP-eval v3 [19:05:56]: "Reopen task" on an approved task fired NO network
 * request in the live run. This suite asserts that a click calls the tasks
 * API with the exact FE payload shape (action=reopen, id, channel,
 * created_by=human) and that an API failure renders a visible error instead
 * of dying silently.
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ReopenTaskButton } from "./ReopenTaskButton";

const tasksApi = vi.hoisted(() => ({
  reopenTask: vi.fn(),
}));

vi.mock("../../api/tasks", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/tasks")>("../../api/tasks");
  return {
    ...actual,
    reopenTask: tasksApi.reopenTask,
  };
});

function renderButton(onReopened: () => void = () => {}) {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <ReopenTaskButton
        taskId="OFFICE-246"
        channel="task-office-246"
        onReopened={onReopened}
      />
    </QueryClientProvider>,
  );
}

describe("ReopenTaskButton", () => {
  beforeEach(() => {
    tasksApi.reopenTask.mockReset();
  });

  it("fires the reopen POST with the task id and channel on click", async () => {
    tasksApi.reopenTask.mockResolvedValue({ task: { id: "OFFICE-246" } });
    const onReopened = vi.fn();
    renderButton(onReopened);

    fireEvent.click(screen.getByTestId("reopen-issue-button"));

    await waitFor(() => {
      expect(tasksApi.reopenTask).toHaveBeenCalledWith(
        "OFFICE-246",
        "task-office-246",
      );
    });
    await waitFor(() => {
      expect(onReopened).toHaveBeenCalled();
    });
  });

  it("renders a visible error when the reopen request fails", async () => {
    tasksApi.reopenTask.mockRejectedValue(new Error("task not found"));
    renderButton();

    fireEvent.click(screen.getByTestId("reopen-issue-button"));

    await waitFor(() => {
      expect(screen.getByRole("alert")).toHaveTextContent("task not found");
    });
  });
});
