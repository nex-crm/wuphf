import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { Task } from "../../api/client";
import { getOfficeMembers, updateTaskStatus } from "../../api/client";
import { TaskDetailModal, taskMemoryWorkflowBadge } from "./TaskDetailModal";

vi.mock("../../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../api/client")>();
  return {
    ...actual,
    getOfficeMembers: vi.fn(),
    updateTaskStatus: vi.fn(),
    reassignTask: vi.fn(),
  };
});

const getOfficeMembersMock = vi.mocked(getOfficeMembers);
const updateTaskStatusMock = vi.mocked(updateTaskStatus);

function renderTaskDetail(task: Task, onClose = vi.fn()) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return {
    onClose,
    ...render(
      <QueryClientProvider client={queryClient}>
        <TaskDetailModal task={task} onClose={onClose} />
      </QueryClientProvider>,
    ),
  };
}

describe("taskMemoryWorkflowBadge", () => {
  it("renders compact progress for required incomplete workflow state", () => {
    const task: Task = {
      id: "task-1",
      title: "Research reuse loop",
      status: "in_progress",
      memory_workflow: {
        required: true,
        status: "pending",
        requirement_reason:
          "process research requires prior context lookup, durable capture, and promotion",
        required_steps: ["lookup", "capture", "promote"],
        lookup: {
          required: true,
          status: "satisfied",
          completed_at: "2026-04-30T10:00:00Z",
          count: 1,
        },
        capture: { required: true, status: "pending" },
        promote: { required: true, status: "pending" },
        citations: [
          {
            title: "Prior onboarding notes",
            path: "notebook/pm/onboarding.md",
            source: "wiki",
            backend: "markdown",
            snippet: "Reuse the prior onboarding benchmark.",
            score: 0.92,
          },
        ],
        captures: [
          {
            source: "notebook",
            title: "Onboarding research capture",
            path: "notebook/research/onboarding.md",
            state: "captured",
          },
        ],
        updated_at: "2026-04-30T10:05:00Z",
      },
    };

    expect(taskMemoryWorkflowBadge(task.memory_workflow)?.label).toBe(
      "memory 1/3",
    );
  });

  it("renders compact issue state for missing artifacts", () => {
    const badge = taskMemoryWorkflowBadge({
      required: true,
      status: "pending",
      required_steps: ["lookup", "capture", "promote"],
      lookup: {
        required: true,
        status: "satisfied",
        completed_at: "2026-04-30T10:00:00Z",
      },
      capture: {
        required: true,
        status: "satisfied",
        completed_at: "2026-04-30T10:05:00Z",
      },
      promote: { required: true, status: "pending" },
      promotions: [
        { source: "wiki", path: "wiki/research/onboarding.md", missing: true },
      ],
    });

    expect(badge?.label).toBe("memory issue");
  });

  it("prioritizes explicit human override state", () => {
    const badge = taskMemoryWorkflowBadge({
      required: true,
      status: "overridden",
      requirement_reason: "urgent customer handoff",
      override: {
        actor: "human",
        reason: "Ship before promotion review",
        timestamp: "2026-04-30T10:15:00Z",
      },
    });

    expect(badge?.label).toBe("memory override");
    expect(badge?.title).toContain("@human");
  });

  it("stays quiet when the workflow is absent or explicitly not required", () => {
    expect(taskMemoryWorkflowBadge(undefined)).toBeNull();
    expect(
      taskMemoryWorkflowBadge({ required: false, status: "not_required" }),
    ).toBeNull();
  });
});

describe("TaskDetailModal memory override", () => {
  it("requires a reason and submits the human override payload", async () => {
    getOfficeMembersMock.mockResolvedValue({
      members: [{ slug: "ceo", name: "CEO", role: "lead" }],
    });
    updateTaskStatusMock.mockResolvedValue({
      task: { id: "task-1", title: "Done", status: "done" },
    });

    const task: Task = {
      id: "task-1",
      title: "Research passport renewal process",
      status: "in_progress",
      channel: "general",
      owner: "ceo",
      memory_workflow: {
        required: true,
        status: "pending",
        required_steps: ["lookup", "capture", "promote"],
        lookup: { required: true, status: "pending" },
        capture: { required: true, status: "pending" },
        promote: { required: true, status: "pending" },
      },
    };

    const { onClose } = renderTaskDetail(task);
    const button = screen.getByRole("button", {
      name: "Mark done with override",
    });
    expect(button).toBeDisabled();

    await userEvent.type(
      screen.getByLabelText("Override reason"),
      "Customer deadline accepted by founder",
    );
    expect(button).toBeEnabled();
    await userEvent.click(button);

    await waitFor(() => {
      expect(updateTaskStatusMock).toHaveBeenCalledWith(
        "task-1",
        "complete",
        "general",
        "human",
        {
          memoryWorkflowOverride: true,
          memoryWorkflowOverrideActor: "human",
          memoryWorkflowOverrideReason: "Customer deadline accepted by founder",
        },
      );
    });
    expect(onClose).toHaveBeenCalled();
  });
});
